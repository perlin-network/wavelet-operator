package wavelet

import (
	"context"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strconv"

	waveletv1alpha1 "github.com/perlin-network/wavelet-operator/pkg/apis/wavelet/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("wavelet.controller")

// Add creates a new Wavelet Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileWavelet{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("wavelet-controller", mgr, controller.Options{Reconciler: r})

	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: new(waveletv1alpha1.Wavelet)}, new(handler.EnqueueRequestForObject))

	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: new(corev1.Pod)}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    new(waveletv1alpha1.Wavelet),
	})

	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: new(appsv1.Deployment)}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    new(waveletv1alpha1.Wavelet),
	})

	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileWavelet{}

type ReconcileWavelet struct {
	client client.Client
	scheme *runtime.Scheme
}

func (r *ReconcileWavelet) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("request.namespace", request.Namespace, "request.name", request.Name)

	cluster := new(waveletv1alpha1.Wavelet)

	var result reconcile.Result

	if err := r.client.Get(context.TODO(), request.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			return result, nil
		}

		return result, err
	}

	if len(cluster.Status.Stage) == 0 {
		cluster.Status.Stage = waveletv1alpha1.StageGenesis

		if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
			return result, err
		}

		return result, nil
	}

	switch cluster.Status.Stage {
	case waveletv1alpha1.StageGenesis:
		return createBootstrapNode(r, logger, cluster)
	case waveletv1alpha1.StageBootstrap:
		return createBootstrappedCluster(r, logger, cluster)
	}

	return result, nil
}

func createBootstrapNode(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet) (reconcile.Result, error) {
	var result reconcile.Result

	if cluster.Spec.Size == 0 {
		return result, nil
	}

	bootstrap, err := getBootstrapNode(r, logger, cluster)
	if err != nil {
		return result, err
	}

	if bootstrap == nil {
		result.Requeue = true
		return result, err
	}

	if len(bootstrap.Status.PodIP) == 0 { // Wait until the bootstrap node has a pod IP available.
		return result, err
	}

	logger.Info("Finished deploying bootstrap node.", "bootstrap_node", bootstrap.Name, "bootstrap_node_ip", bootstrap.Status.PodIP)

	cluster.Status.Stage = waveletv1alpha1.StageBootstrap

	if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
		return result, err
	}

	return result, nil
}

func createBootstrappedCluster(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet) (reconcile.Result, error) {
	var result reconcile.Result

	bootstrap, err := getBootstrapNode(r, logger, cluster)
	if err != nil {
		return result, err
	}

	if bootstrap == nil {
		result.Requeue = true
		return result, err
	}

	if len(bootstrap.Status.PodIP) == 0 { // Wait until the bootstrap node has a pod IP available.
		return result, err
	}

	deployment, err := getDeployment(r, logger, cluster, bootstrap)
	if err != nil {
		return result, err
	}

	if deployment == nil {
		result.Requeue = true
		return result, err
	}

	if cluster.Spec.Size == 0 { // Handle the case where the cluster size is 0.
		if err := r.client.Delete(context.TODO(), bootstrap); err != nil {
			logger.Error(err, "Failed to delete the bootstrap node.")
			return result, err
		}

		if err := r.client.Delete(context.TODO(), deployment); err != nil {
			logger.Error(err, "Failed to delete the clusters deployment.")
			return result, err
		}

		cluster.Status.Stage = waveletv1alpha1.StageGenesis

		if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
			return result, err
		}

		return result, nil
	}

	if *deployment.Spec.Replicas != cluster.Spec.Size-1 { // Update the size of the cluster.
		deployment.Spec.Replicas = func(m int32) *int32 { return &m }(cluster.Spec.Size - 1)

		if err := r.client.Update(context.TODO(), deployment); err != nil {
			logger.Error(err, "Failed to update the clusters deployment size.")
			return result, err
		}

		result.Requeue = true
		return result, nil
	}

	pods, err := getPods(r, cluster)
	if err != nil {
		return result, err
	}

	names := getPodNames(pods)

	if result, err = updateClusterNodeList(r, logger, cluster, names); err != nil {
		return result, err
	}

	return result, nil
}

func getPods(r *ReconcileWavelet, cluster *waveletv1alpha1.Wavelet) ([]corev1.Pod, error) {
	pods := new(corev1.PodList)

	selector := labels.SelectorFromSet(labelsForWavelet(cluster.Name))
	opts := &client.ListOptions{Namespace: cluster.Namespace, LabelSelector: selector}

	if err := r.client.List(context.TODO(), opts, pods); err != nil {
		return nil, err
	}

	var filtered []corev1.Pod

	for _, pod := range pods.Items {
		if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}

		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			filtered = append(filtered, pod)
		}
	}

	return filtered, nil
}

func getBootstrapNode(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet) (*corev1.Pod, error) {
	pod := new(corev1.Pod)

	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, pod); err != nil {
		if errors.IsNotFound(err) { // Create a single bootstrap node.
			logger.Info("Creating a single bootstrap node...")

			pod = getWaveletBootstrapPodSpec(cluster, "config/wallet.txt")

			if err := controllerutil.SetControllerReference(cluster, pod, r.scheme); err != nil {
				return nil, err
			}

			if err := r.client.Create(context.TODO(), pod); err != nil {
				logger.Error(err, "Failed to create a bootstrap node.")
				return nil, err
			}

			return nil, nil
		}

		return nil, err
	}

	return pod, nil
}

func getDeployment(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet, bootstrap *corev1.Pod) (*appsv1.Deployment, error) {
	dep := new(appsv1.Deployment)

	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, dep); err != nil {
		if errors.IsNotFound(err) { // Create node deployment.
			logger.Info("Setting up and bootstrapping nodes...")

			dep = getWaveletDeploymentSpec(cluster, bootstrap.Status.PodIP+":3000")

			if err := controllerutil.SetControllerReference(cluster, dep, r.scheme); err != nil {
				return nil, err
			}

			if err := r.client.Create(context.TODO(), dep); err != nil {
				logger.Error(err, "Failed to create a deployment for the cluster.")
				return nil, err
			}

			return nil, nil
		}

		return nil, err
	}

	return dep, nil
}

func getPodNames(pods []corev1.Pod) []string {
	var names []string

	for _, pod := range pods {
		names = append(names, pod.Name)
	}

	return names
}

func updateClusterNodeList(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet, names []string) (reconcile.Result, error) {
	var result reconcile.Result

	if !reflect.DeepEqual(names, cluster.Status.Nodes) {
		logger.Info("Reconciling changes in the clusters node list.", "old_list", cluster.Status.Nodes, "new_list", names)

		cluster.Status.Nodes = names

		if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
			logger.Error(err, "Failed to update the clusters node list.")
			return result, err
		}

		return result, nil
	}

	return result, nil
}

func labelsForWavelet(name string) labels.Set {
	return labels.Set{"app": name}
}

func getWaveletBootstrapPodSpec(cluster *waveletv1alpha1.Wavelet, wallet string, bootstrapNodes ...string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labelsForWavelet(cluster.Name),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image:   "localhost:5000/wavelet",
					Name:    "wavelet",
					Command: append([]string{"./wavelet", "-api.port", strconv.Itoa(9000), "-wallet", wallet}, bootstrapNodes...),
					Env: []corev1.EnvVar{
						{
							Name: "WAVELET_NODE_HOST",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 3000,
							Name:          "node",
						},
						{
							ContainerPort: 9000,
							Name:          "http",
						},
					},
				},
			},
		},
	}
}

func getWaveletDeploymentSpec(cluster *waveletv1alpha1.Wavelet, bootstrap ...string) *appsv1.Deployment {
	lbls := labelsForWavelet(cluster.Name)
	replicas := cluster.Spec.Size - 1

	if replicas < 0 {
		replicas = 0
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: lbls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: lbls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:   "localhost:5000/wavelet",
							Name:    "wavelet",
							Command: append([]string{"./wavelet", "-api.port", strconv.Itoa(9000), "-wallet", "random"}, bootstrap...),
							Env: []corev1.EnvVar{
								{
									Name: "WAVELET_NODE_HOST",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 3000,
									Name:          "node",
								},
								{
									ContainerPort: 9000,
									Name:          "http",
								},
							},
						},
					},
				},
			},
		},
	}
}

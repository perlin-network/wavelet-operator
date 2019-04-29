package wavelet

import (
	"context"
	"github.com/go-logr/logr"
	waveletv1alpha1 "github.com/perlin-network/wavelet-operator/pkg/apis/wavelet/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	if cluster.Spec.Size == 0 { // Handle the case where the cluster size is 0.
		if bootstrap, err := getBootstrapNode(r, cluster); bootstrap != nil && err == nil {
			if err := r.client.Delete(context.TODO(), bootstrap); err != nil && !errors.IsNotFound(err) && !errors.IsAlreadyExists(err) {
				logger.Error(err, "Failed to delete the bootstrap node.")
				return result, err
			}
		}

		if deployment, err := getBootstrappedCluster(r, cluster); deployment != nil && err == nil {
			if err := r.client.Delete(context.TODO(), deployment); err != nil && !errors.IsNotFound(err) && !errors.IsAlreadyExists(err) {
				logger.Error(err, "Failed to delete the clusters deployment.")
				return result, err
			}
		}

		if cluster.Status.Stage != waveletv1alpha1.StageGenesis {
			cluster.Status.Stage = waveletv1alpha1.StageGenesis

			if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
				return result, err
			}

			return result, nil
		}

		logger.Info("The number of workers for Wavelet is currently set to zero. Please set it to be >= 1.")

		return result, nil
	}

	switch cluster.Status.Stage {
	case waveletv1alpha1.StageGenesis:
		if result, err := stageGenesis(r, logger, cluster); result.Requeue || err != nil {
			return result, err
		}
	case waveletv1alpha1.StageBootstrap:
		if result, err := stageBootstrap(r, logger, cluster); result.Requeue || err != nil {
			return result, err
		}
	case waveletv1alpha1.StageReady:
		if result, err := stageReady(r, logger, cluster); result.Requeue || err != nil {
			return result, err
		}
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

func stageGenesis(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet) (reconcile.Result, error) {
	var result reconcile.Result

	bootstrap, err := createBootstrapNode(r, logger, cluster)
	if err != nil {
		logger.Error(err, "Failed to create a bootstrap node.")
		return result, err
	}

	cluster.Status.Stage = waveletv1alpha1.StageBootstrap

	if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
		return result, err
	}

	logger.Info("Finished deploying bootstrap node.", "bootstrap_node", bootstrap.Name, "bootstrap_node_ip", bootstrap.Status.PodIP)

	return result, nil
}

func stageBootstrap(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet) (reconcile.Result, error) {
	var result reconcile.Result

	bootstrap, err := getBootstrapNode(r, cluster)
	if bootstrap == nil || err != nil {
		if errors.IsNotFound(err) {
			cluster.Status.Stage = waveletv1alpha1.StageGenesis

			if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
				return result, err
			}

			return result, nil
		}

		return result, nil
	}

	if _, err = createBootstrappedCluster(r, logger, cluster, bootstrap); err != nil {
		logger.Error(err, "Failed to create a deployment for the cluster.")
		return result, err
	}

	cluster.Status.Stage = waveletv1alpha1.StageReady

	if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
		return result, err
	}

	logger.Info("Your Wavelet cluster is now ready.", "bootstrap_node", bootstrap.Name, "bootstrap_node_ip", bootstrap.Status.PodIP, "cluster_size", cluster.Spec.Size)

	return result, nil
}

func stageReady(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet) (reconcile.Result, error) {
	var result reconcile.Result

	bootstrap, err := getBootstrapNode(r, cluster)
	if bootstrap == nil || err != nil {
		if errors.IsNotFound(err) {
			cluster.Status.Stage = waveletv1alpha1.StageGenesis

			if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
				return result, err
			}

			return result, nil
		}

		return result, nil
	}

	deployment, err := getBootstrappedCluster(r, cluster)
	if deployment == nil || err != nil {
		if errors.IsNotFound(err) {
			cluster.Status.Stage = waveletv1alpha1.StageBootstrap

			if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
				return result, err
			}

			return result, nil
		}

		return result, nil
	}

	if err = listenForChanges(r, logger, cluster, bootstrap, deployment); err != nil {
		return result, err
	}

	return result, nil
}

func getBootstrapNode(r *ReconcileWavelet, cluster *waveletv1alpha1.Wavelet) (*corev1.Pod, error) {
	bootstrap := new(corev1.Pod)

	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, bootstrap); err != nil {
		return nil, err
	}

	if len(bootstrap.Status.PodIP) == 0 { // Wait until the bootstrap node has a pod IP available.
		return nil, nil
	}

	return bootstrap, nil
}

func getBootstrappedCluster(r *ReconcileWavelet, cluster *waveletv1alpha1.Wavelet) (*appsv1.Deployment, error) {
	deployment := new(appsv1.Deployment)

	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, deployment); err != nil {
		return nil, err
	}

	return deployment, nil
}

func createBootstrapNode(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet) (*corev1.Pod, error) {
	logger.Info("Creating a single bootstrap node...")

	bootstrap := getWaveletBootstrapPod(cluster)

	if err := controllerutil.SetControllerReference(cluster, bootstrap, r.scheme); err != nil {
		return nil, err
	}

	if err := r.client.Create(context.TODO(), bootstrap); err != nil && !errors.IsAlreadyExists(err) {
		return nil, err
	}

	return bootstrap, nil
}

func createBootstrappedCluster(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet, bootstrap *corev1.Pod) (*appsv1.Deployment, error) {
	logger.Info("Setting up and bootstrapping the rest of the cluster.", "num_nodes", cluster.Spec.Size)

	deployment := getWaveletDeployment(cluster, bootstrap.Status.PodIP+":3000")

	if err := controllerutil.SetControllerReference(cluster, deployment, r.scheme); err != nil && !errors.IsAlreadyExists(err) {
		return nil, err
	}

	if err := r.client.Create(context.TODO(), deployment); err != nil && !errors.IsAlreadyExists(err) {
		return nil, err
	}

	return deployment, nil
}

func listenForChanges(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet, bootstrap *corev1.Pod, deployment *appsv1.Deployment) error {
	if *deployment.Spec.Replicas != cluster.Spec.Size-1 { // Update the size of the cluster.
		logger.Info("Updated the size of the cluster.", "old_size", *deployment.Spec.Replicas+1, "new_size", cluster.Spec.Size)

		deployment.Spec.Replicas = func(m int32) *int32 { return &m }(cluster.Spec.Size - 1)

		if err := r.client.Update(context.TODO(), deployment); err != nil {
			return err
		}

		return nil
	}

	return nil
}

func updateClusterNodeList(r *ReconcileWavelet, logger logr.Logger, cluster *waveletv1alpha1.Wavelet, names []string) (reconcile.Result, error) {
	var result reconcile.Result

	if !reflect.DeepEqual(names, cluster.Status.Nodes) {
		logger.Info("Reconciling changes in the clusters node list.", "old_list", cluster.Status.Nodes, "new_list", names)

		cluster.Status.Nodes = names

		if err := r.client.Status().Update(context.TODO(), cluster); err != nil {
			//logger.Error(err, "Failed to update the clusters node list.")
			return result, nil
		}

		return result, nil
	}

	return result, nil
}

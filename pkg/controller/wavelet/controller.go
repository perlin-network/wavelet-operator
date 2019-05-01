package wavelet

import (
	"context"
	waveletv1alpha1 "github.com/perlin-network/wavelet-operator/pkg/apis/wavelet/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sort"
	"strconv"
	"time"

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

	if err := r.client.Get(context.TODO(), request.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	pods := new(corev1.PodList)

	selector := labels.SelectorFromSet(labelsForWavelet(cluster.Name))
	opts := &client.ListOptions{Namespace: cluster.Namespace, LabelSelector: selector}

	if err := r.client.List(context.TODO(), opts, pods); err != nil {
		logger.Error(err, "Failed to list all pods created by the operator.")
		return reconcile.Result{}, err
	}

	if cluster.Spec.Size <= 0 {
		for _, pod := range pods.Items {
			if err := r.client.Delete(context.TODO(), &pod, client.GracePeriodSeconds(0)); err != nil && !errors.IsNotFound(err) {
				return reconcile.Result{}, err
			}
		}

		if len(pods.Items) > 0 {
			logger.Info("Deleting all pods in the cluster.")
		}

		return reconcile.Result{}, nil
	}

	genesis, err := createGenesis(logger, cluster.Spec.NumRichWallets)

	if err != nil || len(genesis) == 0 {
		return reconcile.Result{}, err
	}

	bootstrap := getWaveletBootstrapPod(cluster, genesis)

	if err := controllerutil.SetControllerReference(cluster, bootstrap, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.client.Create(context.TODO(), bootstrap); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.Error(err, "Failed to create bootstrap pod.")
			return reconcile.Result{}, err
		}
	} else {
		logger.Info("Creating a single Wavelet pod for other pods to bootstrap to...")
		return reconcile.Result{Requeue: true}, nil
	}

	if err := r.client.Get(context.TODO(), request.NamespacedName, bootstrap); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		logger.Error(err, "Failed to query details about the bootstrap pod.")
		return reconcile.Result{}, err
	}

	if len(bootstrap.Status.PodIP) == 0 {
		logger.Info("Waiting for the bootstrap pod to have an IP address assigned...")
		return reconcile.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if len(pods.Items) == 1 {
		logger.Info("Bootstrap pod is available.", "bootstrap_pod_name", bootstrap.Name, "bootstrap_pod_ip", bootstrap.Status.PodIP)
	}

	current := int32(len(pods.Items))
	expected := cluster.Spec.Size

	if current > expected { // Scale down number of workers.
		sort.Slice(pods.Items, func(i, j int) bool {
			ii, _ := strconv.ParseInt(pods.Items[i].Name[len(cluster.Name):], 10, 32)
			jj, _ := strconv.ParseInt(pods.Items[j].Name[len(cluster.Name):], 10, 32)

			return ii > jj
		})

		for i := current; i > expected; i-- {
			target := pods.Items[i-1]

			if err := r.client.Delete(context.TODO(), &target, client.GracePeriodSeconds(0)); err != nil {
				logger.Error(err, "Failed to delete worker pod.", "pod_name", target.Name)
				return reconcile.Result{}, err
			}

			logger.Info("Deleted worker pod.", "pod_name", target.Name)
		}

		return reconcile.Result{}, nil
	}

	if current < expected { // Scale up number of workers.
		for idx := current; idx < expected; idx++ {
			pod := getWaveletPod(cluster, genesis, uint(idx), net.JoinHostPort(bootstrap.Status.PodIP, "3000"))

			if err := controllerutil.SetControllerReference(cluster, bootstrap, r.scheme); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.client.Create(context.TODO(), pod); err != nil && !errors.IsAlreadyExists(err) {
				logger.Error(err, "Failed to create worker pod.", "idx", idx)
				return reconcile.Result{}, err
			}

			logger.Info("Created worker pod.", "pod_name", pod.Name)
		}

		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

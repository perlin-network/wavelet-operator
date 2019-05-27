// Copyright (c) 2019 Perlin
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

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

	nodeList := new(corev1.PodList)

	opts := &client.ListOptions{Namespace: cluster.Namespace, LabelSelector: labels.SelectorFromSet(labelsForWavelet(cluster.Name, "node"))}

	if err := r.client.List(context.TODO(), opts, nodeList); err != nil {
		logger.Error(err, "Failed to list all node pods created by the operator.")
		return reconcile.Result{}, err
	}

	var nodePods []corev1.Pod

	for _, pod := range nodeList.Items {
		if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}

		nodePods = append(nodePods, pod)
	}

	benchmarkList := new(corev1.PodList)

	opts = &client.ListOptions{Namespace: cluster.Namespace, LabelSelector: labels.SelectorFromSet(labelsForWavelet(cluster.Name, "benchmark"))}

	if err := r.client.List(context.TODO(), opts, benchmarkList); err != nil {
		logger.Error(err, "Failed to list all benchmark pods created by the operator.")
		return reconcile.Result{}, err
	}

	var benchmarkPods []corev1.Pod

	for _, benchmarkPod := range benchmarkList.Items {
		if benchmarkPod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}

		benchmarkPods = append(benchmarkPods, benchmarkPod)
	}

	if cluster.Spec.Size <= 0 {
		if len(nodePods) > 0 {
			logger.Info("Deleting all node pods in the cluster.")

			for _, pod := range nodePods {
				if err := r.client.Delete(context.TODO(), &pod, client.GracePeriodSeconds(0)); err != nil && !errors.IsNotFound(err) {
					return reconcile.Result{}, err
				}
			}
		}

		if len(benchmarkPods) > 0 {
			logger.Info("Deleting all benchmark pods in the cluster.")

			for _, benchmarkPod := range benchmarkPods {
				if err := r.client.Delete(context.TODO(), &benchmarkPod, client.GracePeriodSeconds(0)); err != nil && !errors.IsNotFound(err) {
					return reconcile.Result{}, err
				}
			}
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

	if len(nodePods) == 1 {
		logger.Info("Bootstrap pod is available.", "bootstrap_pod_name", bootstrap.Name, "bootstrap_pod_ip", bootstrap.Status.PodIP)
	}

	currentNumNode := int32(len(nodePods))
	expectedNumNodes := cluster.Spec.Size

	if currentNumNode > expectedNumNodes { // Scale down number of workers.
		sort.Slice(nodePods, func(i, j int) bool {
			ii, _ := strconv.ParseInt(nodePods[i].Name[len(cluster.Name):], 10, 32)
			jj, _ := strconv.ParseInt(nodePods[j].Name[len(cluster.Name):], 10, 32)

			return ii > jj
		})

		for i := currentNumNode; i > expectedNumNodes; i-- {
			target := nodePods[i-1]

			if err := r.client.Delete(context.TODO(), &target, client.GracePeriodSeconds(0)); err != nil {
				logger.Error(err, "Failed to delete worker pod.", "pod_name", target.Name)
				return reconcile.Result{}, err
			}

			logger.Info("Deleted worker pod.", "pod_name", target.Name)
		}

		return reconcile.Result{Requeue: true}, nil
	}

	if currentNumNode < expectedNumNodes { // Scale up number of workers.
		for idx := currentNumNode; idx < expectedNumNodes; idx++ {
			nodePod := getWaveletNodePod(cluster, genesis, uint(idx), net.JoinHostPort(bootstrap.Status.PodIP, "3000"))

			if err := controllerutil.SetControllerReference(cluster, nodePod, r.scheme); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.client.Create(context.TODO(), nodePod); err != nil && !errors.IsAlreadyExists(err) {
				logger.Error(err, "Failed to create worker pod.", "idx", idx)
				return reconcile.Result{}, err
			}

			logger.Info("Created worker pod.", "pod_name", nodePod.Name)
		}

		return reconcile.Result{Requeue: true}, nil
	}

	sort.Slice(nodePods, func(i, j int) bool {
		ii, _ := strconv.ParseInt(nodePods[i].Name[len(cluster.Name):], 10, 32)
		jj, _ := strconv.ParseInt(nodePods[j].Name[len(cluster.Name):], 10, 32)

		return ii > jj
	})

	for i, nodePod := range nodePods {
		if len(nodePod.Status.PodIP) == 0 {
			logger.Info("Waiting for pod to be ready before initializing benchmark nodes...", "pod_name", nodePod.Name, "pod_idx", i, "pod_status", nodePod.Status.Phase)
			return reconcile.Result{RequeueAfter: 1 * time.Second}, nil
		}
	}

	currentNumBenchmarkPods := uint(len(benchmarkPods))
	expectedNumBenchmarkPods := cluster.Spec.NumBenchmarkPods

	if expectedNumBenchmarkPods > uint(len(nodePods)) {
		logger.Info("There must always be equal to or less benchmark pods than node pods in the cluster. Please reconfigure your cluster.", "expected_num_benchmark_pods", expectedNumBenchmarkPods, "cluster_size", len(nodePods))
		return reconcile.Result{}, nil
	}

	if currentNumBenchmarkPods > expectedNumBenchmarkPods {
		sort.Slice(benchmarkPods, func(i, j int) bool {
			ii, _ := strconv.ParseInt(benchmarkPods[i].Name[len(cluster.Name)+len("-benchmark"):], 10, 32)
			jj, _ := strconv.ParseInt(benchmarkPods[j].Name[len(cluster.Name)+len("-benchmark"):], 10, 32)

			return ii > jj
		})

		for i := currentNumBenchmarkPods; i > expectedNumBenchmarkPods; i-- {
			target := benchmarkPods[i-1]

			if err := r.client.Delete(context.TODO(), &target, client.GracePeriodSeconds(0)); err != nil {
				logger.Error(err, "Failed to delete benchmark pod.", "pod_name", target.Name)
				return reconcile.Result{}, err
			}

			logger.Info("Deleted benchmark pod.", "pod_name", target.Name)
		}

		return reconcile.Result{Requeue: true}, nil
	}

	if currentNumBenchmarkPods < expectedNumBenchmarkPods {
		for idx := currentNumBenchmarkPods; idx < expectedNumBenchmarkPods; idx++ {
			benchmarkPod := getWaveletBenchmarkPod(cluster, nodePods[idx])

			if err := controllerutil.SetControllerReference(cluster, benchmarkPod, r.scheme); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.client.Create(context.TODO(), benchmarkPod); err != nil && !errors.IsAlreadyExists(err) {
				logger.Error(err, "Failed to create benchmark pod.", "idx", idx)
				return reconcile.Result{}, err
			}

			logger.Info("Created benchmark pod.", "pod_name", benchmarkPod.Name)
		}

		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, nil
}

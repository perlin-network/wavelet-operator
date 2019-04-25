package wavelet

import (
	"context"
	waveletv1alpha1 "github.com/perlin-network/wavelet-operator/pkg/apis/wavelet/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

func getPodNames(pods []corev1.Pod) []string {
	var names []string

	for _, pod := range pods {
		names = append(names, pod.Name)
	}

	return names
}

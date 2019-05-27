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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getPods(r *ReconcileWavelet, cluster *waveletv1alpha1.Wavelet, role string) ([]corev1.Pod, error) {
	pods := new(corev1.PodList)

	selector := labels.SelectorFromSet(labelsForWavelet(cluster.Name, role))
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

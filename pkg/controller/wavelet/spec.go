package wavelet

import (
	waveletv1alpha1 "github.com/perlin-network/wavelet-operator/pkg/apis/wavelet/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func labelsForWavelet(name string) labels.Set {
	return labels.Set{"app": name}
}

func getWaveletBootstrapPod(cluster *waveletv1alpha1.Wavelet) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labelsForWavelet(cluster.Name),
		},
		Spec: getWaveletPodSpec("config/wallet.txt"),
	}
}

func getWaveletDeployment(cluster *waveletv1alpha1.Wavelet, bootstrap ...string) *appsv1.Deployment {
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
				Spec: getWaveletPodSpec("random", bootstrap...),
			},
		},
	}
}

func getWaveletPodSpec(wallet string, bootstrap ...string) corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Stdin:   true,
				Image:   "localhost:5000/wavelet",
				Name:    "wavelet",
				Command: append([]string{"./wavelet", "-api.port", strconv.Itoa(9000), "-wallet", wallet}, bootstrap...),
				Env: []corev1.EnvVar{
					{
						Name: "WAVELET_NODE_HOST",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "status.podIP",
							},
						},
					},
					{
						Name:  "SNOWBALL_QUERY_K",
						Value: "10",
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
	}
}

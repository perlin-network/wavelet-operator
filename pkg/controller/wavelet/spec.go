package wavelet

import (
	"fmt"
	waveletv1alpha1 "github.com/perlin-network/wavelet-operator/pkg/apis/wavelet/v1alpha1"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/labels"
	"net"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func labelsForWavelet(name string) labels.Set {
	return labels.Set{"app": name}
}

func getWaveletBenchmarkPod(cluster *waveletv1alpha1.Wavelet, pod *corev1.Pod) *corev1.Pod {
	idx, _ := strconv.ParseInt(pod.Name[len(cluster.Name):], 10, 32)

	host := net.JoinHostPort(pod.Status.PodIP, "9000")
	privateKey := pod.Spec.Containers[0].Env[3].Value

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-benchmark-%d", cluster.Name, idx),
			Namespace: cluster.Namespace,
			Labels:    labelsForWavelet(cluster.Name),
		},
		Spec: getWaveletBenchmarkPodSpec(host, privateKey),
	}
}

func getWaveletBootstrapPod(cluster *waveletv1alpha1.Wavelet, genesis string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labelsForWavelet(cluster.Name),
		},
		Spec: getWaveletPodSpec("config/wallet.txt", genesis),
	}
}

func getWaveletPod(cluster *waveletv1alpha1.Wavelet, genesis string, idx uint, bootstrap ...string) *corev1.Pod {
	privateKey, err := ioutil.ReadFile(fmt.Sprintf("config/wallet%d.txt", idx))

	if err != nil {
		privateKey = []byte("random")
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", cluster.Name, idx),
			Namespace: cluster.Namespace,
			Labels:    labelsForWavelet(cluster.Name),
		},
		Spec: getWaveletPodSpec(string(privateKey), genesis, bootstrap...),
	}
}

func getWaveletBenchmarkPodSpec(host, privateKey string) corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Image:   "localhost:5000/wavelet",
				Name:    "benchmark",
				Command: []string{"./benchmark", "-host", host, "-sk", privateKey},
			},
		},
	}
}

func getWaveletPodSpec(wallet string, genesis string, bootstrap ...string) corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Stdin:   true,
				Image:   "localhost:5000/wavelet",
				Name:    "wavelet",
				Command: append([]string{"./wavelet", "-api.port", strconv.Itoa(9000)}, bootstrap...),
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
						Name:  "WAVELET_SNOWBALL_K",
						Value: "10",
					},
					{
						Name:  "WAVELET_GENESIS",
						Value: genesis,
					},
					{
						Name:  "WAVELET_WALLET",
						Value: wallet,
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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WaveletSpec defines the desired state of Wavelet
// +k8s:openapi-gen=true
type WaveletSpec struct {
	Size           int32 `json:"size"`
	NumRichWallets uint  `json:"num_rich_wallets"`
}

// WaveletStatus defines the observed state of Wavelet
// +k8s:openapi-gen=true
type WaveletStatus struct{}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Wavelet is the Schema for the wavelets API
// +k8s:openapi-gen=true
type Wavelet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WaveletSpec   `json:"spec,omitempty"`
	Status WaveletStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WaveletList contains a list of Wavelet
type WaveletList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Wavelet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Wavelet{}, &WaveletList{})
}

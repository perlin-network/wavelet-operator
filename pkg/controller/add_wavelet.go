package controller

import (
	"github.com/perlin-network/wavelet-operator/pkg/controller/wavelet"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, wavelet.Add)
}

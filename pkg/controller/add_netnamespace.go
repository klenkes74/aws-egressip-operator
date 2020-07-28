package controller

import (
	"github.com/klenkes74/egressip-ipam-operator/pkg/controller/netnamespace"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, netnamespace.Add)
}

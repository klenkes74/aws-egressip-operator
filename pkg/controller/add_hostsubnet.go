package controller

import (
	"github.com/klenkes74/egressip-ipam-operator/pkg/controller/hostsubnet"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, hostsubnet.Add)
}

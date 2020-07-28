package controller

import (
	"github.com/klenkes74/egressip-ipam-operator/pkg/cloudprovider"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var cloud *cloudprovider.CloudProvider

func init() {
	cloud = cloudprovider.CreateCloudProvider()
}

// AddToManagerFuncs is a list of functions to add all Controllers to the Manager
var AddToManagerFuncs []func(manager.Manager, *cloudprovider.CloudProvider) error

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager) error {

	for _, f := range AddToManagerFuncs {
		if err := f(m, cloud); err != nil {
			return err
		}
	}
	return nil
}

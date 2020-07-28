package openshift

import (
	"github.com/klenkes74/egressip-ipam-operator/pkg/cloudprovider"
	"github.com/klenkes74/egressip-ipam-operator/pkg/logger"
	ocpnetv1 "github.com/openshift/api/network/v1"
	corev1 "k8s.io/api/core/v1"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The logger for the whole package.
var log = logger.Log.WithName("egress-ip-handler")

// NewEgressIPHandler - creates a new handler with cloudprovider and OCP client
func NewEgressIPHandler(c cloudprovider.CloudProvider, o client.Client) *EgressIPHandler {
	data := &ProdEgressIPHandler{
		client: o,
		cloud:  c,
	}

	result := EgressIPHandler(data)
	return &result
}

// The EgressIPHandler hides the infrastructure from the workflows defined in the reconcilers.
type EgressIPHandler interface {
	// adds IPs (specified or random) to the infrastructure (AWS and hostSubnet)
	AddIPsToInfrastructure(namespace *corev1.Namespace) ([]*net.IP, error)
	// ensures that the IPs are on the given host
	CheckIPsForHost(hostSubnet *ocpnetv1.HostSubnet, ips []*net.IP) error
	// removes IPs (specified on the NetNamespace) from the infrastructure (AWS and hostSubnet)
	RemoveIPsFromInfrastructure(netNamespace *ocpnetv1.NetNamespace) error
	// redistributes IPs from a failing host
	// returns a map with key=IP and value=new hostname
	RedistributeIPsFromHost(node *ocpnetv1.HostSubnet) (map[string]string, error)
	ReadIpsFromHostSubnet(node *ocpnetv1.HostSubnet) []*net.IP

	// Adds the IPs to the NetNamespace
	AddIPsToNetNamespace(netNamespace *ocpnetv1.NetNamespace, ips []*net.IP) error
	// Removes the IPs from the NetNamespace
	RemoveIPsFromNetNamespace(netNamespace *ocpnetv1.NetNamespace)

	LoadNamespace(name string) (*corev1.Namespace, error)
	SaveNamespace(instance *corev1.Namespace) error

	LoadNetNameSpace(name string) (*ocpnetv1.NetNamespace, error)
	SaveNetNameSpace(instance *ocpnetv1.NetNamespace) error

	LoadHostSubnet(name string) (*ocpnetv1.HostSubnet, error)
	SaveHostSubnet(instance *ocpnetv1.HostSubnet) error

	LoadNode(name string) (*corev1.Node, error)
	SaveNode(instance *corev1.Node) error
}

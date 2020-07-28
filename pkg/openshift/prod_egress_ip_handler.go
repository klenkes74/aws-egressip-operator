package openshift

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/klenkes74/egressip-ipam-operator/pkg/cloudprovider"
	ocpnetv1 "github.com/openshift/api/network/v1"
	"github.com/redhat-cop/egressip-ipam-operator/pkg/controller/egressipam"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"net"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
)

var _ EgressIPHandler = &ProdEgressIPHandler{}

// ProdEgressIPHandler The AWS/OCP implementation of the EgressIPHandler
type ProdEgressIPHandler struct {
	client client.Client
	cloud  cloudprovider.CloudProvider
}

// CheckIPsForHost - tests if all IPs are attached to this host
func (h *ProdEgressIPHandler) CheckIPsForHost(hostSubnet *ocpnetv1.HostSubnet, ips []*net.IP) error {
	i, err := h.cloud.InstanceByHostName(hostSubnet.Name)
	if err != nil {
		return err
	}
	instance := *i

	awsIps := instance.SecondaryIps()

	log.Info("found ips on host",
		"instance", instance.HostName(),
		"ips.ocp", ips,
		"ips.aws", awsIps,
	)

	found := 0
	for _, subnetIP := range ips {
		for _, instanceIP := range awsIps {
			if reflect.DeepEqual(subnetIP, instanceIP) {
				found++
			}
		}
	}

	if found != len(ips) {
		return fmt.Errorf("IP missmatch for host: ips.ocp=%v, ips.aws=%v (found %v ips)", ips, awsIps, found)
	}

	return nil
}

// AddIPsToInfrastructure - adds the annotated IPs of the namespace to the operating system and AWS.
func (h *ProdEgressIPHandler) AddIPsToInfrastructure(namespace *corev1.Namespace) ([]*net.IP, error) {
	var ips []*net.IP
	var err error
	var ipErrors []error

	var instances []string
	ips, err = h.getAnnotatedIPs(namespace)
	if err == nil { // no IPS annotated
		instances, err = h.addSpecifiedIPsToCloudProvider(ips)
	} else {
		instances, ips, err = h.cloud.AddRandomIPs()
	}
	if err != nil {
		return nil, err
	}

	log.Info("added ips to infrastructure",
		"ips", ips,
		"instances", instances,
	)

	ipErrors = make([]error, 0)
	for i, instance := range instances {
		err = h.addIPToOcpNode(instance, ips[i])
		if err != nil {
			ipErrors = append(ipErrors, err)
		}
	}

	if len(ipErrors) > 0 {
		for _, e := range ipErrors {
			err = multierror.Append(err, e)
		}
	} else {
		err = nil
	}

	return ips, err
}

// returns the IPs that are annotated to be used for the egress ips.
func (h *ProdEgressIPHandler) getAnnotatedIPs(instance *corev1.Namespace) ([]*net.IP, error) {
	var ips []*net.IP

	ipstring, found := instance.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
	if !found || len(ipstring) < 7 {
		return nil, fmt.Errorf("no ips annotated ('%s' is not a valid list of ips)", ipstring)
	}

	ipStrings := strings.Split(ipstring, ",")
	ips = make([]*net.IP, len(ipStrings))

	for i, ip := range ipStrings {
		ipStruct := net.ParseIP(ip)
		ips[i] = &ipStruct
	}

	return ips, nil
}

func (h *ProdEgressIPHandler) addIPToOcpNode(instanceID string, ip *net.IP) error {
	instance, err := h.cloud.Instance(instanceID)
	if err != nil {
		return err
	}

	err = h.addIPToHostSubnet(instance, ip)
	if err != nil {
		return err
	}

	return nil
}

func (h *ProdEgressIPHandler) addIPToHostSubnet(instance *cloudprovider.CloudInstance, ip *net.IP) error {
	hostSubnet, err := h.loadHostSubnet((*instance).HostName())
	if err != nil {
		return err
	}

	log.Info(fmt.Sprintf("adding ip '%s' to hostSubnet '%s'", ip.String(), hostSubnet.GetName()))

	found := false
	for _, hostIP := range hostSubnet.EgressIPs {
		if ip.String() == hostIP {
			found = true
		}
	}

	if !found {
		hostSubnet.EgressIPs = append(hostSubnet.EgressIPs, ip.String())
		err := h.updateHostSubnet(hostSubnet)
		if err != nil {
			return err
		}
	}

	log.Info(fmt.Sprintf("added ip '%s' to instanceId '%s' via hostSubnet '%s'", ip.String(), (*instance).ID(), hostSubnet.GetName()))
	return nil
}

func (h *ProdEgressIPHandler) loadHostSubnet(instance string) (*ocpnetv1.HostSubnet, error) {
	result := &ocpnetv1.HostSubnet{}
	err := h.client.Get(context.TODO(), types.NamespacedName{Name: instance}, result)
	if err != nil {
		log.Error(err, "unable to retrieve", "hostSubnet", instance)
		return result, err
	}

	return result, nil
}

func (h *ProdEgressIPHandler) updateHostSubnet(subnet *ocpnetv1.HostSubnet) error {
	return h.client.Update(context.TODO(), subnet)
}

func (h *ProdEgressIPHandler) addSpecifiedIPsToCloudProvider(ips []*net.IP) ([]string, error) {
	instances, err := h.cloud.AddSpecifiedIPs(ips)
	if err != nil {
		return instances, err
	}

	var resultInstances []string
	var resultIps []*net.IP

	for i, instance := range instances {
		resultInstances = append(resultInstances, instance)
		resultIps = append(resultIps, ips[i])
	}

	log.Info("added ip to aws",
		"instance-ids", resultInstances,
		"ips", resultIps,
	)

	return resultInstances, nil
}

// RemoveIPsFromInfrastructure - Removes the IP from AWS and the HostSubnets it had been distributed to. Will return a multierror.
func (h *ProdEgressIPHandler) RemoveIPsFromInfrastructure(netNamespace *ocpnetv1.NetNamespace) error {
	var result []error
	var err error

	result = make([]error, 0)
	for _, ipString := range netNamespace.EgressIPs {
		ip := net.ParseIP(ipString)

		var instanceID string
		instanceID, err = h.cloud.RemoveIP(&ip)
		if err != nil {
			log.Error(err, "ignoring this error - most probably the IP has been removed already",
				"ip", ip,
				"netnamespace", netNamespace.Name,
			)

			err = nil
		}
		if instanceID != "" {
			var instance *cloudprovider.CloudInstance
			instance, err = h.cloud.Instance(instanceID)
			if instance != nil {
				err = h.removeIPFromHostSubnet(*instance, &ip)
			} else {
				log.Error(err, "didn't load the instance from aws - can not remove the IP from host subnet",
					"instance-id", instanceID)
			}
		}

		if err != nil {
			result = append(result, err)
		}
	}

	err = nil
	if len(result) > 0 {
		for _, err2 := range result {
			err = multierror.Append(err, err2)
		}
	}

	netNamespace.EgressIPs = []string{}

	return err
}

func (h *ProdEgressIPHandler) removeIPFromHostSubnet(instance cloudprovider.CloudInstance, ip *net.IP) error {
	hostSubnet, err := h.loadHostSubnet(instance.HostName())
	if err != nil {
		return err
	}

	found := false
	for _, hostIP := range hostSubnet.EgressIPs {
		if ip.String() == hostIP {
			found = true
		}
	}

	if found {
		for i, f := range hostSubnet.EgressIPs {
			if f == ip.String() {
				hostSubnet.EgressIPs[i] = hostSubnet.EgressIPs[len(hostSubnet.EgressIPs)-1]
				hostSubnet.EgressIPs[len(hostSubnet.EgressIPs)-1] = ""
				hostSubnet.EgressIPs = hostSubnet.EgressIPs[:len(hostSubnet.EgressIPs)-1]

				log.Info("removing egressIP from hostSubnet",
					"ip", ip.String(),
					"hostSubnet", hostSubnet.Name,
				)
			}
		}

		err := h.updateHostSubnet(hostSubnet)
		if err != nil {
			return err
		}
	} else {
		log.Info("ip not defined as egressIP on this node - nothing to do",
			"ip", ip.String(),
			"hostSubnet", hostSubnet.Name,
		)
	}

	return nil
}

// AddIPsToNetNamespace - Addres the list of IPs to the OCP netnamespace
func (h *ProdEgressIPHandler) AddIPsToNetNamespace(netNamespace *ocpnetv1.NetNamespace, ips []*net.IP) error {
	ipsString := h.convertIPsToStringArray(ips)

	if !reflect.DeepEqual(netNamespace.EgressIPs, ipsString) {
		oldIps := netNamespace.EgressIPs
		netNamespace.EgressIPs = ipsString

		log.Info("updated netnamespace",
			"old-ips", oldIps,
			"egressips", ipsString,
		)
	} else {
		log.Info("no update needed, netnamespace has already the correct egress ip addresses",
			"egressips", netNamespace.EgressIPs,
		)
	}

	return nil
}

func (h *ProdEgressIPHandler) convertIPsToStringArray(ips []*net.IP) []string {
	result := make([]string, len(ips))
	for i, ip := range ips {
		result[i] = ip.String()
	}
	return result
}

// RemoveIPsFromNetNamespace - Removes all IPs from the given netnamespace
func (h *ProdEgressIPHandler) RemoveIPsFromNetNamespace(netNamespace *ocpnetv1.NetNamespace) {
	if len(netNamespace.EgressIPs) == 0 {
		log.Info("No ips to remove from NetNamespace")
	} else {
		netNamespace.EgressIPs = []string{}

		log.Info("removed all ips from the NetNamespace")
	}
}

func (h *ProdEgressIPHandler) removeFromCloudProvider(ips []*net.IP) error {
	var errList []error
	errList = make([]error, 0)

	for i, ip := range ips {
		_, err := h.cloud.RemoveIP(ip)
		if err != nil {
			if strings.Contains(err.Error(), "found no network interface") {
				log.Info("the IP is already away. no need to work on this one")
			} else if strings.Contains(err.Error(), "found no attached network interface") {
				log.Info("the IP is not attached to any network interface. no need to work on this one")
			} else {
				log.Error(err, "removing of ip failed",
					"ip", ip.String(),
				)
				errList = append(errList, err)
			}
		} else {
			log.Info("removed aws ip "+strconv.Itoa(i+1)+" of "+strconv.Itoa(len(ips)),
				"ip", ip.String(),
			)
		}
	}

	if len(errList) > 0 {
		var result error
		for _, err := range errList {
			result = multierror.Append(result, err)
		}

		return result
	}

	return nil
}

// RedistributeIPsFromHost - redistributes the secondary IPs from the given host and returns a map with key=ip-address
// and the instance id as value.
func (h *ProdEgressIPHandler) RedistributeIPsFromHost(hostSubnet *ocpnetv1.HostSubnet) (map[string]string, error) {
	ips := h.ReadIpsFromHostSubnet(hostSubnet)
	if len(ips) == 0 {
		return nil, fmt.Errorf("hostSubnet '%s' does not carry egress ips", hostSubnet.Name)
	}

	result := make(map[string]string, len(ips))

	ipErrors := make([]error, 0)
	var instances []string

	err := h.removeFromCloudProvider(ips)
	if err != nil {
		log.Error(err, "could not remove IP from cloud provider")
		return nil, err
	}

	hostSubnet.EgressIPs = []string{}

	instances, err = h.addSpecifiedIPsToCloudProvider(ips)
	if err != nil {
		log.Error(err, "could not add specified ip to cloud provider")
		return nil, err
	}

	for i, instance := range instances {
		err := h.addIPToOcpNode(instance, ips[i])
		if err != nil {
			log.Error(err, "could not add IP to OCP",
				"instance", instance,
				"ip", ips[i],
			)
			ipErrors = append(ipErrors, err)
		}
		result[ips[i].String()] = instance
	}

	log.Info("Redistributed the ips to new instances",
		"result", result,
	)

	if len(ipErrors) > 0 {
		for _, err2 := range ipErrors {
			err = multierror.Append(err, err2)
		}
	} else {
		err = nil
	}

	if err != nil {
		log.Error(err, "error when assigning ips")
	}
	return result, err
}

// ReadIpsFromHostSubnet - Reads the IP from the status field of the OCP node object and returns them as array.
func (h *ProdEgressIPHandler) ReadIpsFromHostSubnet(hostSubnet *ocpnetv1.HostSubnet) []*net.IP {
	if len(hostSubnet.EgressIPs) > 0 {
		result := make([]*net.IP, len(hostSubnet.EgressIPs))

		for i, ipString := range hostSubnet.EgressIPs {
			ip := net.ParseIP(ipString)
			result[i] = &ip
		}

		return result
	}

	return make([]*net.IP, 0)
}

// LoadHostSubnet - really?
func (h *ProdEgressIPHandler) LoadHostSubnet(name string) (*ocpnetv1.HostSubnet, error) {
	result := &ocpnetv1.HostSubnet{}
	err := h.client.Get(context.TODO(), types.NamespacedName{Name: name}, result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SaveHostSubnet - really?
func (h *ProdEgressIPHandler) SaveHostSubnet(instance *ocpnetv1.HostSubnet) error {
	return h.client.Update(context.TODO(), instance)
}

// LoadNode - really?
func (h *ProdEgressIPHandler) LoadNode(name string) (*corev1.Node, error) {
	result := &corev1.Node{}
	err := h.client.Get(context.TODO(), types.NamespacedName{Name: name}, result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SaveNode - really?
func (h *ProdEgressIPHandler) SaveNode(instance *corev1.Node) error {
	return h.client.Update(context.TODO(), instance)
}

// LoadNetNameSpace - really?
func (h *ProdEgressIPHandler) LoadNetNameSpace(name string) (*ocpnetv1.NetNamespace, error) {
	result := &ocpnetv1.NetNamespace{}
	err := h.client.Get(context.TODO(), types.NamespacedName{Name: name}, result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SaveNetNameSpace - really?
func (h *ProdEgressIPHandler) SaveNetNameSpace(instance *ocpnetv1.NetNamespace) error {
	err := h.client.Update(context.TODO(), instance)
	if err != nil && strings.Contains(err.Error(), "StorageError: invalid object, Code: 4") {
		log.Info("the object did not match the UID - probably it is already deleted")
		err = nil
	}

	return err
}

// LoadNamespace - really?
func (h *ProdEgressIPHandler) LoadNamespace(name string) (*corev1.Namespace, error) {
	result := &corev1.Namespace{}
	err := h.client.Get(context.TODO(), types.NamespacedName{Name: name}, result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SaveNamespace - really?
func (h *ProdEgressIPHandler) SaveNamespace(instance *corev1.Namespace) error {
	return h.client.Update(context.TODO(), instance)
}

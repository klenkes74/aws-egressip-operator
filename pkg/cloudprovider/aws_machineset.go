package cloudprovider

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/service/ec2"
	"net"
)

var _ CloudMachineSet = &AwsMachineSet{}

// AwsMachineSet is the AWS based implementation of the CloudMachineSet
type AwsMachineSet struct {
	provider *AwsCloudProvider

	name              string                 // Name of the AWS Autoscaling Group
	arn               string                 // ID of the AWS Autoscaling Group (the ARN)
	instances         []string               // List of AWS instance ids
	availabilityZones []*string              // List of AWS availability zones the Autscaling Group
	tags              map[string]string      // The AWS tags and their values
	subnets           map[string]*AwsNetwork // The subnets of the availability zone
}

// Name - the name of the ASG
func (ms *AwsMachineSet) Name() string {
	return ms.name
}

// URI - The ARN of the ASG
func (ms *AwsMachineSet) URI() string {
	return ms.arn
}

// Instances - An array of instance-ids of instances managed by this ASG
func (ms *AwsMachineSet) Instances() *[]string {
	return &ms.instances
}

// FailureZones - An array of the Availability zones this ASG is managing instances in.
func (ms *AwsMachineSet) FailureZones() []*string {
	return ms.availabilityZones
}

// Tags - A map of strings containing all AWS tags of the ASG
func (ms *AwsMachineSet) Tags() *map[string]string {
	return &ms.tags
}

// Networks - A map of CloudNetworks containing all AWS subnets the ASG has access to.
func (ms *AwsMachineSet) Networks() *map[string]*CloudNetwork {
	result := make(map[string]*CloudNetwork, len(ms.subnets))
	for _, network := range ms.subnets {
		n := CloudNetwork(network)
		result[network.availabilityZone] = &n
	}

	return &result
}

// NetworkForIP - Select the correct network for the IP in the given AwsMachineSet
func (ms *AwsMachineSet) NetworkForIP(ip *net.IP) (*CloudNetwork, error) {
	data, err := ms.networkForIP(ip)
	if err != nil {
		return nil, err
	}

	result := CloudNetwork(data)
	return &result, nil
}

// Select the correct network for the IP in the given AwsMachineSet
func (ms *AwsMachineSet) networkForIP(ip *net.IP) (*AwsNetwork, error) {
	for az, network := range ms.subnets {
		if network.IsIPInNetwork(ip) {
			log.Info("Found matching network for IP in machineset.",
				"machinesetId", ms.name,
				"availability-zone", az,
				"subnetId", network.Name(),
				"ip", ip.String(),
				"cidr", network.Cidr().String(),
			)

			return network, nil
		}
	}

	return nil, fmt.Errorf("No matching network found for IPv4 '%s' in machine set '%s'",
		ip.String(), ms.name)
}

// Returns the instance with the least IPs assigned.
// TODO rlichti 2020-06-02 iterates to all instances of an availability zone. That's not nice performance wise.
func (ms *AwsMachineSet) getInstanceWithLeastIPsAssigned(availabilityZone string) (*ec2.Instance, error) {
	found := ms.doesMachineSetUsesAvailabilityZone(availabilityZone)
	if !found {
		return nil, fmt.Errorf("the machine set '%s' does not cover availability zone '%s'", ms.name, availabilityZone)
	}

	if len(ms.instances) < 1 {
		return nil, fmt.Errorf("no instances within machine set '%s'", ms.name)
	}

	var result *ec2.Instance

	for _, instanceID := range ms.instances {
		i, err := ms.provider.instance(instanceID)

		if i != nil && *i.Placement.AvailabilityZone == availabilityZone {
			if err != nil {
				return nil, err
			}
			if result != nil {
				if len(i.NetworkInterfaces[0].PrivateIpAddresses) < len(result.NetworkInterfaces[0].PrivateIpAddresses) {
					result = i
				}
			} else {
				result = i
			}
		}
	}

	if result == nil {
		return nil, fmt.Errorf("No instance in availability zone '%s'", availabilityZone)
	}

	return result, nil
}

func (ms *AwsMachineSet) doesMachineSetUsesAvailabilityZone(availabilityZone string) bool {
	found := false
	for _, zone := range ms.availabilityZones {
		if *zone == availabilityZone {
			found = true
		}
	}
	return found
}

// AddRandomIP - Adds a random IP to a single subnet. It will return the IP assigned or the error that prevented to get the IP.
func (ms *AwsMachineSet) AddRandomIP(subnet *AwsNetwork) (string, *net.IP, error) {
	instance, err := ms.getInstanceWithLeastIPsAssigned(subnet.availabilityZone)
	if err != nil {
		return "", nil, err
	}
	instanceID := *instance.InstanceId

	if *instance.NetworkInterfaces[0].SubnetId != subnet.name {
		return instanceID, nil, errors.New("the found instance subnet does not match the needed subnet for adding the ip (subnet-id=" +
			subnet.name + ", networkinterface-subnet-id" + *instance.NetworkInterfaces[0].SubnetId + ")")
	}

	log.Info("Will assign new random IP to AWS instance",
		"machineset-id", ms.name,
		"subnet-id", subnet.Name(),
		"instance", instanceID,
		"cidr", subnet.Cidr().String(),
		"eni", instance.NetworkInterfaces[0].NetworkInterfaceId,
	)

	result, err := ms.provider.addRandomIPToInterface(*instance.NetworkInterfaces[0].NetworkInterfaceId)
	if err != nil {
		return instanceID, result, err
	}

	subnet.availableIPCount = subnet.availableIPCount - 1
	return instanceID, result, nil
}

// AddSpecifiedIP - adds the given IP one machine within the ASG.
func (ms *AwsMachineSet) AddSpecifiedIP(ip *net.IP) (string, error) {
	subnet, err := ms.networkForIP(ip)
	if err != nil {
		return "", err
	}

	instance, err := ms.getInstanceWithLeastIPsAssigned(subnet.FailureZone())
	if err != nil {
		return "", nil
	}
	instanceID := *instance.InstanceId

	if *instance.NetworkInterfaces[0].SubnetId != subnet.Name() {
		return instanceID, errors.New("the found instance subnet does not match the needed subnet for adding the ip (instance-id=" +
			*instance.InstanceId + ", hostname=" +
			*instance.PrivateDnsName + ", subnet-id=" +
			subnet.name + ", networkinterface-subnet-id" + *instance.NetworkInterfaces[0].SubnetId + ")")
	}

	subnet.availableIPCount = subnet.availableIPCount - 1
	return instanceID, ms.provider.addSpecifiedIPToInterface(*instance.NetworkInterfaces[0].NetworkInterfaceId, *ip)
}

package cloudprovider

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/hashicorp/go-multierror"
	"github.com/klenkes74/aws-egressip-operator/pkg/logger"
	"github.com/patrickmn/go-cache"
	"net"
	"os"
	"strings"
	"time"
)

const defaultAwsRegion = "eu-central-1" // AWS in Frankfurt/Main, Germany is our default
const defaultClusterName = "hugo"

var log = logger.Log.WithName("Aws-cloud")

// The singleton cloud provider. It will be lazy loaded.
var provider *AwsCloudProvider

// AwsProvider returns the singleton provider object
func AwsProvider() *AwsCloudProvider {
	if provider == nil {
		provider = &AwsCloudProvider{}
	}

	return provider
}

var _ CloudProvider = &AwsCloudProvider{}

// AwsCloudProvider implements the generic CloudProvider.
type AwsCloudProvider struct {
	Region string

	Aws AwsClient

	instances           *cache.Cache
	instancesByHostname map[string]string
	instancesBySubnet   map[string][]string
	subnets             *cache.Cache

	ClusterName string // name of the cluster -- will be the AWS tag key="kubernetes.io/cluster/<ClusterName>", value="owned"

	initialized bool // if the general part of this provider is initialized
}

// SetAwsClient -- Injector to get a special AwsClient (e.g. a mocked one) for special purposes ...
func (a *AwsCloudProvider) SetAwsClient(client *AwsClient, region string, clusterName string) {
	a.Aws = *client
	a.ClusterName = clusterName
	a.Region = region

	a.initialized = false
}

// initializeProvider -- initialized the AWS client and the region.
func (a *AwsCloudProvider) initializeProvider() error {
	var err error

	if a.initialized {
		return nil
	}

	if a.Region == "" {
		a.Region, err = a.readEnvironment("AWS_REGION", defaultAwsRegion)
		if err != nil {
			return err
		}
	}

	if a.ClusterName == "" {
		a.ClusterName, err = a.readEnvironment("CLUSTER_NAME", defaultClusterName)
		if err != nil {
			return err
		}
	}

	if a.Aws == nil {
		a.Aws, err = CreateAwsClient(a.Region)
		if err != nil {
			return err
		}
	}

	a.instances = cache.New(time.Minute, 10*time.Minute)
	a.subnets = cache.New(time.Minute, 10*time.Minute)
	a.instancesByHostname = make(map[string]string)
	a.instancesBySubnet = make(map[string][]string)

	a.initialized = true
	log.Info("Initialized AWS Cloud Provider.",
		"Cluster Name", a.ClusterName,
		"AWS Region", a.Region,
	)
	return nil
}

func (a *AwsCloudProvider) readEnvironment(key ...string) (string, error) {
	result, found := os.LookupEnv(key[0])
	if !found {
		if len(key) >= 2 {
			log.V(4).Info("ENVIRONMENT does not contain entry for key. Using provided default",
				"key", key[0],
				"default", key[1],
				"value", key[1],
			)

			return key[1], nil
		}

		err := errors.New("the ENVIRONMENT contains no entry " + key[0])

		return "", err
	}

	log.V(4).Info("Read system environment.",
		"key", key,
		"value", result,
	)
	return result, nil
}

// ClusterTag returns the AWS tag and value the cluster
// marks all AWS resources with.
func (a *AwsCloudProvider) ClusterTag() (string, string) {
	return "kubernetes.io/cluster/" + a.ClusterName, "owned"
}

// Instance -- loads an EC2 instance by its ID
func (a *AwsCloudProvider) Instance(instanceID string) (*CloudInstance, error) {
	_ = a.initializeProvider()

	log.Info("looking for Aws instance",
		"instance-id", instanceID,
	)

	instance, err := a.instance(instanceID)
	if err != nil {
		return nil, err
	}
	log.Info("loaded instance",
		"instance-id", instance.InstanceId,
		"hostname", instance.PrivateDnsName,
	)

	return AwsInstance.New(AwsInstance{}, a, *instance), nil
}

// InstanceByHostName returns the instance for the given hostname
func (a *AwsCloudProvider) InstanceByHostName(hostname string) (*CloudInstance, error) {
	_ = a.initializeProvider()

	if a.instancesByHostname[hostname] != "" {
		result, err := a.instance(a.instancesByHostname[hostname])
		if err != nil {
			return nil, err
		}
		return AwsInstance.New(AwsInstance{}, a, *result), nil
	}

	filter := ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("private-dns-name"),
				Values: aws.StringSlice([]string{hostname}),
			},
		},
	}

	instance, err := a.loadInstanceFromAws(filter)
	if err != nil {
		return nil, err
	}
	return AwsInstance.New(AwsInstance{}, a, *instance), nil
}

func (a *AwsCloudProvider) instance(instanceID string) (*ec2.Instance, error) {
	cached, found := a.instances.Get(instanceID)
	if found {
		return cached.(*ec2.Instance), nil
	}

	filter := ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("instance-id"),
				Values: aws.StringSlice([]string{instanceID}),
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: aws.StringSlice([]string{"running"}),
			},
		},
	}

	return a.loadInstanceFromAws(filter)
}

func (a *AwsCloudProvider) loadInstanceFromAws(filter ec2.DescribeInstancesInput) (*ec2.Instance, error) {
	instances, err := a.loadInstancesFromAws(filter)
	if err != nil {
		return nil, err
	}

	if len(instances) > 0 {
		log.Info("found instance",
			"instance-id", instances[0].InstanceId,
		)
	} else {
		return nil, errors.New("no instance found")
	}

	return instances[0], nil
}

func (a *AwsCloudProvider) loadAllInstancesFromAws() ([]*ec2.Instance, error) {
	key, _ := a.ClusterTag()

	filter := a.allClusterTagFilter(key)
	//goland:noinspection SpellCheckingInspection
	filter = append(filter, &ec2.Filter{Name: aws.String("tag:k8s.io/cluster-autoscaler/enabled"), Values: aws.StringSlice([]string{"true"})})
	filter = append(filter, &ec2.Filter{Name: aws.String("tag:ClusterNode"), Values: aws.StringSlice([]string{"WorkerNode"})})
	filter = append(filter, &ec2.Filter{Name: aws.String("instance-state-name"), Values: aws.StringSlice([]string{"running"})})

	return a.loadInstancesFromAws(ec2.DescribeInstancesInput{Filters: filter})
}

func (a *AwsCloudProvider) allClusterTagFilter(key string) []*ec2.Filter {
	return []*ec2.Filter{
		{
			Name:   aws.String("tag-key"),
			Values: aws.StringSlice([]string{key}),
		},
	}
}

func (a *AwsCloudProvider) loadInstancesFromAws(filter ec2.DescribeInstancesInput) ([]*ec2.Instance, error) {
	instances, err := a.Aws.DescribeInstances(&filter)
	if err != nil {
		return nil, err
	}

	var result []*ec2.Instance
	if len(instances.Reservations) > 0 {
		result = make([]*ec2.Instance, len(instances.Reservations))
		for i, reservation := range instances.Reservations {
			result[i] = reservation.Instances[0]

			_, found := a.instances.Get(*result[i].InstanceId)
			if !found {
				a.initializeInstanceInformation(result[i])
			}
		}
	} else {
		return nil, errors.New("no instance found")
	}

	return result, nil
}

func (a *AwsCloudProvider) initializeInstanceInformation(instance *ec2.Instance) {
	a.instances.Set(*instance.InstanceId, instance, cache.DefaultExpiration)

	log.Info("initializing instance",
		"instance-id", instance.InstanceId,
		"instance-name", instance.PrivateDnsName,
		"instance-ip", instance.PrivateIpAddress,
		"eni-count", len(instance.NetworkInterfaces),
		"subnet-id", instance.SubnetId,
	)

	if len(*instance.PrivateDnsName) > 0 {
		a.instancesByHostname[*instance.PrivateDnsName] = *instance.InstanceId
	}

	if len(instance.NetworkInterfaces) >= 1 {
		networkInterface := instance.NetworkInterfaces[0]

		if a.instancesBySubnet[*networkInterface.SubnetId] == nil {
			a.instancesBySubnet[*networkInterface.SubnetId] = make([]string, 0)
		}
		a.instancesBySubnet[*networkInterface.SubnetId] = append(a.instancesBySubnet[*networkInterface.SubnetId], *instance.InstanceId)
	}
}

func (a *AwsCloudProvider) loadSubnetsFromAws() error {
	key, _ := a.ClusterTag()
	var filter = ec2.DescribeSubnetsInput{
		Filters: a.allClusterTagFilter(key),
	}
	log.Info("loading subnets",
		"filter", filter,
	)

	subnets, err := a.Aws.DescribeSubnets(&filter)
	if err != nil {
		return err
	}

	for _, subnet := range subnets.Subnets {
		a.subnets.Set(*subnet.SubnetId, subnet, cache.DefaultExpiration)
	}

	return nil
}

// addRandomIPToInterface -- adds an additional IP to the given interface.
//
// AWS will assign a free IP address to the given Interface.
func (a *AwsCloudProvider) addRandomIPToInterface(interfaceID string) (*net.IP, error) {
	addressRequest := ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(interfaceID),
		SecondaryPrivateIpAddressCount: aws.Int64(int64(1)),
	}
	addressResponse, err := a.Aws.AssignPrivateIPAddresses(&addressRequest)
	if err != nil {
		return nil, err
	}

	if len(addressResponse.AssignedPrivateIpAddresses) != 1 {
		ips := make([]string, len(addressResponse.AssignedPrivateIpAddresses))
		for i, ipAddress := range addressResponse.AssignedPrivateIpAddresses {
			ips[i] = ipAddress.String()
		}
		return nil, fmt.Errorf("there has been no or too much IP address assigned to the eni '%s': [%s]",
			interfaceID, strings.Join(ips, ","))
	}

	ip := net.ParseIP(*addressResponse.AssignedPrivateIpAddresses[0].PrivateIpAddress)
	log.Info("Assigned IP to eni",
		"eni", interfaceID,
		"ip-address", ip.String())

	return &ip, nil
}

// addSpecifiedIPToInterface -- Adds the specified IP to the given interface.
func (a *AwsCloudProvider) addSpecifiedIPToInterface(interfaceID string, ip net.IP) error {
	addressRequest := ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(interfaceID),
		PrivateIpAddresses: aws.StringSlice([]string{ip.String()}),
	}
	_, err := a.Aws.AssignPrivateIPAddresses(&addressRequest)
	if err != nil {
		return err
	}

	log.Info("Assigned IP to eni",
		"eni", interfaceID,
		"ip-address", ip.String())

	return nil
}

// AddSpecifiedIPs adds the given IPs to the cloud.
func (a *AwsCloudProvider) AddSpecifiedIPs(ips []*net.IP) ([]string, error) {
	_ = a.initializeProvider()

	result := make([]string, len(ips))
	assignmentErrors := make([]error, 0)

	if len(ips) == 0 {
		return nil, errors.New("no ips specified")
	}

	for i, ip := range ips {
		instanceID, err := a.addSpecifiedIP(ip)
		if err != nil {
			assignmentErrors = append(assignmentErrors, err)
		}

		result[i] = instanceID
	}

	log.Info("added specific ips",
		"instances", result,
		"ips", ips,
		"errors", assignmentErrors,
	)

	if len(assignmentErrors) > 0 {
		var err error
		for _, err2 := range assignmentErrors {
			err = multierror.Append(err, err2)
		}

		return result, err
	}

	return result, nil
}

// Adds a specified IP to the cluster. It will look for a matching subnet and then add the IP to the instance with
// least IPs attached.
func (a *AwsCloudProvider) addSpecifiedIP(ip *net.IP) (string, error) {
	subnet, err := a.findSubnetForIP(ip)
	if err != nil {
		log.Error(err, "no matching subnet found",
			"ip", ip,
		)
		return "", fmt.Errorf("can not find a matching subnet for ip '%s'", ip.String())
	}

	instance, err := a.instanceWithLeastNumberOfIps(*subnet.SubnetId)
	if err != nil {
		return "", err
	}

	err = a.addSpecifiedIPToInterface(*instance.NetworkInterfaces[0].NetworkInterfaceId, *ip)
	if err != nil {
		return "", err
	}

	log.Info(fmt.Sprintf("added specified ip '%s' to instance '%s'",
		ip.String(), *instance.InstanceId))

	return *instance.InstanceId, nil
}

func (a *AwsCloudProvider) findSubnetForIP(ip *net.IP) (*ec2.Subnet, error) {
	var result *ec2.Subnet

	if a.subnets == nil || a.subnets.ItemCount() < 1 || len(a.subnets.Items()) < 1 {
		log.Info("initializing subnets from AWS")
		err := a.loadSubnetsFromAws()
		if err != nil {
			return nil, err
		}
	}

	log.Info("checking subnets from AWS",
		"subnet-count-item-count", a.subnets.ItemCount(),
		"subnet-count-len", len(a.subnets.Items()),
	)

	for _, item := range a.subnets.Items() {
		subnet := item.Object.(*ec2.Subnet)

		log.Info("checking subnet for ip",
			"subnet-id", subnet.SubnetId,
			"cidr", subnet.CidrBlock,
			"availability-zone", subnet.AvailabilityZoneId,
			"free-ips", subnet.AvailableIpAddressCount,
		)

		if result == nil {
			_, cidr, err := net.ParseCIDR(*subnet.CidrBlock)
			if err == nil {
				if cidr.Contains(*ip) {
					result = subnet
				}
			}
		}
	}

	if result == nil {
		return nil, fmt.Errorf("no subnet found for IP address '%s'", ip.String())
	}

	return result, nil
}

// AddRandomIPs adds a random IP addresses to any machine of the given machine set. It will add one ip address in every
// subnet used by the machine set.
// It will return either the instances and the new assigned IPs or an error.
func (a *AwsCloudProvider) AddRandomIPs() ([]string, []*net.IP, error) {
	_ = a.initializeProvider()

	err := a.loadSubnetsFromAws()
	if err != nil {
		return nil, nil, err
	}

	log.Info("adding random ips to the infrastructure",
		"no-of-subnets", a.subnets.ItemCount(),
	)

	_, err = a.loadAllInstancesFromAws() // need to have all available instances in our cache.
	if err != nil {
		return nil, nil, err
	}

	instanceIds := make([]string, a.subnets.ItemCount())
	ips := make([]*net.IP, a.subnets.ItemCount())
	assignmentErrors := make([]error, a.subnets.ItemCount())

	i := 0
	for subnetID, item := range a.subnets.Items() {
		subnet := item.Object.(*ec2.Subnet)

		log.Info(fmt.Sprintf("need ip in subnet '%s' for availability zone '%s'",
			subnetID, *subnet.AvailabilityZone))

		instance, err := a.instanceWithLeastNumberOfIps(subnetID)

		if err != nil {
			assignmentErrors[i] = err
		} else {
			instanceIds[i] = *instance.InstanceId

			ips[i], err = a.addRandomIPToInterface(*instance.NetworkInterfaces[0].NetworkInterfaceId)
			if err != nil {
				assignmentErrors[i] = err
			}
		}

		if err != nil {
			log.Error(err, fmt.Sprintf("error while adding random ip '%s' to instance '%s'", ips[i], instance))
		} else {
			log.Info(fmt.Sprintf("added random ip '%s' to instance '%s'", ips[i], *instance.InstanceId))
		}

		i++
	}

	i = 0
	for _, err2 := range assignmentErrors {
		if err2 != nil {
			err = multierror.Append(err, err2)
			i++
		}
	}
	if i == 0 {
		err = nil
	}

	log.Info("added random ips",
		"instances", instanceIds,
		"ips", ips,
		"error", err,
	)
	return instanceIds, ips, err
}

// cycles through all instances within a subnet to find the instance with the least IPs assigned.
func (a *AwsCloudProvider) instanceWithLeastNumberOfIps(subnetID string) (*ec2.Instance, error) {
	var result *ec2.Instance

	instanceIds := a.instancesBySubnet[subnetID]
	if len(instanceIds) > 0 {
		for _, id := range instanceIds {
			if result != nil && len(result.NetworkInterfaces[0].PrivateIpAddresses) <= 2 {
				break // That's good enough. We will use this one ...
			}

			instance, err := a.instance(id)
			if err == nil {
				for _, tag := range instance.Tags {
					if *tag.Key == "ClusterNode" && *tag.Value == "WorkerNode" {
						if result == nil {
							result = instance
						} else {
							if len(result.NetworkInterfaces[0].PrivateIpAddresses) > len(instance.NetworkInterfaces[0].PrivateIpAddresses) {
								result = instance
							}
						}
					}
				}
			}
		}
	} else {
		_, err := a.loadAllInstancesFromAws()
		if err != nil {
			return nil, fmt.Errorf("can not load instanced from AWS for reading instances in subnet '%s'", subnetID)
		}
		instanceIds := a.instancesBySubnet[subnetID]
		if len(instanceIds) > 0 {
			return a.instanceWithLeastNumberOfIps(subnetID)
		}

		result = nil
	}

	if result == nil {
		return nil, fmt.Errorf("no instances in subnet '%s'", subnetID)
	}

	return result, nil
}

// RemoveIP removes the IP from the AWS account
func (a *AwsCloudProvider) RemoveIP(ip *net.IP) (string, error) {
	networkInterface, err := a.findNetworkInterfaceForIP(ip)
	if err != nil {
		return "", err
	}

	return a.unAssignIPFromNetworkInterface(networkInterface, ip)
}

func (a *AwsCloudProvider) findNetworkInterfaceForIP(ip *net.IP) (*ec2.NetworkInterface, error) {
	input := ec2.DescribeNetworkInterfacesInput{
		Filters: a.createEc2Filter("addresses.private-ip-address", []string{ip.String()}),
	}

	output, err := a.Aws.DescribeNetworkInterfaces(&input)
	if err != nil {
		return nil, err
	}

	if len(output.NetworkInterfaces) != 1 {
		return nil, fmt.Errorf("found no network interface for ip '%s'", ip.String())
	}

	if output.NetworkInterfaces[0].Attachment != nil {
		log.Info("found network interface",
			"network-interface-id", output.NetworkInterfaces[0].NetworkInterfaceId,
			"instance-id", output.NetworkInterfaces[0].Attachment.InstanceId,
			"ip", ip,
		)
	} else {
		return nil, fmt.Errorf("found no attached network interface for ip '%s'", ip.String())
	}

	return output.NetworkInterfaces[0], nil
}

func (a *AwsCloudProvider) unAssignIPFromNetworkInterface(networkInterface *ec2.NetworkInterface, ip *net.IP) (string, error) {
	if networkInterface.Attachment != nil {
		instanceID := *networkInterface.Attachment.InstanceId

		log.Info("removing network interface from instance",
			"network-interface-id", networkInterface.NetworkInterfaceId,
			"instance-id", instanceID,
		)
		unAssign := ec2.UnassignPrivateIpAddressesInput{
			NetworkInterfaceId: networkInterface.NetworkInterfaceId,
			PrivateIpAddresses: aws.StringSlice([]string{ip.String()}),
		}
		_, err := a.Aws.UnassignPrivateIPAddresses(&unAssign)
		if err != nil {
			return "", err
		}

		return instanceID, nil
	}

	return "", nil
}

func (a *AwsCloudProvider) createEc2Filter(key string, values []string) []*ec2.Filter {
	return []*ec2.Filter{
		{
			Name:   aws.String(key),
			Values: aws.StringSlice(values),
		},
	}
}

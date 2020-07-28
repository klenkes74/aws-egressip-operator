package cloudprovider

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/hashicorp/go-multierror"
	"github.com/klenkes74/egressip-ipam-operator/pkg/logger"
	"github.com/patrickmn/go-cache"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultAwsRegion = "eu-central-1" // AWS in Frankfurt/Main, Germany is our default
const defaultInstanceMapSize = 100
const autoscalingGroupMaxRetrieve = 50

var log = logger.Log.WithName("aws-cloud")

var provider CloudProvider

func init() {
	data := &AwsCloudProvider{}
	err := data.initializeProvider()
	if err != nil {
		log.Error(err, "can't initialize cloud provider")
		os.Exit(10)
	}

	provider = CloudProvider(data)
}

// AwsProvider returns the singleton provider object
func AwsProvider() *CloudProvider {
	return &provider
}

var _ CloudProvider = &AwsCloudProvider{}

// AwsCloudProvider implements the generic CloudProvider.
type AwsCloudProvider struct {
	Region            string
	AvailabilityZones []*ec2.AvailabilityZone

	aws *cache.Cache // sessions, ec2 client and autoscaling client

	autoscalingGroups           *cache.Cache
	machinesets                 *cache.Cache
	instances                   *cache.Cache
	instancesByHostname         map[string]string
	instancesByPrivateDNS       map[string]string
	instancesByPrivateIPv4      map[string]string
	instancesByNetworkInterface map[string]string
	instancesBySubnet           map[string][]string
	instancesByMachineset       map[string][]string
	subnets                     *cache.Cache
	interfaces                  *cache.Cache

	ClusterName string // name of the cluster -- will be the AWS tag key="kubernetes.io/cluster/<ClusterName>", value="owned"

	initialized            bool  // if the general part of this provider is initialized
	machineSetsLoaded      bool  // if the machine sets (AWS AutoscalingGroups) are loaded
	autoscalingMaxRetrieve int64 // maximum number of autoscaling groups to retrieve in one chunk
}

// initializeProvider -- initialized the AWS client and the region.
func (a *AwsCloudProvider) initializeProvider() error {
	if a.initialized {
		return nil
	}

	a.Region, _ = a.readEnvironment("AWS_REGION", defaultAwsRegion)

	clusterName, err := a.readEnvironment("CLUSTER_NAME")
	if err != nil {
		return err
	}
	a.ClusterName = clusterName

	a.aws = cache.New(60*time.Minute, time.Minute)

	a.instances = cache.New(time.Minute, 10*time.Minute)
	a.subnets = cache.New(time.Minute, 10*time.Minute)
	a.machinesets = cache.New(time.Minute, 10*time.Minute)
	a.autoscalingGroups = cache.New(time.Minute, 10*time.Minute)
	a.interfaces = cache.New(time.Minute, 10*time.Minute)
	a.instancesByHostname = make(map[string]string, defaultInstanceMapSize)
	a.instancesByPrivateDNS = make(map[string]string, defaultInstanceMapSize)
	a.instancesByPrivateIPv4 = make(map[string]string, defaultInstanceMapSize)
	a.instancesByNetworkInterface = make(map[string]string, defaultInstanceMapSize)
	a.instancesBySubnet = make(map[string][]string, defaultInstanceMapSize)
	a.instancesByMachineset = make(map[string][]string, defaultInstanceMapSize)

	err = a.initializeAvailabilityZones()
	if err != nil {
		return err
	}

	asgChunkSize, _ := a.readEnvironment("AWS_RETRIEVE_AUTOSCALING_GROUP_CHUNK_SIZE", strconv.Itoa(autoscalingGroupMaxRetrieve))
	asgChunkSizeInt, _ := strconv.Atoi(asgChunkSize)
	a.autoscalingMaxRetrieve = int64(asgChunkSizeInt)

	a.initialized = true
	log.Info("Initialized AWS Cloud Provider.",
		"Cluster Name", a.ClusterName,
		"AWS Region", a.Region,
		"AWS EC2 Client", a.ec2Client(),
		"AutoscalingGroup Max Retrieve", a.autoscalingMaxRetrieve,
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

func (a *AwsCloudProvider) session() *session.Session {
	var result *session.Session

	cached, found := a.aws.Get("session")
	if found {
		result = cached.(*session.Session)
	} else {
		awssession := a.createSession()
		a.aws.Set("session", awssession, 60*time.Minute)

		result = awssession
	}

	return result
}

func (a *AwsCloudProvider) ec2Client() *ec2.EC2 {
	var result *ec2.EC2

	cached, found := a.aws.Get("ec2Client")
	if found {
		result = cached.(*ec2.EC2)
	} else {
		ec2Client, err := a.createAwsEc2Client()
		if err != nil {
			log.Error(err, "can not create AWS EC2 client")
			os.Exit(10)
		}
		a.aws.Set("ec2Client", ec2Client, 10*time.Minute)

		result = ec2Client
	}

	return result
}

func (a *AwsCloudProvider) awsAutoscalingClient() *autoscaling.AutoScaling {
	var result *autoscaling.AutoScaling

	cached, found := a.aws.Get("autoscalingClient")
	if found {
		result = cached.(*autoscaling.AutoScaling)
	} else {
		ec2Client, err := a.createAwsAutoscalingClient()
		if err != nil {
			log.Error(err, "can not create AWS autoscaling client")
			os.Exit(11)
		}
		a.aws.Set("autoscalingClient", ec2Client, 10*time.Minute)

		result = ec2Client
	}

	return result

}

func (a *AwsCloudProvider) createSession() *session.Session {
	return session.Must(session.NewSession())
}

func (a *AwsCloudProvider) createAwsEc2Client() (*ec2.EC2, error) {
	client := ec2.New(a.session(), aws.NewConfig().WithRegion(a.Region))
	if client == nil {
		return nil, errors.New("can't create new AWS ec2 client")
	}
	return client, nil
}

func (a *AwsCloudProvider) createAwsAutoscalingClient() (*autoscaling.AutoScaling, error) {
	asgClient := autoscaling.New(a.session(), aws.NewConfig().WithRegion(a.Region))
	if asgClient == nil {
		return nil, errors.New("can't create new AWS autoscaling client")
	}
	return asgClient, nil
}

// Loads all availability zones within the configured region.
func (a *AwsCloudProvider) initializeAvailabilityZones() error {
	if len(a.AvailabilityZones) > 0 {
		return nil
	}

	input := ec2.DescribeAvailabilityZonesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("region-name"),
				Values: aws.StringSlice([]string{a.Region}),
			},
		},
	}

	output, err := a.ec2Client().DescribeAvailabilityZones(&input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				log.Error(err, aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}

		return err
	}

	for _, zone := range output.AvailabilityZones {
		log.V(4).Info("Adding aviability zone.",
			"region", a.Region,
			"availability-zone", zone,
		)
		a.AvailabilityZones = append(a.AvailabilityZones, zone)
	}
	log.Info("Added availability zones availailable.",
		"region", a.Region,
		"number-of-availability-zones", len(output.AvailabilityZones),
	)

	return nil
}

// HostName reads the hostname to the given instance-id.
func (a *AwsCloudProvider) HostName(instanceID string) (string, error) {
	_ = a.initializeProvider()

	instance, err := a.instance(instanceID)
	if err != nil {
		return "", err
	}

	return *instance.PrivateDnsName, nil
}

// MachineSet -- retrieves a machine set by specifying the name (AWS autoscaling group name)
//
// Taking the name instead of the ARN (which would resemble the ID concept more) but the ARN is not annotated at the
// AWS instances but the name is as "aws:autoscaling:groupName" tag.
func (a *AwsCloudProvider) MachineSet(machinesetID string) (CloudMachineSet, error) {
	_ = a.initializeProvider()

	data, err := a.getMachineSet(machinesetID)
	if err != nil {
		return nil, err
	}

	return CloudMachineSet(data), nil
}

func (a *AwsCloudProvider) getMachineSet(machineSetID string) (*AwsMachineSet, error) {
	err := a.loadAllMachineSetsOfCluster()
	if err != nil {
		return nil, err
	}

	result, found := a.machinesets.Get(machineSetID)
	if !found {
		return nil, fmt.Errorf("no machine set with id '%s'", machineSetID)
	}

	return result.(*AwsMachineSet), nil
}

// MachineSets returns a full list of machine sets.
func (a *AwsCloudProvider) MachineSets() ([]CloudMachineSet, error) {
	_ = a.initializeProvider()
	err := a.loadAllMachineSetsOfCluster()
	if err != nil {
		return nil, err
	}

	var result []CloudMachineSet
	for _, item := range a.machinesets.Items() {
		result = append(result, CloudMachineSet(item.Object.(*AwsMachineSet)))
	}

	return result, nil
}

// loadAllMachineSetsOfCluster -- loads all AWS autoscaling groups labeled with the clustername
//
// Since AWS doesn't provide a filter for doing that we need to load ALL autoscaling groups and
// filter ourselves by reading the AWS tag.
//
// Need to wrap the call into a loop since AWS delivers a maximum amount of results at most and
// we need to iterate over the returned result sets until all data has been given.
func (a *AwsCloudProvider) loadAllMachineSetsOfCluster() error {
	if a.machineSetsLoaded {
		return nil
	}

	input := autoscaling.DescribeAutoScalingGroupsInput{}

	next := true
	for next {
		output, err := a.awsAutoscalingClient().DescribeAutoScalingGroups(&input)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				default:
					log.Error(err, aerr.Error())
				}
			} else {
				fmt.Println(err.Error())
			}

			return err
		}

		if len(output.AutoScalingGroups) < 1 && a.machinesets.ItemCount() < 1 {
			return errors.New("no autoscaling groups found in AWS")
		}

		for _, group := range output.AutoScalingGroups {
			autoscalerEnabled := false
			correctCluster := false

			for _, tag := range group.Tags {
				if *tag.Key == "k8s.io/cluster-autoscaler/enabled" && *tag.Value == "true" {
					autoscalerEnabled = true
				}

				if *tag.Key == fmt.Sprintf("kubernetes.io/cluster/%s", a.ClusterName) { // the value is not important ...
					correctCluster = true
				}

				if correctCluster && autoscalerEnabled {
					log.Info("adding autoscaling group",
						"name", group.AutoScalingGroupName,
						"availability-zones", group.AvailabilityZones,
					)
					a.autoscalingGroups.Set(*group.AutoScalingGroupName, group, cache.DefaultExpiration)

					machineSet, err := a.convertToMachineSet(group)
					if err != nil {
						return err
					}
					a.machinesets.Set(*group.AutoScalingGroupName, machineSet, cache.DefaultExpiration)

					break // no need to rush through all tags if we already decided to have the machine set added ...
				}
			}
		}

		// Check if there are additional chunks of data left that need to be retrieved.
		if output.NextToken != nil {
			input.SetNextToken(*output.NextToken)
		} else {
			next = false // end the loop to collect all ASGs
		}

		log.Info("Loaded machinesets from AWS",
			"number-of-machinesets", a.machinesets.ItemCount(),
		)
	}

	a.machineSetsLoaded = true // mark as initialized
	log.Info("Loaded all machinesets annotated with the current cluster name",
		"cluster-name", a.ClusterName,
		"count-of-machinesets", a.machinesets.ItemCount(),
		"count-of-autoscaling-groups", a.autoscalingGroups.ItemCount(),
	)
	return nil
}

// convertToMachineSet -- Converts the AWS autoscaling group to our internal AwsMachineSet.
//
// In addition the cache containing the instances of the cluster sorted by availability zones will be polulated.
func (a *AwsCloudProvider) convertToMachineSet(orig *autoscaling.Group) (*AwsMachineSet, error) {

	result := new(AwsMachineSet)
	result.provider = a
	result.name = *orig.AutoScalingGroupName
	result.arn = *orig.AutoScalingGroupARN
	result.availabilityZones = orig.AvailabilityZones
	result.instances = []string{}

	for _, instance := range orig.Instances {
		result.instances = append(result.instances, *instance.InstanceId)
	}

	subnetIds := strings.Split(*orig.VPCZoneIdentifier, ",")
	result.subnets = make(map[string]*AwsNetwork, len(subnetIds))
	for _, subnetID := range subnetIds {
		subnet, err := a.getAWSSubnet(subnetID)
		if err != nil {
			return nil, err
		}

		result.subnets[subnetID] = subnet
	}

	result.tags = make(map[string]string, len(orig.Tags))
	for _, tag := range orig.Tags {
		result.tags[*tag.Key] = *tag.Value
	}

	return result, nil
}

// HostNameByIP returns the hostname of the IP given.
func (a *AwsCloudProvider) HostNameByIP(ip *net.IP) (string, error) {
	_ = a.initializeProvider()

	if a.instancesByPrivateIPv4[ip.String()] != "" {
		result, err := a.instance(a.instancesByHostname[ip.String()])
		if err != nil {
			return "", err
		}
		return *result.PrivateDnsName, nil
	}

	filter := ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("network-interface.addresses.private-ip-address"),
				Values: aws.StringSlice([]string{ip.String()}),
			},
		},
	}

	instance, err := a.loadInstanceFromAws(filter)
	if err != nil {
		return "", err
	}

	return *instance.PrivateDnsName, nil
}

// Instance -- loads an EC2 instance by its ID
func (a *AwsCloudProvider) Instance(instanceID string) (*CloudInstance, error) {
	_ = a.initializeProvider()

	log.Info("looking for aws instance",
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
	add := ec2.Filter{Name: aws.String("tag:k8s.io/cluster-autoscaler/enabled"), Values: aws.StringSlice([]string{"true"})}
	filter = append(filter, &add)
	add = ec2.Filter{Name: aws.String("tag:ClusterNode"), Values: aws.StringSlice([]string{"WorkerNode"})}
	filter = append(filter, &add)
	add = ec2.Filter{Name: aws.String("instance-state-name"), Values: aws.StringSlice([]string{"running"})}
	filter = append(filter, &add)

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
	instances, err := a.ec2Client().DescribeInstances(&filter)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				log.Error(err, aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}

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

	if len(*instance.PrivateIpAddress) > 0 {
		a.instancesByPrivateIPv4[*instance.PrivateIpAddress] = *instance.InstanceId
	}

	if len(instance.NetworkInterfaces) >= 1 {
		networkInterface := instance.NetworkInterfaces[0]
		a.instancesByNetworkInterface[*networkInterface.NetworkInterfaceId] = *instance.InstanceId

		if a.instancesBySubnet[*networkInterface.SubnetId] == nil {
			a.instancesBySubnet[*networkInterface.SubnetId] = make([]string, defaultInstanceMapSize)
		}
		a.instancesBySubnet[*networkInterface.SubnetId] = append(a.instancesBySubnet[*networkInterface.SubnetId], *instance.InstanceId)
	}

	for _, tag := range instance.Tags {
		if *tag.Key == "aws:autoscaling:groupName" {
			if a.instancesByMachineset[*tag.Value] == nil {
				a.instancesByMachineset[*tag.Value] = make([]string, defaultInstanceMapSize)
			}
			a.instancesByMachineset[*tag.Value] = append(a.instancesByMachineset[*tag.Value], *instance.InstanceId)
		}
	}
}

// Network loads an AWS subnet by its ID
func (a *AwsCloudProvider) Network(subnetID string) (*CloudNetwork, error) {
	data, err := a.getAWSSubnet(subnetID)
	if err != nil {
		return nil, err
	}

	result := CloudNetwork(data)
	return &result, nil
}

func (a *AwsCloudProvider) getAWSSubnet(subnetID string) (*AwsNetwork, error) {
	_ = a.initializeProvider()

	var result *ec2.Subnet
	var err error

	cached, found := a.subnets.Get(subnetID)
	if found {
		result = cached.(*ec2.Subnet)
	} else {
		result, err = a.loadSubnetFromAws(subnetID)
		if err != nil {
			return nil, err
		}
	}

	return a.convertToNetwork(result)
}

func (a *AwsCloudProvider) loadSubnetFromAws(subnetID string) (*ec2.Subnet, error) {
	var filter = ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("subnet-id"),
				Values: aws.StringSlice([]string{subnetID}),
			},
		},
	}

	subnets, err := a.ec2Client().DescribeSubnets(&filter)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				log.Error(err, aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}

		return nil, err
	}
	log.V(8).Info("Found subnet", "subnetCount", len(subnets.Subnets))
	a.subnets.Set(*subnets.Subnets[0].SubnetId, subnets.Subnets[0], cache.DefaultExpiration)

	return subnets.Subnets[0], nil
}

func (a *AwsCloudProvider) loadSubnetsFromAws() error {
	key, _ := a.ClusterTag()
	var filter = ec2.DescribeSubnetsInput{
		Filters: a.allClusterTagFilter(key),
	}
	log.Info("loading subnets",
		"filter", filter,
	)

	subnets, err := a.ec2Client().DescribeSubnets(&filter)
	if err != nil {
		return err
	}

	for _, subnet := range subnets.Subnets {
		a.subnets.Set(*subnet.SubnetId, subnet, cache.DefaultExpiration)
	}

	return nil
}

func (a *AwsCloudProvider) convertToNetwork(subnet *ec2.Subnet) (*AwsNetwork, error) {
	result, err := CreateNetwork(a, subnet)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// addRandomIPToInterface -- adds an additional IP to the given interface.
//
// AWS will assign a free IP address to the given Interface.
func (a *AwsCloudProvider) addRandomIPToInterface(interfaceID string) (*net.IP, error) {
	addressRequest := ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(interfaceID),
		SecondaryPrivateIpAddressCount: aws.Int64(int64(1)),
	}
	addressResponse, err := a.ec2Client().AssignPrivateIpAddresses(&addressRequest)
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
	_, err := a.ec2Client().AssignPrivateIpAddresses(&addressRequest)
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

	if a.subnets.ItemCount() < 1 || len(a.subnets.Items()) < 1 {
		log.Info("initializing subnets from AWS")
		err := a.loadSubnetsFromAws()
		if err != nil {
			return nil, err
		}
	}

	log.Info("checking subnets from AWS",
		"subnet-count-itemcount", a.subnets.ItemCount(),
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

// AddRandomIPs adds a random IP addresses to any machine of the given machineset. It will add one ip address in every
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

	return a.unassignIPFromNetworkInterface(networkInterface, ip)
}

func (a *AwsCloudProvider) findNetworkInterfaceForIP(ip *net.IP) (*ec2.NetworkInterface, error) {
	input := ec2.DescribeNetworkInterfacesInput{
		Filters: a.createEc2Filter("addresses.private-ip-address", []string{ip.String()}),
	}

	output, err := a.ec2Client().DescribeNetworkInterfaces(&input)
	if err != nil {
		return nil, err
	}

	if len(output.NetworkInterfaces) != 1 {
		return nil, fmt.Errorf("found no network interface for ip '%s'", ip.String())
	}

	if output.NetworkInterfaces[0].Attachment != nil {
		log.Info("found networkinterface",
			"networkinterface-id", output.NetworkInterfaces[0].NetworkInterfaceId,
			"instance-id", output.NetworkInterfaces[0].Attachment.InstanceId,
			"ip", ip,
		)
	} else {
		return nil, fmt.Errorf("found no attached network interface for ip '%s'", ip.String())
	}

	return output.NetworkInterfaces[0], nil
}

func (a *AwsCloudProvider) unassignIPFromNetworkInterface(networkInterface *ec2.NetworkInterface, ip *net.IP) (string, error) {
	if networkInterface.Attachment != nil {
		instanceID := *networkInterface.Attachment.InstanceId

		log.Info("removing network interface from instance",
			"networkinterface-id", networkInterface.NetworkInterfaceId,
			"instance-id", instanceID,
		)
		unassign := ec2.UnassignPrivateIpAddressesInput{
			NetworkInterfaceId: networkInterface.NetworkInterfaceId,
			PrivateIpAddresses: aws.StringSlice([]string{ip.String()}),
		}
		_, err := a.ec2Client().UnassignPrivateIpAddresses(&unassign)
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

// MachineSetForHost returns the machine set the host is controlled by.
func (a *AwsCloudProvider) MachineSetForHost(hostname string) (CloudMachineSet, error) {
	_ = a.initializeProvider()

	instance, err := a.InstanceByHostName(hostname)
	if err != nil {
		return nil, err
	}

	machinesetID := (*instance).MachineSet()
	return a.MachineSet(machinesetID)
}

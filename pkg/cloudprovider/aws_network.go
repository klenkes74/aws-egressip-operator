package cloudprovider

import (
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/patrickmn/go-cache"
	"net"
	"time"
)

var _ CloudNetwork = &AwsNetwork{}

// The AwsNetwork is the AWS implementation of the CloudNetwork
type AwsNetwork struct {
	provider *AwsCloudProvider

	name                       string       // Name of the AWS Subnet
	arn                        string       // ID of the AWS Subnet
	availabilityZone           string       // AWS availability zone this subnet is attached to
	cidr                       *net.IPNet   // CIDR of the subnet
	instancesInSubnet          *cache.Cache // Cache containing all instances with interfaces in this subnet
	availableIPCount           int64        // Number of free IP addresses of this subnet
	defaultForAvailabilityZone bool         // This is the default subnet for the availability zone
}

// CreateNetwork - creates an AWS subnet into an AwsNetwork
func CreateNetwork(provider *AwsCloudProvider, subnet *ec2.Subnet) (*AwsNetwork, error) {
	var err error

	result := &AwsNetwork{
		provider:                   provider,
		name:                       *subnet.SubnetId,
		arn:                        *subnet.SubnetArn,
		availabilityZone:           *subnet.AvailabilityZone,
		availableIPCount:           *subnet.AvailableIpAddressCount,
		defaultForAvailabilityZone: *subnet.DefaultForAz,
	}
	result.cidr, err = result.convertToIPNet(subnet.CidrBlock)
	if err != nil {
		return nil, err
	}

	result.instancesInSubnet = cache.New(time.Minute, 5*time.Minute)

	return result, nil
}

// converts a string into IPNet. Could have been directly inside the method but so it's more expressive.
func (n *AwsNetwork) convertToIPNet(cidr *string) (*net.IPNet, error) {
	_, result, err := net.ParseCIDR(*cidr)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// IsIPInNetwork checks if the given IP is from the network.
func (n *AwsNetwork) IsIPInNetwork(ip *net.IP) bool {
	return n.cidr.Contains(*ip)
}

// Name - the name of the network.
func (n *AwsNetwork) Name() string {
	return n.name
}

// URI - the cloud provider URI for referencing this network
func (n *AwsNetwork) URI() string {
	return n.arn
}

// FailureZone - The failure zone this network is configured into
func (n *AwsNetwork) FailureZone() string {
	return n.availabilityZone
}

// DefaultForFailureZone - This network is the default network for all instances within this failure zone
func (n *AwsNetwork) DefaultForFailureZone() bool {
	return n.defaultForAvailabilityZone
}

// Cidr - the cidr of the network
func (n *AwsNetwork) Cidr() *net.IPNet {
	return n.cidr
}

// AvailableIPCount - the available IPs. ATTENTION: since the data is cached for some minutes, the value may not be
// correct. Use with care.
func (n *AwsNetwork) AvailableIPCount() int64 {
	return n.availableIPCount
}

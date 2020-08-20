package cloudprovider

import (
	"github.com/aws/aws-sdk-go/service/ec2"
	"net"
)

var _ CloudInstance = &AwsInstance{}

// AwsInstance The single AWS instance information
type AwsInstance struct {
	provider *AwsCloudProvider
	instance ec2.Instance

	tags map[string]string

	networkInterface string
}

// New creates a new AWS CloudInstance
func (a AwsInstance) New(provider *AwsCloudProvider, data ec2.Instance) *CloudInstance {
	instance := &AwsInstance{
		provider: provider,
		instance: data,
	}

	result := CloudInstance(instance)
	log.Info("created cloud instance",
		"instance-id", result.ID(),
		"hostname", result.HostName(),
	)
	return &result
}

// ID returns the ID of the cloud instance
func (a AwsInstance) ID() string {
	return *a.instance.InstanceId
}

// URI returns the URI of the cloud instance
func (a AwsInstance) URI() string {
	return *a.instance.OutpostArn
}

// HostName returns the DNS hostname of the cloud instance
func (a AwsInstance) HostName() string {
	return *a.instance.PrivateDnsName
}

// FailureRegion returns the failure region of the cloud instance
func (a AwsInstance) FailureRegion() string {
	result := *a.instance.Placement.AvailabilityZone
	return result[:len(result)-1] // cut of the last character normally transforms the az to region in AWS ...
}

// FailureZone returns the failure zone of the cloud instance
func (a AwsInstance) FailureZone() string {
	return *a.instance.Placement.AvailabilityZone
}

// Tags returns a map containing all Tags of the instance
func (a AwsInstance) Tags() *map[string]string {
	a.initializeTags()

	return &a.tags
}

func (a AwsInstance) initializeTags() {
	if a.tags == nil {
		a.tags = make(map[string]string, len(a.instance.Tags))
		for _, tag := range a.instance.Tags {
			a.tags[*tag.Key] = *tag.Value
		}
	}
}

// NetworkInterface returns the network interface id of the cloud instance
func (a AwsInstance) NetworkInterface() string {
	return a.networkInterface
}

// PrimaryIP returns the primary ip of the cloud instance
func (a AwsInstance) PrimaryIP() *net.IP {
	result := net.ParseIP(*a.instance.PrivateIpAddress)
	return &result
}

// SecondaryIps returns the secondary IPs of the cloud instance
func (a AwsInstance) SecondaryIps() []*net.IP {
	result := make([]*net.IP, 0)
	if len(a.instance.NetworkInterfaces[0].PrivateIpAddresses) > 1 {
		for _, ip := range a.instance.NetworkInterfaces[0].PrivateIpAddresses[1:len(a.instance.NetworkInterfaces[0].PrivateIpAddresses)] {
			netIP := net.ParseIP(*ip.PrivateIpAddress)
			result = append(result, &netIP)
		}
	}
	return result
}

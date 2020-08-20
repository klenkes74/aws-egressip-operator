package cloudprovider

import (
	"net"
	"os"
)

// CreateCloudProvider - Creates a matching cloud provider. The switch is done by reading the environment variable
// CLOUD_PROVIDER with a default to AWS.
func CreateCloudProvider() *CloudProvider {
	provider, _ := os.LookupEnv("CLOUD_PROVIDER") // TODO rlichti 2020-06-03 document this switch

	switch provider {
	case "MOCK":
		panic("No mock provider implemented!")
	case "AWS":
	default:
		result := CloudProvider(AwsProvider())
		return &result
	}

	panic("can't come to here, the default should be returned by switch case!")
}

// The CloudProvider interface hides the different cloud providers. Currently only AWS is implemented though.
type CloudProvider interface {
	ClusterTag() (string, string)

	Instance(instanceID string) (*CloudInstance, error)
	InstanceByHostName(hostname string) (*CloudInstance, error)

	AddSpecifiedIPs(ips []*net.IP) ([]string, error)
	AddRandomIPs() ([]string, []*net.IP, error)
	RemoveIP(ip *net.IP) (string, error)
}

// CloudInstance is a single computing instance in the cloud.
type CloudInstance interface {
	ID() string       // InstanceId of this instance
	URI() string      // Unique Resource Identifier -- e.g. the ARN at AWS
	HostName() string // Hostname of this instance

	FailureRegion() string // Cloud Region of this instance
	FailureZone() string   // Failure zone this instance is located in

	Tags() *map[string]string // The tags of this instance
	NetworkInterface() string // The ids of the network interfaces of this instance

	PrimaryIP() *net.IP      // primary IP of this instance
	SecondaryIps() []*net.IP // secondary IPs of this instance
}

// CloudNetwork is a defined network within the cloud.
type CloudNetwork interface {
	IsIPInNetwork(ip *net.IP) bool

	Name() string
	URI() string // Unique Resource Identifier -- e.g. the ARN at AWS
	FailureZone() string
	DefaultForFailureZone() bool
	Cidr() *net.IPNet
	AvailableIPCount() int64
}

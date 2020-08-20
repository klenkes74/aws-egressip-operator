package cloudprovider

import (
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// AwsClient -- abstracts the direct calls to ec2 SDK since we need to mock them out for enabling testings.
type AwsClient interface {
	AssignPrivateIPAddresses(request *ec2.AssignPrivateIpAddressesInput) (*ec2.AssignPrivateIpAddressesOutput, error)
	UnassignPrivateIPAddresses(request *ec2.UnassignPrivateIpAddressesInput) (*ec2.UnassignPrivateIpAddressesOutput, error)

	DescribeInstances(request *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	DescribeNetworkInterfaces(request *ec2.DescribeNetworkInterfacesInput) (*ec2.DescribeNetworkInterfacesOutput, error)

	DescribeSubnets(request *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error)

	GetRegion() string
}

var _ AwsClient = &AwsClientImpl{}

// AwsClientImpl -- The production implementation of the AwsClient interface.
type AwsClientImpl struct {
	region string

	session   *session.Session
	ec2Client *ec2.EC2
}

// CreateAwsClient -- creates a fully initialized AwsClient for the specified region. A new session is created and used
// for the ec2 and autoscaling client.
func CreateAwsClient(region string) (AwsClient, error) {
	var err error
	result := AwsClientImpl{}

	result.region = region

	result.session = result.createSession()
	result.ec2Client, err = result.createAwsEc2Client()
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetRegion -- returns the region this AwsClient is defined for.
func (a *AwsClientImpl) GetRegion() string {
	return a.region
}

func (a *AwsClientImpl) createSession() *session.Session {
	return session.Must(session.NewSession())
}

func (a *AwsClientImpl) createAwsEc2Client() (*ec2.EC2, error) {
	client := ec2.New(a.session, aws.NewConfig().WithRegion(a.region))
	if client == nil {
		return nil, errors.New("can't create new AWS ec2 client")
	}
	return client, nil
}

// AssignPrivateIPAddresses -- Assigns IP addresses to an instance.
func (a AwsClientImpl) AssignPrivateIPAddresses(request *ec2.AssignPrivateIpAddressesInput) (*ec2.AssignPrivateIpAddressesOutput, error) {
	return a.ec2Client.AssignPrivateIpAddresses(request)
}

// UnassignPrivateIPAddresses -- Removes the IP addresses from an instance.
func (a AwsClientImpl) UnassignPrivateIPAddresses(request *ec2.UnassignPrivateIpAddressesInput) (*ec2.UnassignPrivateIpAddressesOutput, error) {
	return a.ec2Client.UnassignPrivateIpAddresses(request)
}

// DescribeInstances -- Retrieves all information for a specific instance or all instances matching the request.
func (a AwsClientImpl) DescribeInstances(request *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	return a.ec2Client.DescribeInstances(request)
}

// DescribeNetworkInterfaces -- Retrieves all information for the network interface(s) matching the request.
func (a AwsClientImpl) DescribeNetworkInterfaces(request *ec2.DescribeNetworkInterfacesInput) (*ec2.DescribeNetworkInterfacesOutput, error) {
	return a.ec2Client.DescribeNetworkInterfaces(request)
}

// DescribeSubnets -- Describes the subnet(s) matching the request.
func (a AwsClientImpl) DescribeSubnets(request *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
	return a.ec2Client.DescribeSubnets(request)
}

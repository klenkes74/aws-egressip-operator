package main

import (
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRedistributeIPsFromHostOK(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)
	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")
	mockedInstance := mockedInstanceByName("ip-1-1-1-34.my-local.inf")
	mockDescribeNetworkInterfaceByIPMock(mockAws, "1.1.1.11")

	mockAws.On("UnassignPrivateIPAddresses", &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String("vm-1"),
		PrivateIpAddresses: aws.StringSlice([]string{"1.1.1.11"}),
	}).Return(&ec2.UnassignPrivateIpAddressesOutput{}, nil)

	mockAddSpecifiedIPSuccessfully(mockAws, "vm-1", "1.1.1.11")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-2", "1.1.1.11")

	if mockedInstance == nil {
		t.Errorf("Can't find the instance with name '%v'", "ip-1-1-1-34.my-local.inf")
		return
	}

	subnet := defaultHostSubnet("ip-1-1-1-34.my-local.inf", *mockedInstance.PrivateIpAddress, createSecondaryIPs(mockedInstance)...)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)
	result, err := service.RedistributeIPsFromHost(subnet)

	assert.Equal(t, "vm-1", result["1.1.1.11"])
	assert.Nil(t, err)
}

func TestRedistributeIPsFromHostFailUnassign(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)
	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")
	mockedInstance := mockedInstanceByName("ip-1-1-1-34.my-local.inf")
	mockDescribeNetworkInterfaceByIPMock(mockAws, "1.1.1.11")

	mockAws.On("UnassignPrivateIPAddresses", &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String("vm-1"),
		PrivateIpAddresses: aws.StringSlice([]string{"1.1.1.11"}),
	}).Return(nil, errors.New("failing to un assign the ip"))

	if mockedInstance == nil {
		t.Errorf("Can't find the instance with name '%v'", "ip-1-1-1-34.my-local.inf")
		return
	}

	subnet := defaultHostSubnet("ip-1-1-1-34.my-local.inf", *mockedInstance.PrivateIpAddress, createSecondaryIPs(mockedInstance)...)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)
	_, err := service.RedistributeIPsFromHost(subnet)

	assert.NotNil(t, err)
}

func TestRedistributeIPsFromHostFailAssign(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)
	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")
	mockedInstance := mockedInstanceByName("ip-1-1-1-34.my-local.inf")
	mockDescribeNetworkInterfaceByIPMock(mockAws, "1.1.1.11")

	mockAws.On("UnassignPrivateIPAddresses", &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String("vm-1"),
		PrivateIpAddresses: aws.StringSlice([]string{"1.1.1.11"}),
	}).Return(&ec2.UnassignPrivateIpAddressesOutput{}, nil)

	mockAddSpecifiedIPFail(mockAws, "vm-1", "1.1.1.11")
	mockAddSpecifiedIPFail(mockAws, "vm-2", "1.1.1.11")

	if mockedInstance == nil {
		t.Errorf("Can't find the instance with name '%v'", "ip-1-1-1-34.my-local.inf")
		return
	}

	subnet := defaultHostSubnet("ip-1-1-1-34.my-local.inf", *mockedInstance.PrivateIpAddress, createSecondaryIPs(mockedInstance)...)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)
	_, err := service.RedistributeIPsFromHost(subnet)

	assert.NotNil(t, err)
}

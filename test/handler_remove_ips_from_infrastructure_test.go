package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"testing"
)

func TestRemoveIPsFromInfrastructureOK(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)
	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	mockDescribeInstance(mockAws, "vm-1")
	mockDescribeNetworkInterfaceByIPMock(mockAws, "1.1.1.11")
	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")

	mockAws.On("UnassignPrivateIPAddresses", &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String("vm-1"),
		PrivateIpAddresses: aws.StringSlice([]string{"1.1.1.11"}),
	}).Return(&ec2.UnassignPrivateIpAddressesOutput{}, nil)

	mockOcp.On("Update", mock.Anything, mock.Anything).Return(nil)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	err := service.RemoveIPsFromInfrastructure(defaultNetNamespace("1.1.1.11"))
	assert.Nil(t, err)
}

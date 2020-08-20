package main

import (
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestReadIPsFromHostOK(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)
	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")
	mockedInstance := mockedInstanceByName("ip-1-1-1-34.my-local.inf")
	if mockedInstance == nil {
		t.Errorf("Can't find the instance with name '%v'", "ip-1-1-1-34.my-local.inf")
		return
	}

	subnet := defaultHostSubnet("ip-1-1-1-34.my-local.inf", *mockedInstance.PrivateIpAddress, createSecondaryIPs(mockedInstance)...)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)
	result := service.ReadIpsFromHostSubnet(subnet)

	assert.ElementsMatch(t, defaultIPs("1.1.1.11"), result, "The IPs should match (result-len='%v')", result)
}

func TestReadIPsFromHostFail(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)
	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1")
	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")
	mockedInstance := mockedInstanceByName("ip-1-1-1-34.my-local.inf")
	if mockedInstance == nil {
		t.Errorf("Can't find the instance with name '%v'", "ip-1-1-1-34.my-local.inf")
		return
	}

	subnet := defaultHostSubnet("ip-1-1-1-34.my-local.inf", *mockedInstance.PrivateIpAddress, createSecondaryIPs(mockedInstance)...)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	result := service.ReadIpsFromHostSubnet(subnet)

	assert.True(t, len(result) == 0, "There should be no IP")
}

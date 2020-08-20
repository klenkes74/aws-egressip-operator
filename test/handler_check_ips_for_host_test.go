package main

import (
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCheckIPsForHostOk(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	mockDescribeInstance(mockAws, "vm-1")

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	data := defaultHostSubnet("ip-1-1-1-34.my-local.inf", "1.1.1.34")
	ips := defaultIPs()

	err := service.CheckIPsForHost(data, ips)

	assert.Nil(t, err)
}

func TestCheckIPsForHostMissing(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}
	cloud := createAwsCloudProviderMock(mockAws)

	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1")
	mockDescribeInstance(mockAws, "vm-1")

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	data := defaultHostSubnet("ip-1-1-1-34.my-local.inf", "1.1.1.34")
	ips := defaultIPs()

	err := service.CheckIPsForHost(data, ips)

	assert.NotNil(t, err)
}

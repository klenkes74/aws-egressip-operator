package main

import (
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"testing"
)

func TestAddIPsToInfrastructureRandomOK(t *testing.T) {
	successAddIPsToInfrastructure(t, defaultNamespace())
}

func TestAddIPsToInfrastructureRandomFail(t *testing.T) {
	failAddIPsToInfrastructure(t, defaultNamespace())
}

func TestAddIPsToInfrastructureSpecifiedOK(t *testing.T) {
	successAddIPsToInfrastructure(t, defaultNamespace("1.1.1.11", "1.1.2.22", "1.1.3.33"))
}

func TestAddIPsToInfrastructureSpecifiedFail(t *testing.T) {
	failAddIPsToInfrastructure(t, defaultNamespace("1.1.1.11", "1.1.2.22", "1.1.3.33"))
}

func successAddIPsToInfrastructure(t *testing.T, namespace *corev1.Namespace) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)

	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	instances["vm-2"] = createInstance("vm-2", "1.1.1.93", "nice-a", "ip-1-1-1-93.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	instances["vm-3"] = createInstance("vm-3", "1.1.2.52", "nice-a", "ip-1-1-2-52.my-local.inf", "subnet-2", []string{"1.1.2.22"}...)
	instances["vm-4"] = createInstance("vm-4", "1.1.2.75", "nice-a", "ip-1-1-2-75.my-local.inf", "subnet-2", []string{"1.1.2.22"}...)
	instances["vm-5"] = createInstance("vm-5", "1.1.3.21", "nice-a", "ip-1-1-3-21.my-local.inf", "subnet-3", []string{"1.1.3.33"}...)
	instances["vm-6"] = createInstance("vm-6", "1.1.3.123", "nice-a", "ip-1-1-3-123.my-local.inf", "subnet-3", []string{"1.1.3.33"}...)
	mockDescribeInstance(mockAws, "vm-1")
	mockDescribeInstance(mockAws, "vm-2")
	mockDescribeInstance(mockAws, "vm-3")
	mockDescribeInstance(mockAws, "vm-4")
	mockDescribeInstance(mockAws, "vm-5")
	mockDescribeInstance(mockAws, "vm-6")

	mockAddRandomIPSuccessfully(mockAws, "vm-1", "1.1.1.11")
	mockAddRandomIPSuccessfully(mockAws, "vm-2", "1.1.1.11")
	mockAddRandomIPSuccessfully(mockAws, "vm-3", "1.1.2.22")
	mockAddRandomIPSuccessfully(mockAws, "vm-4", "1.1.2.22")
	mockAddRandomIPSuccessfully(mockAws, "vm-5", "1.1.3.33")
	mockAddRandomIPSuccessfully(mockAws, "vm-6", "1.1.3.33")

	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-1-93.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-2-52.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-2-75.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-3-21.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-3-123.my-local.inf")

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	result, err := service.AddIPsToInfrastructure(namespace)

	assert.ElementsMatch(t, defaultIPs("1.1.1.11", "1.1.2.22", "1.1.3.33"), result, "The IPs should match!")

	assert.Nil(t, err)
}

func failAddIPsToInfrastructure(t *testing.T, namespace *corev1.Namespace) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	instances = make(map[string]*ec2.Instance)

	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	instances["vm-2"] = createInstance("vm-2", "1.1.1.93", "nice-a", "ip-1-1-1-93.my-local.inf", "subnet-1", []string{"1.1.1.11"}...)
	instances["vm-3"] = createInstance("vm-3", "1.1.2.52", "nice-a", "ip-1-1-2-52.my-local.inf", "subnet-2", []string{"1.1.2.22"}...)
	instances["vm-4"] = createInstance("vm-4", "1.1.2.75", "nice-a", "ip-1-1-2-75.my-local.inf", "subnet-2", []string{"1.1.2.22"}...)
	instances["vm-5"] = createInstance("vm-5", "1.1.3.21", "nice-a", "ip-1-1-3-21.my-local.inf", "subnet-3", []string{"1.1.3.33"}...)
	instances["vm-6"] = createInstance("vm-6", "1.1.3.123", "nice-a", "ip-1-1-3-123.my-local.inf", "subnet-3", []string{"1.1.3.33"}...)
	mockDescribeInstance(mockAws, "vm-1")
	mockDescribeInstance(mockAws, "vm-2")
	mockDescribeInstance(mockAws, "vm-3")
	mockDescribeInstance(mockAws, "vm-4")
	mockDescribeInstance(mockAws, "vm-5")
	mockDescribeInstance(mockAws, "vm-6")

	mockAddRandomIPFail(mockAws, "vm-1")
	mockAddRandomIPFail(mockAws, "vm-2")
	mockAddRandomIPFail(mockAws, "vm-3")
	mockAddRandomIPFail(mockAws, "vm-4")
	mockAddRandomIPFail(mockAws, "vm-5")
	mockAddRandomIPFail(mockAws, "vm-6")

	mockHostSubnet(t, mockOcp, "ip-1-1-1-34.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-1-93.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-2-52.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-2-75.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-3-21.my-local.inf")
	mockHostSubnet(t, mockOcp, "ip-1-1-3-123.my-local.inf")

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	_, err := service.AddIPsToInfrastructure(namespace)

	assert.NotNil(t, err)
}

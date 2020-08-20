package main

import (
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAddIpsToNetnamespace(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	err := service.AddIPsToNetNamespace(defaultNetNamespace(), defaultIPs("1.1.1.11", "1.1.2.22", "1.1.3.33"))
	assert.Nil(t, err)
}

func TestAddIpsToNetnamespaceAlreadyThere(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	err := service.AddIPsToNetNamespace(defaultNetNamespace("1.1.1.11", "1.1.2.22", "1.1.3.33"), defaultIPs("1.1.1.11", "1.1.2.22", "1.1.3.33"))

	assert.Nil(t, err)
}

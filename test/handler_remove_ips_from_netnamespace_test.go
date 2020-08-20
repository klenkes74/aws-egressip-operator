package main

import (
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"testing"
)

func TestRemoveIPsFromNetnamespace(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	service.RemoveIPsFromNetNamespace(defaultNetNamespace("1.1.1.11", "1.1.2.22", "1.1.3.33"))
}

func TestRemoveIPsFromNetnamespaceNotHavingTheseIPs(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	mockOcp := &mocks.OcpClient{}

	cloud := createAwsCloudProviderMock(mockAws)

	service := *openshift.NewEgressIPHandler(cloud, mockOcp)

	service.RemoveIPsFromNetNamespace(defaultNetNamespace())
}

package main

import (
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
)

func TestSuccessfulAddRandomIPs(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockAddRandomIPSuccessfully(mockAws, "vm-1", "1.1.1.11")
	mockAddRandomIPSuccessfully(mockAws, "vm-2", "1.1.1.11")
	mockAddRandomIPSuccessfully(mockAws, "vm-3", "1.1.2.22")
	mockAddRandomIPSuccessfully(mockAws, "vm-4", "1.1.2.22")
	mockAddRandomIPSuccessfully(mockAws, "vm-5", "1.1.3.33")
	mockAddRandomIPSuccessfully(mockAws, "vm-6", "1.1.3.33")

	instances, ips, err := service.AddRandomIPs()

	assert.Len(t, instances, 3)
	assert.Len(t, ips, 3)
	assert.Nil(t, err)

	mockAws.AssertExpectations(t)
}

func TestFailedAddRandomIPs(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockAddRandomIPSuccessfully(mockAws, "vm-1", "1.1.1.11")
	mockAddRandomIPSuccessfully(mockAws, "vm-2", "1.1.1.11")
	mockAddRandomIPFail(mockAws, "vm-3")
	mockAddRandomIPFail(mockAws, "vm-4")
	mockAddRandomIPSuccessfully(mockAws, "vm-5", "1.1.3.33")
	mockAddRandomIPSuccessfully(mockAws, "vm-6", "1.1.3.33")

	instances, ips, err := service.AddRandomIPs()

	assert.Len(t, instances, 3)
	assert.Len(t, ips, 3)

	ip1 := net.ParseIP("1.1.1.11")
	ip2 := net.ParseIP("1.1.3.33")
	assert.ElementsMatch(t, ips, []*net.IP{&ip1, &ip2, nil})
	assert.NotNil(t, err)

	mockAws.AssertExpectations(t)
}

package main

import (
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
)

func TestSuccessfulAddSpecifiedIPs(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockAddSpecifiedIPSuccessfully(mockAws, "vm-1", "1.1.1.11")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-2", "1.1.1.11")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-3", "1.1.2.22")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-4", "1.1.2.22")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-5", "1.1.3.33")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-6", "1.1.3.33")

	ips := make([]*net.IP, 0)
	for _, ip := range []string{"1.1.1.11", "1.1.2.22", "1.1.3.33"} {
		parsed := net.ParseIP(ip)
		ips = append(ips, &parsed)
	}

	instances, err := service.AddSpecifiedIPs(ips)

	assert.Len(t, instances, 3)
	assert.Nil(t, err)

	mockAws.AssertExpectations(t)
}

func TestFailedAddSpecifiedIPs(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockAddSpecifiedIPSuccessfully(mockAws, "vm-1", "1.1.1.11")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-2", "1.1.1.11")
	mockAddSpecifiedIPFail(mockAws, "vm-3", "1.1.2.22")
	mockAddSpecifiedIPFail(mockAws, "vm-4", "1.1.2.22")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-5", "1.1.3.33")
	mockAddSpecifiedIPSuccessfully(mockAws, "vm-6", "1.1.3.33")

	ips := make([]*net.IP, 0)
	for _, ip := range []string{"1.1.1.11", "1.1.2.22", "1.1.3.33"} {
		parsed := net.ParseIP(ip)
		ips = append(ips, &parsed)
	}

	instances, err := service.AddSpecifiedIPs(ips)

	assert.Len(t, instances, 3)
	assert.NotNil(t, err)

	mockAws.AssertExpectations(t)
}

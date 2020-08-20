package main

import (
	"errors"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSuccessfulInstance(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockDescribeInstance(mockAws, "vm-1")

	instance, err := service.Instance("vm-1")

	assert.Equal(t, "vm-1", (*instance).ID())

	assert.Nil(t, err)

	mockAws.AssertExpectations(t)
}

func TestFailedInstance(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockAws.On("DescribeInstances", &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			createFilter("instance-id", []string{"err-1"}),
			createFilter("instance-state-name", []string{"running"}),
		},
	}).Return(nil, errors.New("AWS could not find instance"))

	_, err := service.Instance("err-1")

	assert.NotNil(t, err)

	mockAws.AssertExpectations(t)
}

func TestSuccessfulInstanceByHostname(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockDescribeInstance(mockAws, "vm-1")

	_, err := service.InstanceByHostName("ip-1-1-1-34.my-local.inf")

	assert.Nil(t, err)

	mockAws.AssertExpectations(t)
}

func TestFailedInstanceByHostname(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	mockAws.On("DescribeInstances", &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			createFilter("private-dns-name", []string{"ip-1-1-1-31.my-local.inf"}),
		},
	}).Return(nil, errors.New("AWS could not find instance"))

	_, err := service.InstanceByHostName("ip-1-1-1-31.my-local.inf")

	assert.NotNil(t, err)

	mockAws.AssertExpectations(t)
}

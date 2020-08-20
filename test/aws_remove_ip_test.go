package main

import (
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
)

func TestSuccessfulRemoveIP(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)
	ip := net.ParseIP("1.1.1.11")

	mockAws.On("DescribeNetworkInterfaces", &ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("addresses.private-ip-address"),
				Values: aws.StringSlice([]string{ip.String()}),
			},
		},
	}).Return(&ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{
			{
				Attachment: &ec2.NetworkInterfaceAttachment{
					InstanceId: aws.String("instance"),
				},
				NetworkInterfaceId: aws.String("vm-1"),
			},
		},
	}, nil)

	mockAws.On("UnassignPrivateIPAddresses", &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String("vm-1"),
		PrivateIpAddresses: aws.StringSlice([]string{ip.String()}),
	}).Return(&ec2.UnassignPrivateIpAddressesOutput{}, nil)

	_, err := service.RemoveIP(&ip)

	assert.Nil(t, err)

	mockAws.AssertExpectations(t)
}

func TestFailedRemoveIP(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)
	ip := net.ParseIP("1.1.1.11")

	mockAws.On("DescribeNetworkInterfaces", &ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("addresses.private-ip-address"),
				Values: aws.StringSlice([]string{ip.String()}),
			},
		},
	}).Return(&ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{
			{
				Attachment: &ec2.NetworkInterfaceAttachment{
					InstanceId: aws.String("instance"),
				},
				NetworkInterfaceId: aws.String("vm-1"),
			},
		},
	}, nil)

	mockAws.On("UnassignPrivateIPAddresses", &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String("vm-1"),
		PrivateIpAddresses: aws.StringSlice([]string{ip.String()}),
	}).Return(nil, errors.New("AWS could not remove the IP"))

	_, err := service.RemoveIP(&ip)

	assert.NotNil(t, err)

	mockAws.AssertExpectations(t)
}

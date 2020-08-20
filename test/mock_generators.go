package main

import (
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/klenkes74/aws-egressip-operator/pkg/cloudprovider"
	"github.com/klenkes74/aws-egressip-operator/pkg/logger"
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	netv1 "github.com/openshift/api/network/v1"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	apiv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"net"
	"strings"
	"testing"
	"time"
)

var region = "nice"
var clusterName = "nicer"

//goland:noinspection GoUnusedGlobalVariable
var log = logger.Log.WithName("test-runner")

var subnets = []*ec2.Subnet{
	createSubnet("subnet-1", "1.1.1.0/24", int64(50), "nice-a", "niceA", false),
	createSubnet("subnet-2", "1.1.2.0/24", int64(50), "nice-b", "niceB", false),
	createSubnet("subnet-3", "1.1.3.0/24", int64(50), "nice-c", "niceC", false),
}

var instances = make(map[string]*ec2.Instance, 6)

var networkInterfaces = make(map[string]*ec2.NetworkInterface)

func init() {
	instances["vm-1"] = createInstance("vm-1", "1.1.1.34", "nice-a", "ip-1-1-1-34.my-local.inf", "subnet-1")
	instances["vm-2"] = createInstance("vm-2", "1.1.1.93", "nice-a", "ip-1-1-1-93.my-local.inf", "subnet-1")
	instances["vm-3"] = createInstance("vm-3", "1.1.2.75", "nice-b", "ip-1-1-2-75.my-local.inf", "subnet-2")
	instances["vm-4"] = createInstance("vm-4", "1.1.2.52", "nice-b", "ip-1-1-2-52.my-local.inf", "subnet-2")
	instances["vm-5"] = createInstance("vm-5", "1.1.3.21", "nice-c", "ip-1-1-3-21.my-local.inf", "subnet-3")
	instances["vm-6"] = createInstance("vm-6", "1.1.3.123", "nice-c", "ip-1-1-3-123.my-local.inf", "subnet-3")
}

func createSubnet(subnetID string, cidr string, availableIPs int64, failureZone string, failureZoneID string, defaultForFailureZone bool) *ec2.Subnet {
	return &ec2.Subnet{
		AvailabilityZone:        &failureZone,
		AvailabilityZoneId:      &failureZoneID,
		AvailableIpAddressCount: &availableIPs,
		CidrBlock:               &cidr,
		DefaultForAz:            &defaultForFailureZone,
		OutpostArn:              &subnetID,
		SubnetArn:               &subnetID,
		SubnetId:                &subnetID,
	}
}

func mockedInstanceByName(name string) *ec2.Instance {
	for _, instance := range instances {
		if *instance.PrivateDnsName == name {
			return instance
		}
	}

	return nil
}

func createInstance(instanceID string, ip string, failureZone string, hostName string, subnetID string, ips ...string) *ec2.Instance {
	tagKey := "ClusterNode"
	tagValue := "WorkerNode"

	return &ec2.Instance{
		InstanceId:        &instanceID,
		NetworkInterfaces: []*ec2.InstanceNetworkInterface{createInstanceNetworkInterface(instanceID, ip, failureZone, hostName, subnetID, ips...)},
		OutpostArn:        &instanceID,
		PrivateDnsName:    &hostName,
		PrivateIpAddress:  &ip,
		SubnetId:          &subnetID,
		Tags: []*ec2.Tag{
			{
				Key:   &tagKey,
				Value: &tagValue,
			},
		},
	}
}

func createInstanceNetworkInterface(instanceID string, ip string, failureZone string, hostName string, subnetID string, ips ...string) *ec2.InstanceNetworkInterface {
	privateIPs := make([]*ec2.InstancePrivateIpAddress, len(ips)+1)
	networkPrivateIPs := make([]*ec2.NetworkInterfacePrivateIpAddress, len(ips)+1)

	privateIPs[0] = &ec2.InstancePrivateIpAddress{
		Primary:          &[]bool{true}[0],
		PrivateDnsName:   &hostName,
		PrivateIpAddress: &ip,
	}
	networkPrivateIPs[0] = &ec2.NetworkInterfacePrivateIpAddress{
		Primary:          &[]bool{true}[0],
		PrivateDnsName:   &hostName,
		PrivateIpAddress: &ip,
	}

	if len(ips) > 0 {
		for i, sip := range ips {
			privateIPs[i+1] = &ec2.InstancePrivateIpAddress{
				Primary:          &[]bool{false}[0],
				PrivateIpAddress: &sip,
			}
			networkPrivateIPs[i+1] = &ec2.NetworkInterfacePrivateIpAddress{
				Primary:          &[]bool{false}[0],
				PrivateIpAddress: &sip,
			}
		}
	}

	result := ec2.InstanceNetworkInterface{
		NetworkInterfaceId: &instanceID,
		PrivateDnsName:     &hostName,
		PrivateIpAddress:   &ip,
		PrivateIpAddresses: privateIPs,
		SubnetId:           &subnetID,
		Attachment: &ec2.InstanceNetworkInterfaceAttachment{
			AttachmentId: &instanceID,
		},
	}

	networkInterface := &ec2.NetworkInterface{
		Attachment: &ec2.NetworkInterfaceAttachment{
			AttachmentId: &instanceID,
			InstanceId:   &instanceID,
		},
		AvailabilityZone:   &failureZone,
		NetworkInterfaceId: &instanceID,
		OutpostArn:         &instanceID,
		PrivateDnsName:     &hostName,
		PrivateIpAddress:   &ip,
		PrivateIpAddresses: networkPrivateIPs,
		SubnetId:           &subnetID,
	}

	networkInterfaces[ip] = networkInterface
	for _, nIP := range ips {
		networkInterfaces[nIP] = networkInterface
	}

	return &result
}

func mockDescribeNetworkInterfaceByIPMock(mockAws *mocks.AwsClient, ips ...string) {
	resultInterfaces := make([]*ec2.NetworkInterface, 0)

	for _, ip := range ips {
		if networkInterfaces[ip] != nil {
			resultInterfaces = append(resultInterfaces, networkInterfaces[ip])
		}
	}

	mockAws.On("DescribeNetworkInterfaces", &ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{
			createFilter("addresses.private-ip-address", ips),
		},
	}).Return(
		&ec2.DescribeNetworkInterfacesOutput{
			NetworkInterfaces: resultInterfaces,
		}, nil).Maybe()
}

func createReservations() []*ec2.Reservation {
	result := make([]*ec2.Reservation, 0)

	for _, instance := range instances {
		result = append(result, &ec2.Reservation{Instances: []*ec2.Instance{instance}})
	}

	return result
}

func createReservation(instanceID string) []*ec2.Reservation {
	result := make([]*ec2.Reservation, 0)

	result = append(result, &ec2.Reservation{Instances: []*ec2.Instance{instances[instanceID]}})

	return result
}

func createAwsCloudProviderMock(mockAws *mocks.AwsClient) cloudprovider.CloudProvider {
	service := &cloudprovider.AwsCloudProvider{
		Aws:         mockAws,
		Region:      region,
		ClusterName: clusterName,
	}

	mockDefaultInstanceAwsCalls(mockAws)
	mockDefaultSubnetAwsCalls(mockAws)

	result := cloudprovider.CloudProvider(service)
	return result
}

func mockDefaultInstanceAwsCalls(mockAws *mocks.AwsClient) {
	mockAws.On("DescribeInstances", &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			createFilter("tag-key", []string{"kubernetes.io/cluster/nicer"}),
			createFilter("instance-state-name", []string{"running"}),
		},
	}).Return(&ec2.DescribeInstancesOutput{
		Reservations: createReservations(),
	}, nil).Maybe()

	//goland:noinspection SpellCheckingInspection
	mockAws.On("DescribeInstances", &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			createFilter("tag-key", []string{"kubernetes.io/cluster/nicer"}),
			createFilter("tag:k8s.io/cluster-autoscaler/enabled", []string{"true"}),
			createFilter("tag:ClusterNode", []string{"WorkerNode"}),
			createFilter("instance-state-name", []string{"running"}),
		},
	}).Return(&ec2.DescribeInstancesOutput{
		Reservations: createReservations(),
	}, nil).Maybe()
}

func mockDescribeInstance(mockAws *mocks.AwsClient, instanceID string) {
	mockAws.On("DescribeInstances", &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			createFilter("instance-id", []string{instanceID}),
			createFilter("instance-state-name", []string{"running"}),
		},
	}).Return(&ec2.DescribeInstancesOutput{
		Reservations: createReservation(instanceID),
	}, nil).Maybe()

	mockAws.On("DescribeInstances", &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			createFilter("private-dns-name", []string{*instances[instanceID].PrivateDnsName}),
		},
	}).Return(&ec2.DescribeInstancesOutput{
		Reservations: createReservation(instanceID),
	}, nil).Maybe()
}

func mockDefaultSubnetAwsCalls(mockAws *mocks.AwsClient) {
	mockAws.On("DescribeSubnets", &ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			createFilter("tag-key", []string{"kubernetes.io/cluster/nicer"}),
		},
	}).Return(&ec2.DescribeSubnetsOutput{
		Subnets: subnets,
	}, nil).Maybe()
}

func createFilter(key string, value []string) *ec2.Filter {
	values := make([]*string, 0)

	for _, s := range value {
		values = append(values, &s)
	}

	return &ec2.Filter{
		Name:   &key,
		Values: values,
	}
}

func defaultHostSubnet(dns string, ip string, ips ...string) *netv1.HostSubnet {
	annotations := make(map[string]string)

	if len(ips) >= 1 {
		for _, ip := range ips {
			annotations["egressip-ipam-operator.redhat-cop.io/"+ip] = "default-test"
		}
	}

	return &netv1.HostSubnet{
		TypeMeta: apiv1.TypeMeta{
			Kind:       "HostSubnet",
			APIVersion: "network.openshift.io/v1",
		},
		ObjectMeta: apiv1.ObjectMeta{
			Name:        dns,
			Annotations: annotations,
			Finalizers: []string{
				"egressip-ipam-operator.redhat-cop.io/hostsubnet-handler",
			},
		},
		Host:      dns,
		HostIP:    ip,
		EgressIPs: ips,
	}
}

func defaultIPs(ips ...string) []*net.IP {
	result := make([]*net.IP, 0)

	if len(ips) == 0 {
		ips = []string{"1.1.1.11"}
	}

	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		result = append(result, &parsed)
	}

	return result
}

func mockHostSubnet(t *testing.T, mockOcp *mocks.OcpClient, hostName string) *mock.Call {
	return mockOcp.On("Get", mock.Anything, types.NamespacedName{Namespace: "",
		Name: hostName}, mock.AnythingOfType("*v1.HostSubnet"),
	).Run(func(args mock.Arguments) {
		name := args.Get(1).(types.NamespacedName)
		arg := args.Get(2).(*netv1.HostSubnet)

		mockedInstance := mockedInstanceByName(name.Name)
		if mockedInstance == nil {
			t.Errorf("Can't find the instance with name '%v'", name.Name)
			return
		}

		created := defaultHostSubnet(name.Name, *mockedInstance.PrivateIpAddress, createSecondaryIPs(mockedInstance)...)

		arg.APIVersion = created.APIVersion
		arg.Kind = created.Kind
		arg.Name = created.Name
		arg.Annotations = created.Annotations
		arg.Labels = created.Labels
		arg.Subnet = created.Subnet
		arg.Host = created.Host
		arg.HostIP = created.HostIP
		arg.EgressIPs = created.EgressIPs
	}).Return(nil).Maybe()
}

func createSecondaryIPs(instance *ec2.Instance) []string {
	result := make([]string, 0)

	if len(instance.NetworkInterfaces[0].PrivateIpAddresses) > 1 {
		for _, ip := range instance.NetworkInterfaces[0].PrivateIpAddresses[1:len(instance.NetworkInterfaces[0].PrivateIpAddresses)] {
			if *ip.PrivateIpAddress != "" {
				result = append(result, *ip.PrivateIpAddress)
			}
		}
	}
	return result
}

func defaultNamespace(ips ...string) *corev1.Namespace {
	var annotations map[string]string

	if len(ips) >= 1 {
		annotations = map[string]string{
			"egressip-ipam-operator.redhat-cop.io/egressipam": "aws",
			"egressip-ipam-operator.redhat-cop.io/egressips:": strings.Join(ips, ","),
		}
	} else {
		annotations = map[string]string{
			"egressip-ipam-operator.redhat-cop.io/egressipam": "aws",
		}
	}

	result := &corev1.Namespace{
		TypeMeta: apiv1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: apiv1.ObjectMeta{
			Name: "default-namespace",
			Labels: map[string]string{
				"test": "label-test",
			},
			Annotations: annotations,
			Finalizers: []string{
				"egressip-ipam-operator.redhat-cop.io/namespace-handler",
			},
			CreationTimestamp: apiv1.Time{
				Time: time.Time{},
			},
		},
		Status: corev1.NamespaceStatus{
			Phase: "Active",
		},
	}

	return result
}

func mockAddRandomIPSuccessfully(mockAws *mocks.AwsClient, networkInterfaceID string, ip string) *mock.Call {
	return mockAws.On("AssignPrivateIPAddresses", &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(networkInterfaceID),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}).Return(&ec2.AssignPrivateIpAddressesOutput{
		AssignedPrivateIpAddresses: []*ec2.AssignedPrivateIpAddress{
			{PrivateIpAddress: aws.String(ip)},
		},
	}, nil).Maybe()
}

func mockAddRandomIPFail(mockAws *mocks.AwsClient, networkInterfaceID string) *mock.Call {
	return mockAws.On("AssignPrivateIPAddresses", &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(networkInterfaceID),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}).Return(nil, errors.New("assigning IP for network interface '"+networkInterfaceID+"' failed")).Maybe()
}

func mockAddSpecifiedIPSuccessfully(mockAws *mocks.AwsClient, networkInterfaceID string, ip string) *mock.Call {
	return mockAws.On("AssignPrivateIPAddresses", &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(networkInterfaceID),
		PrivateIpAddresses: aws.StringSlice([]string{ip}),
	}).Return(&ec2.AssignPrivateIpAddressesOutput{
		AssignedPrivateIpAddresses: []*ec2.AssignedPrivateIpAddress{
			{PrivateIpAddress: aws.String(ip)},
		},
	}, nil).Maybe()
}

func mockAddSpecifiedIPFail(mockAws *mocks.AwsClient, networkInterfaceID string, ip string) *mock.Call {
	return mockAws.On("AssignPrivateIPAddresses", &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(networkInterfaceID),
		PrivateIpAddresses: aws.StringSlice([]string{ip}),
	}).Return(nil, errors.New("assigning IP for network interface '"+networkInterfaceID+"' failed")).Maybe()
}

func defaultNetNamespace(ips ...string) *netv1.NetNamespace {
	var annotations map[string]string

	if len(ips) >= 1 {
		annotations = map[string]string{
			"egressip-ipam-operator.redhat-cop.io/egressipam": "aws",
			"egressip-ipam-operator.redhat-cop.io/egressips:": strings.Join(ips, ","),
		}
	} else {
		annotations = map[string]string{
			"egressip-ipam-operator.redhat-cop.io/egressipam": "aws",
		}
	}

	result := &netv1.NetNamespace{
		TypeMeta: apiv1.TypeMeta{
			Kind:       "NetNamespace",
			APIVersion: "v1",
		},
		ObjectMeta: apiv1.ObjectMeta{
			Name: "default-namespace",
			Labels: map[string]string{
				"test": "label-test",
			},
			Annotations: annotations,
			Finalizers: []string{
				"egressip-ipam-operator.redhat-cop.io/netnamespace-handler",
			},
			CreationTimestamp: apiv1.Time{
				Time: time.Time{},
			},
		},
		EgressIPs: ips,
	}

	return result
}

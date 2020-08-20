package main

import (
	"github.com/klenkes74/aws-egressip-operator/test/mocks"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestClusterTag(t *testing.T) {
	mockAws := &mocks.AwsClient{}
	service := createAwsCloudProviderMock(mockAws)

	tag, name := service.ClusterTag()

	assert.Equal(t, "kubernetes.io/cluster/nicer", tag)
	assert.Equal(t, "owned", name)

	mockAws.AssertExpectations(t)
}

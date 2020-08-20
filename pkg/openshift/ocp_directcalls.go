package openshift

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewOcpClient -- returns a client with the given OCP client.Client (may be mocked)
func NewOcpClient(ocp client.Client) *OcpClient {
	ocpClient := &OcpClientImpl{
		client: ocp,
	}

	result := OcpClient(ocpClient)
	return &result
}

// OcpClient -- Abstraction needed to mock out the infrastructure calls.
type OcpClient interface {
	Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error
	Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error
}

// ensure the type of OcpClientImpl
var _ OcpClient = &OcpClientImpl{}

// OcpClientImpl -- the implementation to access an OCP cluster.
type OcpClientImpl struct {
	client client.Client
}

// Get -- retrieve an OCP object.
func (o OcpClientImpl) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	return o.client.Get(ctx, key, obj)
}

// Update -- update an OCP opbject.
func (o OcpClientImpl) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	return o.client.Update(ctx, obj, opts...)
}

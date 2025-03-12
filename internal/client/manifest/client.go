package manifest

import (
	"context"
	"fmt"

	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Client struct {
	client client.Client
}

func NewManifestClient(client client.Client) *Client {
	return &Client{
		client: client,
	}
}

func (c *Client) Get(ctx context.Context, key client.ObjectKey) (v1beta2.Manifest, error) {
	var obj client.Object

	module := &unstructured.Unstructured{}
	module.SetGroupVersionKind(m.Manifest.GroupVersionKind())
	module.SetName(key.Name)
	module.SetNamespace(key.Namespace)

	err := c.client.Get(ctx, key, module)
	if err != nil {
		return nil, fmt.Errorf("failed to get module by name-namespace: %w", err)
	}

	return Manifest{}, nil

}

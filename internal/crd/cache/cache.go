package cache

import (
	"sync"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/kyma-project/lifecycle-manager/internal/crd"
)

type CrdCache struct {
	cache sync.Map
}

func NewCrdCache() *CrdCache {
	return &CrdCache{
		cache: sync.Map{},
	}
}

func (c *CrdCache) Get(key crd.Name) (apiextensionsv1.CustomResourceDefinition, bool) {
	value, ok := c.cache.Load(key)
	if !ok {
		return apiextensionsv1.CustomResourceDefinition{}, false
	}
	cachedCrd, ok := value.(apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return apiextensionsv1.CustomResourceDefinition{}, false
	}

	return cachedCrd, true
}

func (c *CrdCache) Add(key crd.Name, value apiextensionsv1.CustomResourceDefinition) {
	c.cache.Store(key, value)
}

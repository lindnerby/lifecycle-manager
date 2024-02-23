package remote

import (
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewClientCache() *ClientCache {
	return &ClientCache{internal: &sync.Map{}}
}

type ClientCache struct {
	internal *sync.Map
}

func (cache *ClientCache) Get(key client.ObjectKey) *ConfigAndClient {
	value, ok := cache.internal.Load(key)
	if !ok {
		return nil
	}
	clnt, ok := value.(*ConfigAndClient)
	if !ok {
		return nil
	}

	return clnt
}

func (cache *ClientCache) Set(key client.ObjectKey, value *ConfigAndClient) {
	cache.internal.Store(key, value)
}

func (cache *ClientCache) Del(key client.ObjectKey) {
	cache.internal.Delete(key)
}

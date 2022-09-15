/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// UIDDataKey is the key used in config maps to store the UID.
	UIDDataKey = "uid"
	// ProviderDataKey is the key used in config maps to store the Provider
	// UID which we use to ensure unique firewalls.
	ProviderDataKey = "provider-uid"
)

// ConfigMapVault stores cluster UIDs in config maps.
// It's a layer on top of configMapStore that just implements the utils.uidVault
// interface.
type ConfigMapVault struct {
	storeLock      sync.Mutex
	configMapStore cache.Store
	namespace      string
	name           string
}

// Get retrieves the value associated to the provided 'key' from the cluster config map.
// If this method returns an error, it's guaranteed to be apiserver flake.
// If the error is a not found error it sets the boolean to false and
// returns and error of nil instead.
func (c *ConfigMapVault) Get(key string) (string, bool, error) {
	keyStore := fmt.Sprintf("%v/%v", c.namespace, c.name)
	item, found, err := c.configMapStore.GetByKey(keyStore)
	if err != nil || !found {
		return "", false, err
	}
	data := item.(*api_v1.ConfigMap).Data
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	if k, ok := data[key]; ok {
		return k, true, nil
	}
	klog.Infof("Found config map %v but it doesn't contain key %v: %+v", keyStore, key, data)
	return "", false, nil
}

// Put inserts a key/value pair in the cluster config map.
// If the key already exists, the value provided is stored.
func (c *ConfigMapVault) Put(key, val string, createOnly bool) error {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	apiObj := &api_v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.name,
			Namespace: c.namespace,
		},
	}
	cfgMapKey := fmt.Sprintf("%v/%v", c.namespace, c.name)

	item, exists, err := c.configMapStore.GetByKey(cfgMapKey)
	if err == nil && exists {
		data := item.(*api_v1.ConfigMap).Data
		if createOnly {
			return fmt.Errorf("failed to create configmap %v, it is already existed with data %v.", cfgMapKey, data)
		}
		existingVal, ok := data[key]
		if ok && existingVal == val {
			// duplicate, no need to update.
			return nil
		}
		data[key] = val
		apiObj.Data = data
		if existingVal != val {
			klog.Infof("Configmap %v has key %v but wrong value %v, updating to %v", cfgMapKey, key, existingVal, val)
		} else {
			klog.Infof("Configmap %v will be updated with %v = %v", cfgMapKey, key, val)
		}
		if err := c.configMapStore.Update(apiObj); err != nil {
			return fmt.Errorf("failed to update %v: %v", cfgMapKey, err)
		}
	} else {
		apiObj.Data = map[string]string{key: val}
		if err := c.configMapStore.Add(apiObj); err != nil {
			return fmt.Errorf("failed to add %v: %v", cfgMapKey, err)
		}
	}
	klog.Infof("Successfully stored key %v = %v in config map %v", key, val, cfgMapKey)
	return nil
}

// Delete deletes the configMapStore.
func (c *ConfigMapVault) Delete() error {
	cfgMapKey := fmt.Sprintf("%v/%v", c.namespace, c.name)
	item, _, err := c.configMapStore.GetByKey(cfgMapKey)
	if err == nil {
		return c.configMapStore.Delete(item)
	}
	klog.Warningf("Couldn't find item %v in vault, unable to delete", cfgMapKey)
	return nil
}

// NewConfigMapVault creates a config map client.
// This client is essentially meant to abstract out the details of
// configmaps and the API, and just store/retrieve a single value, the cluster uid.
func NewConfigMapVault(c kubernetes.Interface, uidNs, uidConfigMapName string) *ConfigMapVault {
	return &ConfigMapVault{
		configMapStore: newConfigMapStore(c),
		namespace:      uidNs,
		name:           uidConfigMapName}
}

// NewFakeConfigMapVault is an implementation of the configMapStore that doesn't
// persist configmaps. Only used in testing.
func NewFakeConfigMapVault(ns, name string) *ConfigMapVault {
	return &ConfigMapVault{
		configMapStore: cache.NewStore(cache.MetaNamespaceKeyFunc),
		namespace:      ns,
		name:           name}
}

// configMapStore wraps the store interface. Implementations usually persist
// contents of the store transparently.
type configMapStore interface {
	cache.Store
}

// APIServerconfigMapStore only services Add and GetByKey from apiserver.
// TODO: Implement all the other store methods and make this a write
// through cache.
type APIServerconfigMapStore struct {
	configMapStore
	client kubernetes.Interface
}

// Add adds the given config map to the apiserver's store.
func (a *APIServerconfigMapStore) Add(obj interface{}) error {
	cfg := obj.(*api_v1.ConfigMap)
	_, err := a.client.CoreV1().ConfigMaps(cfg.Namespace).Create(context.TODO(), cfg, metav1.CreateOptions{})
	return err
}

// Update updates the existing config map object.
func (a *APIServerconfigMapStore) Update(obj interface{}) error {
	cfg := obj.(*api_v1.ConfigMap)
	_, err := a.client.CoreV1().ConfigMaps(cfg.Namespace).Update(context.TODO(), cfg, metav1.UpdateOptions{})
	return err
}

// Delete deletes the existing config map object.
func (a *APIServerconfigMapStore) Delete(obj interface{}) error {
	cfg := obj.(*api_v1.ConfigMap)
	return a.client.CoreV1().ConfigMaps(cfg.Namespace).Delete(context.TODO(), cfg.Name, metav1.DeleteOptions{})
}

// GetByKey returns the config map for a given key.
// The key must take the form namespace/name.
func (a *APIServerconfigMapStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	nsName := strings.Split(key, "/")
	if len(nsName) != 2 {
		return nil, false, fmt.Errorf("failed to get key %v, unexpected format, expecting ns/name", key)
	}
	ns, name := nsName[0], nsName[1]
	cfg, err := a.client.CoreV1().ConfigMaps(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		// Translate not found errors to found=false, err=nil
		if errors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return cfg, true, nil
}

// newConfigMapStore returns a config map store capable of persisting updates
// to apiserver.
func newConfigMapStore(c kubernetes.Interface) configMapStore {
	return &APIServerconfigMapStore{
		configMapStore: cache.NewStore(cache.MetaNamespaceKeyFunc),
		client:         c,
	}
}

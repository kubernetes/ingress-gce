/*
Copyright 2018 The Kubernetes Authors.

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

package cloudlist

import (
	"fmt"

	"github.com/golang/glog"
)

// Lister is an interface to list the names of one specific object from the cloud.
type Lister interface {
	// List returns the names of one specific object from the cloud.
	List() ([]string, error)
}

type keyFunc func(interface{}) (string, error)

type objectLister interface {
	List() ([]interface{}, error)
}

// GCECLister lists the names of a single GCE object.
type GCELister struct {
	// name is used to distinguish different listers in the logs.
	name string
	// An interface that lists objects of a specific type from GCE.
	objectLister objectLister
	// A function capable of producing a key for a given object.
	keyGetter keyFunc
}

// List implements Lister.
func (c *GCELister) List() ([]string, error) {
	glog.V(4).Infof("GCELister %q is pulling objects", c.name)

	items, err := c.objectLister.List()
	if err != nil {
		return []string{}, fmt.Errorf("Failed to list %q: %v", c.name, err)
	}

	objectNames := make([]string, 0)
	for i := range items {
		key, err := c.keyGetter(items[i])
		if err != nil {
			glog.V(5).Infof("GCELister %q failed to extract key from object %+v: %v", c.name, i, err)
			continue
		}
		objectNames = append(objectNames, key)
	}

	return objectNames, nil
}

// NewGCELister lists the names of a specific object from GCE.
func NewGCELister(name string, k keyFunc, objectLister objectLister) Lister {
	gceLister := &GCELister{
		name:         name,
		objectLister: objectLister,
		keyGetter:    k,
	}
	glog.V(4).Infof("Starting GCELister %q", gceLister.name)
	return gceLister
}

/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
)

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.queue.AddRateLimited(key)
}

func (c *Controller) enqueueAll() {
	mcrts, err := c.lister.List(labels.Everything())
	if err != nil {
		//TODO(krzyk) generate k8s event - can't fetch mcrt
		runtime.HandleError(err)
		return
	}

	if len(mcrts) <= 0 {
		glog.Info("Controller: no Managed Certificates found in cluster")
		return
	}

	var names []string
	for _, mcrt := range mcrts {
		names = append(names, mcrt.Name)
	}

	glog.Infof("Controller: enqueuing Managed Certificates found in cluster: %+v", names)
	for _, mcrt := range mcrts {
		c.enqueue(mcrt)
	}
}

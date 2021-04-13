/*
Copyright 2021 The Kubernetes Authors.

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
package adapterclient

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	networkingv1 "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/klog"
)

type v1IngressClassAdapterClient struct {
	v1 networkingv1.IngressClassInterface
}

// Create creates the provided ingressClass using the V1 API
func (c *v1IngressClassAdapterClient) Create(ctx context.Context, ingressClass *v1beta1.IngressClass, opts metav1.CreateOptions) (*v1beta1.IngressClass, error) {
	v1Class, err := GetV1IngressClass(ingressClass)
	if err != nil {
		klog.Errorf("AdapterClient.Create() Failed to convert v1beta1 IngressClass: %+v: %q", ingressClass, err)
		return nil, err
	}

	createdClass, err := c.v1.Create(ctx, v1Class, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating v1ingress %+v: %q", v1Class, err)
	}

	createdV1beta1Class, err := GetV1beta1IngressClass(createdClass)
	if err != nil {
		klog.Errorf("AdapterClient.Create() Failed to convert to v1 IngressClass after successful creation: %q", err)
	}
	return createdV1beta1Class, nil
}

// Update updates the provided ingressClass using the V1 API
func (c *v1IngressClassAdapterClient) Update(ctx context.Context, ingressClass *v1beta1.IngressClass, opts metav1.UpdateOptions) (*v1beta1.IngressClass, error) {
	v1Class, err := GetV1IngressClass(ingressClass)
	if err != nil {
		klog.Errorf("AdapterClient.Update() Failed to convert v1beta1 IngressClass: %+v: %q", ingressClass, err)
		return nil, err
	}

	updatedClass, err := c.v1.Update(ctx, v1Class, opts)
	if err != nil {
		return nil, fmt.Errorf("error updating v1 IngressClass %+v: %q", v1Class, err)
	}

	updatedV1beta1Class, err := GetV1beta1IngressClass(updatedClass)
	if err != nil {
		klog.Errorf("AdapterClient.Update() Failed to convert to v1 Ingress Classafter successful update: %q", err)
	}
	return updatedV1beta1Class, nil
}

// Delete deletes the provided ingressClass using the V1 API
func (c *v1IngressClassAdapterClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.v1.Delete(ctx, name, opts)
}

// DeleteCollection deletes the collection using the V1 API
func (c *v1IngressClassAdapterClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return c.v1.DeleteCollection(ctx, opts, listOpts)
}

// Get gets specificed ingress class using the V1 API
func (c *v1IngressClassAdapterClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1beta1.IngressClass, error) {
	v1Class, err := c.v1.Get(ctx, name, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting v1 Ingress Class %s: %q", name, err)
	}

	v1beta1Class, err := GetV1beta1IngressClass(v1Class)
	if err != nil {
		klog.Errorf("AdapterClient.Get() Failed to convert to v1 Ingress Class after successful retrieval: %q", err)
	}
	return v1beta1Class, err
}

// List returns a list of ingress classes using the V1 API
func (c *v1IngressClassAdapterClient) List(ctx context.Context, opts metav1.ListOptions) (*v1beta1.IngressClassList, error) {
	ingList, err := c.v1.List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting v1 Ingress List: %q", err)
	}

	var classes []v1beta1.IngressClass
	for _, class := range ingList.Items {
		v1Class := class
		v1beta1Class, err := GetV1beta1IngressClass(&v1Class)
		if err != nil {
			klog.Errorf("AdapterClient.List() failed to convert v1 Ingress Class %s/%s to v1beta1", class.Namespace, class.Name)
		}
		classes = append(classes, *v1beta1Class)
	}

	return &v1beta1.IngressClassList{Items: classes}, nil
}

// Watch returns a v1 watcher that converts v1 ingress class events into v1beta1 ingress class events
func (c *v1IngressClassAdapterClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	watcher, err := c.v1.Watch(ctx, opts)
	if err != nil {
		msg := fmt.Sprintf("AdapterClient.Watch() failed to create v1 ingress class watcher: %q", err)
		klog.Error(msg)
		return nil, fmt.Errorf(msg)
	}
	return NewV1IngressClassAdapterWatcher(watcher), nil
}

// Patch patches the specified resources using the v1 API. Patch expects that the patch bytes provided
// will work with the v1 version of the ingress class
func (c *v1IngressClassAdapterClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1beta1.IngressClass, err error) {
	v1Class, err := c.v1.Patch(ctx, name, pt, data, opts, subresources...)
	if err != nil {
		klog.Errorf("AdapterClient.Patch() Failed to patch v1 ingress class: %q", err)
		return nil, fmt.Errorf("error patching v1 IngressClass: %q", err)
	}

	v1beta1Class, err := GetV1beta1IngressClass(v1Class)
	if err != nil {
		klog.Errorf("AdapterClient.Patch() failed to convert v1 Ingress %s/%s to v1beta1", v1Class.Namespace, v1Class.Name)
	}
	return v1beta1Class, err
}

type v1IngressClassAdapterWatcher struct {
	resultLock sync.Mutex
	result     chan watch.Event
	watch.Interface
}

// NewV1IngressClassAdapterWatcher returns a v1 watcher that converts v1ingress class events
// into v1beta1 ingress class events
func NewV1IngressClassAdapterWatcher(v1Watcher watch.Interface) watch.Interface {
	v1Result := v1Watcher.ResultChan()
	result := make(chan watch.Event, cap(v1Result))

	watcher := &v1IngressClassAdapterWatcher{Interface: v1Watcher, result: result}
	go func() {
		for {
			select {
			case event, ok := <-v1Result:
				if !ok {
					// underlying watcher has been closed, stop reading from the chan
					return
				}

				var eventToSend watch.Event
				class, ok := event.Object.(*v1.IngressClass)
				if !ok {
					eventToSend = event
					result <- event
				} else {
					v1beta1Class, err := GetV1beta1IngressClass(class)
					if err != nil {
						klog.Errorf("skipping event because v1 ingress class %+v could not be converted to v1beta1: %q", class, err)
						continue
					}
					eventToSend = watch.Event{Type: event.Type, Object: v1beta1Class}
				}

				watcher.resultLock.Lock()
				result <- eventToSend
				watcher.resultLock.Unlock()
			}
		}
	}()

	return watcher
}

// Stop stops the underlying v1 watcher and closes the result channel
func (w *v1IngressClassAdapterWatcher) Stop() {
	w.Interface.Stop()
	w.resultLock.Lock()
	close(w.result)
	w.resultLock.Unlock()
}

// ResultChan returns the ready only event channel
func (w *v1IngressClassAdapterWatcher) ResultChan() <-chan watch.Event {
	return w.result
}

// GetV1IngressClass converts the v1beta1 ingress class into a v1 ingress class
func GetV1IngressClass(v1beta1Class *v1beta1.IngressClass) (*v1.IngressClass, error) {
	v1Class := &v1.IngressClass{}
	err := Convert_v1beta1_IngressClass_To_networking_IngressClass(v1beta1Class, v1Class, nil)
	if err != nil {
		return nil, fmt.Errorf("error converting from v1beta1 IngressClass to v1 IngressClass: %q", err)
	}
	return v1Class, nil
}

// GetV1beta1IngressClass converts the v1ingress class into a v1beta1 ingress class
func GetV1beta1IngressClass(v1Class *v1.IngressClass) (*v1beta1.IngressClass, error) {
	v1beta1Class := &v1beta1.IngressClass{}
	err := Convert_networking_IngressClass_To_v1beta1_IngressClass(v1Class, v1beta1Class, nil)
	if err != nil {
		return nil, fmt.Errorf("error converting v1 IngressClass to v1beta1 IngressClass: %q", err)
	}
	return v1beta1Class, nil
}

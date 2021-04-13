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

type v1IngressAdapterClient struct {
	v1 networkingv1.IngressInterface
}

// Create creates the provided ingress using the V1 API
func (c *v1IngressAdapterClient) Create(ctx context.Context, ingress *v1beta1.Ingress, opts metav1.CreateOptions) (*v1beta1.Ingress, error) {
	v1Ingress, err := GetV1Ingress(ingress)
	if err != nil {
		klog.Errorf("AdapterClient.Create() Failed to convert v1beta1 Ingress: %+v: %q", ingress, err)
		return nil, err
	}

	createdIngress, err := c.v1.Create(ctx, v1Ingress, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating v1ingress %+v: %q", v1Ingress, err)
	}

	createdV1beta1Ing, err := GetV1beta1Ingress(createdIngress)
	if err != nil {
		klog.Errorf("AdapterClient.Create() Failed to convert to v1 Ingress after successful creation: %q", err)
	}
	return createdV1beta1Ing, nil
}

// Update updates the provided ingress using the V1 API
func (c *v1IngressAdapterClient) Update(ctx context.Context, ingress *v1beta1.Ingress, opts metav1.UpdateOptions) (*v1beta1.Ingress, error) {
	v1Ingress, err := GetV1Ingress(ingress)
	if err != nil {
		klog.Errorf("AdapterClient.Update() Failed to convert v1beta1 Ingress: %+v: %q", ingress, err)
		return nil, err
	}

	updatedIngress, err := c.v1.Update(ctx, v1Ingress, opts)
	if err != nil {
		return nil, fmt.Errorf("error updating v1ingress %+v: %q", v1Ingress, err)
	}

	updatedV1beta1Ing, err := GetV1beta1Ingress(updatedIngress)
	if err != nil {
		klog.Errorf("AdapterClient.Update() Failed to convert to v1 Ingress after successful update: %q", err)
	}
	return updatedV1beta1Ing, nil
}

// UpdateStatus updates the status of the provided ingress using the V1 API
func (c *v1IngressAdapterClient) UpdateStatus(ctx context.Context, ingress *v1beta1.Ingress, opts metav1.UpdateOptions) (*v1beta1.Ingress, error) {
	v1Ingress, err := GetV1Ingress(ingress)
	if err != nil {
		klog.Errorf("AdapterClient.Update() Failed to convert v1beta1 Ingress: %+v: %q", ingress, err)
		return nil, err
	}

	updatedIngress, err := c.v1.UpdateStatus(ctx, v1Ingress, opts)
	if err != nil {
		return nil, fmt.Errorf("error updating v1ingress %+v: %q", v1Ingress, err)
	}

	updatedV1beta1Ing, err := GetV1beta1Ingress(updatedIngress)
	if err != nil {
		klog.Errorf("AdapterClient.Update() Failed to convert to v1 Ingress after successful update: %q", err)
	}
	return updatedV1beta1Ing, nil
}

// Delete deletes the provided ingress using the V1 API
func (c *v1IngressAdapterClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.v1.Delete(ctx, name, opts)
}

// DeleteCollection deletes the collection of ingressesusing the V1 API
func (c *v1IngressAdapterClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return c.v1.DeleteCollection(ctx, opts, listOpts)
}

// Get gets the specified ingress using the V1 API and returns it after converting it into a v1beta1 ingress
func (c *v1IngressAdapterClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1beta1.Ingress, error) {
	v1Ingress, err := c.v1.Get(ctx, name, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting v1 Ingress %s: %q", name, err)
	}

	v1beta1Ingress, err := GetV1beta1Ingress(v1Ingress)
	if err != nil {
		klog.Errorf("AdapterClient.Get() Failed to convert to v1 Ingress after successful retrieval: %q", err)
	}
	return v1beta1Ingress, err
}

// List gets the collection of ingresses using the V1 API
func (c *v1IngressAdapterClient) List(ctx context.Context, opts metav1.ListOptions) (*v1beta1.IngressList, error) {
	ingList, err := c.v1.List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting v1 Ingress List: %q", err)
	}

	var ingresses []v1beta1.Ingress
	for _, ing := range ingList.Items {
		v1Ingress := ing
		v1beta1Ingress, err := GetV1beta1Ingress(&v1Ingress)
		if err != nil {
			klog.Errorf("AdapterClient.List() failed to convert v1 Ingress %s/%s to v1beta1", ing.Namespace, ing.Name)
		}
		ingresses = append(ingresses, *v1beta1Ingress)
	}

	return &v1beta1.IngressList{Items: ingresses}, nil
}

// Watch returns a v1 watcher that converts v1 Ingress Events into v1beta1 Ingress Events
func (c *v1IngressAdapterClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	watcher, err := c.v1.Watch(ctx, opts)
	if err != nil {
		msg := fmt.Sprintf("AdapterClient.Watch() failed to create v1 ingress watcher: %q", err)
		klog.Error(msg)
		return nil, fmt.Errorf(msg)
	}
	return NewV1IngressAdapterWatcher(watcher), nil
}

// Patch patches the specified ingress using the V1 API
//
// **NOTE** Patch requires that the patch bytes are for v1. v1beta1 patches cannot be
// applied to v1 resources because API fields were renamed from v1beta1 to v1.
func (c *v1IngressAdapterClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1beta1.Ingress, err error) {
	v1Ing, err := c.v1.Patch(ctx, name, pt, data, opts, subresources...)
	if err != nil {
		klog.Errorf("AdapterClient.Patch() Failed to patch v1 ingress: %q", err)
		return nil, fmt.Errorf("error patching v1 Ingress: %q", err)
	}

	v1beta1Ingress, err := GetV1beta1Ingress(v1Ing)
	if err != nil {
		klog.Errorf("AdapterClient.Patch() failed to convert v1 Ingress %s/%s to v1beta1", v1Ing.Namespace, v1Ing.Name)
	}
	return v1beta1Ingress, err
}

type v1IngressAdapterWatcher struct {
	resultLock sync.Mutex
	result     chan watch.Event
	watch.Interface
}

// NewV1IngressAdapterWatcher returns a v1 watcher that converts v1 Ingress events into v1beta1 Ingress events
func NewV1IngressAdapterWatcher(v1Watcher watch.Interface) watch.Interface {

	v1Result := v1Watcher.ResultChan()
	result := make(chan watch.Event, cap(v1Result))

	watcher := &v1IngressAdapterWatcher{Interface: v1Watcher, result: result}
	go func() {
		for {
			select {
			case event, ok := <-v1Result:
				if !ok {
					// underlying watcher has been closed, stop reading from the chan
					return
				}

				var eventToSend watch.Event
				ing, ok := event.Object.(*v1.Ingress)
				if !ok {
					eventToSend = event
					result <- event
				} else {
					v1beta1Ing, err := GetV1beta1Ingress(ing)
					if err != nil {
						klog.Errorf("skipping event because v1 ingress %+v could not be converted to v1beta1: %q", ing, err)
						continue
					}
					eventToSend = watch.Event{Type: event.Type, Object: v1beta1Ing}
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
func (w *v1IngressAdapterWatcher) Stop() {
	w.Interface.Stop()
	w.resultLock.Lock()
	close(w.result)
	w.resultLock.Unlock()
}

// ResultChan returns the read only event channel
func (w *v1IngressAdapterWatcher) ResultChan() <-chan watch.Event {
	return w.result
}

// GetV1Ingress converts the provided v1beta1 Ingress to a v1 Ingress
func GetV1Ingress(v1beta1Ing *v1beta1.Ingress) (*v1.Ingress, error) {
	v1Ingress := &v1.Ingress{}
	err := Convert_v1beta1_Ingress_To_networking_Ingress(v1beta1Ing, v1Ingress, nil)
	if err != nil {
		return nil, fmt.Errorf("error converting from v1beta1 Ingress to v1 Ingress: %q", err)
	}
	return v1Ingress, nil
}

// GetV1beta1Ingress converts the provided v1Ingress to a v1beta1 Ingress
func GetV1beta1Ingress(v1Ing *v1.Ingress) (*v1beta1.Ingress, error) {
	v1beta1Ingress := &v1beta1.Ingress{}
	err := Convert_networking_Ingress_To_v1beta1_Ingress(v1Ing, v1beta1Ingress, nil)
	if err != nil {
		return nil, fmt.Errorf("error converting v1 Ingress to v1beta1 Ingress: %q", err)
	}
	return v1beta1Ingress, nil
}

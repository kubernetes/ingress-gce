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

package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
)

const (
	ingressPollInterval = 30 * time.Second
	ingressPollTimeout  = 20 * time.Minute

	gclbDeletionInterval = 30 * time.Second
	gclbDeletionTimeout  = 5 * time.Minute
)

// WaitForIngressOptions holds options dictating how we wait for an ingress to stabilize.
type WaitForIngressOptions struct {
	// ExpectUnreachable is true when we expect the LB to still be
	// programming itself (i.e 404's / 502's)
	ExpectUnreachable bool
}

// WaitForIngress to stabilize.
func WaitForIngress(s *Sandbox, ing *v1beta1.Ingress, options *WaitForIngressOptions) (*v1beta1.Ingress, error) {
	err := wait.Poll(ingressPollInterval, ingressPollTimeout, func() (bool, error) {
		var err error
		ing, err = s.f.Clientset.Extensions().Ingresses(s.Namespace).Get(ing.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		validator, err := fuzz.NewIngressValidator(s.ValidatorEnv, ing, features.All, nil)
		if err != nil {
			return true, err
		}
		result := validator.Check(context.Background())
		if result.Err == nil {
			return true, nil
		} else {
			if options == nil || options.ExpectUnreachable {
				return false, nil
			}
			return true, fmt.Errorf("Unexpected error from validation: %v", result.Err)
		}
	})
	return ing, err
}

// WaitForIngressDeletion deletes the given ingress and waits for the
// resources associated with it to be deleted.
func WaitForIngressDeletion(ctx context.Context, g *fuzz.GCLB, s *Sandbox, ing *v1beta1.Ingress, options *fuzz.GCLBDeleteOptions) error {
	if err := s.f.Clientset.Extensions().Ingresses(s.Namespace).Delete(ing.Name, &metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("Delete(%q) = %v, want nil", ing.Name, err)
	}
	glog.Infof("Waiting for GCLB resources to be deleted (%s/%s), IngressDeletionOptions=%+v", s.Namespace, ing.Name, options)
	if err := WaitForGCLBDeletion(ctx, s.f.Cloud, g, options); err != nil {
		return fmt.Errorf("WaitForGCLBDeletion(...) = %v, want nil", err)
	}
	glog.Infof("GCLB resources deleted (%s/%s)", s.Namespace, ing.Name)
	return nil
}

// WaitForGCLBDeletion waits for the resources associated with the GLBC to be
// deleted.
func WaitForGCLBDeletion(ctx context.Context, c cloud.Cloud, g *fuzz.GCLB, options *fuzz.GCLBDeleteOptions) error {
	return wait.Poll(gclbDeletionInterval, gclbDeletionTimeout, func() (bool, error) {
		if err := g.CheckResourceDeletion(ctx, c, options); err != nil {
			glog.Infof("WaitForGCLBDeletion(%q) = %v", g.VIP, err)
			return false, nil
		}
		return true, nil
	})
}

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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/klog"
)

const (
	ingressPollInterval = 30 * time.Second
	ingressPollTimeout  = 25 * time.Minute

	gclbDeletionInterval = 30 * time.Second
	gclbDeletionTimeout  = 15 * time.Minute

	negPollInterval = 5 * time.Second
	negPollTimeout  = 3 * time.Minute
)

// WaitForIngressOptions holds options dictating how we wait for an ingress to stabilize
type WaitForIngressOptions struct {
	// ExpectUnreachable is true when we expect the LB to still be
	// programming itself (i.e 404's / 502's)
	ExpectUnreachable bool
}

// WaitForIngress to stabilize.
// We expect the ingress to be unreachable at first as LB is
// still programming itself (i.e 404's / 502's)
func WaitForIngress(s *Sandbox, ing *v1beta1.Ingress, options *WaitForIngressOptions) (*v1beta1.Ingress, error) {
	err := wait.Poll(ingressPollInterval, ingressPollTimeout, func() (bool, error) {
		var err error
		ing, err = s.f.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Get(ing.Name, metav1.GetOptions{})
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
			return true, fmt.Errorf("unexpected error from validation: %v", result.Err)
		}
	})
	return ing, err
}

// WaitForIngressDeletion deletes the given ingress and waits for the
// resources associated with it to be deleted.
func WaitForIngressDeletion(ctx context.Context, g *fuzz.GCLB, s *Sandbox, ing *v1beta1.Ingress, options *fuzz.GCLBDeleteOptions) error {
	if err := s.f.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Delete(ing.Name, &metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete(%q) = %v, want nil", ing.Name, err)
	}
	klog.Infof("Waiting for GCLB resources to be deleted (%s/%s), IngressDeletionOptions=%+v", s.Namespace, ing.Name, options)
	if err := WaitForGCLBDeletion(ctx, s.f.Cloud, g, options); err != nil {
		return fmt.Errorf("WaitForGCLBDeletion(...) = %v, want nil", err)
	}
	klog.Infof("GCLB resources deleted (%s/%s)", s.Namespace, ing.Name)
	return nil
}

// WaitForGCLBDeletion waits for the resources associated with the GLBC to be
// deleted.
func WaitForGCLBDeletion(ctx context.Context, c cloud.Cloud, g *fuzz.GCLB, options *fuzz.GCLBDeleteOptions) error {
	return wait.Poll(gclbDeletionInterval, gclbDeletionTimeout, func() (bool, error) {
		if err := g.CheckResourceDeletion(ctx, c, options); err != nil {
			klog.Infof("WaitForGCLBDeletion(%q) = %v", g.VIP, err)
			return false, nil
		}
		return true, nil
	})
}

// WaitForNEGDeletion waits for all NEGs associated with a GCLB to be deleted via GC
func WaitForNEGDeletion(ctx context.Context, c cloud.Cloud, g *fuzz.GCLB, options *fuzz.GCLBDeleteOptions) error {
	return wait.Poll(negPollInterval, negPollTimeout, func() (bool, error) {
		if err := g.CheckNEGDeletion(ctx, c, options); err != nil {
			klog.Infof("WaitForNegDeletion(%q) = %v", g.VIP, err)
			return false, nil
		}
		return true, nil
	})
}

// WaitForNegConfiguration waits until the NEGStatus of the service is updated to at least one NEG
// TODO: (shance) make this more robust so it handles multiple NEGS
func WaitForNEGConfiguration(svc *v1.Service, f *Framework, s *Sandbox) error {
	return wait.Poll(negPollInterval, negPollTimeout, func() (bool, error) {
		// Get Annotation
		svc, _ = f.Clientset.CoreV1().Services(s.Namespace).Get(svc.Name, metav1.GetOptions{})

		negStatus, found, err := annotations.FromService(svc).NEGStatus()

		if found {
			if err != nil {
				return false, fmt.Errorf("Error parsing neg status: %v", err)
			}
			if negStatus.NetworkEndpointGroups != nil {
				return true, nil
			}
		}

		return false, nil
	})
}

func CheckGCLB(gclb *fuzz.GCLB, numForwardingRules int, numBackendServices int) error {
	// Do some cursory checks on the GCP objects.
	if len(gclb.ForwardingRule) != numForwardingRules {
		return fmt.Errorf("got %d forwarding rules, want %d", len(gclb.ForwardingRule), numForwardingRules)
	}
	if len(gclb.BackendService) != numBackendServices {
		return fmt.Errorf("got %d backend services, want %d", len(gclb.BackendService), numBackendServices)
	}

	return nil
}

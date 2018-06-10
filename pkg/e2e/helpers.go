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
	"time"

	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

const (
	ingressPollInterval = 30 * time.Second
	ingressPollTimeout  = 20 * time.Minute
)

// WaitForIngress to stabilize.
func WaitForIngress(s *Sandbox, ing *v1beta1.Ingress) (*v1beta1.Ingress, error) {
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
		}
		return false, nil
	})
	return ing, err
}

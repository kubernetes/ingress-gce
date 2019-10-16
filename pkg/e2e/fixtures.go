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

// Put common test fixtures (e.g. resources to be created) here.

import (
	"context"
	"fmt"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/ingress-gce/pkg/utils"
	"math/rand"
	"net/http"
	"reflect"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	apps "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/cmd/echo/app"
	"k8s.io/klog"
)

const (
	echoheadersImage = "gcr.io/k8s-ingress-image-push/ingress-gce-echo-amd64:master"
)

var ErrSubnetExists = fmt.Errorf("ILB subnet in region already exists")

// NoopModify does not modify the input deployment
func NoopModify(*apps.Deployment) {}

// SpreadPodAcrossZones sets pod anti affinity rules to try to spread pods across zones
func SpreadPodAcrossZones(deployment *apps.Deployment) {
	podLabels := deployment.Spec.Template.Labels
	deployment.Spec.Template.Spec.Affinity = &v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
				{
					Weight: int32(1),
					PodAffinityTerm: v1.PodAffinityTerm{
						LabelSelector: metav1.SetAsLabelSelector(labels.Set(podLabels)),
						TopologyKey:   v1.LabelZoneFailureDomain,
					},
				},
			},
		},
	}
}

// CreateEchoService creates the pod and service serving echoheaders
// Todo: (shance) remove this and replace uses with EnsureEchoService()
func CreateEchoService(s *Sandbox, name string, annotations map[string]string) (*v1.Service, error) {
	return EnsureEchoService(s, name, annotations, v1.ServiceTypeNodePort, 1)
}

// EnsureEchoService that the Echo service with the given description is set up
func EnsureEchoService(s *Sandbox, name string, annotations map[string]string, svcType v1.ServiceType, numReplicas int32) (*v1.Service, error) {
	if err := EnsureEchoDeployment(s, name, numReplicas, NoopModify); err != nil {
		return nil, err
	}

	expectedSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http-port",
					Protocol:   v1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
				{
					Name:       "https-port",
					Protocol:   v1.ProtocolTCP,
					Port:       443,
					TargetPort: intstr.FromInt(8443),
				},
			},
			Selector: map[string]string{"app": name},
			Type:     svcType,
		},
	}
	svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Get(name, metav1.GetOptions{})

	if svc == nil || err != nil {
		if svc, err = s.f.Clientset.CoreV1().Services(s.Namespace).Create(expectedSvc); err != nil {
			return nil, err
		}
		return svc, err
	}

	if !reflect.DeepEqual(svc.Spec, expectedSvc.Spec) {
		// Update the fields individually since we don't want to override everything
		svc.ObjectMeta.Annotations = expectedSvc.ObjectMeta.Annotations
		svc.Spec.Ports = expectedSvc.Spec.Ports
		svc.Spec.Type = expectedSvc.Spec.Type

		if svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Update(svc); err != nil {
			return nil, fmt.Errorf("svc: %v\nexpectedSvc: %v\nerr: %v", svc, expectedSvc, err)
		}
	}
	return svc, nil
}

// Ensures that the Echo deployment with the given description is set up
func EnsureEchoDeployment(s *Sandbox, name string, numReplicas int32, modify func(deployment *apps.Deployment)) error {
	podTemplate := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": name},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "echoheaders",
					Image: echoheadersImage,
					Ports: []v1.ContainerPort{
						{ContainerPort: 8080, Name: "http-port"},
						{ContainerPort: 8443, Name: "https-port"},
					},
					Env: []v1.EnvVar{
						{
							Name: app.HostEnvVar,
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "spec.nodeName",
								},
							},
						},
						{
							Name: app.PodEnvVar,
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.name",
								},
							},
						},
						{
							Name: app.NamespaceEnvVar,
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.namespace",
								},
							},
						},
					},
				},
			},
		},
	}

	deployment := &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apps.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: podTemplate,
			Replicas: &numReplicas,
		},
	}
	modify(deployment)

	existingDeployment, err := s.f.Clientset.ExtensionsV1beta1().Deployments(s.Namespace).Get(name, metav1.GetOptions{})
	if existingDeployment == nil || err != nil {
		if _, err = s.f.Clientset.AppsV1().Deployments(s.Namespace).Create(deployment); err != nil {
			return err
		}
	} else {
		if _, err = s.f.Clientset.AppsV1().Deployments(s.Namespace).Update(deployment); err != nil {
			return err
		}
	}

	deployment.Spec.Replicas = &numReplicas
	if _, err = s.f.Clientset.AppsV1().Deployments(s.Namespace).Update(deployment); err != nil {
		return fmt.Errorf("Error updating deployment scale: %v", err)
	}
	return nil
}

// CreateSecret creates a secret from the given data.
func CreateSecret(s *Sandbox, name string, data map[string][]byte) (*v1.Secret, error) {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: data,
	}
	var err error
	if secret, err = s.f.Clientset.CoreV1().Secrets(s.Namespace).Create(secret); err != nil {
		return nil, err
	}
	klog.V(2).Infof("Secret %q:%q created", s.Namespace, name)

	return secret, nil
}

// DeleteSecret deletes a secret.
func DeleteSecret(s *Sandbox, name string) error {
	if err := s.f.Clientset.CoreV1().Secrets(s.Namespace).Delete(name, &metav1.DeleteOptions{}); err != nil {
		return err
	}
	klog.V(2).Infof("Secret %q:%q created", s.Namespace, name)

	return nil
}

// EnsureIngress creates a new Ingress or updates an existing one.
func EnsureIngress(s *Sandbox, ing *v1beta1.Ingress) (*v1beta1.Ingress, error) {
	crud := &IngressCRUD{s.f.Clientset}
	currentIng, err := crud.Get(ing.ObjectMeta.Namespace, ing.ObjectMeta.Name)
	if currentIng == nil || err != nil {
		return crud.Create(ing)
	}
	// Update ingress spec if they are not equal
	if !reflect.DeepEqual(ing.Spec, currentIng.Spec) {
		return crud.Update(ing)
	}
	return ing, nil
}

// NewGCPAddress reserves a global static IP address with the given name.
func NewGCPAddress(s *Sandbox, name string) error {
	addr := &compute.Address{Name: name}
	if err := s.f.Cloud.GlobalAddresses().Insert(context.Background(), meta.GlobalKey(addr.Name), addr); err != nil {
		return err
	}
	klog.V(2).Infof("Global static IP %s created", name)

	return nil
}

// DeleteGCPAddress deletes a global static IP address with the given name.
func DeleteGCPAddress(s *Sandbox, name string) error {
	if err := s.f.Cloud.GlobalAddresses().Delete(context.Background(), meta.GlobalKey(name)); err != nil {
		return err
	}
	klog.V(2).Infof("Global static IP %s deleted", name)

	return nil
}

// CreateILBSubnet creates the ILB subnet
func CreateILBSubnet(s *Sandbox) error {
	klog.V(3).Info("CreateILBSubnet()")

	// If no network is provided, we don't try to create the subnet
	if s.f.Network == "" {
		return ErrSubnetExists
	}

	name := "ilb-subnet-ingress-e2e"

	// Try up to 10 different subnets since we can't conflict with anything in the test project
	// TODO(shance): find a more reliable way to pick the subnet
	var err error
	// Start at a random place in the range 0-256
	start := rand.Int()
	for i := 0; i < 10; i++ {
		ipCidrRange := fmt.Sprintf("192.168.%d.0/24", i+start%256)
		err = trySubnetCreate(s, name, ipCidrRange)
		if err == nil || err == ErrSubnetExists {
			return err
		}
		klog.V(4).Info(err)
	}
	return err
}

// trySubnetCreate is a helper for CreateILBSubnet
func trySubnetCreate(s *Sandbox, name, ipCidrRange string) error {
	networkID := cloud.ResourceID{ProjectID: s.f.Project, Resource: "networks", Key: meta.GlobalKey(s.f.Network)}

	subnet := &computebeta.Subnetwork{
		Name:        name,
		IpCidrRange: ipCidrRange,
		Purpose:     "INTERNAL_HTTPS_LOAD_BALANCER",
		Network:     networkID.SelfLink(meta.VersionBeta),
		Role:        "ACTIVE",
	}

	err := s.f.Cloud.BetaSubnetworks().Insert(context.Background(), meta.RegionalKey(subnet.Name, s.f.Region), subnet)
	if err != nil {
		// GCE returns a 409 when the subnet *with the same name* already exists
		if utils.IsHTTPErrorCode(err, http.StatusConflict) {
			klog.V(3).Infof("ILB subnet already exists: %v", err)
			return ErrSubnetExists
		} else {
			return fmt.Errorf("Error creating ILB subnet: %v", err)
		}
	}

	klog.V(3).Infof("ILB Subnet created in region %q: %v", s.f.Region, subnet)
	return nil
}

// DeleteILBSubnet deletes the ILB subnet
func DeleteILBSubnet(s *Sandbox, name string) error {
	klog.V(2).Infof("Deleting ILB Subnet %q", name)
	return s.f.Cloud.BetaSubnetworks().Delete(context.Background(), meta.RegionalKey(name, s.f.Region))
}

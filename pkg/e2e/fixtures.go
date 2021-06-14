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
	"math/rand"
	"net/http"
	"reflect"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	apiappsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	frontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/utils"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/cmd/echo/app"
	"k8s.io/klog"
	utilpointer "k8s.io/utils/pointer"
)

const (
	echoheadersImage        = "gcr.io/k8s-ingress-image-push/ingress-gce-echo-amd64:master"
	echoheadersImageWindows = "gcr.io/gke-windows-testing/ingress-gce-echo-amd64-windows:master"
	porterPort              = 80
	ILBSubnetPurpose        = "INTERNAL_HTTPS_LOAD_BALANCER"
	ILBSubnetName           = "ilb-subnet-ingress-e2e"
	PSCSubnetPurpose        = "PRIVATE_SERVICE_CONNECT"
	PSCSubnetName           = "psc-nat-subnet"
)

type OS int

const (
	Linux OS = iota
	Windows
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

// CreateEchoServiceWithOS creates the pod and service serving echoheaders
// Todo: (shance) remove this and replace uses with EnsureEchoService()
func CreateEchoServiceWithOS(s *Sandbox, name string, annotations map[string]string, os OS) (*v1.Service, error) {
	return ensureEchoService(s, name, annotations, v1.ServiceTypeNodePort, 1, os)
}

// EnsureEchoService that the Echo service with the given description is set up
func EnsureEchoService(s *Sandbox, name string, annotations map[string]string, svcType v1.ServiceType, numReplicas int32) (*v1.Service, error) {
	return ensureEchoService(s, name, annotations, svcType, numReplicas, Linux)
}

func ensureEchoService(s *Sandbox, name string, annotations map[string]string, svcType v1.ServiceType, numReplicas int32, os OS) (*v1.Service, error) {
	if err := ensureEchoDeployment(s, name, numReplicas, NoopModify, os); err != nil {
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
	svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Get(context.TODO(), name, metav1.GetOptions{})

	if svc == nil || err != nil {
		if svc, err = s.f.Clientset.CoreV1().Services(s.Namespace).Create(context.TODO(), expectedSvc, metav1.CreateOptions{}); err != nil {
			return nil, err
		}
		return svc, err
	}

	if !reflect.DeepEqual(svc.Spec, expectedSvc.Spec) {
		// Update the fields individually since we don't want to override everything
		svc.ObjectMeta.Annotations = expectedSvc.ObjectMeta.Annotations
		svc.Spec.Ports = expectedSvc.Spec.Ports
		svc.Spec.Type = expectedSvc.Spec.Type

		if svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Update(context.TODO(), svc, metav1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("svc: %v\nexpectedSvc: %v\nerr: %v", svc, expectedSvc, err)
		}
	}
	return svc, nil
}

// DeleteEchoService deletes the K8s service
func DeleteEchoService(s *Sandbox, svcName string) error {
	return s.f.Clientset.CoreV1().Services(s.Namespace).Delete(context.TODO(), svcName, metav1.DeleteOptions{})
}

// EnsureEchoDeployment ensures that the Echo deployment with the given description is set up
func EnsureEchoDeployment(s *Sandbox, name string, numReplicas int32, modify func(deployment *apps.Deployment)) error {
	return ensureEchoDeployment(s, name, numReplicas, modify, Linux)
}

func ensureEchoDeployment(s *Sandbox, name string, numReplicas int32, modify func(deployment *apps.Deployment), os OS) error {
	image := echoheadersImage
	var nodeSelector map[string]string
	if os == Windows {
		image = echoheadersImageWindows
		nodeSelector = map[string]string{"kubernetes.io/os": "windows"}
	}
	podTemplate := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": name},
		},
		Spec: v1.PodSpec{
			NodeSelector: nodeSelector,
			Containers: []v1.Container{
				{
					Name:  "echoheaders",
					Image: image,
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

	existingDeployment, err := s.f.Clientset.AppsV1().Deployments(s.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if existingDeployment == nil || err != nil {
		if _, err = s.f.Clientset.AppsV1().Deployments(s.Namespace).Create(context.TODO(), deployment, metav1.CreateOptions{}); err != nil {
			return err
		}
	} else {
		if _, err = s.f.Clientset.AppsV1().Deployments(s.Namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	deployment.Spec.Replicas = &numReplicas
	if _, err = s.f.Clientset.AppsV1().Deployments(s.Namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{}); err != nil {
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
	if secret, err = s.f.Clientset.CoreV1().Secrets(s.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		return nil, err
	}
	klog.V(2).Infof("Secret %q:%q created", s.Namespace, name)

	return secret, nil
}

// DeleteSecret deletes a secret.
func DeleteSecret(s *Sandbox, name string) error {
	if err := s.f.Clientset.CoreV1().Secrets(s.Namespace).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
		return err
	}
	klog.V(2).Infof("Secret %q:%q deleted", s.Namespace, name)

	return nil
}

// EnsureIngress creates a new Ingress or updates an existing one.
func EnsureIngress(s *Sandbox, ing *networkingv1.Ingress) (*networkingv1.Ingress, error) {
	crud := &adapter.IngressCRUD{C: s.f.Clientset}
	currentIng, err := crud.Get(ing.ObjectMeta.Namespace, ing.ObjectMeta.Name)
	if currentIng == nil || err != nil {
		return crud.Create(ing)
	}
	// Update ingress spec if they are not equal
	if !reflect.DeepEqual(ing.Spec, currentIng.Spec) {
		newIng := currentIng.DeepCopy()
		newIng.Spec = ing.Spec
		return crud.Patch(currentIng, newIng)
	}
	return ing, nil
}

// TODO(shance) add frontendconfig CRUD
func EnsureFrontendConfig(s *Sandbox, fc *frontendconfig.FrontendConfig) (*frontendconfig.FrontendConfig, error) {
	currentFc, err := s.f.FrontendConfigClient.NetworkingV1beta1().FrontendConfigs(s.Namespace).Get(context.TODO(), fc.Name, metav1.GetOptions{})
	if currentFc == nil || err != nil {
		return s.f.FrontendConfigClient.NetworkingV1beta1().FrontendConfigs(s.Namespace).Create(context.TODO(), fc, metav1.CreateOptions{})
	}
	// Update fc spec if they are not equal
	if !reflect.DeepEqual(fc.Spec, currentFc.Spec) {
		currentFc.Spec = fc.Spec
		return s.f.FrontendConfigClient.NetworkingV1beta1().FrontendConfigs(s.Namespace).Update(context.TODO(), currentFc, metav1.UpdateOptions{})
	}
	return fc, nil
}

// NewGCPAddress reserves a global static IP address with the given name.
func NewGCPAddress(s *Sandbox, name string, region string) error {
	addr := &compute.Address{Name: name}

	if region == "" {
		if err := s.f.Cloud.GlobalAddresses().Insert(context.Background(), meta.GlobalKey(addr.Name), addr); err != nil {
			return err
		}
		klog.V(2).Infof("Global static IP %s created", name)
	} else {
		addr.AddressType = "INTERNAL"
		if err := s.f.Cloud.Addresses().Insert(context.Background(), meta.RegionalKey(addr.Name, region), addr); err != nil {
			return err
		}
		klog.V(2).Infof("Regional static IP %s created", name)
	}

	return nil
}

// DeleteGCPAddress deletes a global static IP address with the given name.
func DeleteGCPAddress(s *Sandbox, name string, region string) error {
	if region == "" {
		if err := s.f.Cloud.GlobalAddresses().Delete(context.Background(), meta.GlobalKey(name)); err != nil {
			return err
		}
		klog.V(2).Infof("Global static IP %s deleted", name)
	} else {
		if err := s.f.Cloud.Addresses().Delete(context.Background(), meta.RegionalKey(name, region)); err != nil {
			return err
		}
		klog.V(2).Infof("Regional static IP %s deleted", name)
	}
	return nil
}

// CreateILBSubnet creates the ILB subnet
func CreateILBSubnet(s *Sandbox) error {
	klog.V(2).Info("CreateILBSubnet()")
	return CreateSubnet(s, ILBSubnetName, ILBSubnetPurpose)
}

// CreateSubnet creates a subnet with the provided name and purpose
func CreateSubnet(s *Sandbox, subnetName, purpose string) error {
	klog.V(2).Infof("CreateSubnet(%s)", subnetName)

	// If no network is provided, we don't try to create the subnet
	if s.f.Network == "" {
		return fmt.Errorf("error no network provided, cannot create Subnet")
	}

	// Try up to 10 different subnets since we can't conflict with anything in the test project
	// TODO(shance): find a more reliable way to pick the subnet
	var err error
	// Start at a random place in the range 0-256
	start := rand.Int()
	for i := 0; i < 10; i++ {
		ipCidrRange := fmt.Sprintf("192.168.%d.0/24", i+start%256)
		err = trySubnetCreate(s, subnetName, ipCidrRange, purpose)
		if err == nil || err == ErrSubnetExists {
			return err
		}
		klog.V(4).Info(err)
	}
	return err
}

// trySubnetCreate is a helper for CreateSubnet
func trySubnetCreate(s *Sandbox, name, ipCidrRange, purpose string) error {
	networkID := cloud.ResourceID{ProjectID: s.f.Project, Resource: "networks", Key: meta.GlobalKey(s.f.Network)}

	subnet := &computebeta.Subnetwork{
		Name:        name,
		IpCidrRange: ipCidrRange,
		Purpose:     purpose,
		Network:     networkID.SelfLink(meta.VersionBeta),
		Role:        "ACTIVE",
	}

	err := s.f.Cloud.BetaSubnetworks().Insert(context.Background(), meta.RegionalKey(subnet.Name, s.f.Region), subnet)
	if err != nil {
		// GCE returns a 409 when the subnet *with the same name* already exists
		if utils.IsHTTPErrorCode(err, http.StatusConflict) {
			klog.V(3).Infof("subnet %s already exists: %v", name, err)
			return ErrSubnetExists
		}
		return fmt.Errorf("Error creating subnet %s: %v", name, err)
	}

	klog.V(3).Infof("Subnet %s created in region %q: %v", name, s.f.Region, subnet)
	return nil
}

// DeleteSubnet deletes the subnet
func DeleteSubnet(s *Sandbox, name string) error {
	klog.V(2).Infof("Deleting Subnet %q", name)
	return s.f.Cloud.BetaSubnetworks().Delete(context.Background(), meta.RegionalKey(name, s.f.Region))
}

// CreatePorterDeployment creates a Deployment with porter image.
func CreatePorterDeployment(s *Sandbox, name string, replics int32, version string) error {
	env := fmt.Sprintf("SERVE_PORT_%d", porterPort)
	labels := map[string]string{"app": "porter", "version": version}
	deployment := apiappsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: s.Namespace, Name: name},
		Spec: apiappsv1.DeploymentSpec{
			Replicas: &replics,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:  "hostname",
							Image: "gcr.io/kubernetes-e2e-test-images/porter-alpine:1.0",
							Env:   []apiv1.EnvVar{{Name: env, Value: env}},
							Ports: []apiv1.ContainerPort{{Name: "server", ContainerPort: porterPort}},
						},
					},
				},
			},
		},
	}
	_, err := s.f.Clientset.AppsV1().Deployments(s.Namespace).Create(context.TODO(), &deployment, metav1.CreateOptions{})
	return err
}

// CreatePorterService creates a service that refers to Porter pods.
func CreatePorterService(s *Sandbox, name string) error {
	svc := apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: s.Namespace, Name: name},
		Spec: apiv1.ServiceSpec{
			Selector: map[string]string{"app": "porter"},
			Ports: []apiv1.ServicePort{
				{
					Port: porterPort,
					Name: "http",
				},
			},
		},
	}
	_, err := s.f.Clientset.CoreV1().Services(svc.Namespace).Create(context.TODO(), &svc, metav1.CreateOptions{})
	return err
}

// GetConfigMap gets ConfigMap and returns the Data field.
func GetConfigMap(s *Sandbox, namespace, name string) (map[string]string, error) {
	cm, err := s.f.Clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cm.Data, nil
}

// EnsureConfigMap ensures the namespace:name ConfigMap Data fieled, create if the target not exist.
func EnsureConfigMap(s *Sandbox, namespace, name string, data map[string]string) error {
	cm := v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Data: data}
	_, err := s.f.Clientset.CoreV1().ConfigMaps(namespace).Update(context.TODO(), &cm, metav1.UpdateOptions{})
	if err != nil && errors.IsNotFound(err) {
		_, err = s.f.Clientset.CoreV1().ConfigMaps(namespace).Create(context.TODO(), &cm, metav1.CreateOptions{})
	}
	return err
}

// DeleteConfigMap deletes the namespace:name ConfigMap
func DeleteConfigMap(s *Sandbox, namespace, name string) error {
	return s.f.Clientset.CoreV1().ConfigMaps(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

// EnsurePorterDestinationRule ensures the namespace:name DestinationRule.
func EnsurePorterDestinationRule(s *Sandbox, name, svcName string, versions []string) error {
	destinationRule := istioV1alpha3.DestinationRule{}
	subset := []*istioV1alpha3.Subset{}
	for _, v := range versions {
		subset = append(subset, &istioV1alpha3.Subset{Name: v, Labels: map[string]string{"version": v}})
	}
	destinationRule.Subsets = subset
	destinationRule.Host = svcName
	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&destinationRule)
	if err != nil {
		return fmt.Errorf("Failed convert DestinationRule to Unstructured: %v", err)
	}

	usDr, err := s.f.DestinationRuleClient.Namespace(s.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		usDr := unstructured.Unstructured{}
		usDr.SetName(name)
		usDr.SetNamespace(s.Namespace)
		usDr.SetKind("DestinationRule")
		usDr.SetAPIVersion("networking.istio.io/v1alpha3")
		usDr.Object["spec"] = spec

		_, err = s.f.DestinationRuleClient.Namespace(s.Namespace).Create(context.TODO(), &usDr, metav1.CreateOptions{})
		return err
	}
	usDr.Object["spec"] = spec
	_, err = s.f.DestinationRuleClient.Namespace(s.Namespace).Update(context.TODO(), usDr, metav1.UpdateOptions{})
	return err
}

// DeleteDestinationRule deletes the namespace:name DestinationRule.
func DeleteDestinationRule(s *Sandbox, namespace, name string) error {
	return s.f.DestinationRuleClient.Namespace(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

// EnsureServiceAttachment ensures a ServiceAttachment resource
func EnsureServiceAttachment(s *Sandbox, saName, svcName, subnetName string) (*sav1beta1.ServiceAttachment, error) {
	sa := &sav1beta1.ServiceAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name: saName,
		},
		Spec: sav1beta1.ServiceAttachmentSpec{
			ConnectionPreference: "ACCEPT_AUTOMATIC",
			NATSubnets:           []string{subnetName},
			ResourceRef: v1.TypedLocalObjectReference{
				APIGroup: utilpointer.StringPtr(""),
				Kind:     "service",
				Name:     svcName,
			},
		},
	}

	existingSA, err := s.f.SAClient.NetworkingV1beta1().ServiceAttachments(s.Namespace).Get(context.TODO(), saName, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		return s.f.SAClient.NetworkingV1beta1().ServiceAttachments(s.Namespace).Create(context.TODO(), sa, metav1.CreateOptions{})
	}
	existingSA.Spec = sa.Spec
	return s.f.SAClient.NetworkingV1beta1().ServiceAttachments(s.Namespace).Update(context.TODO(), existingSA, metav1.UpdateOptions{})
}

// DeleteServiceAttachment ensures a ServiceAttachment resource
func DeleteServiceAttachment(s *Sandbox, saName string) error {
	return s.f.SAClient.NetworkingV1beta1().ServiceAttachments(s.Namespace).Delete(context.TODO(), saName, metav1.DeleteOptions{})
}

/*
Copyright 2017 The Kubernetes Authors.

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

package app

import (
	"strings"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
)

func DefaultBackendServicePort(kubeClient kubernetes.Interface) *utils.ServicePort {
	// TODO: make this not fatal
	if flags.F.DefaultSvc == "" {
		glog.Fatalf("Please specify --default-backend")
	}

	// Wait for the default backend Service. There's no pretty way to do this.
	parts := strings.Split(flags.F.DefaultSvc, "/")
	if len(parts) != 2 {
		glog.Fatalf("Default backend should take the form namespace/name: %v",
			flags.F.DefaultSvc)
	}
	port, nodePort, err := getNodePort(kubeClient, parts[0], parts[1])
	if err != nil {
		glog.Fatalf("Could not configure default backend %v: %v",
			flags.F.DefaultSvc, err)
	}

	return &utils.ServicePort{
		NodePort: int64(nodePort),
		Protocol: annotations.ProtocolHTTP, // The default backend is HTTP.
		SvcName:  types.NamespacedName{Namespace: parts[0], Name: parts[1]},
		SvcPort:  intstr.FromInt(int(port)),
	}
}

// getNodePort waits for the Service, and returns its first node port.
func getNodePort(client kubernetes.Interface, ns, name string) (port, nodePort int32, err error) {
	glog.V(2).Infof("Waiting for %v/%v", ns, name)

	var svc *v1.Service
	wait.Poll(1*time.Second, 5*time.Minute, func() (bool, error) {
		svc, err = client.Core().Services(ns).Get(name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		for _, p := range svc.Spec.Ports {
			if p.NodePort != 0 {
				port = p.Port
				nodePort = p.NodePort
				glog.V(3).Infof("Node port %v", nodePort)
				break
			}
		}
		return true, nil
	})
	return
}

/*
Copyright 2020 The Kubernetes Authors.

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

package utils

import (
	"bytes"
	"os"
	"path/filepath"
	"text/template"

	"k8s.io/klog"
)

const kubeConfigUserTemp = `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: {{.clusterCa}}
    server: https://{{.clusterIP}}
  name: {{.clusterName}}
contexts:
- context:
    cluster: {{.clusterName}}
    user: {{.clusterName}}
  name: {{.clusterName}}
current-context: {{.clusterName}}
kind: Config
preferences: {}
users:
- name: {{.clusterName}}
  user:
    auth-provider:
      config:
        cmd-args: get-credentials
        cmd-path: {{.path}}
        expiry-key: '{.token_expiry}'
        token-key: '{.access_token}'
      name: {{.authProvider}}`

const kubeConfigKsaTemp = `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: {{.clusterCa}}
    server: https://{{.clusterIP}}
  name: {{.clusterName}}
contexts:
- context:
    cluster: {{.clusterName}}
    user: {{.saName}}
  name: {{.clusterName}}
current-context: {{.clusterName}}
kind: Config
preferences: {}
users:
- name: {{.saName}}
  user:
    token: {{.accessToken}}`

// GenKubeConfigForKSA generates a KubeConfig to access the cluster using a Kubernetes service account
func GenKubeConfigForKSA(clusterCa, clusterIP, clusterName, saName, accessToken string) []byte {
	var kubeConfig bytes.Buffer
	t, err := template.New("user").Parse(kubeConfigUserTemp)
	if err != nil {
		klog.Fatalf("unablt to create KubeConfig template: %+v", err)
	}
	err = t.Execute(&kubeConfig, map[string]string{
		"clusterCa":   clusterCa,
		"clusterIP":   clusterIP,
		"clusterName": clusterName,
		"saName":      saName,
		"accessToken": accessToken,
	})
	if err != nil {
		klog.Fatalf("unablt to execute KubeConfig template: %+v", err)
	}
	return kubeConfig.Bytes()
}

// GenKubeConfigForUser generates a KubeConfig to access the cluster using a third-party identity
func GenKubeConfigForUser(clusterCa, clusterIP, clusterName, authProvider string) []byte {
	pwd, err := os.Getwd()
	if err != nil {
		klog.Fatalf("failed to get current dir: %+v", err)
	}
	path := filepath.Join(pwd, os.Args[0])

	var kubeConfig bytes.Buffer
	t, err := template.New("user").Parse(kubeConfigUserTemp)
	if err != nil {
		klog.Fatalf("unablt to create KubeConfig template: %+v", err)
	}
	err = t.Execute(&kubeConfig, map[string]string{
		"clusterCa":    clusterCa,
		"clusterIP":    clusterIP,
		"clusterName":  clusterName,
		"path":         path,
		"authProvider": authProvider,
	})
	if err != nil {
		klog.Fatalf("unablt to execute KubeConfig template: %+v", err)
	}
	return kubeConfig.Bytes()
}

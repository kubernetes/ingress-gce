package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/klog"
)

const kubeConfigUserTemp = `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: %[1]s
    server: https://%[2]s
  name: %[3]s
contexts:
- context:
    cluster: %[3]s
    user: %[3]s
  name: %[3]s
current-context: %[3]s
kind: Config
preferences: {}
users:
- name: %[3]s
  user:
    auth-provider:
      config:
        cmd-args: get-credentials
        cmd-path: %[4]s
        expiry-key: '{.token_expiry}'
        token-key: '{.access_token}'
      name: %[5]s`

const kubeConfigKsaTemp = `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: %[1]s
    server: https://%[2]s
  name: %[3]s
contexts:
- context:
    cluster: %[3]s
    user: %[4]s
  name: %[3]s
current-context: %[3]s
kind: Config
preferences: {}
users:
- name: %[4]s
  user:
    token: %[5]s`

// GenKubeConfigForKSA generates a KubeConfig to access the cluster using a Kubernetes service account
func GenKubeConfigForKSA(clusterCa, clusterIP, clusterName, saName, accessToken string) []byte {
	kubeConfig := fmt.Sprintf(kubeConfigKsaTemp, clusterCa, clusterIP, clusterName, saName, accessToken)
	return []byte(kubeConfig)
}

// GenKubeConfigForUser generates a KubeConfig to access the cluster using a third-party identity
func GenKubeConfigForUser(clusterCa, clusterIP, clusterName, authProvider string) []byte {
	pwd, err := os.Getwd()
	if err != nil {
		klog.Fatalf("failed to get current dir: %+v", err)
	}
	path := filepath.Join(pwd, os.Args[0])

	kubeConfig := fmt.Sprintf(kubeConfigUserTemp, clusterCa, clusterIP, clusterName, path, authProvider)
	return []byte(kubeConfig)
}

// OutputCredentials prints the credentials to stdout
func OutputCredentials(credentials ClusterCredentials) {
	ret, err := json.Marshal(credentials)
	if err != nil {
		klog.Fatalf("unable to serialize credentials: %+v", err)
	}
	fmt.Println(string(ret))
}

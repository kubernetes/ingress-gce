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

package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/kr/pretty"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	backendconfig "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/fuzz/whitebox"

	// Pull in the auth library for GCP.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

var (
	validateOptions struct {
		kubeconfig   string
		ns           string
		name         string
		listFeatures bool
		featureRegex string
		project      string
	}
	// ValidateFlagSet is the flag set for the validate subcommand.
	ValidateFlagSet = flag.NewFlagSet("validate", flag.ExitOnError)
)

func init() {
	if home := homeDir(); home != "" {
		ValidateFlagSet.StringVar(&validateOptions.kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		ValidateFlagSet.StringVar(&validateOptions.kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
	ValidateFlagSet.StringVar(&validateOptions.name, "name", "", "name of the Ingress object to validate")
	ValidateFlagSet.StringVar(&validateOptions.ns, "ns", "default", "namespace of the Ingress object to validate")
	ValidateFlagSet.BoolVar(&validateOptions.listFeatures, "listFeatures", false, "list features available to be validated")
	ValidateFlagSet.StringVar(&validateOptions.featureRegex, "featureRegex", "", "features matching regex will be included in validation")
	ValidateFlagSet.StringVar(&validateOptions.project, "project", "", "GCP project where the load balancer will be created")

	// Merges in the global flags into the subcommand FlagSet.
	flag.VisitAll(func(f *flag.Flag) {
		ValidateFlagSet.Var(f.Value, f.Name, f.Usage)
	})
}

// Validate the load balancer matches the Ingress spec.
func Validate() {
	if validateOptions.listFeatures {
		fmt.Println("Feature names:")
		for _, f := range features.All {
			fmt.Println(f.Name())
		}
		os.Exit(0)
	}

	for _, o := range []struct {
		flag, val string
	}{
		{validateOptions.name, "-name"},
		{validateOptions.project, "-project"},
	} {
		if o.val == "" {
			fmt.Fprintf(ValidateFlagSet.Output(), "You must specify the %s flag.\n", o.flag)
			os.Exit(1)
		}
	}

	config, err := clientcmd.BuildConfigFromFlags("", validateOptions.kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	gce, err := e2e.NewCloud(validateOptions.project, "")
	if err != nil {
		panic(err)
	}

	env, err := fuzz.NewDefaultValidatorEnv(config, validateOptions.ns, gce)

	if err != nil {
		panic(err)
	}

	var fs []fuzz.Feature
	if validateOptions.featureRegex == "" {
		fs = features.All
	} else {
		fregexp := regexp.MustCompile(validateOptions.featureRegex)
		for _, f := range features.All {
			if fregexp.Match([]byte(f.Name())) {
				fs = append(fs, f)
			}
		}
	}
	var fsNames []string
	for _, f := range fs {
		fsNames = append(fsNames, f.Name())
	}
	fmt.Printf("Features = %v\n\n", fsNames)

	k8s := k8sClientSet(config)
	ing, err := k8s.NetworkingV1().Ingresses(validateOptions.ns).Get(context.TODO(), validateOptions.name, metav1.GetOptions{})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Ingress =\n%s\n\n", pretty.Sprint(*ing))

	iv, err := fuzz.NewIngressValidator(env, ing, nil, whitebox.AllTests, nil, fs)
	if err != nil {
		panic(err)
	}

	result := iv.Check(context.Background())
	fmt.Printf("Result =\n%s\n", pretty.Sprint(*result))

	if result.Err != nil {
		os.Exit(1)
	}

	vip := ing.Status.LoadBalancer.Ingress[0].IP
	params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
	gclb, err := fuzz.GCLBForVIP(context.Background(), gce, params)
	if err != nil {
		panic(err)
	}
	fmt.Printf("GCP resources = \n%s\n", pretty.Sprint(gclb))

	if err := iv.PerformWhiteboxTests(gclb); err != nil {
		panic(err)
	}

	if err := iv.FrontendNamingSchemeTest(gclb); err != nil {
		panic(err)
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func k8sClientSet(config *rest.Config) *kubernetes.Clientset {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientset
}

func backendConfigClientset(config *rest.Config) *backendconfig.Clientset {
	clientset, err := backendconfig.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientset
}

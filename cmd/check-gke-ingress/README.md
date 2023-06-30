# Overview

check-gke-ingress is a CLI to inspect ingress misconfiguration in GKE clusters.

## Build and install

### Install with makefile
Before this, you will need to have docker installed and docker daemon started. Also, you will need to know your machine archtecture.
You can learn your machine architecture using `uname -m`, and find the corresponding GOARCH value [here](https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63#goarch-values). 

For linux machine:
```
make build CONTAINER_BINARIES="check-gke-ingress" ARCH=<your-arch>
sudo chmod +x bin/<your-arch>/check-gke-ingress
sudo mv bin/<your-arch>/check-gke-ingress /usr/local/bin
```

For Macbook:
```
sudo make build OS="darwin" CONTAINER_BINARIES="check-gke-ingress" ARCH=<your-arch>
sudo chmod +x .go/bin/darwin_<your-arch>/check-gke-ingress
sudo mv .go/bin/darwin_<your-arch>/check-gke-ingress /usr/local/bin
```

### Install with go build
Before this, you will need to have Go installed.

```
cd cmd/check-gke-ingress
go build
sudo chmod +x check-gke-ingress
sudo mv check-gke-ingress /usr/local/bin
```

## Usage

### Prerequisites

Before running the binary, make sure you have your gcloud and GKE cluster authenticated: 

```
gcloud auth application-default login
gcloud container clusters get-credentials name-of-your-cluster
```

### Check all ingress

You can run the command after installation
```
check-gke-ingress
```
By default, `check-gke-ingress` will inspect all ingresses of the GKE cluster in current kubectl config.
It will print all check results in json format like this:
```
{
  "resources": [
    {
      "kind": "Ingress",
      "namespace": "default",
      "name": "ingress-1",
      "checks": [
        {
          "name": "IngressRuleCheck",
          "message": "IngressRule has no field `http`",
          "result": "FAILED"
        },
        {
          "name": "L7ILBFrontendConfigCheck",
          "message": "Ingress default/ingress-1 is not for L7 internal load balancing",
          "result": "SKIPPED"
        },
        {
          "name": "ServiceExistenceCheck",
          "message": "Service default/svc-1 found",
          "result": "PASSED"
        },
      ]
    },
    {
      "kind": "Ingress",
      "namespace": "test",
      "name": "internal-ingress",
      "checks": [
        {
          "name": "IngressRuleCheck",
          "message": "IngressRule has field `http`",
          "result": "PASSED"
        },
        {
          "name": "L7ILBFrontendConfigCheck",
          "message": "Ingress test/internal-ingress for L7 internal load balancing has a frontendConfig annotation, frontendConfig can only be used with external ingresses",
          "result": "FAILED"
        }
      ]
    }
  ]
}
```

`resources` is the list of resources which are inspected by the tool, only ingress is supported in this tool.   
`kind` is the kind of the kubernetes resource being inspected.   
`namespace` is the namespace of the kubernetes resource being inspected.  
`name` is the name of the kubernetes resource being inspected.  
`checks` is the list of checks on the resource.   

### Check a specific ingress
To inspect a specific ingress, you can add the ingress name you want to check as an argument and specify the namespace of that ingress:
```
check-gke-ingress <your-ingress-name> --namespace <your-namespace>
```
The output will be the same as checking all ingresses.

### Flags

```
-k, --kubeconfig string         kubeconfig file to use for Kubernetes config
-c, --context string            context to use for Kubernetes config
-n, --namespace string          only include pods from this namespace
```

## Development

### Add new check rules
There are four kinds of check functions defined: `ingressCheckFunc`, `serviceCheckFunc`, `backendConfigCheckFunc`, `frontendConfigCheckFunc`. 
To add a new rule for those resources, create a check function accroding to the function type defined in [rule.go](app/ingress/rule.go), 
and add the new check rule function to the corresponding list defined in [ingress.go](app/ingress/ingress.go).

To add new checks for resources other than `ingress`, `service`, `backendConfig` and `frontendConfig`, you will need to define new
function types and new checker structs:
```
type fooCheckFunc func(c *FooChecker) (string, string, string)

type FooChecker struct {
	// foo client
	client client.Interface
	// Namespace of foo resource 
	namespace string
	// Name of the foo resource 
	name string
	// Foo resource object to be checked
	feConfig *foov1.foo
}

```

### Tests
For each newly added check rule, you will need to add an individual rule test in [rule_test.go](app/ingress/rule_test.go) and update the `TestCheckAllIngresses` test to include the result check for your new rule.






# End-to-end tests

The executable built here (`e2e-test`) is intended to be run manually by a
developer using a cluster and GCP credentials as well as in a container from
within a cluster that has the right service account credentials.

Tests should be written using the standard Golang unit test framework.
`basic_test.go` is an example of a simple test that creates an Ingress,
validates the configuration, deletes the Ingress and waits for the associated
cloud resources to be removed.

Tests have access to the global variable `Framework` that contains a k8s
clientset, GCP Cloud client to interact with the environment under test. Per
namespace sandboxes should be created with `Framework.RunWithSandbox()`. This
is similar in behavior to `t.Run()` but includes creation of the sandbox:

```go
Framework.RunWithSandbox("my test", t, func(t*testing.T, s *e2e.Sandbox) {
  t.Parallel()
  // Do your test here.
})
```

As GCLB provisioning may take some time, it is important to perform your tests
in parallel as much as possible. The easiest way to do this is to put a
`t.Parallel()` invocation at the start of the `func TestFoo(t *testing.T)` and
immediately within each call to `RunWithSandbox()`.

## Running the tests

### Building the tests

For Linux users, the `e2e-test` binary will be built in a container using the
build make target:

```console
make build
```

For Mac Users, or to build the tests locally:
```console
go test -c -o bin/amd64/e2e-test k8s.io/ingress-gce/cmd/e2e-test
```

### From the command line

Run tests against your cluster and GCP environment. This uses your default
cluster credentials. `-v` and `-logtostderr` will enable verbose logging:

```go
$ bin/amd64/e2e-test -run -project my-project -v 2 -logtostderr -region my-region -network my-network

Version: "v1.1.0-183-gfaefb0f2", Commit: "faefb0f257a6c591d19a4768e3a8bc776ad14d33"
I0618 23:51:03.023843   62186 main_test.go:101] Using random seed = 1529391063023834550
I0618 23:51:03.024330   62186 framework.go:104] Catching SIGINT
I0618 23:51:03.024411   62186 framework.go:79] Checking connectivity with Kubernetes API
I0618 23:51:03.464677   62186 framework.go:84] Checking connectivity with Google Cloud API (get project "my-project")
I0618 23:51:04.068408   62186 framework.go:89] Checking external Internet connectivity
...
```

To run a specific test case, you can use something similar to the command below:

```bash
# Ref https://golang.org/cmd/go/#hdr-Testing_flags.
$ bin/amd64/e2e-test -run -project my-project -v 2 -logtostderr -region my-region -network my-network -test.run=TestIAP
```

Note that killing the test with `CTRL-C` will cause the existing namespace
sandboxes to be deleted, hopefully reducing the amount of cleanup necessary on
an aborted test run:

```text
^C
W0618 23:51:04.157786   62186 framework.go:120] SIGINT received, cleaning up sandboxes (disable with -handleSIGINT=false)
E0618 23:51:04.157869   62186 framework.go:129] Exiting due to SIGINT
```

To prevent the test from cleaning up the namespace sandboxes (for debugging purposes), use the flag `-destroySandboxes=false`.

### Within a cluster

The YAML resource definition `cmd/e2e-test/e2e-test.yaml` will create RBAC
bindings and a pod built with the end-to-end test binary. Build and push the
image to your own registry:

```shell
$ REGISTRY=gcr.io/my-reg make push
$ sed 's/k8s-ingress-image-push/my-reg/g' cmd/e2e-test/e2e-test.yaml > my-e2e-test.yaml
$ kubectl apply -f my-e2e-test.yaml

# Check the test results:
$ kubectl logs -n default ingress-e2e
```

Note: e2e-test.yaml assumes your image is build from the tip of `master` branch.  If you are using a different tag or version, update the image tag in e2e-test.yaml accordingly.

## Limitations

The tests in [`ilb_test.go`](ilb_test.go) cannot be run from your local machine because they need access to the Internal HTTP(S) Load Balancer VIP provisioned by the test.  There are two ways to run these tests:

1) Run the e2e-test pod in the cluster
2) Run the e2e-test binary from a VM located inside the same VPC and Region as the [ILB Proxy-Only Subnet](https://cloud.google.com/load-balancing/docs/proxy-only-subnets)

If your VPC and Region does not already have a proxy-only subnet, the test binary can create one with the flag `createILBSubnet=true`

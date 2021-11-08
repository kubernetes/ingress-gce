<!--
-----------------NOTICE------------------------
This file is referenced in code as
https://github.com/kubernetes/ingress/blob/master/docs/troubleshooting.md
Do not move it without providing redirects.
-----------------------------------------------
-->

# Troubleshooting

## General

This controller is complicated because it exposes a tangled set of external resources as a single logical abstraction. It's recommended that you are at least *aware* of how one creates a GCE L7 [without a kubernetes Ingress](https://cloud.google.com/container-engine/docs/tutorials/http-balancer). If weird things happen, here are some basic debugging guidelines:

* Check loadbalancer controller pod logs via kubectl
A typical sign of trouble is repeated retries in the logs:
```shell
I1006 18:58:53.451869       1 loadbalancer.go:268] Forwarding rule k8-fw-default-echomap already exists
I1006 18:58:53.451955       1 backends.go:162] Syncing backends [30301 30284 30301]
I1006 18:58:53.451998       1 backends.go:134] Deleting backend k8-be-30302
E1006 18:58:57.029253       1 utils.go:71] Requeuing default/echomap, err googleapi: Error 400: The backendService resource 'projects/Kubernetesdev/global/backendServices/k8-be-30302' is already being used by 'projects/Kubernetesdev/global/urlMaps/k8-um-default-echomap'
I1006 18:58:57.029336       1 utils.go:83] Syncing default/echomap
```

This could be a bug or quota limitation. In the case of the former, please head over to slack or github.

* If you see a GET hanging, followed by a 502 with the following response:

```
<html><head>
<meta http-equiv="content-type" content="text/html;charset=utf-8">
<title>502 Server Error</title>
</head>
<body text=#000000 bgcolor=#ffffff>
<h1>Error: Server Error</h1>
<h2>The server encountered a temporary error and could not complete your request.<p>Please try again in 30 seconds.</h2>
<h2></h2>
</body></html>
```
The loadbalancer is probably bootstrapping itself.

* If a GET responds with a 404 and the following response:
```
  <a href=//www.google.com/><span id=logo aria-label=Google></span></a>
  <p><b>404.</b> <ins>That’s an error.</ins>
  <p>The requested URL <code>/hostless</code> was not found on this server.  <ins>That’s all we know.</ins>
```
It means you have lost your IP somehow, or just typed in the wrong IP.

* If you see requests taking an abnormal amount of time, run the echoheaders pod and look for the client address
```shell
CLIENT VALUES:
client_address=('10.240.29.196', 56401) (10.240.29.196)
```

Then head over to the GCE node with internal IP 10.240.29.196 and check that the [Service is functioning](https://github.com/kubernetes/kubernetes/blob/release-1.0/docs/user-guide/debugging-services.md) as expected. Remember that the GCE L7 is routing you through the NodePort service, and try to trace back.

* Check if you can access the backend service directly via nodeip:nodeport
* Check the GCE console
* Make sure you only have a single loadbalancer controller running
* Make sure the initial GCE health checks have passed
* A crash loop looks like:
```shell
$ kubectl get pods
glbc-fjtlq             0/1       CrashLoopBackOff   17         1h
```
If you hit that it means the controller isn't even starting. Re-check your input flags, especially the required ones.


## Authentication to the Kubernetes API Server


A number of components are involved in the authentication process and the first step is to narrow
down the source of the problem, namely whether it is a problem with service authentication or with the kubeconfig file.
Both authentications must work:

```
+-------------+   service          +------------+
|             |   authentication   |            |
+  apiserver  +<-------------------+  ingress   |
|             |                    | controller |
+-------------+                    +------------+

```

__Service authentication__

The Ingress controller needs information from the Kubernetes API Server. Therefore, authentication is required, which can be achieved in two different ways:

1. _Service Account:_ This is recommended, because nothing has to be configured. The Ingress controller will use information provided by the system to communicate with the API server. See 'Service Account' section for details.

2. _Kubeconfig file:_ In some Kubernetes environments service accounts are not available. In this case a manual configuration is required. The Ingress controller binary can be started with the `--kubeconfig` flag. The value of the flag is a path to a file specifying how to connect to the API server. Using the `--kubeconfig` does not requires the flag `--apiserver-host`.
The format of the file is identical to `~/.kube/config` which is used by kubectl to connect to the API server. See 'kubeconfig' section for details.

3. _Using the flag `--apiserver-host`:_ Using this flag `--apiserver-host=http://localhost:8080` it is possible to specify an unsecure API server or reach a remote Kubernetes cluster using [kubectl proxy](https://kubernetes.io/docs/user-guide/kubectl/kubectl_proxy/).
Please do not use this approach in production.

In the diagram below you can see the full authentication flow with all options, starting with the browser
on the lower left hand side.
```

Kubernetes                                                  Workstation
+---------------------------------------------------+     +------------------+
|                                                   |     |                  |
|  +-----------+   apiserver        +------------+  |     |  +------------+  |
|  |           |   proxy            |            |  |     |  |            |  |
|  | apiserver |                    |  ingress   |  |     |  |  ingress   |  |
|  |           |                    | controller |  |     |  | controller |  |
|  |           |                    |            |  |     |  |            |  |
|  |           |                    |            |  |     |  |            |  |
|  |           |  service account/  |            |  |     |  |            |  |
|  |           |  kubeconfig        |            |  |     |  |            |  |
|  |           +<-------------------+            |  |     |  |            |  |
|  |           |                    |            |  |     |  |            |  |
|  +------+----+      kubeconfig    +------+-----+  |     |  +------+-----+  |
|         |<--------------------------------------------------------|        |
|                                                   |     |                  |
+---------------------------------------------------+     +------------------+
```


## Service Account
If using a service account to connect to the API server, Dashboard expects the file
`/var/run/secrets/kubernetes.io/serviceaccount/token` to be present. It provides a secret
token that is required to authenticate with the API server.

Verify with the following commands:

```shell
# start a container that contains curl
$ kubectl run test --image=tutum/curl -- sleep 10000

# check that container is running
$ kubectl get pods
NAME                   READY     STATUS    RESTARTS   AGE
test-701078429-s5kca   1/1       Running   0          16s

# check if secret exists
$ kubectl exec test-701078429-s5kca ls /var/run/secrets/kubernetes.io/serviceaccount/
ca.crt
namespace
token

# get service IP of master
$ kubectl get services
NAME         CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
kubernetes   10.0.0.1     <none>        443/TCP   1d

# check base connectivity from cluster inside
$ kubectl exec test-701078429-s5kca -- curl -k https://10.0.0.1
Unauthorized

# connect using tokens
$ TOKEN_VALUE=$(kubectl exec test-701078429-s5kca -- cat /var/run/secrets/kubernetes.io/serviceaccount/token)
$ echo $TOKEN_VALUE
eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3Mi....9A
$ kubectl exec test-701078429-s5kca -- curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H  "Authorization: Bearer $TOKEN_VALUE" https://10.0.0.1
{
  "paths": [
    "/api",
    "/api/v1",
    "/apis",
    "/apis/apps",
    "/apis/apps/v1alpha1",
    "/apis/authentication.k8s.io",
    "/apis/authentication.k8s.io/v1beta1",
    "/apis/authorization.k8s.io",
    "/apis/authorization.k8s.io/v1beta1",
    "/apis/autoscaling",
    "/apis/autoscaling/v1",
    "/apis/batch",
    "/apis/batch/v1",
    "/apis/batch/v2alpha1",
    "/apis/certificates.k8s.io",
    "/apis/certificates.k8s.io/v1alpha1",
    "/apis/extensions",
    "/apis/extensions/v1beta1",
    "/apis/policy",
    "/apis/policy/v1alpha1",
    "/apis/rbac.authorization.k8s.io",
    "/apis/rbac.authorization.k8s.io/v1alpha1",
    "/apis/storage.k8s.io",
    "/apis/storage.k8s.io/v1beta1",
    "/healthz",
    "/healthz/ping",
    "/logs",
    "/metrics",
    "/swaggerapi/",
    "/ui/",
    "/version"
  ]
}
```

If it is not working, there are two possible reasons:

1. The contents of the tokens are invalid. Find the secret name with `kubectl get secrets | grep service-account` and
delete it with `kubectl delete secret <name>`. It will automatically be recreated.

2. You have a non-standard Kubernetes installation and the file containing the token
may not be present. The API server will mount a volume containing this file, but
only if the API server is configured to use the ServiceAccount admission controller.
If you experience this error, verify that your API server is using the ServiceAccount
admission controller. If you are configuring the API server by hand, you can set
this with the `--admission-control` parameter. Please note that you should use
other admission controllers as well. Before configuring this option, you should
read about admission controllers.

More information:

* [User Guide: Service Accounts](http://kubernetes.io/docs/user-guide/service-accounts/)
* [Cluster Administrator Guide: Managing Service Accounts](http://kubernetes.io/docs/admin/service-accounts-admin/)

## Kubeconfig
If you want to use a kubeconfig file for authentication, create a deployment file similar to the one below:

*Note:* the important part is the flag `--kubeconfig=/etc/kubernetes/kubeconfig.yaml`.


```
kind: Service
apiVersion: v1
metadata:
  name: nginx-default-backend
  labels:
    k8s-addon: ingress-nginx.addons.k8s.io
spec:
  ports:
  - port: 80
    targetPort: http
  selector:
    app: nginx-default-backend

---

kind: Deployment
apiVersion: apps/v1
metadata:
  name: nginx-default-backend
  labels:
    k8s-addon: ingress-nginx.addons.k8s.io
spec:
  replicas: 1
  template:
    metadata:
      labels:
        k8s-addon: ingress-nginx.addons.k8s.io
        app: nginx-default-backend
    spec:
      terminationGracePeriodSeconds: 60
      containers:
      - name: default-http-backend
        image: k8s.gcr.io/defaultbackend:1.5
        volumeMounts:
        - mountPath: /etc/kubernetes
          name: kubeconfig
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
            scheme: HTTP
          initialDelaySeconds: 30
          timeoutSeconds: 5
        resources:
          limits:
            cpu: 10m
            memory: 20Mi
          requests:
            cpu: 10m
            memory: 20Mi
        ports:
        - name: http
          containerPort: 8080
          protocol: TCP

---

kind: ConfigMap
apiVersion: v1
metadata:
  name: ingress-nginx
  labels:
    k8s-addon: ingress-nginx.addons.k8s.io

---

kind: Service
apiVersion: v1
metadata:
  name: ingress-nginx
  labels:
    k8s-addon: ingress-nginx.addons.k8s.io
spec:
  type: LoadBalancer
  selector:
    app: ingress-nginx
  ports:
  - name: http
    port: 80
    targetPort: http
  - name: https
    port: 443
    targetPort: https

---

kind: Deployment
apiVersion: apps/v1
metadata:
  name: ingress-nginx
  labels:
    k8s-addon: ingress-nginx.addons.k8s.io
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: ingress-nginx
        k8s-addon: ingress-nginx.addons.k8s.io
    spec:
      terminationGracePeriodSeconds: 60
      containers:
      - image: gcr.io/google_containers/nginx-ingress-controller:0.9.0-beta.14
        name: ingress-nginx
        imagePullPolicy: Always
        ports:
          - name: http
            containerPort: 80
            protocol: TCP
          - name: https
            containerPort: 443
            protocol: TCP
        livenessProbe:
          httpGet:
            path: /healthz
            port: 10254
            scheme: HTTP
          initialDelaySeconds: 30
          timeoutSeconds: 5
        env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
        args:
        - /nginx-ingress-controller
        - --default-backend-service=$(POD_NAMESPACE)/nginx-default-backend
        - --configmap=$(POD_NAMESPACE)/ingress-nginx
        - --kubeconfig=/etc/kubernetes/kubeconfig.yaml
      volumes:
      - name: "kubeconfig"
        hostPath:
          path: "/etc/kubernetes/"
```

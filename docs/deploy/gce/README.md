# Overview

This guide walks you through how to deploy the Ingress-GCE controller in a kubernetes cluster on GCE.

## Build the images
To build the images, run
```
git clone https://github.com/kubernetes/ingress-gce.git
cd ingress-gce
make build
```

Upload the built images to Artifact Registry
```
export PROJECT_ID=<project-id>
export REGISTRY=gcr.io/${PROJECT_ID}
make push
```

## Prepare the config for the controller
Create a file [gce.conf](../resources/gce.conf).

`network-name` is the network where your cluster is in.  
`subnetwork-name` is the subnetwork where your cluster is in.  
`node-tags` is the network tag of your cluster’s instance group.  
`node-instance-prefix` is the prefix of your cluster’s instance group.  


Create a configmap from the file:
```
kubectl create configmap gce-config --from-file=gce.conf -n kube-system
```

## RBAC configs
Apply [rbac.yaml](../resources/rbac.yaml).

## Create default backend deployment and service

> [!NOTE]
> This is only needed if you're deploying the L7 controller for Ingress.

Replace `IMAGE_URL` with your `ingress-gce-404-server-with-metrics-amd64` image (for example, `gcr.io/my-project/ingress-gce-404-server-with-metrics-amd64:latest`) and apply [default-http-backend.yaml](../resources/default-http-backend.yaml).

## Create Google Service Account and generate a key

Create a service account
```
gcloud iam service-accounts create glbc-service-account \
  --display-name "Service Account for GLBC" --project $PROJECT_ID
```

Bind `compute.admin` role to the service account
```
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/compute.admin
```

Alternatively, you can narrow down the permission with 2 roles
```
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/compute.loadBalancerAdmin
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/compute.securityAdmin
```

Generate a SA key and store it as `key.json`
```
gcloud iam service-accounts keys create key.json \
  --iam-account="glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com" \
  --project=${PROJECT_ID}
```

Create a secret from the key
```
kubectl create secret generic glbc-gcp-key --from-file=key.json -n kube-system
```

## Create GLBC deployment
`ingress-gce` provides two primary functionalities: reconciling L7 Load Balancers (via Kubernetes `Ingress`) and L4 Load Balancers (via `Services` of `type: LoadBalancer`). The container also includes a Network Endpoint Group (NEG) controller. This component is a mandatory dependency for both the L4 and L7 controllers. The active components are controlled by the configuration flags.

To ensure operational isolation and easier troubleshooting, it is highly recommended to deploy the L7 and L4 controllers as separate instances.

### L4 Load Balancer Controller
[glbc-l4.yaml](../resources/glbc-l4.yaml) is an example manifest for GLBC that enables the L4 LB controller with recommended flags.
Replace the image placeholder `[IMAGE_URL]` with your own `ingress-gce-glbc-amd64` image.

### L7 Load Balancer Controller
[glbc-l7.yaml](../resources/glbc-l7.yaml) is an example manifest for GLBC that enables the L7 LB controller with recommended flags.
Replace the image placeholder `[IMAGE_URL]` with your own `ingress-gce-glbc-amd64` image.

## Test GLBC

### L4 External LB
Apply the following configs:
```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: store
spec:
  replicas: 1
  selector:
    matchLabels:
      app: store
  template:
    metadata:
      labels:
        app: store
    spec:
      containers:
      - image: gcr.io/google_containers/echoserver:1.10
        imagePullPolicy: Always
        name: echoserver
        ports:
          - name: http
            containerPort: 8080
        readinessProbe:
          httpGet:
            path: /healthz
            port: 8080
            scheme: HTTP
---
apiVersion: v1
kind: Service
metadata:
  name: store-v1-lb-svc
spec:
  type: LoadBalancer
  loadBalancerClass: networking.gke.io/l4-regional-external
  selector:
    app: store
  ports:
  - name: tcp-port
    protocol: TCP
    port: 8080
    targetPort: 8080
```

Wait until there is an IP for the service
```
$ kubectl get svc
NAME              TYPE           CLUSTER-IP      EXTERNAL-IP       PORT(S)          AGE
kubernetes        ClusterIP      100.64.0.1      <none>            443/TCP          6d3h
store-v1-lb-svc   LoadBalancer   100.70.49.245   104.154.218.233   8080:32474/TCP   2d22h
```

Make a request to the public IP and port:
```
$ curl 104.154.218.233:8080


Hostname: store-5fbc45ff9d-ph6mk

Pod Information:
	-no pod information available-

Server values:
	server_version=nginx: 1.13.3 - lua: 10008

Request Information:
	client_address=104.135.180.78
	method=GET
	real path=/
	query=
	request_version=1.1
	request_scheme=http
	request_uri=http://104.154.219.234:8080/

Request Headers:
	accept=*/*
	host=104.154.219.234:8080
	user-agent=curl/8.16.0

Request Body:
	-no body in request-
```

### L4 Internal LB
Apply the following configs:
```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ilb-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: ilb-deployment
  template:
    metadata:
      labels:
        app: ilb-deployment
    spec:
      containers:
      - name: hello-app
        image: us-docker.pkg.dev/google-samples/containers/gke/hello-app:1.0
---
apiVersion: v1
kind: Service
metadata:
  name: ilb-svc
spec:
  type: LoadBalancer
  loadBalancerClass: networking.gke.io/l4-regional-internal
  # Evenly route external traffic to all endpoints.
  externalTrafficPolicy: Cluster
  # Prioritize routing traffic to endpoints that are in the same zone.
  trafficDistribution: PreferClose
  selector:
    app: ilb-deployment
  # Forward traffic from TCP port 80 to port 8080 in backend Pods.
  ports:
  - name: tcp-port
    protocol: TCP
    port: 80
    targetPort: 8080
```

Wait until there is an internal IP for the service:
```
$ kubectl get svc
NAME              TYPE           CLUSTER-IP      EXTERNAL-IP       PORT(S)          AGE
ilb-svc           LoadBalancer   100.66.66.20    10.0.16.5         80:31542/TCP     2d22h
kubernetes        ClusterIP      100.64.0.1      <none>            443/TCP          6d3h
```

Use a VM that is within the same network as the LB and make a request to the endpoint

```
$ curl 10.0.16.5:80
Hello, world!
Version: 1.0.0
Hostname: ilb-deployment-5cccfb4574-dpfcj
```

### L7 External LB

Apply the configs
```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  selector:
    matchLabels:
      run: web
  template:
    metadata:
      labels:
        run: web
    spec:
      containers:
      - image: us-docker.pkg.dev/google-samples/containers/gke/hello-app:1.0
        imagePullPolicy: IfNotPresent
        name: web
        ports:
        - containerPort: 8080
          protocol: TCP
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: default
spec:
  ports:
  - port: 8080
    protocol: TCP
    targetPort: 8080
  selector:
    run: web
  type: NodePort
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: basic-ingress
spec:
  defaultBackend:
    service:
      name: web
      port:
        number: 8080
```

Wait until there is an IP for the ingress
```
$ k get ingress basic-ingress
NAME            CLASS    HOSTS   ADDRESS         PORTS   AGE
basic-ingress   <none>   *       34.36.129.198   80      21h
```

Make a request to the public IP
```
$ curl 34.36.129.198
Hello, world!
Version: 1.0.0
Hostname: web-5b6c8d9476-p5q6k
```

### L7 Internal LB

Apply the config
```
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: hostname
  name: hostname-server
spec:
  selector:
    matchLabels:
      app: hostname
  minReadySeconds: 60
  replicas: 1
  template:
    metadata:
      labels:
        app: hostname
    spec:
      containers:
      - image: registry.k8s.io/serve_hostname
        name: hostname-server
        ports:
        - containerPort: 9376
          protocol: TCP
      terminationGracePeriodSeconds: 90
---
apiVersion: v1
kind: Service
metadata:
  name: hostname
  namespace: default
  annotations:
    cloud.google.com/neg: '{"ingress": true}'
spec:
  ports:
  - name: host1
    port: 80
    protocol: TCP
    targetPort: 9376
  selector:
    app: hostname
  type: ClusterIP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ilb-demo-ingress
  namespace: default
  annotations:
    kubernetes.io/ingress.class: "gce-internal"
spec:
  defaultBackend:
    service:
      name: hostname
      port:
        number: 80
```

Wait until there is an IP for the ingress
```
$ k get ingress ilb-demo-ingress
NAME               CLASS    HOSTS   ADDRESS      PORTS   AGE
ilb-demo-ingress   <none>   *       10.0.16.12   80      178m
```

Use a VM that is within the same network as the LB and make a request to the endpoint
```
$ curl 10.0.16.10
Hello, world!
Version: 1.0.0
Hostname: ilb-deployment-5cccfb4574-n6sm2
```

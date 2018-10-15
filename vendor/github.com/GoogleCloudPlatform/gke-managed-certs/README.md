# Managed Certificates in GKE

Managed Certificates in GKE simplify user flow in managing HTTPS traffic. Instead of manually acquiring an SSL certificate from a Certificate Authority, configuring it on the load balancer and renewing it on time, now it is only necessary to create a Managed Certificate k8s [Custom Resource object](https://kubernetes.io/docs/concepts/api-extension/custom-resources/) and provide a domain for which you want to obtain a certificate. The certificate will be auto-renewed when necessary.

For that to work you need to run your cluster on a platform with [Google Cloud Load Balancer](https://github.com/kubernetes/ingress-gce), that is a cluster in GKE or your own cluster in GCP.

# Installation

## Prerequisites

1. You need to use Kubernetes 1.10 or newer.
2. Configure your domain example.com so that it points at the load balancer created for your cluster by Ingress.

## Steps

To install Managed Certificates in your own cluster on GCP, you need to:

1. Deploy the Managed Certficate CRD  
```console
$ kubectl create -f deploy/managedcertificates-crd.yaml
```
2. Deploy the managed-certificate-controller  
```console
$ kubectl create -f deploy/managed-certificate-controller.yaml
```

# Usage

1. Create a Managed Certificate custom object, specifying a single non-wildcard domain not longer than 63 characters, for which you want to obtain a certificate:  
```
apiVersion: gke.googleapis.com/v1alpha1
kind: ManagedCertificate
metadata:
  name: example-certificate
spec:
  domains:
    - example.com
```
2. Configure Ingress to use this custom object to terminate SSL connections:  
```console
kubectl annotate ingress [your-ingress-name] gke.googleapis.com/managed-certificates example-certificate
```  
If you need, you can specify more multiple managed certificates here, separating their names with commas.

# Clean up

You can do the below steps in any order and doing even one of them will turn SSL off:

* Remove annotation from Ingress  
```console
kubectl annotate ingress [your-ingress-name] gke.googleapis.com/managed-certificates-
```  
(note the minus sign at the end of annotation name)
* Tear down the controller  
```console
$ kubectl delete -f deploy/managed-certificate-controller.yaml
```
* Tear down the Managed Certificate CRD  
```console
$ kubectl delete -f deploy/managedcertificates-crd.yaml
```

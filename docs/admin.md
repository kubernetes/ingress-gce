# Ingress Admin Guide

This is a guide to the different deployment styles of an Ingress controller.

## Vanillla deployments

__GKE__: On GKE, the Ingress controller runs on the
master. If you wish to stop this controller and run another instance on your
nodes instead, you can do so by following this [example](/examples/deployment/gce).

__Generic__: You can deploy a generic (nginx or haproxy) Ingress controller by simply
running it as a pod in your cluster, as shown in the [examples](/examples/deployment).
Please note that you must specify the `ingress.class`
[annotation](/examples/PREREQUISITES.md#ingress-class) if you're running on a
cloudprovider, or the cloudprovider controller will fight the nginx controller
for the Ingress.

## Stacked deployments

__Behind a LoadBalancer Service__: You can deploy a generic controller behind a
Service of `Type=LoadBalancer`, by following this [example](/examples/static-ip/nginx#acquiring-an-ip).
More specifically, first create a LoadBalancer Service that selects the generic
controller pods, then start the generic controller with the `--publish-service`
flag.


__Behind another Ingress__: Sometimes it is desirable to deploy a stack of
Ingresses, like the GCE Ingress -> nginx Ingress -> application. You might
want to do this because the GCE HTTP LB offers some features that the GCE
network LB does not, like a global static IP or CDN, but doesn't offer all the
features of nginx, like URL rewriting or redirects.

TODO: Write an example

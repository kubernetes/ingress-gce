# GLBC

[![GitHub release](https://img.shields.io/github/release/kubernetes/ingress-gce.svg)](https://github.com/kubernetes/ingress-gce/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/kubernetes/ingress-gce)](https://goreportcard.com/report/github.com/kubernetes/ingress-gce)

GLBC is a GCE L7 load balancer controller that manages external loadbalancers configured through the Kubernetes Ingress API.

## A word to the wise

Please read the [beta limitations](BETA_LIMITATIONS.md) doc to before using this controller. In summary:

- This is a **work in progress**.
- It relies on a beta Kubernetes resource.
- The loadbalancer controller pod is not aware of your GCE quota.

**If you are running a cluster on GKE and interested in trying out alpha releases of the GLBC before they are officially released please visit the deploy/glbc/ directory.**

## Overview

See [here](https://kubernetes.io/docs/concepts/services-networking/ingress/) for high-level concepts on Ingress in Kubernetes.

For GCP-specific documentation, please visit [here](https://cloud.google.com/kubernetes-engine/docs/how-to/load-balance-ingress) (core use-cases) and [here](https://cloud.google.com/kubernetes-engine/docs/concepts/ingress) (other cool features).

## Releases

Please visit the [changelog](CHANGELOG.md) for both high-level release notes and a detailed changelog.

## GKE Version Mapping

The table below describes what version of Ingress-GCE is running on GKE. Note that these versions are simply the defaults. Users still have the power to change the version manually if they want to (see deploy/).

   *Format: k8s version -> glbc version* ('+' indicates that version or above)

       * 1.9.6-gke.2+ -> 0.9.7
       * 1.9.7-gke.5+ -> 0.9.7
       * 1.10.4-gke.0+ -> v1.1.1
       * 1.10.5-gke.1+ -> v1.2.2
       * 1.10.5-gke.3+ -> v1.2.3
       * 1.10.6-gke.2+ -> v1.3.0
       * 1.10.7-gke.1+ -> v1.3.2
       * 1.11.2-gke.4+ -> v1.3.3
       * 1.11.3-gke.14+ -> v1.4.0
       * 1.11.6-gke.2+ -> v1.4.1
       * 1.11.6-gke.6+ -> v1.4.2
       * 1.11.7-gke.7+ -> v1.4.3

[![Analytics](https://kubernetes-site.appspot.com/UA-36037335-10/GitHub/contrib/service-loadbalancer/gce/README.md?pixel)]()

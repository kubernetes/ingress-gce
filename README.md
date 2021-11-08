# GLBC

[![GitHub release](https://img.shields.io/github/release/kubernetes/ingress-gce.svg)](https://github.com/kubernetes/ingress-gce/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/kubernetes/ingress-gce)](https://goreportcard.com/report/github.com/kubernetes/ingress-gce)

GLBC is a GCE L7 load balancer controller that manages external loadbalancers configured through the Kubernetes Ingress API.

## Overview

See [here](https://kubernetes.io/docs/concepts/services-networking/ingress/) for high-level concepts on Ingress in Kubernetes.

For GCP-specific documentation, please visit [here](https://cloud.google.com/kubernetes-engine/docs/how-to/load-balance-ingress) (core use-cases) and [here](https://cloud.google.com/kubernetes-engine/docs/concepts/ingress) (other cool features).

## Releases

Please visit the [changelog](CHANGELOG.md) for both high-level release notes and a detailed changelog.

## Documentation

Please visit our [docs](docs/) for more information on how to run, contribute, troubleshoot and much more!

## GKE Version Mapping

The table below describes what version of Ingress-GCE is running on GKE. Note that these versions are simply the defaults.

   *Format: k8s version -> glbc version* ('+' indicates that version or above)

       * 1.12.7-gke.16+ -> v1.5.2
       * 1.13.7-gke.5+ -> v1.6.0
       * 1.14.10-gke.31+ -> 1.6.2
       * 1.14.10-gke.42+ -> 1.6.4
       * 1.15.4-gke.21+ -> 1.7.2
       * 1.15.9-gke.22+ -> 1.7.3
       * 1.15.11-gke.15+ -> 1.7.4
       * 1.15.12-gke.3+ -> 1.7.5
       * 1.16.8-gke.3+ -> 1.9.1
       * 1.16.8-gke.12+ -> 1.9.2
       * 1.16.9-gke.2+ -> 1.9.3
       * 1.16.10-gke.6+ -> 1.9.7
       * 1.17.6-gke.11+ -> 1.9.7
       * 1.18.4-gke.1201+ -> 1.9.7
       * 1.16.13-gke.400+ -> 1.9.8
       * 1.17.9-gke.600+ -> 1.9.8
       * 1.18.6-gke.500+ -> 1.9.8
       * 1.18.6-gke.4800+ -> 1.9.9
       * 1.18.10-gke.1500+ -> 1.10.8
       * 1.18.10-gke.2300+ -> 1.10.9
       * 1.18.12-gke.1200+ -> 1.10.13
       * 1.18.18-gke.1200+ -> 1.10.15
       * 1.18.19-gke.1400+ -> 1.11.1
       * 1.18.20-gke.5100+ -> 1.11.5
       * 1.19.14-gke.1900 -> 1.11.5
       * 1.20.10-gke.301 -> 1.11.5
       * 1.21.3-gke.210 -> 1.13.4


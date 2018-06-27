# Changelog

### 1.2.0

*Major Changes:*
- Support for backendconfig.cloud.google.com (Beta)
- Support for IAP (Beta)
- Support for CDN (Beta)
- Support for CloudArmor (Beta)
- Network Endpoint Groups support (Beta)

### 1.1.1
**Image:**  `k8s.gcr.io/ingress-gce-glbc-amd64:v1.1.1`

*Major Changes:*
- [#213](https://github.com/kubernetes/ingress-gce/pull/213) Fix for controller error when an ingress has multiple secrets backed by the same certificate.  

### 1.1.0
**Image:**  `k8s.gcr.io/ingress-gce-glbc-amd64:v1.1.0`

*New Features:*
- [#146](https://github.com/kubernetes/ingress-gce/pull/146) Add alpha support of HTTP2 services.
- [#195](https://github.com/kubernetes/ingress-gce/pull/195) Add support of multiple certificates.

*Major Changes:*
- [#189](https://github.com/kubernetes/ingress-gce/pull/189) Build binaries with Go 1.10.1 (previously 1.9).
- [#199](https://github.com/kubernetes/ingress-gce/pull/199) Only update instance group named ports with ports targeted by the syncing ingress - not all ingresses.

*Known Issues:*
- [#213](https://github.com/kubernetes/ingress-gce/pull/213) Controller may orphan certificates if two secrets with the same certificate are referenced from the same ingress.  

### 1.0.1

**Image:**  `k8s.gcr.io/ingress-gce-glbc-amd64:v1.0.1`

*Major Changes:*
- [#187](https://github.com/kubernetes/ingress-gce/pull/187) Fix sync of multi-cluster ingress objects.

### 1.0.0

**Image:**  `k8s.gcr.io/ingress-gce-glbc-amd64:v1.0.0`

*New Features:*
- [#148](https://github.com/kubernetes/ingress-gce/pull/148) Add rate limiting of GCP API calls, configurable through flags.
- [#121](https://github.com/kubernetes/ingress-gce/pull/121) Add HTTP endpoint enabling users to change the verbosity level at runtime.

*Major Changes:*
- [#133](https://github.com/kubernetes/ingress-gce/pull/133) Emit event when TLS cert cannot be found.
- [#123](https://github.com/kubernetes/ingress-gce/pull/123) Only sync backend services and health checks for services targeted by the syncing ingress - not all ingresses.
- [#122](https://github.com/kubernetes/ingress-gce/pull/122) Firewall now opens up the entire nodeport range to GCP health checkers and proxies.
- [#106](https://github.com/kubernetes/ingress-gce/pull/106) Only sync front-end loadbalancer resources of the syncing ingress - not all ingresses.

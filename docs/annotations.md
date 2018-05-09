# Ingress Annotations

This file defines a list of annotations which are supported by various Ingress controllers (both those based on the common ingress code, and alternative implementations).
The intention is to ensure the maximum amount of compatibility between different implementations.

All annotations are assumed to be prefixed with `ingress.kubernetes.io/` except where otherwise specified.
There is no attempt to record implementation-specific annotations using other prefixes.

Key:

* `nginx`: the `kubernetes/ingress` nginx controller
* `gce`: the `kubernetes/ingress-gce` GCE controller
* `traefik`: [Traefik](https://github.com/containous/traefik) as a built-in Ingress controller
* `haproxy`: Joao Morais' [HAProxy Ingress controller](https://github.com/jcmoraisjr/haproxy-ingress)
* `trafficserver`: Torchbox's [Apache Traffic Server controller plugin](https://github.com/torchbox/k8s-ts-ingress)

## TLS-related

| Name | Meaning | Default | Controller
| --- | --- | --- | --- |
| `ssl-passthrough` | Pass TLS connections directly to backend; do not offload. | `false` | nginx, haproxy
| `ssl-redirect` | Redirect non-TLS requests to TLS when TLS is enabled. | `true` | nginx, haproxy, traefik, trafficserver
| `force-ssl-redirect` | Redirect non-TLS requests to TLS even when TLS is not configured. | `false` | nginx, trafficserver
| `secure-backends` | Use TLS to communicate with origin (pods). | `false` | nginx, haproxy, trafficserver
| `kubernetes.io/ingress.allow-http` | Whether to accept non-TLS HTTP connections. | `true` | gce
| `ingress.gcp.kubernetes.io/pre-shared-cert` | Name of the TLS certificate in GCP to use when provisioning the HTTPS load balancer. | empty string | gce
| `hsts-max-age` | Set an HSTS header with this lifetime. | | traefik, trafficserver
| `hsts-include-subdomains` | Add includeSubdomains to the HSTS header. | | traefik, trafficserver

## Authentication related

| Name | Meaning | Default | Controller
| --- | --- | --- | --- |
| `auth-type` | Authentication type: `basic`, `digest`, ... | | nginx, haproxy, traefik, trafficserver
| `auth-secret` | Secret name for authentication. | | nginx, haproxy, traefik, trafficserver
| `auth-realm` | Authentication realm. | | nginx, haproxy, trafficserver
| `auth-tls-secret` | Name of secret for TLS client certification validation. | | nginx, haproxy
| `auth-tls-verify-depth` | Maximum chain length of TLS client certificate. | | nginx
| `auth-tls-error-page` | The page that user should be redirected in case of Auth error | | string
| `auth-satisfy` | Behavior when more than one of `auth-type`, `auth-tls-secret` or `whitelist-source-range` are configured: `all` or `any`. | `all` | trafficserver | `trafficserver`
| `whitelist-source-range` | Comma-separate list of IP addresses to enable access to. | | nginx, haproxy, traefik, trafficserver

## URL related

| Name | Meaning | Default | Controller
| --- | --- | --- | --- |
| `app-root` | Redirect requests without a path (i.e., for `/`) to this location. | | nginx, haproxy, traefik, trafficserver
| `rewrite-target` | Replace matched Ingress `path` with this value. | | nginx, traefik, trafficserver
| `add-base-url` | Add `<base>` tag to HTML. | | nginx
| `base-url-scheme` | Specify the scheme of the `<base>` tags. | | nginx
| `preserve-host` | Whether to pass the client request host (`true`) or the origin hostname (`false`) in the HTTP Host field. | | traefik, trafficserver

## Miscellaneous

| Name | Meaning | Default | Controller
| --- | --- | --- | --- |
| `configuration-snippet` | Arbitrary text to put in the generated configuration file. | | nginx
| `enable-cors` | Enable CORS headers in response. | | nginx
| `limit-connections` | Limit concurrent connections per IP address[1]. | | nginx
| `limit-rps` | Limit requests per second per IP address[1]. | | nginx
| `limit-rpm` | Limit requests per minute per IP address. | | nginx
| `affinity` | Specify a method to stick clients to origins across requests.  Found in `nginx`, where the only supported value is `cookie`. | | nginx
| `session-cookie-name` | When `affinity` is set to `cookie`, the name of the cookie to use. | | nginx
| `session-cookie-hash` | When `affinity` is set to `cookie`, the hash algorithm used: `md5`, `sha`, `index`. | | nginx
| `proxy-body-size` | Maximum request body size. | | nginx, haproxy
| `proxy-pass-params` | Parameters for proxy-pass directives. | |
| `follow-redirects` | Follow HTTP redirects in the response and deliver the redirect target to the client. | | trafficserver
| `kubernetes.io/ingress.global-static-ip-name` | Name of the static global IP address in GCP to use when provisioning the HTTPS load balancer. | empty string | gce

[1] The documentation for the `nginx` controller says that only one of `limit-connections` or `limit-rps` may be specified; it's not clear why this is.

## Caching

| Name | Meaning | Default | Controller
| --- | --- | --- | --- |
| `cache-enable` | Cache responses according to Expires or Cache-Control headers. | | trafficserver
| `cache-generation` | An arbitrary numeric value included in the cache key; changing this effectively clears the cache for this ingress. | | trafficserver
| `cache-ignore-query-params` | Space-separate list of globs matching URL parameters to ignore when doing cache lookups. | | trafficserver
| `cache-whitelist-query-params` | Ignore any URL parameters not in this whitespace-separate list of globs. | | trafficserver
| `cache-sort-query-params` | Lexically sort the query parameters by name before cache lookup. | | trafficserver
| `cache-ignore-cookies` | Requests containing a `Cookie:` header will not use the cache unless all the cookie names match this whitespace-separate list of globs. | | trafficserver

## Ingress-GCE Supported Annotations
These are annotations on the Kubernetes **Ingress** resource which are only relevant to the ingress-gce
controller. Do not expect other controllers to evaluate them. Likewise, do not expect other controller's
annotations to work with ingress-gce unless specified in this list.

#### Ingress Class
`kubernetes.io/ingress.class`  

The ingress-gce controller will only operate on ingresses with non-set ingress.class values or when
the value equals 'gce'. If you are using another ingress controller, be sure to set this to the
respective controller's key, such as 'nginx'.

#### Disable HTTP Front-End
`kubernetes.io/ingress.allow-http` default: `"true"`  

This flag indicates whether the controller should create an HTTP Forwarding Rule and Target Proxy
for the GCP Load Balancer. If either unset or true, the controller will create these resources. If
set to `"false"`, the controller will only create HTTPS resources assuming TLS for the ingress is
configured.

#### Use GCP SSL Certificate
`ingress.gcp.kubernetes.io/pre-shared-cert`  

Instead of storing certificates and keys in Kubernetes secrets, you can upload them to GCP and
reference them by name through this annotation.  

#### Specify Reserved GCP address  
`kubernetes.io/ingress.global-static-ip-name`   

Provide the name of a GCP Address (Global) through this annotation and all forwarding rules for this
ingress will utilize this IP.  
[Example YAML](/examples/static-ip)


## Service Annotations  
These are annotations on the Kubernetes **Service** resource which are only relevant to the ingress-gce
controller. Do not expect other controllers to evaluate them.

#### Set Protocol of Service Ports
`service.alpha.kubernetes.io/app-protocols`  

Provide a mapping of service-port name to either `HTTP`, `HTTPS`, or `HTTP2` to indicate what protocol
the GCP Backend Service should use.

Example Value: `'{"my-https-port":"HTTPS"}'`  
[Example YAML](/examples/backside-https)  

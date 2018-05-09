# Simple TLS example

Create secret
```console
$ make keys
$ kubectl create secret tls foo-secret --key /tmp/tls.key --cert /tmp/tls.crt
```

Make sure you have the l7 controller running:
```console
$ kubectl --namespace=kube-system get pod -l name=glbc
NAME
l7-lb-controller-v0.6.0-1770t ...
```
Also make sure you have a [firewall rule](https://github.com/kubernetes/ingress/blob/master/controllers/gce/BETA_LIMITATIONS.md#creating-the-fir-glbc-health-checks) for the node port of the Service.

Create Ingress:
```console
$ kubectl create -f tls-app.yaml
```

Test reachability:
```console
$ curl --resolve example.com:443:130.211.21.233 https://example.com --cacert /tmp/tls.crt
CLIENT VALUES:
client_address=10.240.0.4
command=GET
real path=/
query=nil
request_version=1.1
request_uri=http://bitrot.com:8080/
...
```

# Simple Websocket Example

Any websocket server will suffice; however, for the purpose of demonstration, we'll use the gorilla/websocket package in a Go process.

### Build
```shell
➜ CGO_ENABLED=0 go build -o wsserver
```

### Containerize
```shell
➜ docker build -t [YOUR_IMAGE] .
...
➜ docker push [YOUR_IMAGE]
...
```

### Deploy
Either update the image in the `Deployment` to your newly created image.
```shell
➜ vi deployment.yaml
# Change image to your own
```

```shell
➜ kubectl create -f deployment.yaml
deployment "ws-example" created
service "ws-example-svc" created
ingress "ws-example-ing" created

```

### Test
Retrieve the ingress external IP:
```shell
➜ kubectl get ing/ws-example-ing
NAME             HOSTS     ADDRESS          PORTS     AGE
ws-example-ing   *         xxx.xxx.xxx.xxx   80        3m
```

Wait for the loadbalancer to be created and functioning. Visit http://xxx.xxx.xxx.xxx and click 'Connect'. You should receive messages from server with timestamps.

### Change backend timeout

At this point, the websocket connection will be destroyed by the HTTP(S) Load Balancer after 30 seconds, which is the default timeout. Note: this timeout is not an idle timeout - it's a timeout on the connection lifetime.

Currently, the GCE ingress controller does not provide a way to set this timeout via Ingress specification. You'll need to change this value either through the GCP Cloud Console or through gcloud CLI.


```shell
➜  kubectl describe ingress/ws-example-ing   
Name:			ws-example-ing
Namespace:		default
Address:		xxxxxxxxxxxx
Default backend:	ws-example-svc:80 (10.48.10.12:8080,10.48.5.14:8080,10.48.7.11:8080)
Rules:
  Host	Path	Backends
  ----	----	--------
  *	* 	ws-example-svc:80 (10.48.10.12:8080,10.48.5.14:8080,10.48.7.11:8080)
Annotations:
  target-proxy:		k8s-tp-default-ws-example-ing--52aa8ae8221ffa9c
  url-map:		k8s-um-default-ws-example-ing--52aa8ae8221ffa9c
  backends:		{"k8s-be-31127--52aa8ae8221ffa9c":"HEALTHY"}
  forwarding-rule:	k8s-fw-default-ws-example-ing--52aa8ae8221ffa9c
Events:
  FirstSeen	LastSeen	Count	From			SubObjectPath	Type		Reason	Message
  ---------	--------	-----	----			-------------	--------	------	-------
  12m		12m		1	loadbalancer-controller			Normal		ADD	default/ws-example-ing
  11m		11m		1	loadbalancer-controller			Normal		CREATE	ip: xxxxxxxxxxxx
  11m		9m		5	loadbalancer-controller			Normal		Service	default backend set to ws-example-svc:31127
```

Retrieve the name of the backend service from within the annotation section.

Update the timeout field for every backend that needs a higher timeout.

```shell
➜ export BACKEND=k8s-be-31127--52aa8ae8221ffa9c
➜ gcloud compute backend-services update $BACKEND --global --timeout=86400 # seconds
Updated [https://www.googleapis.com/compute/v1/projects/xxxxxxxxx/global/backendServices/k8s-be-31127--52aa8ae8221ffa9c].
```

Wait up to twenty minutes for this change to propagate.

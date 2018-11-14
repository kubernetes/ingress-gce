# 404-server (default backend)

404-server is a simple webserver that satisfies the ingress, which means it has to do two things:

 1. Serves a 404 page at `/`
 2. Serves a 200 at `/healthz`

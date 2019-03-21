# echoserver

The echoserver binary is a simple webserver that responds to HTTP GET calls on
the following two paths:

        * "/healthcheck" - Responds with a 200.

        * "/" - Responds w/ information from the request such as host, headers, etc.

This server is suitable for use as a backend for an Ingress. See [here](echo.yaml)
for an example usage.

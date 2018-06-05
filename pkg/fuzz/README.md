# Fuzz

## Validator TODO

- Specific SSL certificate presented is correct.
    - For every SSL certificate, check one domain and make sure we get the cert
        assigned to the given domain.
- Service specific contents (Add IngressModel interface)
    - annotations will decorate the request, response
    - validators transform the response
    - ModelResponder creates the responses that
    - Responder() takes HTTP request and writes the response back.
- Snapshots of GCP state for whitebox testing.
    - Take a snapshot of the load balancer tree starting from the ForwardingRule.
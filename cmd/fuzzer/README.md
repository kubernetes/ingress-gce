# fuzzer

## validate

`fuzzer validate` will validate the Ingress spec against the load balancer that
was instantiated with the given spec.

Usage:

```
$ fuzzer validate -name ingress1 -ns my-namespace
```

You can select the set of features to enable for the
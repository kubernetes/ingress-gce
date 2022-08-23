## Tools
---

This directory lists all the tools that will be built and used by the test runners,
in order to have a stable version for the tests to use. This directory is not built from and is
only used for versioning.

Currently we use `golangci-lint` in our tests, which contains a suite of linters. 

We configure golangci using `.golangci.yaml `. This file is the source of truth of which tools are running and their configuration.

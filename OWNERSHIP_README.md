### Code Ownership Structure

This document outlines the ownership structure for directories within `pkg/` and `cmd/glbc/`. Ownership is categorized into three groups: L7, L4_NEG_PSC, and Shared. The designated owners for each category can be found in the `L7_OWNERS`, `L4_NEG_PSC_OWNERS` and `OWNERS` files respectively.

#### 1. L7 Ownership

These directories contain code exclusive to the L7 Ingress controller.

*   **`pkg/apis/backendconfig`**: Defines the `BackendConfig` CRD for L7 features.
*   **`pkg/apis/frontendconfig`**: Defines the `FrontendConfig` CRD for L7 frontend configurations.
*   **`pkg/backendconfig`**: Implements the `BackendConfig` CRD logic.
*   **`pkg/controller`**: Contains the core reconciliation loop for the L7 Ingress controller.
*   **`pkg/frontendconfig`**: Implements the `FrontendConfig` CRD logic.
*   **`pkg/healthchecks`**, **`pkg/healthchecksprovider`**: Manages generic health check resources.
*   **`pkg/metrics`**: Exposes L7 Prometheus metrics.
*   **`pkg/translator`**: Translates Ingress/Service state into L7 load balancer configurations.
*   **`pkg/utils/healthcheck`**: Used by `pkg/healthchecks` and `pkg/translator`.

#### 2. L4_NEG_PSC Ownership

These directories contain code exclusive to the L4 (Internal Load Balancer), NEG (Network Endpoint Group), and PSC (Private Service Connect) controllers.

*   **`pkg/apis/serviceattachment`**: Defines the `ServiceAttachment` CRD for PSC.
*   **`pkg/apis/svcneg`**: Defines APIs for Service NEGs, used by L4/NEG controllers.
*   **`pkg/apis/providerconfig`**: Contains shared API types for cloud provider configuration.
*   **`pkg/healthchecksl4`**: Manages health checks specifically for L4 load balancers.
*   **`pkg/l4lb`**: Contains the core implementation of the L4 ILB controller.
*   **`pkg/neg`**: Home of the NEG controller.
*   **`pkg/multiproject`**: Shared logic for multi-project support.
*   **`pkg/providerconfig`**: Shared cloud provider configuration.
*   **`pkg/network`**: Provides network-related utilities for L4/NEG controllers.
*   **`pkg/psc`**: Implements the Private Service Connect controller.
*   **`pkg/serviceattachment`**: Implements the `ServiceAttachment` CRD logic.
*   **`pkg/svcneg`**: Implements the Service NEG controller for L4 and standalone NEGs.
*   **`pkg/utils/serviceattachment`**: Used by `pkg/psc`.

#### 3. Shared Ownership

These directories contain shared components, utilities, and abstractions used by all controllers.

*   **`cmd/glbc`** and **`cmd/glbc/app`**: Main entrypoint for the controller binary.
*   **`pkg/address`**: Manages shared IP address resources.
*   **`pkg/annotations`**: Manages annotations for both Ingress and Service resources.
*   **`pkg/backends`**: Provides common abstractions for managing backend services.
*   **`pkg/backoff`**, **`pkg/ratelimit`**, **`pkg/throttling`**: Shared utilities for managing API calls.
*   **`pkg/common`**: Collections of common data structures and helper functions.
*   **`pkg/composite`**: Shared utilities for handling composite Google Cloud resources.
*   **`pkg/context`**: Manages the shared controller context.
*   **`pkg/crd`**: Shared logic for interacting with CRDs.
*   **`pkg/e2e`**, **`pkg/fuzz`**, **`pkg/test`**: Shared testing infrastructure.
*   **`pkg/events`**, **`pkg/recorders`**: Provides a standardized way to emit Kubernetes events.
*   **`pkg/experimental`**: Contains experimental features not tied to a specific controller.
*   **`pkg/firewalls`**: Manages firewall rules.
*   **`pkg/flags`**: Defines and parses command-line flags.
*   **`pkg/forwardingrules`**: Manages forwarding rules.
*   **`pkg/instancegroups`**: Manages instance groups.
*   **`pkg/klog`**: A shared logging wrapper.
*   **`pkg/loadbalancers`**: Manages load balancer resources for both Ingress
    and L4 controllers.
*   **`pkg/nodetopology`**: Shared logic for node topology.
*   **`pkg/storage`**: Shared storage utilities.
*   **`pkg/sync`**: Contains the shared sync loop implementation.
*   **`pkg/systemhealth`**: Shared system health monitoring.
*   **`pkg/validation`**: Shared resource validation logic.
*   **`pkg/version`**: Provides shared controller version information.
*   **`pkg/utils/common`**: Used by both L4 and L7 controllers.
*   **`pkg/utils/descutils`**: Used by `pkg/utils/healthcheck` (L7) and `pkg/utils/serviceattachment` (L4).
*   **`pkg/utils/endpointslices`**: Used by `pkg/neg` (L4) and `pkg/controller/translator` (L7).
*   **`pkg/utils/namer`**: Used by a wide variety of L4 and L7 components.
*   **`pkg/utils/patch`**: Used by `pkg/neg` (L4), `pkg/psc` (L4), and `pkg/l4lb` (L4), and also by the shared `pkg/utils/common`.
*   **`pkg/utils/slice`**: Used by `pkg/psc` (L4), `pkg/loadbalancers` (L7), and `pkg/firewalls` (shared).
*   **`pkg/utils/zonegetter`**: Used by `pkg/neg` (L4), `pkg/l4lb` (L4), and `pkg/controller` (L7).

#### 4. Updating Owners

When an owners file is updated, please refer to this README to help understand which set of owners files need to be updated.

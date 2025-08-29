# Multi-Project Controller Architecture

## Overview

The multi-project controller enables Kubernetes ingress-gce to manage Network Endpoint Groups (NEGs) across multiple Google Cloud Platform (GCP) projects. This allows for multi-tenant scenarios where different namespaces or services can be associated with different GCP projects through ProviderConfig resources.

## Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                     Main Process                             │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │                 start.Start()                          │ │
│  │  - Creates base SharedIndexInformers                   │ │
│  │  - Starts informers with globalStopCh                  │ │
│  │  - Creates ProviderConfigController                    │ │
│  └────────────────────┬───────────────────────────────────┘ │
│                       │                                      │
│  ┌────────────────────▼───────────────────────────────────┐ │
│  │         ProviderConfigController                       │ │
│  │  - Watches ProviderConfig resources                    │ │
│  │  - Manages lifecycle of per-PC controllers             │ │
│  └────────────────────┬───────────────────────────────────┘ │
│                       │                                      │
│  ┌────────────────────▼───────────────────────────────────┐ │
│  │    ProviderConfigControllersManager                    │ │
│  │  - Starts/stops NEG controllers per ProviderConfig     │ │
│  │  - Manages controller lifecycle                        │ │
│  └──────────┬─────────────────────┬───────────────────────┘ │
│             │                     │                          │
│    ┌────────▼──────────┐ ┌───────▼──────────┐              │
│    │ NEG Controller #1  │ │ NEG Controller #2 │ ...         │
│    │ (ProviderConfig A) │ │ (ProviderConfig B) │            │
│    └────────────────────┘ └──────────────────┘              │
└─────────────────────────────────────────────────────────────┘
```

### Key Design Principles

1. **Shared Informers**: Base informers are created once and shared across all ProviderConfig controllers
2. **Filtered Views**: Each NEG controller gets a filtered view of resources based on ProviderConfig
3. **Lifecycle Management**: Controllers can be started/stopped independently as ProviderConfigs are added/removed
4. **Channel Management**: Proper channel lifecycle ensures clean shutdown and resource cleanup

## Component Details

### start/start.go
Main entry point that:
- Creates base SharedIndexInformers via InformerSet (no factories)
- Starts all informers with the global stop channel
- Creates the ProviderConfigController
- Manages leader election (when enabled)

### controller/controller.go
ProviderConfigController that:
- Watches ProviderConfig resources
- Enqueues changes for processing
- Delegates to ProviderConfigControllersManager

### manager/manager.go
ProviderConfigControllersManager that:
- Maintains a map of active controllers per ProviderConfig
- Starts NEG controllers when ProviderConfigs are added
- Stops NEG controllers when ProviderConfigs are deleted
- Manages finalizers for cleanup

### neg/neg.go
NEG controller factory that:
- Wraps base SharedIndexInformers with provider-config filters via ProviderConfigFilteredInformer
- Sets up the NEG controller with proper GCE client
- Manages channel lifecycle (globalStopCh vs providerConfigStopCh)

### filteredinformer/
Filtered informer implementation that:
- Wraps base SharedIndexInformers
- Filters resources based on ProviderConfig labels
- Provides filtered cache/store views

## Channel Lifecycle

The implementation uses three types of channels:

1. **globalStopCh**: Process-wide shutdown signal
   - Closes on leader election loss or process termination
   - Used by base informers and shared resources

2. **providerConfigStopCh**: Per-ProviderConfig shutdown signal
   - Closed when a ProviderConfig is deleted
   - Used to stop PC-specific controllers

3. **joinedStopCh**: Combined shutdown signal
   - Closes when either globalStopCh OR providerConfigStopCh closes
   - Used by PC-specific resources that should stop in either case

## Resource Filtering

Resources are associated with ProviderConfigs through labels:
- Services, Ingresses, etc. have a label indicating their ProviderConfig
- The filtered informer only passes through resources matching the PC name
- This ensures each controller only sees and manages its own resources

## Informer Lifecycle

### Creation
1. Base informers are created via `InformerSet` using `NewXInformer()` functions
2. Base informers are started by `InformerSet.Start` with `globalStopCh`
3. Filtered wrappers are created per ProviderConfig using `ProviderConfigFilteredInformer`

### Synchronization
- `InformerSet.Start` waits for base informer caches to sync
- Filtered informers rely on the synced base caches
- Controllers use `CombinedHasSynced()` from filtered informers before processing

### Shutdown
- Base informers stop when globalStopCh closes
- Filtered informers are just wrappers (no separate shutdown)
- Controllers stop when their providerConfigStopCh closes

## Configuration

Key configuration flags:
- `--provider-config-name-label-key`: Label key for PC association (default: cloud.gke.io/provider-config-name)
- `--multi-project-owner-label-key`: Label key for PC owner
- `--resync-period`: Informer resync period

## Testing

### Unit Tests
- Controller logic testing
- Filter functionality testing
- Channel lifecycle testing

### Integration Tests
- Multi-ProviderConfig scenarios
- Controller start/stop sequencing
- Resource cleanup verification

### Key Test Scenarios
1. Single ProviderConfig with services
2. Multiple ProviderConfigs
3. ProviderConfig deletion and cleanup
4. Shared informer survival across PC changes

## Common Operations

### Adding a ProviderConfig
1. Create ProviderConfig resource
2. Controller detects addition
3. Manager starts NEG controller
4. NEG controller creates filtered informers
5. NEGs are created in target GCP project

### Removing a ProviderConfig

The deletion process follows a specific sequence to ensure proper cleanup:

1. **External automation initiates deletion**:
   - Server-side automation triggers the deletion process
   - All namespaces belonging to the ProviderConfig are deleted first

2. **Namespace cleanup**:
   - Kubernetes deletes all resources within the namespaces
   - Services are deleted, triggering NEG cleanup
   - NEG controller removes NEGs from GCP as services are deleted

3. **Wait for namespace deletion**:
   - External automation waits for all namespaces to be fully deleted
   - This ensures all NEGs and other resources are cleaned up

4. **ProviderConfig deletion**:
   - Only after namespaces are gone, ProviderConfig is deleted
   - Controller stops the NEG controller for this ProviderConfig
   - Finalizer is removed from ProviderConfig
   - ProviderConfig resource is removed from Kubernetes

**Important**: NEGs are not automatically deleted when a ProviderConfig is removed. They are cleaned up as part of the namespace/service deletion process that happens before ProviderConfig deletion.

# Multi-Project Controller

Enables ingress-gce to manage GCP resources across multiple projects through ProviderConfig CRs.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Main Controller                          │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              Shared Kubernetes Informers                │ │
│  │         (Services, Ingresses, EndpointSlices)           │ │
│  └─────────────────────┬──────────────────────────────────┘ │
│                        │                                     │
│  ┌─────────────────────▼──────────────────────────────────┐ │
│  │           ProviderConfig Controller                     │ │
│  │         Watches ProviderConfig resources                │ │
│  │         Manages per-project controllers                 │ │
│  └─────────────────────┬──────────────────────────────────┘ │
│                        │                                     │
│       ┌────────────────┼────────────────┐                   │
│       │                │                │                   │
│  ┌────▼─────┐   ┌─────▼─────┐   ┌─────▼─────┐             │
│  │Project A │   │ Project B │   │ Project C │   ...        │
│  │Controller│   │Controller │   │Controller │              │
│  └──────────┘   └───────────┘   └───────────┘              │
└─────────────────────────────────────────────────────────────┘
```

## Key Concepts

- **ProviderConfig**: CR defining a GCP project configuration
- **Resource Filtering**: Resources are associated via labels; each controller sees only its labeled resources
- **Shared Informers**: Base informers are created once and shared; controllers get filtered views
- **Dynamic Lifecycle**: Controllers start/stop with ProviderConfig create/delete

## Usage

### Create ProviderConfig

```yaml
apiVersion: networking.gke.io/v1
kind: ProviderConfig
metadata:
  name: team-a-project
spec:
  projectID: team-a-gcp-project
  network: team-a-network
```

### Associate Resources

```yaml
apiVersion: v1
kind: Service
metadata:
  name: my-service
  labels:
    ${PROVIDER_CONFIG_LABEL_KEY}: provider-config-a
spec:
  # Service spec...
```

## Operations

### Adding a Project
1. Create ProviderConfig
2. Label services/ingresses with PC name
3. NEGs created in target project

### Removing a Project
1. Remove/relabel services using the PC
2. Wait for NEG cleanup
3. Delete ProviderConfig

## Guarantees

- Controllers only manage explicitly labeled resources
- One controller per ProviderConfig
- Base infrastructure survives individual controller failures
- PC deletion doesn't affect other projects
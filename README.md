[![REUSE status](https://api.reuse.software/badge/github.com/cloudoperators/shoot-grafter)](https://api.reuse.software/info/github.com/cloudoperators/shoot-grafter)

# shoot-grafter

## About this project

**shoot-grafter** is a Kubernetes operator that automates the onboarding of [Gardener](https://gardener.cloud/) Shoots (Kubernetes clusters managed by Gardener) to [Greenhouse](https://github.com/cloudoperators/greenhouse), the cloud operations platform. It bridges the gap between Gardener-managed infrastructure and Greenhouse's centralized cluster management by dynamically discovering and registering Shoots as Greenhouse Clusters.

This project is part of the [NeoNephos Foundation](https://neonephos.org/).

## What does shoot-grafter do?

shoot-grafter continuously monitors Garden clusters for Shoots matching specific criteria and automatically:

1. **Discovers Shoots**: Watches for Gardener Shoot resources in specified namespaces based on label selectors
2. **Extracts cluster credentials**: Retrieves API server URLs and CA certificates from Shoot resources
3. **Creates Greenhouse Clusters**: Automatically registers discovered Shoots as Greenhouse Cluster resources
4. **Propagates labels**: Transfers specified labels from Shoots to Greenhouse Clusters for consistent organization
5. **Configures RBAC**: Optionally sets up role-based access control on Shoot clusters for Greenhouse service accounts
6. **Maintains synchronization**: Keeps Greenhouse Cluster resources in sync with their corresponding Shoots

## Architecture

The operator consists of two main controllers:

### CareInstruction Controller

The `CareInstruction` is the primary Custom Resource Definition (CRD) that configures how shoot-grafter operates. Each CareInstruction:

- Defines which Garden cluster to monitor (via kubeconfig secret or Greenhouse Cluster reference)
- Specifies which namespace to watch for Shoots
- Declares label selectors to filter which Shoots to onboard
- Configures label propagation and additional metadata
- Manages the lifecycle of dynamically created Shoot controllers

**Key features**:

- Dynamically spawns and manages Shoot controllers for each CareInstruction
- Monitors Garden cluster accessibility
- Tracks onboarding status and statistics
- Handles cleanup when CareInstructions are deleted

### Shoot Controller

For each CareInstruction, a dedicated Shoot controller is dynamically created and runs in its own manager. This controller:

- Watches Shoot resources in the specified Garden cluster namespace
- Extracts cluster connection details (API server URL, CA certificate)
- Creates or updates corresponding Secret resources with OIDC configuration
- Generates Greenhouse Cluster resources with appropriate labels
- Optionally configures RBAC on the Shoot cluster for Greenhouse access

## Custom Resource: CareInstruction

A `CareInstruction` defines the configuration for onboarding Shoots from a specific Garden cluster.

### Example CareInstruction

```yaml
apiVersion: shoot-grafter.cloudoperators/v1alpha1
kind: CareInstruction
metadata:
  name: production-shoots
  namespace: greenhouse-team
spec:
  # Option 1: Reference a Greenhouse Cluster resource
  gardenClusterName: garden-prod-cluster
  
  # Option 2: Reference a kubeconfig secret directly
  # gardenClusterKubeConfigSecretName:
  #   name: garden-kubeconfig
  #   key: kubeconfig
  
  # Namespace in the Garden cluster to watch
  gardenNamespace: garden-production
  
  # Label selector for Shoots to onboard
  shootSelector:
    matchLabels:
      environment: production
      team: platform
  
  # Labels to propagate from Shoot to Greenhouse Cluster
  propagateLabels:
    - region
    - cost-center
    - business-unit
  
  # Additional labels to add to all created Clusters
  additionalLabels:
    managed-by: shoot-grafter
    onboarding-source: garden-prod
  
  # Disable automatic RBAC configuration (default: false)
  disableRBAC: false
```

### CareInstruction Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `gardenClusterName` | string | No* | Name of the Greenhouse Cluster resource representing the Garden cluster |
| `gardenClusterKubeConfigSecretName` | SecretKeyReference | No* | Reference to a secret containing the kubeconfig for the Garden cluster |
| `gardenNamespace` | string | Yes | Namespace in the Garden cluster where Shoots are located |
| `shootSelector` | LabelSelector | No | Label selector to filter which Shoots to onboard (if omitted, all Shoots in namespace are selected) |
| `propagateLabels` | []string | No | List of label keys to copy from Shoot to Greenhouse Cluster |
| `additionalLabels` | map[string]string | No | Additional labels to add to all created Greenhouse Clusters |
| `disableRBAC` | bool | No | When true, skips automatic RBAC setup on Shoot clusters (default: false) |

*Note: Either `gardenClusterName` or `gardenClusterKubeConfigSecretName` must be provided (priority: kubeconfig secret > cluster name)

### CareInstruction Status

The CareInstruction status provides real-time information about the onboarding process:

```yaml
status:
  statusConditions:
    conditions:
      - type: GardenClusterAccessReady
        status: "True"
        reason: GardenClusterAccessReady
      - type: ShootControllerStarted
        status: "True"
        reason: Started
      - type: ShootsReconciled
        status: "True"
        reason: Reconciled
        message: All shoots and clusters are reconciled
  lastUpdateTime: "2025-11-18T09:00:00Z"
  totalShoots: 15
  failedShoots: 0
  createdClusters: 15
  failedClusters: 0
```

**Status Fields**:

- `Ready`: Overall readiness of the CareInstruction (derived from sub-conditions)
- `GardenClusterAccessReady`: Indicates whether the Garden cluster is accessible
- `ShootControllerStarted`: Shows if the dynamic Shoot controller has been started
- `ShootsReconciled`: Reports whether all targeted Shoots have been successfully onboarded
- `totalShoots`: Total number of Shoots matched by the selector
- `createdClusters`: Number of Greenhouse Clusters created by this CareInstruction
- `failedShoots`: Number of Shoots that failed to be onboarded
- `failedClusters`: Number of Greenhouse Clusters that are not ready

## Usage Examples

### Example 1: Onboard all Shoots in a namespace

```yaml
apiVersion: shoot-grafter.cloudoperators/v1alpha1
kind: CareInstruction
metadata:
  name: all-dev-shoots
  namespace: greenhouse-dev
spec:
  gardenClusterName: dev-garden
  gardenNamespace: garden--dev
  # No shootSelector - onboards all Shoots
```

### Example 2: Onboard Shoots with specific labels

```yaml
apiVersion: shoot-grafter.cloudoperators/v1alpha1
kind: CareInstruction
metadata:
  name: production-critical
  namespace: greenhouse-prod
spec:
  gardenClusterName: prod-garden
  gardenNamespace: garden--production
  shootSelector:
    matchLabels:
      environment: production
  propagateLabels:
    - region
    - owned-by
  additionalLabels:
    monitoring: enabled
    backup: daily
```

### Example 3: Using matchExpressions for advanced selection

```yaml
apiVersion: shoot-grafter.cloudoperators/v1alpha1
kind: CareInstruction
metadata:
  name: multi-env-shoots
  namespace: greenhouse
spec:
  gardenClusterName: my-garden
  gardenNamespace: garden--myproject
  shootSelector:
    matchExpressions:
      - key: environment
        operator: In
        values:
          - staging
          - production
      - key: owned-by
        operator: Exists
  propagateLabels:
    - environment
    - region
    - owned-by
  additionalLabels:
    onboarding-method: shoot-grafter
```

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/cloudoperators/shoot-grafter/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/cloudoperators/shoot-grafter/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2025 SAP SE or an SAP affiliate company and shoot-grafter contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/cloudoperators/shoot-grafter).

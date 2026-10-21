---
title: "Reference"
linkTitle: "Reference"
weight: 3
description: >
  CareInstruction CRD specification
---

## CareInstruction

A `CareInstruction` defines the configuration for onboarding Shoots from a specific Garden cluster.

### Example

```yaml
apiVersion: shoot-grafter.cloudoperators.dev/v1alpha1
kind: CareInstruction
metadata:
  name: production-shoots
  namespace: greenhouse-org
spec:
  # Option 1: Reference a Greenhouse Cluster resource
  gardenClusterName: garden-prod-cluster

  # Option 2: Reference a kubeconfig secret directly
  # gardenClusterKubeConfigSecretName:
  #   name: garden-kubeconfig
  #   key: kubeconfig

  # Namespace in the Garden cluster to watch
  gardenNamespace: garden-production

  # Label selector and CEL expression for Shoots to onboard
  shootSelector:
    labelSelector:
      matchLabels:
        environment: production
        team: platform
    expression: 'object.status.lastOperation.state == "Succeeded"'

  # Labels to propagate from Shoot to Greenhouse Cluster
  propagateLabels:
    - metadata.greenhouse.sap/region
    - greenhouse.sap/owned-by

  # Additional labels to add to all created Clusters
  additionalLabels:
    onboarding-source: garden-prod

  # Reference to AuthenticationConfiguration ConfigMap (optional)
  authenticationConfigMapName: greenhouse-oidc-config

  # Enable automatic RBAC configuration (default: true)
  enableRBAC: true
```

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `gardenClusterName` | string | No* | Name of the Greenhouse Cluster resource representing the Garden cluster |
| `gardenClusterKubeConfigSecretName` | SecretKeyReference | No* | Reference to a secret containing the kubeconfig for the Garden cluster |
| `gardenNamespace` | string | Yes | Namespace in the Garden cluster where Shoots are located |
| `shootSelector` | ShootSelector | No | Combines label-based and CEL expression-based filtering (if omitted, all Shoots in namespace are selected) |
| `shootSelector.labelSelector` | LabelSelector | No | Label selector to filter which Shoots to onboard |
| `shootSelector.expression` | string | No | CEL expression for filtering shoots by status or other fields (max 1024 chars). The shoot object is available as `object` |
| `propagateLabels` | []string | No | List of label keys to copy from Shoot to Greenhouse Cluster |
| `additionalLabels` | map[string]string | No | Additional labels to add to all created Greenhouse Clusters |
| `authenticationConfigMapName` | string | No | Name of ConfigMap in Greenhouse cluster containing AuthenticationConfiguration [(config.yaml with apiserver.config.k8s.io/v1beta1 content)](https://gardener.cloud/docs/guides/administer-shoots/oidc-login/#configure-the-shoot-cluster). Multiple CareInstructions may share the same ConfigMap. |
| `enableRBAC` | bool | No | When false, skips automatic RBAC setup on Shoot clusters (default: true) |

*Either `gardenClusterName` or `gardenClusterKubeConfigSecretName` must be provided (priority: kubeconfig secret > cluster name).

### Triggering Reconciliation

The annotation `shoot-grafter.cloudoperators.dev/reconcile: "true"` can be set on a **CareInstruction** or a **Greenhouse Cluster** to trigger reconciliation on demand.

| Target | Effect |
|--------|--------|
| `CareInstruction` | Restarts the shoot controller for that CareInstruction, causing shoot-grafter to re-apply its config (OIDC, RBAC, labels) to all matching Shoots |
| `Greenhouse Cluster` | Sets `gardener.cloud/operation: reconcile` on the matching Shoot in the Garden cluster, triggering Gardener's own reconciliation |

The annotation is removed from the resource after processing.

### Status

The CareInstruction status provides real-time information about the onboarding process:

```yaml
status:
  statusConditions:
    conditions:
      - type: Ready
        status: "True"
        reason: Ready
        message: CareInstruction is ready
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
  shoots:
    - name: shoot-cluster-1
      status: Onboarded
    - name: shoot-cluster-2
      status: Onboarded
    - name: shoot-cluster-3
      status: Failed
      message: couldn't get current server API group list: the server has asked for the client to provide credentials
    - name: shoot-cluster-4
      status: Excluded
      message: "filtered out by CEL expression"
  totalTargetShootCount: 4
  createdClusters: 2
  failedClusters: 1
  shootControllerRestartCount: 0
```

**Status Fields**:

- `Ready`: Overall readiness of the CareInstruction (derived from sub-conditions)
- `GardenClusterAccessReady`: Indicates whether the Garden cluster is accessible
- `ShootControllerStarted`: Shows if the dynamic Shoot controller has been started
- `ShootsReconciled`: Reports whether all targeted Shoots have been successfully onboarded
- `shoots`: Detailed list of all Shoots targeted by this CareInstruction with their individual status (`Onboarded`, `Failed`, or `Excluded`) and an optional message
- `totalTargetShootCount`: Total number of Shoots matching the label selector (before CEL filtering)
- `createdClusters`: Number of Greenhouse Clusters successfully created by this CareInstruction
- `failedClusters`: Number of Greenhouse Clusters that failed to be created
- `shootControllerRestartCount`: Number of times the dynamic Shoot controller has been restarted

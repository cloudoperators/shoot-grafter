---
title: "Architecture"
linkTitle: "Architecture"
weight: 2
description: >
  How shoot-grafter is structured internally
---

```mermaid
graph LR
    subgraph Garden["Garden API"]
        direction TB
        subgraph NS1[" "]
            NS1Title["Shoots in ns: garden--production"]
            Shoot1["Shoot-prod-1<br/>(env=prod)"]
            Shoot2["Shoot-prod-2<br/>(env=prod)"]
        end
        subgraph NS2[" "]
            NS2Title["Shoots in ns: garden--development"]
            Shoot3["Shoot-dev-1<br/>(env=dev)"]
            Shoot4["Shoot-dev-2<br/>(env=staging)"]
        end
    end

    subgraph Greenhouse["Greenhouse"]
        direction TB

        subgraph ShootGrafter["shoot-grafter"]
            CIC[CareInstruction Controller]

            subgraph CareInstructions[" "]
                CITitle["CareInstructions"]
                CI1["prod-shoots<br/>gardenNamespace: garden--production<br/>shootSelector.labelSelector: env=prod"]
                CI2["dev-shoots<br/>gardenNamespace: garden--development<br/>shootSelector.labelSelector: env=dev"]
            end

            subgraph DynamicControllers[" "]
                DCTitle["Dynamically Created Shoot Controllers"]
                SC1[shoot-controller-prod-shoots<br/>watches: garden--production]
                SC2[shoot-controller-dev-shoots<br/>watches: garden--development]
            end
        end

        subgraph Resources["Greenhouse Resources"]
            subgraph Secrets[" "]
                SecretsTitle["Secrets"]
                SecretProd1[shoot-prod-1]
                SecretProd2[shoot-prod-2]
                SecretDev1[shoot-dev-1]
            end
            subgraph Clusters[" "]
                ClustersTitle["Clusters"]
                ClusterProd1[shoot-prod-1]
                ClusterProd2[shoot-prod-2]
                ClusterDev1[shoot-dev-1]
            end
        end
    end

    CIC -->|reconciles| CI1
    CIC -->|reconciles| CI2

    CI1 -->|creates & manages| SC1
    CI2 -->|creates & manages| SC2

    SC1 -->|watches| Shoot1
    SC1 -->|watches| Shoot2
    SC2 -->|watches| Shoot3
    SC2 -->|watches| Shoot4

    SC1 -->|creates| SecretProd1
    SC1 -->|creates| SecretProd2
    SC2 -->|creates| SecretDev1

    ClusterProd1 -->|depends on| SecretProd1
    ClusterProd2 -->|depends on| SecretProd2
    ClusterDev1 -->|depends on| SecretDev1

    style Garden fill:#f5f5f5,stroke:#000,stroke-width:2px,color:#000
    style Greenhouse fill:#f5f5f5,stroke:#000,stroke-width:2px,color:#000
    style ShootGrafter fill:#333,stroke:#000,stroke-width:2px,color:#fff
    style CareInstructions fill:#555,stroke:#000,stroke-width:2px,color:#fff
    style CIC fill:#444,stroke:#000,stroke-width:2px,color:#fff
    style DynamicControllers fill:#555,stroke:#000,stroke-width:2px,color:#fff
    style SC1 fill:#666,stroke:#000,stroke-width:2px,color:#fff
    style SC2 fill:#666,stroke:#000,stroke-width:2px,color:#fff
    style Resources fill:#333,stroke:#000,stroke-width:2px,color:#fff
    style Secrets fill:#444,stroke:#000,stroke-width:2px,color:#fff
    style Clusters fill:#444,stroke:#000,stroke-width:2px,color:#fff
    style NS1 fill:#d0d0d0,stroke:#000,stroke-width:2px,color:#000
    style NS2 fill:#d0d0d0,stroke:#000,stroke-width:2px,color:#000
    style NS1Title fill:#d0d0d0,stroke:#d0d0d0,color:#000
    style NS2Title fill:#d0d0d0,stroke:#d0d0d0,color:#000
    style CITitle fill:#555,stroke:#555,color:#fff
    style DCTitle fill:#555,stroke:#555,color:#fff
    style SecretsTitle fill:#444,stroke:#444,color:#fff
    style ClustersTitle fill:#444,stroke:#444,color:#fff

    linkStyle default stroke:#000,stroke-width:2px
```

## Controllers

### CareInstruction Controller

The `CareInstruction` is the primary Custom Resource Definition (CRD) that configures how shoot-grafter operates. Each CareInstruction:

- Defines which Garden cluster to monitor (via kubeconfig secret or Greenhouse Cluster reference)
- Specifies which namespace to watch for Shoots
- Declares label selectors and CEL expressions to filter which Shoots to onboard
- Configures label propagation and additional metadata
- Manages the lifecycle of dynamically created Shoot controllers

### Shoot Controller

For each CareInstruction, a dedicated Shoot controller is dynamically created and runs in its own manager. This controller:

- Watches Shoot resources in the specified Garden cluster namespace
- Extracts cluster connection details (API server URL, CA certificate)
- Creates or updates corresponding Secret resources with OIDC configuration
- Generates Greenhouse Cluster resources with appropriate labels
- Optionally configures OIDC authentication on Shoot clusters for Greenhouse access. Also see respective [Greenhouse docs](https://cloudoperators.github.io/greenhouse/docs/user-guides/cluster/oidc_connectivity/) and [Gardener docs](https://gardener.cloud/docs/guides/administer-shoots/oidc-login/#configure-the-shoot-cluster)
- Optionally configures RBAC on the Shoot cluster for Greenhouse access
- Cleans up Greenhouse clusters when the corresponding Gardener Shoot is removed

> **Auth ConfigMap labeling & watch**: When `authenticationConfigMapName` is set, the shoot controller:
> - Labels the referenced Greenhouse ConfigMap with `shoot-grafter.cloudoperators.dev/auth-configmap: "true"` so the CareInstruction controller can watch it. When the data changes, all CareInstructions referencing that ConfigMap are re-enqueued.
> - Creates or overwrites a ConfigMap in the Garden cluster with the Greenhouse content verbatim. That Garden CM is labeled `shoot-grafter.cloudoperators.dev/careinstruction: <careInstructionName>` to mark ownership and to scope the CM-change watch to Shoots managed by the same CareInstruction.
> - Labels each configured Shoot with `shoot-grafter.cloudoperators.dev/careinstruction: <careInstructionName>` so that Garden CM changes trigger reconciliation of only the relevant Shoots.

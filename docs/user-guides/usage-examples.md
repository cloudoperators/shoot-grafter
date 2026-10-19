---
title: "Usage Examples"
linkTitle: "Usage Examples"
weight: 1
description: >
  Common CareInstruction configurations
---

## Example 1: Onboard all Shoots in a namespace

```yaml
apiVersion: shoot-grafter.cloudoperators/v1alpha1
kind: CareInstruction
metadata:
  name: all-dev-shoots
  namespace: greenhouse-dev
spec:
  gardenClusterName: dev-garden
  gardenNamespace: garden--dev
```

## Example 2: Onboard Shoots with specific labels

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
    labelSelector:
      matchLabels:
        environment: production
  propagateLabels:
    - metadata.greenhouse.sap/region
    - greenhouse.sap/owned-by
  additionalLabels:
    monitoring: enabled
    backup: daily
```

## Example 3: Using matchExpressions for advanced selection

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
    labelSelector:
      matchLabels:
        shoot.gardener.cloud/status: "healthy"
      matchExpressions:
        - key: environment
          operator: In
          values:
            - staging
            - production
        - key: owned-by
          operator: Exists
    expression: 'object.status.lastOperation.state == "Succeeded"'
  propagateLabels:
    - metadata.greenhouse.sap/region
    - metadata.greenhouse.sap/environment
    - greenhouse.sap/owned-by
  additionalLabels:
    onboarding-method: shoot-grafter
```

## Example 4: Configuring OIDC Authentication

This example shows how to configure OIDC authentication on Shoots to enable Greenhouse authentication:

```yaml
apiVersion: shoot-grafter.cloudoperators/v1alpha1
kind: CareInstruction
metadata:
  name: oidc-enabled-shoots
  namespace: greenhouse-prod
spec:
  gardenClusterName: prod-garden
  gardenNamespace: garden--production
  shootSelector:
    labelSelector:
      matchLabels:
        enabled-oidc: "true"
  authenticationConfigMapName: greenhouse-oidc-config
  propagateLabels:
    - metadata.greenhouse.sap/environment
```

The referenced ConfigMap should contain an AuthenticationConfiguration:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: greenhouse-oidc-config
  namespace: greenhouse-prod
data:
  config.yaml: |
    apiVersion: apiserver.config.k8s.io/v1beta1
    kind: AuthenticationConfiguration
    jwt:
    - issuer:
        url: https://oidc.greenhouse.example.com
        audiences:
        - greenhouse
      claimMappings:
        username:
          claim: sub
          prefix: "greenhouse:"
        groups:
          claim: groups
          prefix: "greenhouse:"
```

## OIDC Configuration Updates and Shoot Reconciliation

When `spec.authenticationConfigMapName` is configured in a CareInstruction, shoot-grafter automatically manages OIDC authentication on Shoots:

1. **Initial Setup**: When a Shoot is first onboarded, shoot-grafter creates an AuthenticationConfiguration ConfigMap in the Garden cluster and updates the Shoot's spec to reference it.

2. **Configuration Updates**: When the Greenhouse AuthenticationConfiguration ConfigMap is updated with new OIDC settings:
   - shoot-grafter overwrites the Garden cluster ConfigMap verbatim with the Greenhouse content — shoot-grafter is the sole owner of that ConfigMap's data
   - If the Shoot spec already references the correct ConfigMap (no spec change needed), shoot-grafter automatically triggers a Shoot reconciliation by annotating it with `gardener.cloud/operation: reconcile` to apply the changes immediately without waiting for the Shoot's maintenance window
   - See the [Gardener documentation on immediate reconciliation](https://gardener.cloud/docs/gardener/shoot-operations/shoot_operations/#immediate-reconciliation) for more details

**Note**: If the Shoot spec needs to be updated (e.g., when the ConfigMap reference changes), the spec update itself triggers reconciliation automatically, so no additional annotation is needed.

## Moving a Shoot to another CareInstruction

Ownership of an onboarded Shoot is recorded in the `shoot-grafter.cloudoperators.dev/careinstruction` label on its Greenhouse Secret, which Greenhouse propagates to the Cluster. Ownership follows this label, not the CareInstruction selectors, so changing selectors alone moves nothing.

Relabel the Secret, then trigger the new CareInstruction:

```bash
kubectl label secret <shoot-name> -n <greenhouse-namespace> --overwrite \
  shoot-grafter.cloudoperators.dev/careinstruction=<new-careinstruction>

kubectl annotate careinstruction <new-careinstruction> -n <greenhouse-namespace> --overwrite \
  shoot-grafter.cloudoperators.dev/reconcile=true
```

The annotation is required. After the label change the new CareInstruction already reports the Shoot as `Onboarded`, because that status is derived from the Cluster labels, but the shoot controller only runs on Shoot events from the Garden cluster and never re-applies OIDC, RBAC or the authentication ConfigMap. The annotation restarts it. The value must be the literal string `true`. See the [CareInstruction reference](../../reference/#triggering-reconciliation).

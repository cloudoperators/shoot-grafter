---
title: "Debugging Shoot Reconciliation"
linkTitle: "Debugging"
weight: 2
description: >
  Using Kubernetes events to monitor and debug Shoot onboarding
---

shoot-grafter emits Kubernetes events to help you monitor and debug the Shoot onboarding process. Events are associated with the CareInstruction resource.

## View Events for a CareInstruction

```bash
# View events in the resource description
kubectl describe careinstruction <careinstruction-name> -n <namespace>

# List all events for a specific CareInstruction
kubectl get events -n <namespace> \
  --field-selector involvedObject.name=<careinstruction-name>

# Filter events by reason
kubectl get events -n <namespace> \
  --field-selector involvedObject.name=<careinstruction-name>,reason=ShootReconciled

# Watch events in real-time
kubectl get events -n <namespace> \
  --field-selector involvedObject.name=<careinstruction-name> \
  --watch
```

## Event Types

### Normal Events (Successful Operations)

| Event Reason | Description |
|-------------|-------------|
| `ShootReconciled` | Successfully completed reconciliation for a Shoot |
| `SecretCreated` | Created Greenhouse secret with cluster credentials |
| `SecretUpdated` | Updated existing Greenhouse secret with new credentials |
| `ClusterDeleted` | Cluster deletion was requested in the Greenhouse |
| `ShootDeleted` | Shoot was deleted from the Garden cluster |
| `OIDCConfigured` | Successfully configured OIDC authentication for the Shoot |
| `RBACCreated` | Created RBAC ClusterRoleBinding for Greenhouse ServiceAccount on the Shoot |

### Warning Events (Issues Requiring Attention)

| Event Reason | Description | Resolution |
|-------------|-------------|------------|
| `APIServerURLMissing` | Shoot doesn't have an external API server URL | Check Shoot status in Garden cluster; ensure Shoot is fully reconciled |
| `CAConfigMapFetchFailed` | Failed to fetch CA certificate ConfigMap | Verify ConfigMap `<shoot-name>.ca-cluster` exists in Garden namespace |
| `CADataMissing` | CA certificate data is empty in ConfigMap | Check ConfigMap data contains valid `ca.crt` entry |
| `SecretOperationFailed` | Failed to create or update Greenhouse secret | Check RBAC permissions and Greenhouse cluster connectivity |
| `ClusterDeletionFailed` | Failed to delete a Cluster in the Greenhouse | Check event details for the particular error reason |
| `OIDCConfigurationFailed` | Failed to configure OIDC authentication on the Shoot | Verify AuthenticationConfigMap exists and contains valid configuration; check Garden cluster connectivity and permissions |
| `ShootClientFetchFailed` | Failed to get Shoot cluster client | Verify Shoot is accessible and kubeconfig is valid; check network connectivity to Shoot cluster |
| `RBACCreationFailed` | Failed to create RBAC ClusterRoleBinding on the Shoot | Check connectivity to Shoot cluster; verify service account has sufficient permissions |
| `CELFilterError` | CEL expression evaluation failed for a Shoot | Verify the CEL expression syntax in `shootSelector.expression` |

## Event Retention

Kubernetes automatically cleans up events after a TTL period (typically 1 hour by default). This TTL can be configured via the `--event-ttl` flag on the kube-apiserver.

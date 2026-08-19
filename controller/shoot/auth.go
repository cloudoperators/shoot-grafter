// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"fmt"

	"shoot-grafter/api/v1alpha1"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	authConfigMapKey                 = "config.yaml"
	authConfigMapManagedByAnnotation = "shoot-grafter.cloudoperators.dev/managed-by"
)

// ConfigureOIDCAuthentication configures OIDC authentication for the Shoot by:
// 1. Reading the AuthenticationConfiguration from the Greenhouse auth ConfigMap
// 2. Writing it verbatim to a ConfigMap in the Garden cluster (always overwrite)
// 3. Updating the Shoot spec to reference that ConfigMap
func (r *ShootController) ConfigureOIDCAuthentication(ctx context.Context, shoot *gardenerv1beta1.Shoot) error {
	// Fetch the Greenhouse auth ConfigMap and ensure it carries the watch label.
	var greenhouseAuthConfigMap corev1.ConfigMap
	if err := r.GreenhouseClient.Get(ctx, client.ObjectKey{
		Namespace: r.CareInstruction.Namespace,
		Name:      r.CareInstruction.Spec.AuthenticationConfigMapName,
	}, &greenhouseAuthConfigMap); err != nil {
		return fmt.Errorf("failed to fetch AuthenticationConfiguration ConfigMap %s from Greenhouse cluster: %w",
			r.CareInstruction.Spec.AuthenticationConfigMapName, err)
	}

	base := greenhouseAuthConfigMap.DeepCopy()
	if greenhouseAuthConfigMap.Labels == nil {
		greenhouseAuthConfigMap.Labels = make(map[string]string)
	}
	if _, hasAuthLabel := greenhouseAuthConfigMap.Labels[v1alpha1.AuthConfigMapLabel]; !hasAuthLabel {
		greenhouseAuthConfigMap.Labels[v1alpha1.AuthConfigMapLabel] = "true"
		if patchErr := r.GreenhouseClient.Patch(ctx, &greenhouseAuthConfigMap, client.MergeFrom(base)); patchErr != nil {
			r.Info("failed to patch labels on auth ConfigMap", "configMap", greenhouseAuthConfigMap.Name, "error", patchErr)
		}
	}

	if greenhouseAuthConfigMap.Data == nil || greenhouseAuthConfigMap.Data[authConfigMapKey] == "" {
		return fmt.Errorf("AuthenticationConfiguration ConfigMap %s does not contain %s key",
			r.CareInstruction.Spec.AuthenticationConfigMapName, authConfigMapKey)
	}

	// Determine the ConfigMap name for the Garden cluster.
	// Preserve an existing reference on the Shoot; otherwise use the default name.
	configMapName := r.CareInstruction.Name + "-greenhouse-auth"
	if shoot.Spec.Kubernetes.KubeAPIServer != nil &&
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication != nil &&
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName != "" {
		configMapName = shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName
	}

	// Always overwrite the garden-cluster CM with the Greenhouse content verbatim.
	gardenConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: shoot.Namespace,
		},
	}
	authContent := greenhouseAuthConfigMap.Data[authConfigMapKey]

	configMapResult, err := ctrl.CreateOrUpdate(ctx, r.GardenClient, &gardenConfigMap, func() error {
		if gardenConfigMap.Labels == nil {
			gardenConfigMap.Labels = make(map[string]string)
		}
		gardenConfigMap.Labels[v1alpha1.CareInstructionLabel] = r.CareInstruction.Name

		if gardenConfigMap.Annotations == nil {
			gardenConfigMap.Annotations = make(map[string]string)
		}
		gardenConfigMap.Annotations[authConfigMapManagedByAnnotation] = "shoot-grafter - do not edit by hand, this is maintained by automation"

		gardenConfigMap.Data = map[string]string{authConfigMapKey: authContent}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create/update AuthenticationConfiguration ConfigMap: %w", err)
	}

	switch configMapResult {
	case controllerutil.OperationResultCreated:
		r.Info("AuthenticationConfiguration ConfigMap created", "name", configMapName, "shoot", shoot.Name)
	case controllerutil.OperationResultUpdated:
		r.Info("AuthenticationConfiguration ConfigMap updated", "name", configMapName, "shoot", shoot.Name)
	}

	// Ensure the Shoot carries the auth-configured-by label.
	if shoot.Labels == nil || shoot.Labels[v1alpha1.ShootAuthConfiguredByLabel] != r.CareInstruction.Name {
		shootBase := shoot.DeepCopy()
		if shoot.Labels == nil {
			shoot.Labels = make(map[string]string)
		}
		shoot.Labels[v1alpha1.ShootAuthConfiguredByLabel] = r.CareInstruction.Name
		if patchErr := r.GardenClient.Patch(ctx, shoot, client.MergeFrom(shootBase)); patchErr != nil {
			return fmt.Errorf("failed to patch auth-configured-by label on Shoot: %w", patchErr)
		}
	}

	// Update the Shoot spec to reference the ConfigMap if not already pointing to it.
	shootNeedsUpdate := false
	if shoot.Spec.Kubernetes.KubeAPIServer == nil {
		shoot.Spec.Kubernetes.KubeAPIServer = &gardenerv1beta1.KubeAPIServerConfig{}
		shootNeedsUpdate = true
	}
	if shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication == nil {
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication = &gardenerv1beta1.StructuredAuthentication{}
		shootNeedsUpdate = true
	}
	if shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName != configMapName {
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName = configMapName
		shootNeedsUpdate = true
	}

	if shootNeedsUpdate {
		if err := r.GardenClient.Update(ctx, shoot); err != nil {
			return fmt.Errorf("failed to update Shoot spec with OIDC authentication ConfigMap reference: %w", err)
		}
		r.Info("Updated Shoot spec with OIDC configuration", "shoot", shoot.Name, "configMap", configMapName)
		return nil // Spec change triggers reconciliation automatically
	}

	// Trigger Shoot reconciliation if ConfigMap was created or updated.
	// Reference: https://gardener.cloud/docs/gardener/shoot-operations/shoot_operations/#immediate-reconciliation
	if configMapResult != controllerutil.OperationResultNone {
		if err := AnnotateShootForReconcile(ctx, r.GardenClient, shoot.Namespace, shoot.Name); err != nil {
			return fmt.Errorf("failed to annotate Shoot for reconciliation: %w", err)
		}
		r.Info("Annotated Shoot for reconciliation due to ConfigMap change",
			"shoot", shoot.Name,
			"configMap", configMapName)
	}

	return nil
}

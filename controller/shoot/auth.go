// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"fmt"

	"shoot-grafter/api/v1alpha1"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiserverv1beta1 "k8s.io/apiserver/pkg/apis/apiserver/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

const authConfigMapKey = "config.yaml"

// configureOIDCAuthentication configures OIDC authentication for the Shoot by:
// 1. Reading the AuthenticationConfiguration from the Greenhouse auth ConfigMap
// 2. Merging it with any existing configuration on the Garden cluster
// 3. Updating the Shoot spec to reference the merged configuration
func (r *ShootController) configureOIDCAuthentication(ctx context.Context, shoot *gardenerv1beta1.Shoot) error {
	// Label the Greenhouse auth ConfigMap so the CareInstruction controller's watch predicate can
	// identify it and associate it with this CareInstruction.
	// We fetch the live CM to get the current metadata, then patch only the labels.
	var greenhouseAuthConfigMap corev1.ConfigMap
	if err := r.GreenhouseClient.Get(ctx, client.ObjectKey{
		Namespace: r.CareInstruction.Namespace,
		Name:      r.CareInstruction.Spec.AuthenticationConfigMapName,
	}, &greenhouseAuthConfigMap); err != nil {
		if !errors.IsNotFound(err) {
			r.Info("failed to fetch auth ConfigMap for labeling; skipping label patch",
				"configMap", r.CareInstruction.Spec.AuthenticationConfigMapName, "error", err)
		}
	} else {
		base := greenhouseAuthConfigMap.DeepCopy()
		if greenhouseAuthConfigMap.Labels == nil {
			greenhouseAuthConfigMap.Labels = make(map[string]string)
		}
		labelsNeedUpdate := false

		// If the CM is already owned by a different CareInstruction, skip relabelling.
		existingOwner, hasCILabel := greenhouseAuthConfigMap.Labels[v1alpha1.CareInstructionLabel]
		if hasCILabel && existingOwner != r.CareInstruction.Name {
			r.Info("auth ConfigMap is already owned by another CareInstruction; skipping relabel",
				"configMap", greenhouseAuthConfigMap.Name,
				"existingOwner", existingOwner,
				"thisCareInstruction", r.CareInstruction.Name)
		} else {
			if _, hasAuthLabel := greenhouseAuthConfigMap.Labels[v1alpha1.AuthConfigMapLabel]; !hasAuthLabel {
				greenhouseAuthConfigMap.Labels[v1alpha1.AuthConfigMapLabel] = "true"
				labelsNeedUpdate = true
			}
			if !hasCILabel {
				greenhouseAuthConfigMap.Labels[v1alpha1.CareInstructionLabel] = r.CareInstruction.Name
				labelsNeedUpdate = true
			}
		}

		if labelsNeedUpdate {
			if patchErr := r.GreenhouseClient.Patch(ctx, &greenhouseAuthConfigMap, client.MergeFrom(base)); patchErr != nil {
				r.Info("failed to patch labels on auth ConfigMap", "configMap", greenhouseAuthConfigMap.Name, "error", patchErr)
			}
		}
	}

	if greenhouseAuthConfigMap.Data == nil || greenhouseAuthConfigMap.Data[authConfigMapKey] == "" {
		r.Info("auth ConfigMap has no data, skipping OIDC configuration",
			"configMap", r.CareInstruction.Spec.AuthenticationConfigMapName)
		return nil
	}

	var greenhouseAuthConfig apiserverv1beta1.AuthenticationConfiguration
	if err := yaml.Unmarshal([]byte(greenhouseAuthConfigMap.Data[authConfigMapKey]), &greenhouseAuthConfig); err != nil {
		return fmt.Errorf("failed to parse Greenhouse AuthenticationConfiguration: %w", err)
	}

	// Determine the ConfigMap name for Garden cluster
	// We create one CM per CareInstruction unless Shoot already has one configured
	configMapName := r.CareInstruction.Name + "-greenhouse-auth"
	useExistingConfigMap := false

	// Check if Shoot already has a ConfigMap configured
	if shoot.Spec.Kubernetes.KubeAPIServer != nil &&
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication != nil &&
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName != "" {
		configMapName = shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName
		useExistingConfigMap = true
		r.Info("Shoot already has AuthenticationConfiguration ConfigMap", "shoot", shoot.Name, "configMap", configMapName)
	}

	var gardenConfigMap corev1.ConfigMap

	if useExistingConfigMap {
		// Fetch the existing ConfigMap from Garden cluster
		if err := r.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: shoot.Namespace,
			Name:      configMapName,
		}, &gardenConfigMap); err != nil {
			return fmt.Errorf("failed to fetch existing AuthenticationConfiguration ConfigMap %s from Garden cluster: %w", configMapName, err)
		}
	} else {
		// Create new ConfigMap structure
		gardenConfigMap = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: shoot.Namespace,
			},
			Data: map[string]string{
				authConfigMapKey: "",
			},
		}
	}

	// Create or update the ConfigMap, merging configurations
	configMapResult, err := ctrl.CreateOrUpdate(ctx, r.GardenClient, &gardenConfigMap, func() error {
		return r.mergeAuthenticationConfigurations(&gardenConfigMap, &greenhouseAuthConfig)
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

	// Update Shoot spec to reference the ConfigMap if needed
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

	// At this point, Shoot spec doesn't need updates (ConfigMapName reference already exists)
	// Trigger Shoot reconciliation if ConfigMap content was updated
	// Reference: https://gardener.cloud/docs/gardener/shoot-operations/shoot_operations/#immediate-reconciliation
	if configMapResult == controllerutil.OperationResultUpdated {
		if shoot.Annotations == nil {
			shoot.Annotations = make(map[string]string)
		}
		shoot.Annotations["gardener.cloud/operation"] = "reconcile"

		if err := r.GardenClient.Update(ctx, shoot); err != nil {
			return fmt.Errorf("failed to annotate Shoot for reconciliation: %w", err)
		}
		r.Info("Annotated Shoot for reconciliation due to ConfigMap content update",
			"shoot", shoot.Name,
			"configMap", configMapName)
	}

	return nil
}

// mergeAuthenticationConfigurations merges the Greenhouse AuthenticationConfiguration
// with any existing configuration in the Garden ConfigMap. It intelligently merges:
// - JWT authenticators from both configurations
// - Deduplicates issuers by URL (Greenhouse config takes precedence)
// - Preserves Garden-specific configurations that don't conflict
func (r *ShootController) mergeAuthenticationConfigurations(gardenConfigMap *corev1.ConfigMap, greenhouseAuthConfig *apiserverv1beta1.AuthenticationConfiguration) error {
	var gardenAuthConfig apiserverv1beta1.AuthenticationConfiguration

	// Parse existing Garden configuration if present
	if gardenConfigMap.Data != nil && gardenConfigMap.Data[authConfigMapKey] != "" {
		existingConfigYAML := gardenConfigMap.Data[authConfigMapKey]
		if err := yaml.Unmarshal([]byte(existingConfigYAML), &gardenAuthConfig); err != nil {
			return fmt.Errorf("failed to parse existing Garden AuthenticationConfiguration: %w", err)
		}
	} else {
		// Create new configuration structure
		gardenAuthConfig = apiserverv1beta1.AuthenticationConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apiserver.config.k8s.io/v1beta1",
				Kind:       "AuthenticationConfiguration",
			},
			JWT: []apiserverv1beta1.JWTAuthenticator{},
		}
	}

	if gardenAuthConfig.JWT == nil {
		gardenAuthConfig.JWT = []apiserverv1beta1.JWTAuthenticator{}
	}

	// Create a map of existing Garden issuer URLs for quick lookup
	gardenIssuerURLs := make(map[string]int) // URL -> index in gardenAuthConfig.JWT
	for i, jwtAuth := range gardenAuthConfig.JWT {
		gardenIssuerURLs[jwtAuth.Issuer.URL] = i
	}

	// Merge JWT authenticators from Greenhouse configuration
	// Greenhouse issuers take precedence over Garden issuers with the same URL
	for _, greenhouseJWT := range greenhouseAuthConfig.JWT {
		if existingIndex, exists := gardenIssuerURLs[greenhouseJWT.Issuer.URL]; exists {
			// Update existing issuer with Greenhouse configuration
			r.Info("Updating issuer from Greenhouse configuration", "url", greenhouseJWT.Issuer.URL)
			gardenAuthConfig.JWT[existingIndex] = greenhouseJWT
		} else {
			// Add new issuer from Greenhouse configuration, no conflict since not added to gardenIssuerURLs map
			r.Info("Adding new issuer from Greenhouse configuration", "url", greenhouseJWT.Issuer.URL)
			gardenAuthConfig.JWT = append(gardenAuthConfig.JWT, greenhouseJWT)
		}
	}

	// Marshal merged configuration back to YAML
	mergedConfigYAML, err := yaml.Marshal(&gardenAuthConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal merged AuthenticationConfiguration: %w", err)
	}

	// Update ConfigMap data
	if gardenConfigMap.Data == nil {
		gardenConfigMap.Data = make(map[string]string)
	}
	gardenConfigMap.Data[authConfigMapKey] = string(mergedConfigYAML)

	return nil
}

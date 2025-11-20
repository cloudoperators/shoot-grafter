// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"fmt"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apiserver "k8s.io/apiserver/pkg/apis/apiserver"
	apiserverv1beta1 "k8s.io/apiserver/pkg/apis/apiserver/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

const (
	// greenhouseClaimPrefix is the prefix used for Greenhouse OIDC claims
	greenhouseClaimPrefix = "greenhouse:"

	// greenhouseAudience is the audience value for Greenhouse OIDC tokens
	greenhouseAudience = "greenhouse"
)

var (
	// scheme for decoding/encoding AuthenticationConfiguration
	authScheme = runtime.NewScheme()
	// codec factory for AuthenticationConfiguration
	authCodecFactory serializer.CodecFactory
)

func init() {
	// Register the apiserver types with the scheme
	_ = apiserver.AddToScheme(authScheme)
	_ = apiserverv1beta1.AddToScheme(authScheme)
	authCodecFactory = serializer.NewCodecFactory(authScheme)
}

// configureOIDCAuthentication configures OIDC authentication for the Shoot by creating/updating
// an AuthenticationConfiguration ConfigMap and updating the Shoot spec to reference it
func (r *ShootController) configureOIDCAuthentication(ctx context.Context, shoot *gardenerv1beta1.Shoot) error {

	// init configMapName with default value
	var configMapName = fmt.Sprintf("%s-greenhouse-auth", shoot.Name)
	var useExistingConfigMap = false

	// Check if Shoot already has a ConfigMap configured
	if shoot.Spec.Kubernetes.KubeAPIServer != nil &&
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication != nil &&
		shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName != "" {
		configMapName = shoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName
		useExistingConfigMap = true
		r.Info("Shoot already has AuthenticationConfiguration ConfigMap", "shoot", shoot.Name, "configMap", configMapName)
	}

	var configMap corev1.ConfigMap

	if useExistingConfigMap {

		// Fetch the existing ConfigMap
		if err := r.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: shoot.Namespace,
			Name:      configMapName,
		}, &configMap); err != nil {
			return fmt.Errorf("failed to fetch existing AuthenticationConfiguration ConfigMap %s: %w", configMapName, err)
		}

	} else {
		// Create new ConfigMap with empty configuration
		configMap = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: shoot.Namespace,
			},
			Data: map[string]string{
				"config.yaml": "",
			},
		}
	}

	// Create or update the ConfigMap with upsertGreenhouseIssuer modifying it
	configMapResult, err := ctrl.CreateOrUpdate(ctx, r.GardenClient, &configMap, func() error {
		// Update the ConfigMap data with Greenhouse issuer
		return r.upsertGreenhouseIssuer(&configMap)
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
	}

	return nil
}

// createGreenhouseIssuerConfig creates a JWT issuer configuration for Greenhouse
func (r *ShootController) createGreenhouseIssuerConfig() apiserverv1beta1.JWTAuthenticator {
	prefix := greenhouseClaimPrefix
	return apiserverv1beta1.JWTAuthenticator{
		Issuer: apiserverv1beta1.Issuer{
			URL:       r.CareInstruction.Spec.GreenhouseIssuerUrl,
			Audiences: []string{greenhouseAudience},
		},
		ClaimMappings: apiserverv1beta1.ClaimMappings{
			Username: apiserverv1beta1.PrefixedClaimOrExpression{
				Claim:  "sub",
				Prefix: &prefix,
			},
		},
	}
}

// upsertGreenhouseIssuer modifies the ConfigMap to add or update the Greenhouse issuer.
// It handles YAML unmarshaling, modification, and marshaling internally.
func (r *ShootController) upsertGreenhouseIssuer(configMap *corev1.ConfigMap) error {
	var authConfig apiserverv1beta1.AuthenticationConfiguration

	// Parse existing configuration if present
	if configMap.Data != nil && configMap.Data["config.yaml"] != "" {
		existingConfigYAML := configMap.Data["config.yaml"]
		if err := yaml.Unmarshal([]byte(existingConfigYAML), &authConfig); err != nil {
			return fmt.Errorf("failed to parse existing AuthenticationConfiguration: %w", err)
		}
	} else {
		// Create new configuration
		authConfig = apiserverv1beta1.AuthenticationConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: apiserverv1beta1.SchemeGroupVersion.String(),
				Kind:       "AuthenticationConfiguration",
			},
			JWT: []apiserverv1beta1.JWTAuthenticator{},
		}
	}

	if authConfig.JWT == nil {
		authConfig.JWT = []apiserverv1beta1.JWTAuthenticator{}
	}

	greenhouseURL := r.CareInstruction.Spec.GreenhouseIssuerUrl

	// Check if Greenhouse issuer already exists
	issuerFound := false
	for i, jwtConfig := range authConfig.JWT {
		if jwtConfig.Issuer.URL == greenhouseURL {
			// Update existing issuer configuration
			r.Info("Updating existing Greenhouse issuer configuration", "url", greenhouseURL)
			authConfig.JWT[i] = r.createGreenhouseIssuerConfig()
			issuerFound = true
			break
		}
	}

	if !issuerFound {
		// Greenhouse issuer doesn't exist, add it
		r.Info("Adding new Greenhouse issuer configuration", "url", greenhouseURL)
		authConfig.JWT = append(authConfig.JWT, r.createGreenhouseIssuerConfig())
	}

	// Marshal back to YAML
	configYAML, err := yaml.Marshal(&authConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal AuthenticationConfiguration: %w", err)
	}

	// Update ConfigMap data
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data["config.yaml"] = string(configYAML)

	return nil
}

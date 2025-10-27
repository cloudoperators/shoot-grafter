// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction

import (
	"context"
	"errors"

	"shoot-grafter/api/v1alpha1"

	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	gardenerAuthenticationv1alpha1 "github.com/gardener/gardener/pkg/apis/authentication/v1alpha1"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetGardenClusterAccess retrieves the rest.Config and scheme for the garden cluster. We do not return a client.Client here, since we build a manager later.
func (r *CareInstructionReconciler) GetGardenClusterAccess(ctx context.Context, careInstruction *v1alpha1.CareInstruction) (rest.Config, *runtime.Scheme, error) {
	gardenCluster := greenhousev1alpha1.Cluster{}
	var (
		exists           bool
		gardenKubeConfig []byte
	)

	// If GardenClusterKubeConfigSecretName is provided, use it to get the kubeconfig
	if careInstruction.Spec.GardenClusterKubeConfigSecretName.Name != "" {
		gardenClusterSecret := corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Name: careInstruction.Spec.GardenClusterKubeConfigSecretName.Name, Namespace: careInstruction.Namespace}, &gardenClusterSecret); err != nil {
			return rest.Config{}, nil, err
		}
		gardenKubeConfig, exists = gardenClusterSecret.Data[careInstruction.Spec.GardenClusterKubeConfigSecretName.Key]
		if !exists {
			return rest.Config{}, nil, errors.New("kubeconfig not found in gardenCluster secret")
		}
	} else { // else use GardenClusterName to get the greenhouse Cluster resource
		if err := r.Get(ctx, client.ObjectKey{Name: careInstruction.Spec.GardenClusterName, Namespace: careInstruction.Namespace}, &gardenCluster); err != nil {
			return rest.Config{}, nil, err
		}
		if !gardenCluster.Status.IsReadyTrue() {
			return rest.Config{}, nil, errors.New("GardenCluster is not ready")
		}
		gardenClusterSecret := corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Name: gardenCluster.Name, Namespace: careInstruction.Namespace}, &gardenClusterSecret); err != nil {
			return rest.Config{}, nil, err
		}
		gardenKubeConfig, exists = gardenClusterSecret.Data["greenhousekubeconfig"]
		if !exists {
			return rest.Config{}, nil, errors.New("kubeconfig not found in gardenCluster secret")
		}
	}
	gardenClientConfig, err := clientcmd.RESTConfigFromKubeConfig(gardenKubeConfig)
	if err != nil {
		return rest.Config{}, nil, err
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return *gardenClientConfig, nil, err
	}
	if err := gardenerv1beta1.AddToScheme(scheme); err != nil {
		return *gardenClientConfig, nil, err
	}
	if err := gardenerAuthenticationv1alpha1.AddToScheme(scheme); err != nil {
		return *gardenClientConfig, nil, err
	}
	return *gardenClientConfig, scheme, nil
}

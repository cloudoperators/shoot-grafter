// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"context"

	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	"github.com/cloudoperators/greenhouse/pkg/cel"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (

	// GardenClusterAccessReady indicates that the garden cluster is accessible.
	GardenClusterAccessReady greenhousemetav1alpha1.ConditionType = "GardenClusterAccessReady"

	// ShootControllerStartedCondition indicates that the shoot controller has been started for the shoots targeted by this CareInstruction.
	ShootControllerStartedCondition greenhousemetav1alpha1.ConditionType = "ShootControllerStarted"

	// DeleteCondition indicates that the CareInstruction has been deleted.
	DeleteCondition greenhousemetav1alpha1.ConditionType = "Delete"

	// ShootsReconciledCondition indicates that the shoots targeted by this CareInstruction have been reconciled.
	ShootsReconciledCondition greenhousemetav1alpha1.ConditionType = "ShootsReconciled"

	// CommonCleanupFinalizer is the finalizer used to clean up resources when a CareInstruction is deleted.
	CommonCleanupFinalizer = "shoot-grafter.cloudoperators.dev/finalizer"

	// CareInstructionLabel is the label used to identify resources created by this CareInstruction.
	CareInstructionLabel = "shoot-grafter.cloudoperators.dev/careinstruction"

	// AuthConfigMapLabel is the label used to identify AuthenticationConfiguration ConfigMaps
	AuthConfigMapLabel = "shoot-grafter.cloudoperators/authconfigmap"

	// ClusterStatusReady indicates the cluster is ready.
	ClusterStatusReady = "Ready"

	// ClusterStatusFailed indicates the cluster has failed.
	ClusterStatusFailed = "Failed"
)

// CareInstructionSpec holds the configuration for how to onboard Gardener shoots to Greenhouse.
type CareInstructionSpec struct {

	// GardenClusterKubeConfigSecretName is a reference to the secret containing the kubeconfig for the Garden cluster.
	// This is mutually exclusive to referring to the GardenCluster by a Greenhouse Cluster resource via GardenClusterName.
	// Order is 1. GardenClusterKubeConfigSecretName 2. GardenClusterName
	GardenClusterKubeConfigSecretName greenhousev1alpha1.SecretKeyReference `json:"gardenClusterKubeConfigSecretName,omitempty"`

	// GardenClusterName is the name of the Greenhouse Cluster representing the Gardener seed cluster from which the shoots will be reconciled.
	// This is mutually exclusive to referring to the GardenCluster by a kubeconfig secret via GardenClusterKubeConfigSecretName.
	// Order is 1. GardenClusterKubeConfigSecretName 2. GardenClusterName
	GardenClusterName string `json:"gardenClusterName,omitempty"`

	// GardenNamespace is the namespace in which Greenhouse will look for shoots on the seed cluster.
	GardenNamespace string `json:"gardenNamespace"`

	// ShootSelector is a label selector targeting shoots that should be reconciled.
	ShootSelector *metav1.LabelSelector `json:"shootSelector,omitempty"`

	// ShootFilter is a CEL expression for filtering shoots by status or other fields.
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	ShootFilter string `json:"shootFilter,omitempty"`

	// PropagateLabels is a list of labels that will be propagated from shoot to Greenhouse Cluster.
	PropagateLabels []string `json:"propagateLabels,omitempty"`

	// AdditionalLabels are labels that will be added to every Greenhouse Cluster created by this CareInstruction.
	AdditionalLabels map[string]string `json:"additionalLabels,omitempty"`

	// EnableRBAC indicates whether the automatic configuration of RBAC roles and role bindings for the Greenhouse service account on the shoot cluster should is enabled. Defaulted to true.
	// +kubebuilder:default=true
	EnableRBAC bool `json:"enableRBAC,omitempty"`

	// AuthenticationConfigMapName is a reference to a ConfigMap in the Greenhouse cluster
	// containing an AuthenticationConfiguration same as Gardener uses: https://gardener.cloud/docs/guides/administer-shoots/oidc-login/#configure-the-shoot-cluster
	// When set, the shoot controller will merge this configuration with any existing configuration
	// on the Garden cluster and configure the Shoot to use the merged authentication configuration.
	AuthenticationConfigMapName string `json:"authenticationConfigMapName,omitempty"`
}

// ClusterStatus represents the status of a single cluster managed by this CareInstruction.
type ClusterStatus struct {
	// Name of the cluster.
	Name string `json:"name"`

	// Status represents the current state of the cluster (Ready or Failed).
	// +kubebuilder:validation:Enum=Ready;Failed
	Status string `json:"status"`

	// Message provides additional information about the cluster status when Failed.
	Message string `json:"message,omitempty"`
}

// CareInstructionStatus holds the status of the CareInstruction.
type CareInstructionStatus struct {
	// StatusConditions represent the latest available observations of the CareInstruction's current state.
	greenhousemetav1alpha1.StatusConditions `json:"statusConditions,omitempty"`

	// Clusters is a list of clusters managed by this CareInstruction with their detailed status.
	Clusters []ClusterStatus `json:"clusters,omitempty"`

	// TotalShootCount is the total number of shoots targeted by this CareInstruction.
	TotalShoots int `json:"totalShootCount,omitempty"`

	// CreatedClusters is the number of clusters created by this CareInstruction.
	CreatedClusters int `json:"createdClusters,omitempty"`

	// FailedClusters is the number of clusters that failed to be created by this CareInstruction.
	FailedClusters int `json:"failedClusters,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ci
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="SeedCluster",type="string",JSONPath=".spec.seedClusterName"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.statusConditions.conditions[?(@.type == "Ready")].status`
type CareInstruction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CareInstructionSpec   `json:"spec,omitempty"`
	Status CareInstructionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type CareInstructionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CareInstruction `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CareInstruction{}, &CareInstructionList{})
}

// ListShoots returns shoots matching the ShootSelector and ShootFilter.
func (c *CareInstruction) ListShoots(ctx context.Context, gardenClient client.Client) (gardenerv1beta1.ShootList, error) {
	shootList := gardenerv1beta1.ShootList{}
	if err := gardenClient.List(ctx, &shootList, client.InNamespace(c.Spec.GardenNamespace), client.MatchingLabels(c.Spec.ShootSelector.MatchLabels)); err != nil {
		return gardenerv1beta1.ShootList{}, err
	}

	if c.Spec.ShootFilter == "" {
		return shootList, nil
	}

	shootList.Items = c.filterShootsWithCEL(ctx, shootList.Items)
	return shootList, nil
}

// MatchesCELFilter returns whether the shoot matches the ShootFilter CEL expression.
func (c *CareInstruction) MatchesCELFilter(shoot *gardenerv1beta1.Shoot) (bool, error) {
	if c.Spec.ShootFilter == "" {
		return true, nil
	}
	return cel.EvaluateTyped[bool](c.Spec.ShootFilter, shoot)
}

func (c *CareInstruction) filterShootsWithCEL(ctx context.Context, shoots []gardenerv1beta1.Shoot) []gardenerv1beta1.Shoot {
	logger := log.FromContext(ctx)
	filteredShoots := make([]gardenerv1beta1.Shoot, 0, len(shoots))

	for i := range shoots {
		shoot := &shoots[i]
		matches, err := c.MatchesCELFilter(shoot)
		if err != nil {
			logger.Info("skipping shoot, CEL evaluation failed", "shoot", shoot.Name, "error", err.Error())
			continue
		}
		if matches {
			filteredShoots = append(filteredShoots, *shoot)
		}
	}

	return filteredShoots
}

// ListClusters returns a list of clusters created by this CareInstruction identified by  owning CareInstruction label.
func (c *CareInstruction) ListClusters(ctx context.Context, greenhouseClient client.Client) (greenhousev1alpha1.ClusterList, error) {
	clusterList := greenhousev1alpha1.ClusterList{}
	if err := greenhouseClient.List(ctx, &clusterList, client.InNamespace(c.GetNamespace()), client.MatchingLabels{
		CareInstructionLabel: c.Name,
	}); err != nil {
		return greenhousev1alpha1.ClusterList{}, err
	}
	return clusterList, nil
}

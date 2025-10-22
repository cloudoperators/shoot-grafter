package v1alpha1

import (
	"context"

	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	CommonCleanupFinalizer = "shoot-grafter.cloudoperators/finalizer"

	// CareInstructionLabel is the label used to identify resources created by this CareInstruction.
	CareInstructionLabel = "shoot-grafter.cloudoperators/careinstruction"
)

// CareInstructionSpec holds the configuration for how to onboard Gardener shoots to Greenhouse.
type CareInstructionSpec struct {

	// GardenClusterKubeConfigSecretName is a reference to the secret containing the kubeconfig for the Garden cluster.
	// This is mutually exclusive to referring to the GardenCluster by a Greenhouse Cluster resource via GardenClusterName.
	// Order is 1. GardenClusterKubeConfigSecretName 2. GardenClusterName
	GardenClusterKubeConfigSecretName greenhousev1alpha1.SecretKeyReference `json:"gardenClusterKubeConfigSecretName,omitempty"`

	// GardenClusterName is the name of the Greenhouse Cluster representing the Gardener seed cluster from which the shoots will be reconciled.
	// This is mutually exclusive to referring to the GardenCluster by a kubeconfig secret via GardenClusterKubeConfigSecretName.
	// Order is 1. GardenClusterKubeConfigSecretName 2. GardenClusterNames
	GardenClusterName string `json:"gardenClusterName,omitempty"`

	// GardenNamespace is the namespace in which Greenhouse will look for shoots on the seed cluster.
	GardenNamespace string `json:"gardenNamespace"`

	// ShootSelector is a label selector targeting shoots that should be reconciled.
	ShootSelector *metav1.LabelSelector `json:"shootSelector,omitempty"`

	// TransportLabels is a list of labels that will be transported from shoot to Greenhouse Cluster.
	TransportLabels []string `json:"transportLabels,omitempty"`

	// AdditionalLabels are labels that will be added to every Greenhouse Cluster created by this CareInstruction.
	AdditionalLabels map[string]string `json:"additionalLabels,omitempty"`
}

// CareInstructionStatus holds the status of the CareInstruction.
type CareInstructionStatus struct {
	// StatusConditions represent the latest available observations of the CareInstruction's current state.
	greenhousemetav1alpha1.StatusConditions `json:"statusConditions,omitempty"`

	// LastUpdateTime is the last time the all shoots targeted by this CareInstruction were reconciled.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`

	// TotalShootCount is the total number of shoots targeted by this CareInstruction.
	TotalShoots int `json:"totalShootCount,omitempty"`

	// FailedShoots is the number of shoots that failed to be onboarded to Greenhouse.
	FailedShoots int `json:"failedShoots,omitempty"`

	// CreatedClusters is the number of clusters created by this CareInstruction.
	CreatedClusters int `json:"createdClusters,omitempty"`

	// FailedClusters is the number of clusters that failed to be created by this CareInstruction.
	FailedClusters int `json:"failedClusters,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
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

// ListShoots returns a list of shoots targeted by this CareInstruction.
func (c *CareInstruction) ListShoots(ctx context.Context, gardenClient client.Client) (gardenerv1beta1.ShootList, error) {
	shootList := gardenerv1beta1.ShootList{}
	if err := gardenClient.List(ctx, &shootList, client.InNamespace(c.Spec.GardenNamespace), client.MatchingLabels(c.Spec.ShootSelector.MatchLabels)); err != nil {
		return gardenerv1beta1.ShootList{}, err
	}
	return shootList, nil
}

// ListClusters returns a list of clusters created by this CareInstruction identified by  owning CareInstruction label.
func (c *CareInstruction) ListClusters(ctx context.Context, greenhouseClient client.Client, namespace string) (greenhousev1alpha1.ClusterList, error) {
	clusterList := greenhousev1alpha1.ClusterList{}
	if err := greenhouseClient.List(ctx, &clusterList, client.InNamespace(namespace), client.MatchingLabels{
		CareInstructionLabel: c.Name,
	}); err != nil {
		return greenhousev1alpha1.ClusterList{}, err
	}
	return clusterList, nil
}

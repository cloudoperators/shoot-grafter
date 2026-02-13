package careinstruction

import (
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"shoot-grafter/api/v1alpha1"
)

var (
	totalShootsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_total_shoots",
			Help: "Total number of shoots targeted by the CareInstruction",
		},
		[]string{"care_instruction", "namespace", "garden_namespace"},
	)
	createdClustersGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_created_clusters",
			Help: "Number of clusters created by the CareInstruction",
		},
		[]string{"care_instruction", "namespace", "garden_namespace"},
	)
	failedClustersGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_failed_clusters",
			Help: "Number of clusters failed to be created by the CareInstruction",
		},
		[]string{"care_instruction", "namespace", "garden_namespace"},
	)
	clusterReadyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_cluster_ready",
			Help: "Is cluster created by the CareInstruction ready",
		},
		[]string{"care_instruction", "namespace", "garden_namespace", "cluster_name"},
	)
)

func init() {
	crmetrics.Registry.MustRegister(
		totalShootsGauge,
		createdClustersGauge,
		failedClustersGauge,
		clusterReadyGauge,
	)
}

func UpdateCareInstructionMetrics(careInstruction *v1alpha1.CareInstruction) {
	updateTotalShootsMetric(careInstruction)
	updateCreatedClustersMetric(careInstruction)
	updateFailedClustersMetric(careInstruction)
	updateReadyClustersMetrics(careInstruction)
}

func updateTotalShootsMetric(careInstruction *v1alpha1.CareInstruction) {
	metricLabels := prometheus.Labels{
		"care_instruction": careInstruction.Name,
		"namespace":        careInstruction.Namespace,
		"garden_namespace": careInstruction.Spec.GardenNamespace,
	}
	totalShoots := careInstruction.Status.TotalShoots
	totalShootsGauge.With(metricLabels).Set(float64(totalShoots))
}

func updateCreatedClustersMetric(careInstruction *v1alpha1.CareInstruction) {
	metricLabels := prometheus.Labels{
		"care_instruction": careInstruction.Name,
		"namespace":        careInstruction.Namespace,
		"garden_namespace": careInstruction.Spec.GardenNamespace,
	}
	createdCount := careInstruction.Status.CreatedClusters
	createdClustersGauge.With(metricLabels).Set(float64(createdCount))
}

func updateFailedClustersMetric(careInstruction *v1alpha1.CareInstruction) {
	metricLabels := prometheus.Labels{
		"care_instruction": careInstruction.Name,
		"namespace":        careInstruction.Namespace,
		"garden_namespace": careInstruction.Spec.GardenNamespace,
	}
	failedCount := careInstruction.Status.FailedClusters
	failedClustersGauge.With(metricLabels).Set(float64(failedCount))
}

func updateReadyClustersMetrics(careInstruction *v1alpha1.CareInstruction) {
	for _, cs := range careInstruction.Status.Clusters {
		metricLabels := prometheus.Labels{
			"care_instruction": careInstruction.Name,
			"namespace":        careInstruction.Namespace,
			"garden_namespace": careInstruction.Spec.GardenNamespace,
			"cluster_name":     cs.Name,
		}
		if cs.Status == v1alpha1.ClusterStatusReady {
			clusterReadyGauge.With(metricLabels).Set(float64(1))
		} else {
			clusterReadyGauge.With(metricLabels).Set(float64(0))
		}
	}
}

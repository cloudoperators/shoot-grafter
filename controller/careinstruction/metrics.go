// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction

import (
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"shoot-grafter/api/v1alpha1"
)

const metricLabelCareInstruction = "care_instruction"
const metricLabelNamespace = "namespace"
const metricLabelGardenNamespace = "garden_namespace"
const metricLabelShootName = "shoot_name"

var (
	TotalTargetShootsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_total_target_shoots",
			Help: "Total number of shoots matching the CareInstruction label selector",
		},
		[]string{metricLabelCareInstruction, metricLabelNamespace, metricLabelGardenNamespace},
	)
	CreatedClustersGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_created_clusters",
			Help: "Number of clusters created by the CareInstruction",
		},
		[]string{metricLabelCareInstruction, metricLabelNamespace, metricLabelGardenNamespace},
	)
	FailedClustersGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_failed_clusters",
			Help: "Number of clusters failed to be created by the CareInstruction",
		},
		[]string{metricLabelCareInstruction, metricLabelNamespace, metricLabelGardenNamespace},
	)
	ShootOnboardedGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shoot_grafter_shoot_onboarded",
			Help: "Is shoot onboarded by the CareInstruction",
		},
		[]string{metricLabelCareInstruction, metricLabelNamespace, metricLabelGardenNamespace, metricLabelShootName},
	)
)

func init() {
	crmetrics.Registry.MustRegister(
		TotalTargetShootsGauge,
		CreatedClustersGauge,
		FailedClustersGauge,
		ShootOnboardedGauge,
	)
}

func UpdateCareInstructionMetrics(careInstruction *v1alpha1.CareInstruction) {
	updateTotalTargetShootsMetric(careInstruction)
	updateCreatedClustersMetric(careInstruction)
	updateFailedClustersMetric(careInstruction)
	updateOnboardedShootsMetrics(careInstruction)
}

func updateTotalTargetShootsMetric(careInstruction *v1alpha1.CareInstruction) {
	metricLabels := prometheus.Labels{
		metricLabelCareInstruction: careInstruction.Name,
		metricLabelNamespace:       careInstruction.Namespace,
		metricLabelGardenNamespace: careInstruction.Spec.GardenNamespace,
	}
	totalTargetShoots := careInstruction.Status.TotalTargetShoots
	TotalTargetShootsGauge.With(metricLabels).Set(float64(totalTargetShoots))
}

func updateCreatedClustersMetric(careInstruction *v1alpha1.CareInstruction) {
	metricLabels := prometheus.Labels{
		metricLabelCareInstruction: careInstruction.Name,
		metricLabelNamespace:       careInstruction.Namespace,
		metricLabelGardenNamespace: careInstruction.Spec.GardenNamespace,
	}
	createdCount := careInstruction.Status.CreatedClusters
	CreatedClustersGauge.With(metricLabels).Set(float64(createdCount))
}

func updateFailedClustersMetric(careInstruction *v1alpha1.CareInstruction) {
	metricLabels := prometheus.Labels{
		metricLabelCareInstruction: careInstruction.Name,
		metricLabelNamespace:       careInstruction.Namespace,
		metricLabelGardenNamespace: careInstruction.Spec.GardenNamespace,
	}
	failedCount := careInstruction.Status.FailedClusters
	FailedClustersGauge.With(metricLabels).Set(float64(failedCount))
}

func updateOnboardedShootsMetrics(careInstruction *v1alpha1.CareInstruction) {
	for _, ss := range careInstruction.Status.Shoots {
		metricLabels := prometheus.Labels{
			metricLabelCareInstruction: careInstruction.Name,
			metricLabelNamespace:       careInstruction.Namespace,
			metricLabelGardenNamespace: careInstruction.Spec.GardenNamespace,
			metricLabelShootName:       ss.Name,
		}
		if ss.Status == v1alpha1.ShootStatusOnboarded {
			ShootOnboardedGauge.With(metricLabels).Set(float64(1))
		} else {
			ShootOnboardedGauge.With(metricLabels).Set(float64(0))
		}
	}
}

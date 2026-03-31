// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/controller/careinstruction"
)

var _ = Describe("CareInstruction Metrics", func() {
	It("should update TotalShootsGauge metric correctly", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-1",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-ns-1",
			},
			Status: v1alpha1.CareInstructionStatus{
				TotalTargetShoots: 10,
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		value := promtest.ToFloat64(careinstruction.TotalTargetShootsGauge.With(labels))
		Expect(value).To(Equal(10.0))
	})

	It("should update CreatedClustersGauge metric correctly", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-2",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-ns-2",
			},
			Status: v1alpha1.CareInstructionStatus{
				CreatedClusters: 5,
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		value := promtest.ToFloat64(careinstruction.CreatedClustersGauge.With(labels))
		Expect(value).To(Equal(5.0))
	})

	It("should update FailedClustersGauge metric correctly", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-3",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-ns-3",
			},
			Status: v1alpha1.CareInstructionStatus{
				FailedClusters: 2,
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		value := promtest.ToFloat64(careinstruction.FailedClustersGauge.With(labels))
		Expect(value).To(Equal(2.0))
	})

	It("should update ShootOnboardedGauge metrics for onboarded shoots", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-4",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-ns-4",
			},
			Status: v1alpha1.CareInstructionStatus{
				Shoots: []v1alpha1.ShootStatus{
					{
						Name:   "shoot-1",
						Status: v1alpha1.ShootStatusOnboarded,
					},
					{
						Name:   "shoot-2",
						Status: v1alpha1.ShootStatusOnboarded,
					},
				},
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels1 := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"shoot_name":       "shoot-1",
		}
		labels2 := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"shoot_name":       "shoot-2",
		}

		value1 := promtest.ToFloat64(careinstruction.ShootOnboardedGauge.With(labels1))
		value2 := promtest.ToFloat64(careinstruction.ShootOnboardedGauge.With(labels2))
		Expect(value1).To(Equal(1.0))
		Expect(value2).To(Equal(1.0))
	})

	It("should update ShootOnboardedGauge metrics for non-onboarded shoots", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-5",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-ns-5",
			},
			Status: v1alpha1.CareInstructionStatus{
				Shoots: []v1alpha1.ShootStatus{
					{
						Name:   "shoot-failed",
						Status: v1alpha1.ShootStatusFailed,
					},
				},
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"shoot_name":       "shoot-failed",
		}

		value := promtest.ToFloat64(careinstruction.ShootOnboardedGauge.With(labels))
		Expect(value).To(Equal(0.0))
	})

	It("should update all metrics correctly for a complete CareInstruction", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-complete",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-complete",
			},
			Status: v1alpha1.CareInstructionStatus{
				TotalTargetShoots: 15,
				CreatedClusters:   12,
				FailedClusters:    3,
				Shoots: []v1alpha1.ShootStatus{
					{
						Name:   "shoot-onboarded-1",
						Status: v1alpha1.ShootStatusOnboarded,
					},
					{
						Name:   "shoot-onboarded-2",
						Status: v1alpha1.ShootStatusOnboarded,
					},
					{
						Name:   "shoot-failed-1",
						Status: v1alpha1.ShootStatusFailed,
					},
				},
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		baseLabels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		// Verify all gauge metrics
		totalShoots := promtest.ToFloat64(careinstruction.TotalTargetShootsGauge.With(baseLabels))
		createdClusters := promtest.ToFloat64(careinstruction.CreatedClustersGauge.With(baseLabels))
		failedClusters := promtest.ToFloat64(careinstruction.FailedClustersGauge.With(baseLabels))

		Expect(totalShoots).To(Equal(15.0))
		Expect(createdClusters).To(Equal(12.0))
		Expect(failedClusters).To(Equal(3.0))

		// Verify shoot onboarded states
		onboarded1Labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"shoot_name":       "shoot-onboarded-1",
		}
		onboarded2Labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"shoot_name":       "shoot-onboarded-2",
		}
		failedLabels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"shoot_name":       "shoot-failed-1",
		}

		onboarded1Value := promtest.ToFloat64(careinstruction.ShootOnboardedGauge.With(onboarded1Labels))
		onboarded2Value := promtest.ToFloat64(careinstruction.ShootOnboardedGauge.With(onboarded2Labels))
		failedValue := promtest.ToFloat64(careinstruction.ShootOnboardedGauge.With(failedLabels))

		Expect(onboarded1Value).To(Equal(1.0))
		Expect(onboarded2Value).To(Equal(1.0))
		Expect(failedValue).To(Equal(0.0))
	})

	It("should handle updates to existing metrics", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-update",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-update",
			},
			Status: v1alpha1.CareInstructionStatus{
				TotalTargetShoots: 5,
				CreatedClusters:   3,
				FailedClusters:    2,
			},
		}

		// First update
		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		totalShoots := promtest.ToFloat64(careinstruction.TotalTargetShootsGauge.With(labels))
		Expect(totalShoots).To(Equal(5.0))

		// Update the CareInstruction status
		ci.Status.TotalTargetShoots = 10
		ci.Status.CreatedClusters = 8
		ci.Status.FailedClusters = 1

		// Second update
		careinstruction.UpdateCareInstructionMetrics(ci)

		totalShoots = promtest.ToFloat64(careinstruction.TotalTargetShootsGauge.With(labels))
		createdClusters := promtest.ToFloat64(careinstruction.CreatedClustersGauge.With(labels))
		failedClusters := promtest.ToFloat64(careinstruction.FailedClustersGauge.With(labels))

		Expect(totalShoots).To(Equal(10.0))
		Expect(createdClusters).To(Equal(8.0))
		Expect(failedClusters).To(Equal(1.0))
	})

	It("should handle CareInstruction with no shoots", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-empty",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-empty",
			},
			Status: v1alpha1.CareInstructionStatus{
				TotalTargetShoots: 0,
				CreatedClusters:   0,
				FailedClusters:    0,
				Shoots:            []v1alpha1.ShootStatus{},
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		totalShoots := promtest.ToFloat64(careinstruction.TotalTargetShootsGauge.With(labels))
		createdClusters := promtest.ToFloat64(careinstruction.CreatedClustersGauge.With(labels))
		failedClusters := promtest.ToFloat64(careinstruction.FailedClustersGauge.With(labels))

		Expect(totalShoots).To(Equal(0.0))
		Expect(createdClusters).To(Equal(0.0))
		Expect(failedClusters).To(Equal(0.0))
	})
})

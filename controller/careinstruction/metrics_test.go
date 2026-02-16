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
				TotalShoots: 10,
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		value := promtest.ToFloat64(careinstruction.TotalShootsGauge.With(labels))
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

	It("should update ClusterReadyGauge metrics for ready clusters", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-4",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-ns-4",
			},
			Status: v1alpha1.CareInstructionStatus{
				Clusters: []v1alpha1.ClusterStatus{
					{
						Name:   "cluster-1",
						Status: v1alpha1.ClusterStatusReady,
					},
					{
						Name:   "cluster-2",
						Status: v1alpha1.ClusterStatusReady,
					},
				},
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels1 := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"cluster_name":     "cluster-1",
		}
		labels2 := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"cluster_name":     "cluster-2",
		}

		value1 := promtest.ToFloat64(careinstruction.ClusterReadyGauge.With(labels1))
		value2 := promtest.ToFloat64(careinstruction.ClusterReadyGauge.With(labels2))
		Expect(value1).To(Equal(1.0))
		Expect(value2).To(Equal(1.0))
	})

	It("should update ClusterReadyGauge metrics for failed clusters", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-5",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-ns-5",
			},
			Status: v1alpha1.CareInstructionStatus{
				Clusters: []v1alpha1.ClusterStatus{
					{
						Name:   "cluster-failed",
						Status: v1alpha1.ClusterStatusFailed,
					},
				},
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"cluster_name":     "cluster-failed",
		}

		value := promtest.ToFloat64(careinstruction.ClusterReadyGauge.With(labels))
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
				TotalShoots:     15,
				CreatedClusters: 12,
				FailedClusters:  3,
				Clusters: []v1alpha1.ClusterStatus{
					{
						Name:   "cluster-ready-1",
						Status: v1alpha1.ClusterStatusReady,
					},
					{
						Name:   "cluster-ready-2",
						Status: v1alpha1.ClusterStatusReady,
					},
					{
						Name:   "cluster-failed-1",
						Status: v1alpha1.ClusterStatusFailed,
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
		totalShoots := promtest.ToFloat64(careinstruction.TotalShootsGauge.With(baseLabels))
		createdClusters := promtest.ToFloat64(careinstruction.CreatedClustersGauge.With(baseLabels))
		failedClusters := promtest.ToFloat64(careinstruction.FailedClustersGauge.With(baseLabels))

		Expect(totalShoots).To(Equal(15.0))
		Expect(createdClusters).To(Equal(12.0))
		Expect(failedClusters).To(Equal(3.0))

		// Verify cluster ready states
		ready1Labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"cluster_name":     "cluster-ready-1",
		}
		ready2Labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"cluster_name":     "cluster-ready-2",
		}
		failedLabels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
			"cluster_name":     "cluster-failed-1",
		}

		ready1Value := promtest.ToFloat64(careinstruction.ClusterReadyGauge.With(ready1Labels))
		ready2Value := promtest.ToFloat64(careinstruction.ClusterReadyGauge.With(ready2Labels))
		failedValue := promtest.ToFloat64(careinstruction.ClusterReadyGauge.With(failedLabels))

		Expect(ready1Value).To(Equal(1.0))
		Expect(ready2Value).To(Equal(1.0))
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
				TotalShoots:     5,
				CreatedClusters: 3,
				FailedClusters:  2,
			},
		}

		// First update
		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		totalShoots := promtest.ToFloat64(careinstruction.TotalShootsGauge.With(labels))
		Expect(totalShoots).To(Equal(5.0))

		// Update the CareInstruction status
		ci.Status.TotalShoots = 10
		ci.Status.CreatedClusters = 8
		ci.Status.FailedClusters = 1

		// Second update
		careinstruction.UpdateCareInstructionMetrics(ci)

		totalShoots = promtest.ToFloat64(careinstruction.TotalShootsGauge.With(labels))
		createdClusters := promtest.ToFloat64(careinstruction.CreatedClustersGauge.With(labels))
		failedClusters := promtest.ToFloat64(careinstruction.FailedClustersGauge.With(labels))

		Expect(totalShoots).To(Equal(10.0))
		Expect(createdClusters).To(Equal(8.0))
		Expect(failedClusters).To(Equal(1.0))
	})

	It("should handle CareInstruction with no clusters", func() {
		ci := &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ci-empty",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				GardenNamespace: "garden-empty",
			},
			Status: v1alpha1.CareInstructionStatus{
				TotalShoots:     0,
				CreatedClusters: 0,
				FailedClusters:  0,
				Clusters:        []v1alpha1.ClusterStatus{},
			},
		}

		careinstruction.UpdateCareInstructionMetrics(ci)

		labels := prometheus.Labels{
			"care_instruction": ci.Name,
			"namespace":        ci.Namespace,
			"garden_namespace": ci.Spec.GardenNamespace,
		}

		totalShoots := promtest.ToFloat64(careinstruction.TotalShootsGauge.With(labels))
		createdClusters := promtest.ToFloat64(careinstruction.CreatedClustersGauge.With(labels))
		failedClusters := promtest.ToFloat64(careinstruction.FailedClustersGauge.With(labels))

		Expect(totalShoots).To(Equal(0.0))
		Expect(createdClusters).To(Equal(0.0))
		Expect(failedClusters).To(Equal(0.0))
	})
})

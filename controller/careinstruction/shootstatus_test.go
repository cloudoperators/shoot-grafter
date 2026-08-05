// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction

import (
	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"shoot-grafter/api/v1alpha1"
)

func readyCluster(name string) greenhousev1alpha1.Cluster {
	cluster := greenhousev1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	cluster.Status.SetConditions(greenhousemetav1alpha1.NewCondition(
		greenhousemetav1alpha1.ReadyCondition, metav1.ConditionTrue, "ClusterReady", "Cluster is ready"))
	return cluster
}

func notReadyCluster(name, message string) greenhousev1alpha1.Cluster {
	cluster := greenhousev1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	cluster.Status.SetConditions(greenhousemetav1alpha1.NewCondition(
		greenhousemetav1alpha1.ReadyCondition, metav1.ConditionFalse, "ClusterNotReady", message))
	return cluster
}

var _ = Describe("buildShootStatuses", func() {
	It("reports an excluded shoot once, even when it still owns a ready cluster", func() {
		statuses, failed := buildShootStatuses(
			map[string]string{"shoot-a": "excluded for test reasons"},
			[]greenhousev1alpha1.Cluster{readyCluster("shoot-a")},
			nil,
		)

		Expect(statuses).To(HaveLen(1))
		Expect(statuses[0].Name).To(Equal("shoot-a"))
		Expect(statuses[0].Status).To(Equal(v1alpha1.ShootStatusExcluded))
		Expect(statuses[0].Message).To(Equal("excluded for test reasons"))
		Expect(failed).To(Equal(0))
	})

	It("does not count an excluded shoot as failed when its cluster is not ready", func() {
		statuses, failed := buildShootStatuses(
			map[string]string{"shoot-a": "excluded for test reasons"},
			[]greenhousev1alpha1.Cluster{notReadyCluster("shoot-a", "kubeconfig invalid")},
			nil,
		)

		Expect(statuses).To(HaveLen(1))
		Expect(statuses[0].Status).To(Equal(v1alpha1.ShootStatusExcluded))
		Expect(failed).To(Equal(0))
	})

	It("reports owned clusters as onboarded or failed", func() {
		statuses, failed := buildShootStatuses(
			nil,
			[]greenhousev1alpha1.Cluster{
				readyCluster("shoot-ready"),
				notReadyCluster("shoot-broken", "kubeconfig invalid"),
			},
			nil,
		)

		Expect(statuses).To(HaveLen(2))
		Expect(statuses[0].Name).To(Equal("shoot-broken"))
		Expect(statuses[0].Status).To(Equal(v1alpha1.ShootStatusFailed))
		Expect(statuses[0].Message).To(Equal("kubeconfig invalid"))
		Expect(statuses[1].Name).To(Equal("shoot-ready"))
		Expect(statuses[1].Status).To(Equal(v1alpha1.ShootStatusOnboarded))
		Expect(failed).To(Equal(1))
	})

	It("reports ownership conflicts with the owning CareInstruction", func() {
		statuses, failed := buildShootStatuses(
			nil,
			nil,
			map[string]conflictInfo{
				"shoot-taken":  {owner: "other-ci", ready: true},
				"shoot-broken": {owner: "other-ci", ready: false},
			},
		)

		Expect(statuses).To(HaveLen(2))
		Expect(statuses[0].Name).To(Equal("shoot-broken"))
		Expect(statuses[0].Status).To(Equal(v1alpha1.ShootStatusFailed))
		Expect(statuses[0].Message).To(Equal("Cluster managed by different CareInstruction: other-ci"))
		Expect(statuses[1].Name).To(Equal("shoot-taken"))
		Expect(statuses[1].Status).To(Equal(v1alpha1.ShootStatusOnboarded))
		Expect(failed).To(Equal(1))
	})

	It("returns entries sorted by name regardless of input order", func() {
		statuses, _ := buildShootStatuses(
			map[string]string{"zulu": "excluded for test reasons", "alpha": "excluded for test reasons"},
			[]greenhousev1alpha1.Cluster{readyCluster("mike"), readyCluster("bravo")},
			nil,
		)

		names := make([]string, 0, len(statuses))
		for _, status := range statuses {
			names = append(names, status.Name)
		}
		Expect(names).To(Equal([]string{"alpha", "bravo", "mike", "zulu"}))
	})

	It("returns an empty slice when there is nothing to report", func() {
		statuses, failed := buildShootStatuses(nil, nil, nil)

		Expect(statuses).To(BeEmpty())
		Expect(failed).To(Equal(0))
	})
})

var _ = Describe("shootStatusRank", func() {
	It("ranks Excluded above Failed above Onboarded", func() {
		Expect(shootStatusRank(v1alpha1.ShootStatusExcluded)).To(BeNumerically(">", shootStatusRank(v1alpha1.ShootStatusFailed)))
		Expect(shootStatusRank(v1alpha1.ShootStatusFailed)).To(BeNumerically(">", shootStatusRank(v1alpha1.ShootStatusOnboarded)))
	})
})

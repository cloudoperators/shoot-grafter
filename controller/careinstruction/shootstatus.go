// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction

import (
	"sort"

	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"

	"shoot-grafter/api/v1alpha1"
)

// conflictInfo describes a shoot whose Greenhouse Cluster is owned by a different CareInstruction.
type conflictInfo struct {
	owner string
	ready bool
}

// shootStatusRank orders shoot statuses by precedence. The highest rank wins when a shoot is
// reported by more than one source: Excluded states the shoot is no longer selected at all, and
// Failed must not be masked by Onboarded.
func shootStatusRank(status string) int {
	switch status {
	case v1alpha1.ShootStatusExcluded:
		return 2
	case v1alpha1.ShootStatusFailed:
		return 1
	default:
		return 0
	}
}

// buildShootStatuses merges the shoots excluded by the ShootSelector, the Greenhouse Clusters owned
// by this CareInstruction and the ownership conflicts into a single entry per shoot name, sorted by
// name. It also returns how many entries ended up as Failed.
func buildShootStatuses(
	excluded map[string]string,
	ownedClusters []greenhousev1alpha1.Cluster,
	conflicts map[string]conflictInfo,
) (statuses []v1alpha1.ShootStatus, failed int) {

	byName := make(map[string]v1alpha1.ShootStatus, len(excluded)+len(ownedClusters)+len(conflicts))
	setStatus := func(shootStatus v1alpha1.ShootStatus) {
		if current, ok := byName[shootStatus.Name]; ok && shootStatusRank(current.Status) > shootStatusRank(shootStatus.Status) {
			return
		}
		byName[shootStatus.Name] = shootStatus
	}

	for name, reason := range excluded {
		setStatus(v1alpha1.ShootStatus{
			Name:    name,
			Status:  v1alpha1.ShootStatusExcluded,
			Message: reason,
		})
	}

	for _, cluster := range ownedClusters {
		shootStatus := v1alpha1.ShootStatus{Name: cluster.Name}
		if cluster.Status.IsReadyTrue() {
			shootStatus.Status = v1alpha1.ShootStatusOnboarded
		} else {
			shootStatus.Status = v1alpha1.ShootStatusFailed
			readyCondition := cluster.Status.GetConditionByType(greenhousemetav1alpha1.ReadyCondition)
			if readyCondition != nil && readyCondition.Message != "" {
				shootStatus.Message = readyCondition.Message
			}
		}
		setStatus(shootStatus)
	}

	for name, conflict := range conflicts {
		shootStatus := v1alpha1.ShootStatus{
			Name:    name,
			Message: "Cluster managed by different CareInstruction: " + conflict.owner,
		}
		if conflict.ready {
			shootStatus.Status = v1alpha1.ShootStatusOnboarded
		} else {
			shootStatus.Status = v1alpha1.ShootStatusFailed
		}
		setStatus(shootStatus)
	}

	statuses = make([]v1alpha1.ShootStatus, 0, len(byName))
	for _, shootStatus := range byName {
		statuses = append(statuses, shootStatus)
		if shootStatus.Status == v1alpha1.ShootStatusFailed {
			failed++
		}
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })

	return statuses, failed
}

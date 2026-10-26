// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction

import (
	"context"

	"shoot-grafter/api/v1alpha1"

	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	"github.com/cloudoperators/greenhouse/pkg/lifecycle"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var knownConditionTypes = []greenhousemetav1alpha1.ConditionType{
	v1alpha1.GardenClusterAccessReady,
	v1alpha1.ShootControllerStartedCondition,
	v1alpha1.ShootsReconciledCondition,
}

// InitializeConditionsToUnknown initializes all known conditions to unknown if they are not set yet.
func initializeConditionsToUnknown(careInstruction *v1alpha1.CareInstruction) {
	if careInstruction.Status.Conditions == nil {
		careInstruction.Status.Conditions = make([]greenhousemetav1alpha1.Condition, 0)
	}

	for _, conditionType := range knownConditionTypes {
		if careInstruction.Status.GetConditionByType(conditionType) == nil {
			careInstruction.Status.SetConditions(
				greenhousemetav1alpha1.UnknownCondition(
					conditionType,
					"Unknown",
					"Condition is unknown",
				),
			)
		}
	}
}

func (r *CareInstructionReconciler) reconcileStatus(ctx context.Context, obj lifecycle.RuntimeObject) {
	careInstruction := obj.(*v1alpha1.CareInstruction)
	// Initialize ready condition to true, will be overwritten if any sub condition != true
	careInstruction.Status.SetConditions(
		greenhousemetav1alpha1.TrueCondition(
			greenhousemetav1alpha1.ReadyCondition,
			"Ready",
			"CareInstruction is ready",
		),
	)
	// Compute overall ready condition
	// Subconditions need to be true for the overall condition to be true
	// Message and Reason is taken from the first failing sub condition
	for _, conditionType := range knownConditionTypes {
		subCondition := careInstruction.Status.GetConditionByType(conditionType)
		if subCondition == nil {
			continue
		}
		if subCondition.Status != metav1.ConditionTrue {
			careInstruction.Status.SetConditions(
				greenhousemetav1alpha1.FalseCondition(
					greenhousemetav1alpha1.ReadyCondition,
					subCondition.Reason,
					subCondition.Message,
				),
			)
			break
		}
	}
}

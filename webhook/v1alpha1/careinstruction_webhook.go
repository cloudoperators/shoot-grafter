package v1alpha1

import (
	"context"
	"fmt"

	"shoot-grafter/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

// +kubebuilder:webhook:path=/mutate-shoot-grafter-cloudoperators-v1alpha1-careinstruction,mutating=true,failurePolicy=fail,groups=shoot-grafter.cloudoperators,resources=careinstructions,verbs=create;update,versions=v1alpha1,name=mcareinstruction.kb.io,sideEffects=None,admissionReviewVersions=v1

type CareInstructionWebhook struct{}

// SetupWebhookWithManager sets up the webhook with the manager.
func (r *CareInstructionWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&v1alpha1.CareInstruction{}).
		WithDefaulter(&CareInstructionWebhook{}).
		Complete()
}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *CareInstructionWebhook) Default(ctx context.Context, obj runtime.Object) error {
	careInstruction, ok := obj.(*v1alpha1.CareInstruction)
	if !ok {
		return fmt.Errorf("expected CareInstruction, got %T", obj)
	}

	if careInstruction.Spec.ShootSelector == nil {
		careInstruction.Spec.ShootSelector = &metav1.LabelSelector{}
	}

	return nil
}

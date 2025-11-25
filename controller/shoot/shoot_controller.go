// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"shoot-grafter/api/v1alpha1"

	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// shootCACMSuffix is the suffix used to identify the ConfigMap containing the CA for the shoot api-server.
	shootCACMSuffix = ".ca-cluster"
)

type ShootController struct {
	GreenhouseClient client.Client
	GardenClient     client.Client
	logr.Logger
	Name            string
	CareInstruction *v1alpha1.CareInstruction
	EventRecorder   record.EventRecorder // EventRecorder to emit events on the Greenhouse cluster
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// emitEvent safely emits an event if EventRecorder is available
func (r *ShootController) emitEvent(object client.Object, eventType, reason, message string) {
	if r.EventRecorder != nil {
		r.EventRecorder.Event(object, eventType, reason, message)
	} else {
		r.Info("Event (EventRecorder not available)", "type", eventType, "reason", reason, "message", message)
	}
}

func (r *ShootController) SetupWithManager(mgr ctrl.Manager) error {
	shootSelectorPredicate, err := predicate.LabelSelectorPredicate(*r.CareInstruction.Spec.ShootSelector)

	if err != nil {
		return err
	}

	// Log missing event recorder
	if r.EventRecorder == nil {
		r.Error(nil, "EventRecorder is not set for ShootController", "name", r.Name)
	}

	// Setup the shoot controller with the manager
	return ctrl.NewControllerManagedBy(mgr).
		Named(r.Name).
		For(&gardenerv1beta1.Shoot{}, builder.WithPredicates(shootSelectorPredicate)).
		Complete(r)
}

func (r *ShootController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Info("Reconciling Shoot", "name", req.Name, "namespace", req.Namespace)

	var shoot gardenerv1beta1.Shoot
	if err := r.GardenClient.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &shoot); err != nil {
		r.Info("unable to fetch Shoot")
		if client.IgnoreNotFound(err) == nil {
			// Shoot was deleted
			r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "ShootDeleted",
				fmt.Sprintf("Shoot %s/%s was deleted", req.Namespace, req.Name))
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	apiServerURL := ""
	// ApiServerURL is the Advertised Address with .name="external".
	if shoot.Status.AdvertisedAddresses != nil {
		for _, addr := range shoot.Status.AdvertisedAddresses {
			if addr.Name == "external" {
				apiServerURL = addr.URL
			}
		}
	}
	if apiServerURL == "" {
		r.Info("no external API server URL found for Shoot", "name", shoot.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "APIServerURLMissing",
			fmt.Sprintf("No external API server URL found for shoot %s/%s", shoot.Namespace, shoot.Name))
		return ctrl.Result{}, nil
	}

	// Initialize labels and label propagation with the CareInstruction labels
	labels := map[string]string{
		v1alpha1.CareInstructionLabel: r.CareInstruction.Name,
	}
	labelPropagateString := v1alpha1.CareInstructionLabel + ","
	// get labels specified via TransportLabels on the CareInstruction from the shoot
	for _, v := range r.CareInstruction.Spec.PropagateLabels {
		if labelValue, ok := shoot.Labels[v]; ok {
			labels[v] = labelValue
			labelPropagateString += v + ","
		}
	}
	// get additional labels specified via AdditionalLabels on the CareInstruction
	if r.CareInstruction.Spec.AdditionalLabels != nil {
		for k, v := range r.CareInstruction.Spec.AdditionalLabels {
			labels[k] = v
			labelPropagateString += k + ","
		}
	}

	annotations := map[string]string{
		"greenhouse.sap/propagate-labels":           labelPropagateString,
		greenhouseapis.SecretAPIServerURLAnnotation: apiServerURL,
	}

	// create or update Secret with the CA data from the shoot
	// and the labels from the CareInstruction
	var cm corev1.ConfigMap
	if err := r.GardenClient.Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name + shootCACMSuffix}, &cm); err != nil {
		r.Info("unable to fetch CA ConfigMap for Shoot")
		r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "CAConfigMapFetchFailed",
			fmt.Sprintf("Failed to fetch CA ConfigMap for shoot %s/%s: %v", shoot.Namespace, shoot.Name, err))
		return ctrl.Result{}, err
	}

	CAData := cm.Data["ca.crt"]
	if CAData == "" {
		r.Info("no CA data found in ConfigMap for Shoot", "name", cm.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "CADataMissing",
			fmt.Sprintf("No CA data found in ConfigMap %s for shoot %s/%s", cm.Name, shoot.Namespace, shoot.Name))
		return ctrl.Result{}, nil
	}
	CADataBytes := []byte(CAData)
	CADataBase64Enc := make([]byte, base64.StdEncoding.EncodedLen(len(CADataBytes)))
	base64.StdEncoding.Encode(CADataBase64Enc, CADataBytes)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        shoot.Name,
			Namespace:   r.CareInstruction.Namespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Data: map[string][]byte{
			"ca.crt": CADataBase64Enc,
		},
		Type: greenhouseapis.SecretTypeOIDCConfig,
	}

	result, err := ctrl.CreateOrUpdate(ctx, r.GreenhouseClient, secret, func() error {
		secret.Data = map[string][]byte{
			"ca.crt": CADataBase64Enc,
		}
		// Merge annotations - preserve existing ones and add/update ours
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		for k, v := range annotations {
			secret.Annotations[k] = v
		}
		// Merge labels - preserve existing ones and add/update ours
		if secret.Labels == nil {
			secret.Labels = make(map[string]string)
		}
		for k, v := range labels {
			secret.Labels[k] = v
		}
		return nil
	})
	if err != nil {
		r.Info("unable to create or update Secret for Shoot", "name", shoot.Name)
		// Check if the error is due to a conflict (concurrent update)
		// In this case, just requeue without emitting an event
		if apierrors.IsConflict(err) || strings.Contains(err.Error(), "the object has been modified") {
			r.Info("Secret was modified concurrently, requeueing", "name", shoot.Name)
			return ctrl.Result{Requeue: true}, nil
		}
		r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "SecretOperationFailed",
			fmt.Sprintf("Failed to create or update secret for shoot %s/%s: %v", shoot.Namespace, shoot.Name, err))
		return ctrl.Result{}, err
	}
	switch result {
	case controllerutil.OperationResultCreated:
		r.Info("Secret for Shoot created", "name", shoot.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "SecretCreated",
			fmt.Sprintf("Created Greenhouse secret %s for shoot %s/%s with API server URL %s",
				secret.Name, shoot.Namespace, shoot.Name, apiServerURL))
	case controllerutil.OperationResultUpdated:
		r.Info("Secret for Shoot updated", "name", shoot.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "SecretUpdated",
			fmt.Sprintf("Updated Greenhouse secret %s for shoot %s/%s with API server URL %s",
				secret.Name, shoot.Namespace, shoot.Name, apiServerURL))
	case controllerutil.OperationResultNone:
		r.Info("Secret for Shoot unchanged", "name", shoot.Name)
	default:
		r.Info("Secret for Shoot processed", "name", shoot.Name, "result", result)
	}

	r.Info("Successfully reconciled Shoot", "name", shoot.Name)

	if !r.CareInstruction.Spec.DisableRBAC {
		shootClient, err := getShootClusterClient(ctx, r.GardenClient, &shoot)
		if err != nil {
			r.Error(err, "unable to get Shoot cluster client", "name", shoot.Name)
			return ctrl.Result{}, err
		}
		r.setRBAC(ctx, shootClient, shoot.GetName())
	}

	return ctrl.Result{}, nil
}

// GenerateName generates a name for the shoot controller based on the garden cluster name.
func GenerateName(gardenClusterName string) string {
	return "shoot-controller-" + gardenClusterName
}

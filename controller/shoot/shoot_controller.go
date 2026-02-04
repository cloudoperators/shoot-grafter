// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"strings"

	"shoot-grafter/api/v1alpha1"

	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	"github.com/cloudoperators/greenhouse/pkg/lifecycle"
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

	// Check if a cluster with this name already exists and is owned by a different CareInstruction
	// Do this early to avoid unnecessary work
	var existingCluster greenhousev1alpha1.Cluster
	err := r.GreenhouseClient.Get(ctx, client.ObjectKey{Name: req.Name, Namespace: r.CareInstruction.Namespace}, &existingCluster)
	if err == nil {
		// Cluster exists - check ownership
		if ownerLabel, hasLabel := existingCluster.Labels[v1alpha1.CareInstructionLabel]; hasLabel && ownerLabel != r.CareInstruction.Name {
			// TODO: emit event on CareInstruction
			r.Info("Skipping shoot - cluster already owned by different CareInstruction",
				"shoot", req.Name,
				"currentOwner", ownerLabel,
				"attemptedOwner", r.CareInstruction.Name)
			return ctrl.Result{}, nil
		}
	}

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

	matches, err := r.CareInstruction.MatchesCELFilter(&shoot)
	if err != nil {
		r.Info("CEL filter evaluation failed, skipping shoot", "shoot", shoot.Name, "error", err.Error())
		return ctrl.Result{}, nil
	}
	if !matches {
		r.Info("shoot filtered out by CEL", "shoot", shoot.Name)
		return ctrl.Result{}, nil
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

	// Specify which labels to propagate from the Shoot to the Secret
	if shoot.Annotations == nil {
		shoot.Annotations = make(map[string]string)
	}
	shoot.Annotations[lifecycle.PropagateLabelsAnnotation] = strings.Join(r.CareInstruction.Spec.PropagateLabels, ",")
	// Specify which labels should be propagated from the Secret (by Greenhouse to create Cluster)
	labelKeysToPropagate := r.CareInstruction.Spec.PropagateLabels

	// Initialize secret labels based on CareInstruction
	secretLabels := make(map[string]string)

	// Get additional labels to set on the Secret
	if r.CareInstruction.Spec.AdditionalLabels != nil {
		for k, v := range r.CareInstruction.Spec.AdditionalLabels {
			secretLabels[k] = v
			labelKeysToPropagate = append(labelKeysToPropagate, k)
		}
	}

	// Set the identifying label
	secretLabels[v1alpha1.CareInstructionLabel] = r.CareInstruction.Name
	labelKeysToPropagate = append(labelKeysToPropagate, v1alpha1.CareInstructionLabel)

	secretAnnotations := map[string]string{
		"greenhouse.sap/propagate-labels":           strings.Join(labelKeysToPropagate, ","),
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

	caData := cm.Data["ca.crt"]
	if caData == "" {
		r.Info("no CA data found in ConfigMap for Shoot", "name", cm.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "CADataMissing",
			fmt.Sprintf("No CA data found in ConfigMap %s for shoot %s/%s", cm.Name, shoot.Namespace, shoot.Name))
		return ctrl.Result{}, nil
	}
	caDataBytes := []byte(caData)
	caDataBase64Enc := make([]byte, base64.StdEncoding.EncodedLen(len(caDataBytes)))
	base64.StdEncoding.Encode(caDataBase64Enc, caDataBytes)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        shoot.Name,
			Namespace:   r.CareInstruction.Namespace,
			Annotations: secretAnnotations,
			Labels:      secretLabels,
		},
		Data: map[string][]byte{
			"ca.crt": caDataBase64Enc,
		},
		Type: greenhouseapis.SecretTypeOIDCConfig,
	}

	result, err := ctrl.CreateOrUpdate(ctx, r.GreenhouseClient, secret, func() error {
		secret.Data = map[string][]byte{
			"ca.crt": caDataBase64Enc,
		}
		// Merge annotations - preserve existing ones and add/update ours
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		maps.Copy(secret.Annotations, secretAnnotations)
		// Merge labels - preserve existing ones and add/update ours
		if secret.Labels == nil {
			secret.Labels = make(map[string]string)
		}
		maps.Copy(secret.Labels, secretLabels)

		// Transport Shoot labels to the Secret
		secret = (lifecycle.NewPropagator(&shoot, secret).Apply()).(*corev1.Secret)
		return nil
	})
	if err != nil {
		r.Info("unable to create or update Secret for Shoot", "name", shoot.Name)
		// Check if the error is due to a conflict (concurrent update)
		// In this case, just requeue without emitting an event
		if apierrors.IsConflict(err) || strings.Contains(err.Error(), "the object has been modified") {
			r.Info("Secret was modified concurrently, requeuing", "name", shoot.Name)
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

	// Configure OIDC authentication if AuthenticationConfigMapName is set
	// Do this before RBAC setup so RBAC errors don't prevent OIDC configuration
	if r.CareInstruction.Spec.AuthenticationConfigMapName != "" {
		if err := r.configureOIDCAuthentication(ctx, &shoot); err != nil {
			r.Info("failed to configure OIDC authentication for Shoot", "name", shoot.Name, "error", err)
			r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "OIDCConfigurationFailed",
				fmt.Sprintf("Failed to configure OIDC authentication for shoot %s/%s: %v", shoot.Namespace, shoot.Name, err))
			return ctrl.Result{}, err
		}
		r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "OIDCConfigured",
			fmt.Sprintf("Successfully configured OIDC authentication for shoot %s/%s", shoot.Namespace, shoot.Name))
	}

	// Set up RBAC if enabled in the CareInstruction
	if r.CareInstruction.Spec.EnableRBAC {
		shootClient, err := getShootClusterClient(ctx, r.GardenClient, &shoot)
		if err != nil {
			r.Info("unable to get Shoot cluster client", "name", shoot.Name, "error", err)
			r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "ShootClientFetchFailed",
				fmt.Sprintf("Failed to get Shoot cluster client for shoot %s/%s: %v", shoot.Namespace, shoot.Name, err))
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

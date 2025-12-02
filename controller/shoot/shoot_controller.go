// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"encoding/base64"
	"strings"

	"shoot-grafter/api/v1alpha1"

	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
}

func (r *ShootController) SetupWithManager(mgr ctrl.Manager) error {
	shootSelectorPredicate, err := predicate.LabelSelectorPredicate(*r.CareInstruction.Spec.ShootSelector)

	if err != nil {
		return err
	}
	// Setup the shoot controller with the manager
	return ctrl.NewControllerManagedBy(mgr).
		Named(r.Name).
		For(&gardenerv1beta1.Shoot{}, builder.WithPredicates(shootSelectorPredicate)).
		Complete(r)
}

// TODO: defer some status collection --> persist on CareInstruction status, maybe use events?
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
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	var shoot gardenerv1beta1.Shoot
	if err := r.GardenClient.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &shoot); err != nil {
		r.Error(err, "unable to fetch Shoot")
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
		r.Error(nil, "no external API server URL found for Shoot", "name", shoot.Name)
		return ctrl.Result{}, nil
	}

	// Initialize labels and label propagation with the CareInstruction labels
	labels := map[string]string{
		v1alpha1.CareInstructionLabel: r.CareInstruction.Name,
	}
	var labelPropagateBuilder strings.Builder
	labelPropagateBuilder.WriteString(v1alpha1.CareInstructionLabel)
	labelPropagateBuilder.WriteString(",")
	// get labels specified via TransportLabels on the CareInstruction from the shoot
	for _, v := range r.CareInstruction.Spec.PropagateLabels {
		if labelValue, ok := shoot.Labels[v]; ok {
			labels[v] = labelValue
			labelPropagateBuilder.WriteString(v)
			labelPropagateBuilder.WriteString(",")
		}
	}
	// get additional labels specified via AdditionalLabels on the CareInstruction
	if r.CareInstruction.Spec.AdditionalLabels != nil {
		for k, v := range r.CareInstruction.Spec.AdditionalLabels {
			labels[k] = v
			labelPropagateBuilder.WriteString(k)
			labelPropagateBuilder.WriteString(",")
		}
	}
	labelPropagateString := labelPropagateBuilder.String()

	annotations := map[string]string{
		"greenhouse.sap/propagate-labels":           labelPropagateString,
		greenhouseapis.SecretAPIServerURLAnnotation: apiServerURL,
	}

	// create or update Secret with the CA data from the shoot
	// and the labels from the CareInstruction
	var cm corev1.ConfigMap
	if err := r.GardenClient.Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name + shootCACMSuffix}, &cm); err != nil {
		r.Error(err, "unable to fetch CA ConfigMap for Shoot")
		return ctrl.Result{}, err
	}

	caData := cm.Data["ca.crt"]
	if caData == "" {
		r.Error(nil, "no CA data found in ConfigMap for Shoot", "name", cm.Name)
		return ctrl.Result{}, nil
	}
	caDataBytes := []byte(caData)
	caDataBase64Enc := make([]byte, base64.StdEncoding.EncodedLen(len(caDataBytes)))
	base64.StdEncoding.Encode(caDataBase64Enc, caDataBytes)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        shoot.Name,
			Namespace:   r.CareInstruction.Namespace,
			Annotations: annotations,
			Labels:      labels,
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
		secret.Annotations = annotations
		secret.Labels = labels
		return nil
	})
	if err != nil {
		r.Error(err, "unable to create or update Secret for Shoot", "name", shoot.Name)
		return ctrl.Result{}, err
	}
	switch result {
	case controllerutil.OperationResultCreated:
		r.Info("Secret for Shoot created", "name", shoot.Name)
	case controllerutil.OperationResultUpdated:
		r.Info("Secret for Shoot updated", "name", shoot.Name)
	case controllerutil.OperationResultNone:
		r.Info("Secret for Shoot unchanged", "name", shoot.Name)
	default:
		r.Info("Secret for Shoot processed", "name", shoot.Name, "result", result)
	}

	r.Info("Successfully reconciled Shoot", "name", shoot.Name)

	return ctrl.Result{}, nil
}

// GenerateName generates a name for the shoot controller based on the garden cluster name.
func GenerateName(gardenClusterName string) string {
	return "shoot-controller-" + gardenClusterName
}

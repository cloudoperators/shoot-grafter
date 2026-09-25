// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package clientutil

import (
	"maps"
	"slices"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// PredicateFilterBySecretTypes filters secrets by the given types.
func PredicateFilterBySecretTypes(secretTypes ...corev1.SecretType) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		if secret, ok := o.(*corev1.Secret); ok {
			return slices.Contains(secretTypes, secret.Type)
		}
		return false
	})
}

// PredicateHasLabel checks if an object has a specific label.
func PredicateHasLabel(key string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		_, exists := o.GetLabels()[key]
		return exists
	})
}

// PredicateConfigMapDataChanged fires on Create and on Update only when the ConfigMap Data changes.
func PredicateConfigMapDataChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldCM, ok1 := e.ObjectOld.(*corev1.ConfigMap)
			newCM, ok2 := e.ObjectNew.(*corev1.ConfigMap)
			if !ok1 || !ok2 {
				return false
			}
			return !maps.Equal(oldCM.Data, newCM.Data)
		},
		DeleteFunc: func(_ event.DeleteEvent) bool { return false },
	}
}

// PredicateShootStatusNoise drops Shoot update events that are pure Gardener status noise:
// events where nothing changed except status fields other than AdvertisedAddresses.
func PredicateShootStatusNoise() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldShoot, ok1 := e.ObjectOld.(*gardenerv1beta1.Shoot)
			newShoot, ok2 := e.ObjectNew.(*gardenerv1beta1.Shoot)
			if !ok1 || !ok2 {
				return true
			}
			// Pass if non-status fields changed (Generation/Spec/labels/annotations).
			if oldShoot.Generation != newShoot.Generation ||
				!apiequality.Semantic.DeepEqual(oldShoot.Spec, newShoot.Spec) ||
				!apiequality.Semantic.DeepEqual(oldShoot.Labels, newShoot.Labels) ||
				!apiequality.Semantic.DeepEqual(oldShoot.Annotations, newShoot.Annotations) {
				return true
			}
			// Pass if AdvertisedAddresses changed (the API server URL lives there).
			if !apiequality.Semantic.DeepEqual(oldShoot.Status.AdvertisedAddresses, newShoot.Status.AdvertisedAddresses) {
				return true
			}
			// Drop all other status-only changes.
			return false
		},
	}
}

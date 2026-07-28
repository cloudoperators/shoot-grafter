// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package clientutil

import (
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
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

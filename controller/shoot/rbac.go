// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	greenhouseSATemplate = "greenhouse:system:serviceaccount:%s:%s"
)

// SetRBAC ensures that the necessary RBAC permissions are set for the Greenhouse controller to operate on the shoot cluster.
// This currently defaults to cluster-admin permissions
// Uses controllerutil.CreateOrUpdate to handle both creation and updates with Kubernetes-aware comparison.
// Note: RoleRef is immutable, so if it differs, the ClusterRoleBinding must be deleted and recreated.
// TODO: expose possibility to spec finegrained permissions for the Greenhouse controller
func (r *ShootController) SetRBAC(ctx context.Context, shootClient client.Client, shootName string) {
	greenhouseOrg := r.CareInstruction.GetNamespace()

	desiredRoleRef := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     "cluster-admin",
	}

	// Check if we need to handle RoleRef change (immutable field)
	existingCRB := &rbacv1.ClusterRoleBinding{}
	err := shootClient.Get(ctx, client.ObjectKey{Name: "greenhouse:system:cluster-admin"}, existingCRB)
	if err == nil && existingCRB.RoleRef != desiredRoleRef {
		// RoleRef differs and is immutable - delete and recreate
		r.Info("RoleRef differs and is immutable, deleting ClusterRoleBinding for recreation",
			"ClusterRoleBinding", existingCRB.Name,
			"existing", fmt.Sprintf("%s/%s", existingCRB.RoleRef.Kind, existingCRB.RoleRef.Name),
			"desired", fmt.Sprintf("%s/%s", desiredRoleRef.Kind, desiredRoleRef.Name))

		if err := shootClient.Delete(ctx, existingCRB); err != nil {
			r.Error(err, "failed to delete ClusterRoleBinding with different RoleRef", "ClusterRoleBinding", existingCRB.Name)
			r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "RBACOperationFailed",
				fmt.Sprintf("Failed to delete ClusterRoleBinding %s for shoot %s/%s: %v", existingCRB.Name, r.CareInstruction.Namespace, shootName, err))
			return
		}
	} else if err != nil && !apierrors.IsNotFound(err) {
		r.Error(err, "failed to get existing ClusterRoleBinding", "ClusterRoleBinding", "greenhouse:system:cluster-admin")
		return
	}

	// Now use CreateOrUpdate for normal create/update operations
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "greenhouse:system:cluster-admin",
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, shootClient, clusterRoleBinding, func() error {
		// Set the desired state
		clusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:     "User",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     fmt.Sprintf(greenhouseSATemplate, greenhouseOrg, shootName),
			},
		}
		clusterRoleBinding.RoleRef = desiredRoleRef
		return nil
	})

	if err != nil {
		r.Error(err, "failed to create or update ClusterRoleBinding for Greenhouse ServiceAccount", "ClusterRoleBinding", clusterRoleBinding.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "RBACOperationFailed",
			fmt.Sprintf("Failed to create or update ClusterRoleBinding %s for shoot %s/%s: %v", clusterRoleBinding.Name, r.CareInstruction.Namespace, shootName, err))
		return
	}

	switch op {
	case controllerutil.OperationResultCreated:
		r.Info("Created ClusterRoleBinding for Greenhouse ServiceAccount", "ClusterRoleBinding", clusterRoleBinding.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "RBACCreated",
			fmt.Sprintf("Created ClusterRoleBinding %s for shoot %s/%s", clusterRoleBinding.Name, r.CareInstruction.Namespace, shootName))
	case controllerutil.OperationResultUpdated:
		r.Info("Updated ClusterRoleBinding for Greenhouse ServiceAccount", "ClusterRoleBinding", clusterRoleBinding.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "RBACUpdated",
			fmt.Sprintf("Updated ClusterRoleBinding %s for shoot %s/%s", clusterRoleBinding.Name, r.CareInstruction.Namespace, shootName))
	case controllerutil.OperationResultNone:
		r.Info("ClusterRoleBinding matches desired state", "ClusterRoleBinding", clusterRoleBinding.Name, "shoot", shootName)
	}
}

// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
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
)

const (
	greenhouseSATemplate = "greenhouse:system:serviceaccount:%s:%s"
)

// SetRBAC ensures that the necessary RBAC permissions are set for the Greenhouse controller to operate on the shoot cluster.
// This currently defaults to cluster-admin permissions
// If an existing ClusterRoleBinding with our naming exists, we compare it with the desired state and recreate if different.
// TODO: expose possibility to spec finegrained permissions for the Greenhouse controller
func (r *ShootController) SetRBAC(ctx context.Context, shootClient client.Client, shootName string) {
	greenhouseOrg := r.CareInstruction.GetNamespace()
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "greenhouse:system:cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     fmt.Sprintf(greenhouseSATemplate, greenhouseOrg, shootName),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
	}
	if err := shootClient.Create(ctx, clusterRoleBinding); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			r.Error(err, "failed to create ClusterRoleBinding for Greenhouse ServiceAccount", "ClusterRoleBinding", clusterRoleBinding.Name)
			r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "RBACCreationFailed",
				fmt.Sprintf("Failed to create ClusterRoleBinding %s for shoot %s/%s: %v", clusterRoleBinding.Name, r.CareInstruction.Namespace, shootName, err))
			return
		}

		// ClusterRoleBinding already exists - get it and compare
		r.Info("ClusterRoleBinding already exists, comparing with desired state", "ClusterRoleBinding", clusterRoleBinding.Name, "shoot", shootName)

		existingCRB := &rbacv1.ClusterRoleBinding{}
		if err := shootClient.Get(ctx, client.ObjectKey{Name: clusterRoleBinding.Name}, existingCRB); err != nil {
			r.Error(err, "failed to get existing ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBinding.Name)
			return
		}

		// Compare RoleRef and Subjects
		needsRecreate := false

		// Compare RoleRef
		if existingCRB.RoleRef != clusterRoleBinding.RoleRef {
			r.Info("RoleRef differs, will recreate",
				"existing", fmt.Sprintf("%s/%s", existingCRB.RoleRef.Kind, existingCRB.RoleRef.Name),
				"desired", fmt.Sprintf("%s/%s", clusterRoleBinding.RoleRef.Kind, clusterRoleBinding.RoleRef.Name))
			needsRecreate = true
		}

		// Compare Subjects
		// Note: This comparison assumes subjects are in the same order. If order differs, this will trigger a recreate.
		if !needsRecreate && fmt.Sprint(existingCRB.Subjects) != fmt.Sprint(clusterRoleBinding.Subjects) {
			r.Info("Subjects differ, will recreate",
				"existing", existingCRB.Subjects,
				"desired", clusterRoleBinding.Subjects)
			needsRecreate = true
		}

		if needsRecreate {
			r.Info("Deleting existing ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBinding.Name, "shoot", shootName)
			if err := shootClient.Delete(ctx, existingCRB); err != nil {
				r.Error(err, "failed to delete existing ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBinding.Name)
				return
			}

			r.Info("Creating new ClusterRoleBinding with correct configuration", "ClusterRoleBinding", clusterRoleBinding.Name, "shoot", shootName)
			if err := shootClient.Create(ctx, clusterRoleBinding); err != nil {
				r.Error(err, "failed to recreate ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBinding.Name)
				r.emitEvent(r.CareInstruction, corev1.EventTypeWarning, "RBACRecreationFailed",
					fmt.Sprintf("Failed to recreate ClusterRoleBinding %s for shoot %s/%s: %v", clusterRoleBinding.Name, r.CareInstruction.Namespace, shootName, err))
				return
			}

			r.Info("Successfully recreated ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBinding.Name, "shoot", shootName)
			r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "RBACUpdated",
				fmt.Sprintf("Updated ClusterRoleBinding %s for shoot %s/%s", clusterRoleBinding.Name, r.CareInstruction.Namespace, shootName))
		} else {
			r.Info("ClusterRoleBinding matches desired state", "ClusterRoleBinding", clusterRoleBinding.Name, "shoot", shootName)
		}
	} else {
		r.Info("Created ClusterRoleBinding for Greenhouse ServiceAccount", "ClusterRoleBinding", clusterRoleBinding.Name)
		r.emitEvent(r.CareInstruction, corev1.EventTypeNormal, "RBACCreated",
			fmt.Sprintf("Created ClusterRoleBinding %s for shoot %s/%s", clusterRoleBinding.Name, r.CareInstruction.Namespace, shootName))
	}
}

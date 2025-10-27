package shoot

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	greenhouseSATemplate = "greenhouse:system:serviceaccount:%s:%s"
)

// setRBAC ensures that the necessary RBAC permissions are set for the Greenhouse controller to operate on the shoot cluster.
// This currently defaults to cluster-admin permissions
// TODO: expose possibility to spec finegrained permissions for the Greenhouse controller
func (r *ShootController) setRBAC(ctx context.Context, shootClient client.Client, shootName string) {

	greenhouseOrg := r.CareInstruction.GetNamespace()
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "greenhouse:system:cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     fmt.Sprintf(greenhouseSATemplate, greenhouseOrg, &shootName),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
	}
	if err := r.GreenhouseClient.Create(ctx, clusterRoleBinding); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			r.Error(err, "failed to create ClusterRoleBinding for Greenhouse ServiceAccount", "ClusterRoleBinding", clusterRoleBinding.Name)
		} else {
			r.Info("ClusterRoleBinding for Greenhouse ServiceAccount already exists", "ClusterRoleBinding", clusterRoleBinding.Name)
		}
	} else {
		r.Info("Created ClusterRoleBinding for Greenhouse ServiceAccount", "ClusterRoleBinding", clusterRoleBinding.Name)
	}

}

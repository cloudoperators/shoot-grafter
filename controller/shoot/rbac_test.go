// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot_test

import (
	"context"

	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/controller/shoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"shoot-grafter/internal/test"
)

var _ = Describe("RBAC ClusterRoleBinding Management", func() {
	var (
		ctx             context.Context
		careInstruction *v1alpha1.CareInstruction
		controller      *shoot.ShootController
	)

	BeforeEach(func() {
		ctx = context.Background()
		careInstruction = &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rbac-ci",
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				EnableRBAC: true,
			},
		}

		controller = &shoot.ShootController{
			GreenhouseClient: test.K8sClient,
			GardenClient:     test.GardenK8sClient,
			Logger:           ctrl.Log.WithName("test").WithName("RBACTest"),
			Name:             "RBACTestController",
			CareInstruction:  careInstruction,
			EventRecorder:    nil, // Not needed for these tests
		}
	})

	AfterEach(func() {
		// Clean up any ClusterRoleBindings created during tests
		crbList := &rbacv1.ClusterRoleBindingList{}
		Expect(test.GardenK8sClient.List(ctx, crbList)).To(Succeed())
		for _, crb := range crbList.Items {
			Expect(client.IgnoreNotFound(test.GardenK8sClient.Delete(ctx, &crb))).To(Succeed())
		}
	})

	Context("when ClusterRoleBinding does not exist", func() {
		It("should create a new ClusterRoleBinding", func() {
			shootName := "test-shoot-create"

			// Call setRBAC - this uses GardenK8sClient as the shoot client
			controller.SetRBAC(ctx, test.GardenK8sClient, shootName)

			// Verify ClusterRoleBinding was created
			crb := &rbacv1.ClusterRoleBinding{}
			err := test.GardenK8sClient.Get(ctx, client.ObjectKey{
				Name: "greenhouse:system:cluster-admin",
			}, crb)
			Expect(err).NotTo(HaveOccurred())

			// Verify the ClusterRoleBinding has correct configuration
			Expect(crb.RoleRef.Name).To(Equal("cluster-admin"))
			Expect(crb.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(crb.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
			Expect(crb.Subjects).To(HaveLen(1))
			Expect(crb.Subjects[0].Kind).To(Equal("User"))
			Expect(crb.Subjects[0].Name).To(Equal("greenhouse:system:serviceaccount:default:test-shoot-create"))
			Expect(crb.Subjects[0].APIGroup).To(Equal("rbac.authorization.k8s.io"))
		})
	})

	Context("when ClusterRoleBinding already exists and is the same", func() {
		It("should not recreate the ClusterRoleBinding", func() {
			shootName := "test-shoot-same"

			// Pre-create the ClusterRoleBinding with correct configuration
			existingCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "greenhouse:system:cluster-admin",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:     "User",
						APIGroup: "rbac.authorization.k8s.io",
						Name:     "greenhouse:system:serviceaccount:default:test-shoot-same",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "cluster-admin",
				},
			}
			Expect(test.GardenK8sClient.Create(ctx, existingCRB)).To(Succeed())

			// Get the resource version before setRBAC
			originalResourceVersion := existingCRB.ResourceVersion

			// Call setRBAC
			controller.SetRBAC(ctx, test.GardenK8sClient, shootName)

			// Verify ClusterRoleBinding still exists
			crb := &rbacv1.ClusterRoleBinding{}
			err := test.GardenK8sClient.Get(ctx, client.ObjectKey{
				Name: "greenhouse:system:cluster-admin",
			}, crb)
			Expect(err).NotTo(HaveOccurred())

			// Verify it wasn't recreated (resource version should be the same)
			Expect(crb.ResourceVersion).To(Equal(originalResourceVersion))

			// Verify configuration is still correct
			Expect(crb.RoleRef.Name).To(Equal("cluster-admin"))
			Expect(crb.Subjects[0].Name).To(Equal("greenhouse:system:serviceaccount:default:test-shoot-same"))
		})
	})

	Context("when ClusterRoleBinding differs in RoleRef", func() {
		It("should recreate the ClusterRoleBinding with correct RoleRef", func() {
			shootName := "test-shoot-roleref"

			// Pre-create ClusterRoleBinding with wrong RoleRef
			wrongCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "greenhouse:system:cluster-admin",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:     "User",
						APIGroup: "rbac.authorization.k8s.io",
						Name:     "greenhouse:system:serviceaccount:default:test-shoot-roleref",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "view", // Wrong role!
				},
			}
			Expect(test.GardenK8sClient.Create(ctx, wrongCRB)).To(Succeed())

			// Get the resource version before setRBAC
			originalResourceVersion := wrongCRB.ResourceVersion

			// Call setRBAC
			controller.SetRBAC(ctx, test.GardenK8sClient, shootName)

			// Verify ClusterRoleBinding was recreated
			crb := &rbacv1.ClusterRoleBinding{}
			err := test.GardenK8sClient.Get(ctx, client.ObjectKey{
				Name: "greenhouse:system:cluster-admin",
			}, crb)
			Expect(err).NotTo(HaveOccurred())

			// Verify it was recreated (resource version should be different)
			Expect(crb.ResourceVersion).NotTo(Equal(originalResourceVersion))

			// Verify RoleRef is now correct
			Expect(crb.RoleRef.Name).To(Equal("cluster-admin"))
			Expect(crb.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(crb.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
		})
	})

	Context("when ClusterRoleBinding differs in Subjects", func() {
		It("should recreate the ClusterRoleBinding with correct Subjects", func() {
			shootName := "test-shoot-subjects"

			// Pre-create ClusterRoleBinding with wrong subject
			wrongCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "greenhouse:system:cluster-admin",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:     "User",
						APIGroup: "rbac.authorization.k8s.io",
						Name:     "wrong-subject-name", // Wrong subject!
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "cluster-admin",
				},
			}
			Expect(test.GardenK8sClient.Create(ctx, wrongCRB)).To(Succeed())

			// Get the resource version before setRBAC
			originalResourceVersion := wrongCRB.ResourceVersion

			// Call setRBAC
			controller.SetRBAC(ctx, test.GardenK8sClient, shootName)

			// Verify ClusterRoleBinding was recreated
			crb := &rbacv1.ClusterRoleBinding{}
			err := test.GardenK8sClient.Get(ctx, client.ObjectKey{
				Name: "greenhouse:system:cluster-admin",
			}, crb)
			Expect(err).NotTo(HaveOccurred())

			// Verify it was recreated (resource version should be different)
			Expect(crb.ResourceVersion).NotTo(Equal(originalResourceVersion))

			// Verify Subjects are now correct
			Expect(crb.Subjects).To(HaveLen(1))
			Expect(crb.Subjects[0].Name).To(Equal("greenhouse:system:serviceaccount:default:test-shoot-subjects"))
		})
	})

	Context("when ClusterRoleBinding differs in both RoleRef and Subjects", func() {
		It("should recreate the ClusterRoleBinding with correct configuration", func() {
			shootName := "test-shoot-both"

			// Pre-create ClusterRoleBinding with both wrong RoleRef and Subjects
			wrongCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "greenhouse:system:cluster-admin",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:     "User",
						APIGroup: "rbac.authorization.k8s.io",
						Name:     "wrong-subject-name", // Wrong!
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "view", // Wrong!
				},
			}
			Expect(test.GardenK8sClient.Create(ctx, wrongCRB)).To(Succeed())

			// Get the resource version before setRBAC
			originalResourceVersion := wrongCRB.ResourceVersion

			// Call setRBAC
			controller.SetRBAC(ctx, test.GardenK8sClient, shootName)

			// Verify ClusterRoleBinding was recreated
			crb := &rbacv1.ClusterRoleBinding{}
			err := test.GardenK8sClient.Get(ctx, client.ObjectKey{
				Name: "greenhouse:system:cluster-admin",
			}, crb)
			Expect(err).NotTo(HaveOccurred())

			// Verify it was recreated (resource version should be different)
			Expect(crb.ResourceVersion).NotTo(Equal(originalResourceVersion))

			// Verify both RoleRef and Subjects are now correct
			Expect(crb.RoleRef.Name).To(Equal("cluster-admin"))
			Expect(crb.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(crb.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
			Expect(crb.Subjects).To(HaveLen(1))
			Expect(crb.Subjects[0].Name).To(Equal("greenhouse:system:serviceaccount:default:test-shoot-both"))
		})
	})
})

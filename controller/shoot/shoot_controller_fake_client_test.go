// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot_test

import (
	"context"
	"errors"

	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/controller/shoot"
	"shoot-grafter/internal/test"

	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("Shoot Controller with fake client", func() {
	AfterEach(func() {
		clusters := &greenhousev1alpha1.ClusterList{}
		Expect(test.K8sClient.List(test.Ctx, clusters, client.InNamespace("default"))).To(Succeed(), "should list Clusters")
		for _, cluster := range clusters.Items {
			Expect(client.IgnoreNotFound(test.K8sClient.Delete(test.Ctx, &cluster))).To(Succeed(), "should delete Cluster resource")
		}
	})

	When("a shoot controller with fake client is starting", func() {
		BeforeEach(func() {
			careInstruction = &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-non-happy-path",
					Namespace: "default",
				},
			}
		})
		It("should handle shoot removal - non-happy path", func() {
			fakeClient := fake.NewClientBuilder().
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return errors.New("fake cluster conflict or timeout error")
					},
				}).Build()
			shootController := shoot.ShootController{
				GreenhouseClient: test.K8sClient,
				GardenClient:     fakeClient,
				Logger:           ctrl.Log.WithName("controllers").WithName("ShootController"),
				Name:             "ShootController",
				CareInstruction:  careInstruction,
			}

			// Simulate the Greenhouse Cluster existing
			cluster := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      careInstruction.Name,
					Namespace: careInstruction.Namespace,
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster)).To(Succeed(), "should create Cluster resource")

			req := ctrl.Request{}
			req.Name = cluster.Name
			req.Namespace = cluster.Namespace
			_, err := shootController.Reconcile(test.Ctx, req)
			Expect(err).To(HaveOccurred(), "should fail reconciliation")

			existingCluster := &greenhousev1alpha1.Cluster{}
			Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(cluster), existingCluster)).To(Succeed(), "should keep Cluster resource")
		})
	})
})

// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot_test

import (
	"context"
	"errors"

	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/controller/shoot"
	"shoot-grafter/internal/clientutil"
	"shoot-grafter/internal/test"

	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
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

	When("Greenhouse has already written a greenhousekubeconfig into the Secret", func() {
		It("should not overwrite the greenhousekubeconfig key on subsequent reconciles", func() {
			s := authScheme()

			ci := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig-ci", Namespace: "default"},
			}

			shootObj := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig-shoot", Namespace: "default"},
				Status: gardenerv1beta1.ShootStatus{
					AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
						{Name: "external", URL: "https://api.kubeconfig-shoot.example.com"},
					},
				},
			}
			caCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig-shoot.ca-cluster", Namespace: "default"},
				Data:       map[string]string{"ca.crt": "fake-ca"},
			}

			gardenClient := fake.NewClientBuilder().WithScheme(s).WithObjects(shootObj, caCM).Build()
			greenhouseClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()

			sc := &shoot.ShootController{
				GreenhouseClient: greenhouseClient,
				GardenClient:     gardenClient,
				Logger:           logr.Discard(),
				CareInstruction:  ci,
				Name:             "kubeconfig-test",
			}

			req := ctrl.Request{NamespacedName: client.ObjectKey{Name: "kubeconfig-shoot", Namespace: "default"}}

			// First reconcile — creates the Secret with ca.crt only.
			_, err := sc.Reconcile(test.Ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Simulate Greenhouse bootstrap controller writing greenhousekubeconfig.
			var secret corev1.Secret
			Expect(greenhouseClient.Get(test.Ctx, client.ObjectKey{Name: "kubeconfig-shoot", Namespace: "default"}, &secret)).To(Succeed())
			secret.Data["greenhousekubeconfig"] = []byte("fake-kubeconfig")
			Expect(greenhouseClient.Update(test.Ctx, &secret)).To(Succeed())

			// Second reconcile — shoot-grafter must not destroy the greenhousekubeconfig key.
			_, err = sc.Reconcile(test.Ctx, req)
			Expect(err).NotTo(HaveOccurred())

			Expect(greenhouseClient.Get(test.Ctx, client.ObjectKey{Name: "kubeconfig-shoot", Namespace: "default"}, &secret)).To(Succeed())
			Expect(secret.Data).To(HaveKey("greenhousekubeconfig"),
				"keys written by other controllers must survive a shoot-grafter reconcile")
			Expect(secret.Data["greenhousekubeconfig"]).To(Equal([]byte("fake-kubeconfig")))
		})
	})

	When("a CareInstruction has multiple AdditionalLabels", func() {
		It("should produce a stable propagate-labels annotation across reconciles", func() {
			s := authScheme()

			ci := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{Name: "label-ci", Namespace: "default"},
				Spec: v1alpha1.CareInstructionSpec{
					AdditionalLabels: map[string]string{
						"zzz-last":  "val-z",
						"aaa-first": "val-a",
					},
				},
			}

			shootObj := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{Name: "label-shoot", Namespace: "default"},
				Status: gardenerv1beta1.ShootStatus{
					AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
						{Name: "external", URL: "https://api.label-shoot.example.com"},
					},
				},
			}
			caCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "label-shoot.ca-cluster", Namespace: "default"},
				Data:       map[string]string{"ca.crt": "fake-ca"},
			}

			gardenClient := fake.NewClientBuilder().WithScheme(s).WithObjects(shootObj, caCM).Build()
			greenhouseClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()

			sc := &shoot.ShootController{
				GreenhouseClient: greenhouseClient,
				GardenClient:     gardenClient,
				Logger:           logr.Discard(),
				CareInstruction:  ci,
				Name:             "label-test",
			}

			req := ctrl.Request{NamespacedName: client.ObjectKey{Name: "label-shoot", Namespace: "default"}}

			// First reconcile — creates the Secret.
			_, err := sc.Reconcile(test.Ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var secretAfterFirst corev1.Secret
			Expect(greenhouseClient.Get(test.Ctx, client.ObjectKey{Name: "label-shoot", Namespace: "default"}, &secretAfterFirst)).To(Succeed())
			// Keys are sorted AdditionalLabels first, then CareInstructionLabel appended last.
			expectedAnnotation := "aaa-first,zzz-last," + v1alpha1.CareInstructionLabel
			Expect(secretAfterFirst.Annotations["greenhouse.sap/propagate-labels"]).To(Equal(expectedAnnotation),
				"propagate-labels annotation must be sorted")

			// Second reconcile — Secret must not be updated.
			_, err = sc.Reconcile(test.Ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var secretAfterSecond corev1.Secret
			Expect(greenhouseClient.Get(test.Ctx, client.ObjectKey{Name: "label-shoot", Namespace: "default"}, &secretAfterSecond)).To(Succeed())
			Expect(secretAfterSecond.Annotations["greenhouse.sap/propagate-labels"]).To(Equal(expectedAnnotation),
				"propagate-labels annotation must be identical on the second reconcile")
			Expect(secretAfterSecond.ResourceVersion).To(Equal(secretAfterFirst.ResourceVersion),
				"Secret must not be updated on the second reconcile")
		})
	})
})

var _ = Describe("PredicateShootStatusNoise", func() {
	base := &gardenerv1beta1.Shoot{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shoot",
			Namespace:  "default",
			Generation: 1,
			Labels:     map[string]string{"env": "prod"},
		},
		Spec: gardenerv1beta1.ShootSpec{
			Region: "eu-de-1",
		},
	}

	p := clientutil.PredicateShootStatusNoise()

	It("drops status-only updates (LastOperation noise)", func() {
		oldObj := base.DeepCopy()
		newObj := base.DeepCopy()
		newObj.Status.LastOperation = &gardenerv1beta1.LastOperation{
			Description: "Reconciliation of Shoot cluster initialized.",
			Progress:    42,
		}

		Expect(p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})).To(BeFalse())
	})

	It("passes updates where AdvertisedAddresses changed", func() {
		oldObj := base.DeepCopy()
		newObj := base.DeepCopy()
		newObj.Status.AdvertisedAddresses = []gardenerv1beta1.ShootAdvertisedAddress{
			{Name: "external", URL: "https://api.example.com"},
		}

		Expect(p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})).To(BeTrue())
	})

	It("passes updates where Generation changed (spec or operation annotation write)", func() {
		oldObj := base.DeepCopy()
		newObj := base.DeepCopy()
		newObj.Generation = 2

		Expect(p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})).To(BeTrue())
	})

	It("passes updates where spec changed", func() {
		oldObj := base.DeepCopy()
		newObj := base.DeepCopy()
		newObj.Spec.Region = "us-east-1"

		Expect(p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})).To(BeTrue())
	})

	It("passes updates where labels changed", func() {
		oldObj := base.DeepCopy()
		newObj := base.DeepCopy()
		newObj.Labels["new-label"] = "value"

		Expect(p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})).To(BeTrue())
	})

	It("passes updates where annotations changed", func() {
		oldObj := base.DeepCopy()
		newObj := base.DeepCopy()
		newObj.Annotations = map[string]string{"some-annotation": "value"}

		Expect(p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})).To(BeTrue())
	})
})

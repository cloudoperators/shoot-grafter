// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot_test

import (
	"context"
	"encoding/base64"

	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/controller/shoot"
	"shoot-grafter/internal/test"

	webhookv1alpha1 "shoot-grafter/webhook/v1alpha1"

	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	careInstruction *v1alpha1.CareInstruction
	mgrCtx          context.Context
	mgrCancel       context.CancelFunc
)
var _ = Describe("CareInstruction Controller", func() {
	JustBeforeEach(func() {
		// register controllers in JustBeforeEach, as they depend on the CareInstruction.
		// Create CareInstruction in BeforeEach
		skipNameValidation := true // Skip name validation for the controller
		host := test.TestEnv.WebhookInstallOptions.LocalServingHost
		port := test.TestEnv.WebhookInstallOptions.LocalServingPort
		mgr, err := ctrl.NewManager(test.GardenCfg, ctrl.Options{
			Scheme: scheme.Scheme,
			Metrics: server.Options{
				BindAddress: "0", // Disable metrics for the shoot controller manager
			},
			Controller: config.Controller{
				SkipNameValidation: &skipNameValidation, // Skip name validation for the controller
			},
			WebhookServer: webhook.NewServer(webhook.Options{
				Host:    host,
				Port:    port,
				CertDir: test.TestEnv.WebhookInstallOptions.LocalServingCertDir,
			}),
			LeaderElection: false,
		})
		Expect(err).NotTo(HaveOccurred(), "there must be no error creating the manager")
		Expect((&shoot.ShootController{
			GreenhouseClient: test.K8sClient,
			GardenClient:     test.GardenK8sClient,
			Logger:           ctrl.Log.WithName("controllers").WithName("ShootController"),
			Name:             "ShootController",
			CareInstruction:  careInstruction,
		}).SetupWithManager(mgr)).To(Succeed(), "there must be no error setting up the controller with the manager")

		careInstructionWebhook := &webhookv1alpha1.CareInstructionWebhook{}
		Expect(careInstructionWebhook.SetupWebhookWithManager(mgr)).To(Succeed(), "there must be no error setting up the webhook with the manager")

		mgrCtx, mgrCancel = context.WithCancel(test.Ctx)
		// start the manager
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(mgrCtx)).To(Succeed(), "there must be no error starting the manager")
		}()
		test.WaitForWebhookServerReady(host, port)

		// Create a CareInstruction resource
		Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction resource")
	})
	AfterEach(func() {
		// Clean up any resources created during the tests
		careInstructions := &v1alpha1.CareInstructionList{}
		Expect(test.K8sClient.List(test.Ctx, careInstructions)).To(Succeed(), "should list CareInstructions")
		for _, careInstruction := range careInstructions.Items {
			Expect(client.IgnoreNotFound(test.K8sClient.Delete(test.Ctx, &careInstruction))).To(Succeed(), "should delete CareInstruction resource")
		}
		Eventually(func(g Gomega) bool {
			careInstructions := &v1alpha1.CareInstructionList{}
			err := test.K8sClient.List(test.Ctx, careInstructions)
			if err != nil {
				return false
			}
			return len(careInstructions.Items) == 0
		}).Should(BeTrue(), "should eventually not find CareInstruction resources")

		shoots := &gardenerv1beta1.ShootList{}
		Expect(test.GardenK8sClient.List(test.Ctx, shoots)).To(Succeed(), "should list Shoots")
		for _, shoot := range shoots.Items {
			Expect(client.IgnoreNotFound(test.GardenK8sClient.Delete(test.Ctx, &shoot))).To(Succeed(), "should delete Shoot resource")
		}
		Eventually(func(g Gomega) bool {
			shoots := &gardenerv1beta1.ShootList{}
			err := test.GardenK8sClient.List(test.Ctx, shoots)
			if err != nil {
				return false
			}
			return len(shoots.Items) == 0
		}).Should(BeTrue(), "should eventually not find Shoot resources")

		secrets := &corev1.SecretList{}
		Expect(test.K8sClient.List(test.Ctx, secrets)).To(Succeed(), "should list Secrets")
		for _, secret := range secrets.Items {
			Expect(client.IgnoreNotFound(test.K8sClient.Delete(test.Ctx, &secret))).To(Succeed(), "should delete Secret resource")
		}
		Eventually(func(g Gomega) bool {
			secrets := &corev1.SecretList{}
			err := test.K8sClient.List(test.Ctx, secrets)
			if err != nil {
				return false
			}
			return len(secrets.Items) == 0 // Only the garden cluster secret should remain
		}).Should(BeTrue(), "should eventually not find Secret resources")

		// Clean up any ConfigMaps created during the tests
		configMaps := &corev1.ConfigMapList{}
		Expect(test.GardenK8sClient.List(test.Ctx, configMaps)).To(Succeed(), "should list ConfigMaps")
		for _, configMap := range configMaps.Items {
			Expect(client.IgnoreNotFound(test.GardenK8sClient.Delete(test.Ctx, &configMap))).To(Succeed(), "should delete ConfigMap resource")
		}
		Eventually(func(g Gomega) bool {
			configMaps := &corev1.ConfigMapList{}
			err := test.GardenK8sClient.List(test.Ctx, configMaps)
			if err != nil {
				return false
			}
			return len(configMaps.Items) == 0 // Only the garden cluster ConfigMap should remain
		}).Should(BeTrue(), "should eventually not find ConfigMap resources")

		// stop the manager
		mgrCancel()

	})

	When("a CareInstruction with a valid ShootSelector, TransportLabels and AdditionalLabels is created for the garden cluster", func() {
		BeforeEach(func() {
			careInstruction = &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					ShootSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					},
					TransportLabels: []string{
						"foo",
						"baz",
					},
					AdditionalLabels: map[string]string{
						"quux": "corge",
					},
				},
			}
		})

		// We will table test by creating shoot resources with CA ConfigMaps and expecting secrets to be created
		DescribeTable("should correctly create secrets for shoots",
			func(
				shoots []gardenerv1beta1.Shoot,
				statuses []gardenerv1beta1.ShootStatus,
				configMaps []corev1.ConfigMap,
				expectedSecrets []corev1.Secret,
				unExpectedSecrets []corev1.Secret,
			) {
				for k, shoot := range shoots {
					Expect(test.GardenK8sClient.Create(test.Ctx, &shoot)).To(Succeed(), "should create Shoot resource")
					// Update status
					shoot.Status = statuses[k]
					Expect(test.GardenK8sClient.Status().Update(test.Ctx, &shoot)).To(Succeed(), "should update Shoot status with advertised addresses")
				}
				for _, cm := range configMaps {
					Expect(test.GardenK8sClient.Create(test.Ctx, &cm)).To(Succeed(), "should create ConfigMap resource")
				}

				secrets := &corev1.SecretList{}
				// Eventually check that the secrets are created as expected
				Eventually(func(g Gomega) bool {
					g.Expect(test.K8sClient.List(test.Ctx, secrets, client.MatchingLabels{
						v1alpha1.CareInstructionLabel: careInstruction.Name,
					})).To(Succeed(), "should list Secrets with CareInstruction label")
					g.Expect(secrets.Items).To(HaveLen(len(expectedSecrets)), "should find the expected number of Secrets")
					for _, expectedSecret := range expectedSecrets {
						found := false
						for _, secret := range secrets.Items {
							if secret.Name == expectedSecret.Name {
								g.Expect(secret.Namespace).To(Equal(expectedSecret.Namespace), "should have the expected namespace")
								g.Expect(secret.Labels).To(Equal(expectedSecret.Labels), "should have the expected labels")
								g.Expect(secret.Annotations).To(Equal(expectedSecret.Annotations), "should have the expected annotations")
								g.Expect(secret.Data).To(Equal(expectedSecret.Data), "should have the expected data")
								found = true
								break
							}
						}
						g.Expect(found).To(BeTrue(), "should find expected Secret", "name", expectedSecret.Name)
					}
					for _, unExpectedSecret := range unExpectedSecrets {
						g.Expect(secrets.Items).NotTo(ContainElement(client.MatchingFields{
							"metadata.name": unExpectedSecret.Name,
						}), "should not find unexpected Secret", "name", unExpectedSecret.Name)
					}
					return true
				}).Should(BeTrue(), "should eventually find the expected Secrets")

			},
			Entry("with one shoot matching the selector", []gardenerv1beta1.Shoot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1",
						Namespace: "default",
						Labels: map[string]string{
							"foo": "bar",
							"baz": "qux",
						},
					},
				},
			}, []gardenerv1beta1.ShootStatus{

				{
					AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
						{
							Name: "external",
							URL:  "https://api-server.test-shoot-1.example.com",
						},
					},
				},
			}, []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1.ca-cluster",
						Namespace: "default",
					},
					Data: map[string]string{
						"ca.crt": "test-ca-data",
					},
				},
			}, []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1",
						Namespace: "default",
						Labels: map[string]string{
							"shoot-grafter.cloudoperators/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "qux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/propagate-labels":           "shoot-grafter.cloudoperators/careinstruction,foo,baz,quux,",
							greenhouseapis.SecretAPIServerURLAnnotation: "https://api-server.test-shoot-1.example.com",
						},
					},
					Data: map[string][]byte{
						"ca.crt": []byte(base64.StdEncoding.EncodeToString([]byte("test-ca-data"))),
					},
				},
			},
				[]corev1.Secret{},
			),
			Entry("with multiple shoots matching the selector", []gardenerv1beta1.Shoot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1",
						Namespace: "default",
						Labels: map[string]string{
							"foo": "bar",
							"baz": "qux",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-2",
						Namespace: "default",
						Labels: map[string]string{
							"foo": "bar",
							"baz": "quux",
						},
					},
				},
			}, []gardenerv1beta1.ShootStatus{
				{
					AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
						{
							Name: "external",
							URL:  "https://api-server.test-shoot-1.example.com",
						},
					},
				},
				{
					AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
						{
							Name: "external",
							URL:  "https://api-server.test-shoot-2.example.com",
						},
					},
				},
			}, []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1.ca-cluster",
						Namespace: "default",
					},
					Data: map[string]string{
						"ca.crt": "test-ca-data-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-2.ca-cluster",
						Namespace: "default",
					},
					Data: map[string]string{
						"ca.crt": "test-ca-data-2",
					},
				},
			}, []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1",
						Namespace: "default",
						Labels: map[string]string{
							"shoot-grafter.cloudoperators/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "qux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/propagate-labels":           "shoot-grafter.cloudoperators/careinstruction,foo,baz,quux,",
							greenhouseapis.SecretAPIServerURLAnnotation: "https://api-server.test-shoot-1.example.com",
						},
					},
					Data: map[string][]byte{
						"ca.crt": []byte(base64.StdEncoding.EncodeToString([]byte("test-ca-data-1"))),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-2",
						Namespace: "default",
						Labels: map[string]string{
							"shoot-grafter.cloudoperators/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "quux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/propagate-labels":           "shoot-grafter.cloudoperators/careinstruction,foo,baz,quux,",
							greenhouseapis.SecretAPIServerURLAnnotation: "https://api-server.test-shoot-2.example.com",
						},
					},
					Data: map[string][]byte{
						"ca.crt": []byte(base64.StdEncoding.EncodeToString([]byte("test-ca-data-2"))),
					},
				},
			},
				[]corev1.Secret{},
			),
			Entry("with only one of two shoots matching the selector", []gardenerv1beta1.Shoot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1",
						Namespace: "default",
						Labels: map[string]string{
							"foo": "bar",
							"baz": "qux",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-2",
						Namespace: "default",
						Labels: map[string]string{
							"baz": "quux",
						},
					},
				},
			}, []gardenerv1beta1.ShootStatus{
				{
					AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
						{
							Name: "external",
							URL:  "https://api-server.test-shoot-1.example.com",
						},
					},
				},
				{
					AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
						{
							Name: "external",
							URL:  "https://api-server.test-shoot-2.example.com",
						},
					},
				},
			}, []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1.ca-cluster",
						Namespace: "default",
					},
					Data: map[string]string{
						"ca.crt": "test-ca-data-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-2.ca-cluster",
						Namespace: "default",
					},
					Data: map[string]string{
						"ca.crt": "test-ca-data-2",
					},
				},
			}, []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shoot-1",
						Namespace: "default",
						Labels: map[string]string{
							"shoot-grafter.cloudoperators/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "qux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/propagate-labels":           "shoot-grafter.cloudoperators/careinstruction,foo,baz,quux,",
							greenhouseapis.SecretAPIServerURLAnnotation: "https://api-server.test-shoot-1.example.com",
						},
					},
					Data: map[string][]byte{
						"ca.crt": []byte(base64.StdEncoding.EncodeToString([]byte("test-ca-data-1"))),
					},
				},
			},
				[]corev1.Secret{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-shoot-2",
							Namespace: "default",
						},
					},
				},
			),
		)
	})

	When("a CareInstruction with an empty ShootSelector is created", func() {
		BeforeEach(func() {
			careInstruction = &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-empty-selector",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					ShootSelector: &metav1.LabelSelector{},
					TransportLabels: []string{
						"foo",
						"baz",
					},
					AdditionalLabels: map[string]string{
						"quux": "corge",
					},
				},
			}
		})

		// this basically tests the webhook as it defaults nil ShootSelectors in the careInstruction
		It("should target all shoots of the gardenNamespace of the garden cluster", func() {
			// Create a shoot in the garden namespace
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot",
					Namespace: "default",
					Labels: map[string]string{
						"foo": "bar",
						"baz": "qux",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status with advertised addresses")

			// Create a ConfigMap with CA data
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create ConfigMap resource")

			// Eventually check that the secret is created as expected
			Eventually(func(g Gomega) bool {
				secrets := &corev1.SecretList{}
				g.Expect(test.K8sClient.List(test.Ctx, secrets, client.MatchingLabels{
					v1alpha1.CareInstructionLabel: careInstruction.Name,
				})).To(Succeed(), "should list Secrets with CareInstruction label")
				g.Expect(secrets.Items).To(HaveLen(1), "should find one Secret")
				secret := secrets.Items[0]
				g.Expect(secret.Name).To(Equal("test-shoot"), "should have the expected Secret name")
				g.Expect(secret.Labels).To(Equal(map[string]string{
					"shoot-grafter.cloudoperators/careinstruction": "test-careinstruction-empty-selector",
					"foo":  "bar",
					"baz":  "qux",
					"quux": "corge",
				}), "should have the expected labels")
				g.Expect(secret.Annotations).To(Equal(map[string]string{
					"greenhouse.sap/propagate-labels":           "shoot-grafter.cloudoperators/careinstruction,foo,baz,quux,",
					greenhouseapis.SecretAPIServerURLAnnotation: "https://api-server.test-shoot.example.com",
				}), "should have the expected annotations")
				g.Expect(secret.Data).To(HaveKeyWithValue("ca.crt", []byte(base64.StdEncoding.EncodeToString([]byte("test-ca-data")))), "should have the expected data")
				return true
			}).Should(BeTrue(), "should eventually find the expected Secret")
		})
	})
})

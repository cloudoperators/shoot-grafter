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
	apiserverv1beta1 "k8s.io/apiserver/pkg/apis/apiserver/v1beta1"
	"k8s.io/kubectl/pkg/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/yaml"
)

var (
	careInstruction *v1alpha1.CareInstruction
	mgrCtx          context.Context
	mgrCancel       context.CancelFunc
)
var _ = Describe("Shoot Controller", func() {
	JustBeforeEach(func() {
		// register controllers in JustBeforeEach, as they depend on the CareInstruction.
		// Create CareInstruction in BeforeEach
		skipNameValidation := true // Skip name validation for the controller
		host := test.TestEnv.WebhookInstallOptions.LocalServingHost
		port := test.TestEnv.WebhookInstallOptions.LocalServingPort

		// Wait for the port to be free before starting the manager
		// This avoids conflicts when tests run in parallel
		test.WaitForPortFree(host, port)

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
		Expect(err).NotTo(HaveOccurred(), "there must be no error creating the garden manager")

		// Create a manager for the Greenhouse cluster (where events should be emitted)
		greenhouseMgr, err := ctrl.NewManager(test.Cfg, ctrl.Options{
			Scheme: scheme.Scheme,
			Metrics: server.Options{
				BindAddress: "0", // Disable metrics
			},
			LeaderElection: false,
		})
		Expect(err).NotTo(HaveOccurred(), "there must be no error creating the greenhouse manager")

		// Create ShootController with EventRecorder from Greenhouse manager
		Expect(err).NotTo(HaveOccurred(), "there must be no error creating the manager")
		Expect((&shoot.ShootController{
			GreenhouseClient: test.K8sClient,
			GardenClient:     test.GardenK8sClient,
			Logger:           ctrl.Log.WithName("controllers").WithName("ShootController"),
			Name:             "ShootController",
			CareInstruction:  careInstruction,
			EventRecorder:    greenhouseMgr.GetEventRecorderFor("ShootController"), // Get EventRecorder from Greenhouse manager
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

		// Clean up any ConfigMaps created during the tests (Garden Cluster)
		configMaps := &corev1.ConfigMapList{}
		Expect(test.GardenK8sClient.List(test.Ctx, configMaps)).To(Succeed(), "should list ConfigMaps in Garden cluster")
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

		// Clean up auth ConfigMaps in Greenhouse cluster using label selector
		greenhouseAuthConfigMaps := &corev1.ConfigMapList{}
		Expect(test.K8sClient.List(test.Ctx, greenhouseAuthConfigMaps, client.MatchingLabels{
			v1alpha1.AuthConfigMapLabel: "true",
		})).To(Succeed(), "should list auth ConfigMaps in Greenhouse cluster")
		for _, configMap := range greenhouseAuthConfigMaps.Items {
			Expect(client.IgnoreNotFound(test.K8sClient.Delete(test.Ctx, &configMap))).To(Succeed(), "should delete auth ConfigMap resource")
		}
		Eventually(func(g Gomega) bool {
			greenhouseAuthConfigMaps := &corev1.ConfigMapList{}
			err := test.K8sClient.List(test.Ctx, greenhouseAuthConfigMaps, client.MatchingLabels{
				v1alpha1.AuthConfigMapLabel: "true",
			})
			if err != nil {
				return false
			}
			return len(greenhouseAuthConfigMaps.Items) == 0
		}).Should(BeTrue(), "should eventually not find auth ConfigMap resources in Greenhouse cluster")

		// Clean up any Events created during the tests
		events := &corev1.EventList{}
		Expect(test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))).To(Succeed(), "should list Events")
		for _, event := range events.Items {
			Expect(client.IgnoreNotFound(test.K8sClient.Delete(test.Ctx, &event))).To(Succeed(), "should delete Event resource")
		}
		Eventually(func(g Gomega) bool {
			events := &corev1.EventList{}
			err := test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))
			if err != nil {
				return false
			}
			return len(events.Items) == 0
		}).Should(BeTrue(), "should eventually not find Event resources")

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
					PropagateLabels: []string{
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
							"shoot-grafter.cloudoperators.dev/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "qux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/last-applied-propagator":    "{\"labelKeys\":[\"foo\",\"baz\"]}",
							"greenhouse.sap/propagate-labels":           "foo,baz,quux,shoot-grafter.cloudoperators.dev/careinstruction",
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
							"shoot-grafter.cloudoperators.dev/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "qux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/last-applied-propagator":    "{\"labelKeys\":[\"foo\",\"baz\"]}",
							"greenhouse.sap/propagate-labels":           "foo,baz,quux,shoot-grafter.cloudoperators.dev/careinstruction",
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
							"shoot-grafter.cloudoperators.dev/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "quux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/last-applied-propagator":    "{\"labelKeys\":[\"foo\",\"baz\"]}",
							"greenhouse.sap/propagate-labels":           "foo,baz,quux,shoot-grafter.cloudoperators.dev/careinstruction",
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
							"shoot-grafter.cloudoperators.dev/careinstruction": "test-careinstruction",
							"foo":  "bar",
							"baz":  "qux",
							"quux": "corge",
						},
						Annotations: map[string]string{
							"greenhouse.sap/last-applied-propagator":    "{\"labelKeys\":[\"foo\",\"baz\"]}",
							"greenhouse.sap/propagate-labels":           "foo,baz,quux,shoot-grafter.cloudoperators.dev/careinstruction",
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

		It("should merge annotations and labels with existing ones on secret updates", func() {
			// Create a shoot
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-merge",
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
						URL:  "https://api-server.test-shoot-merge.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create ConfigMap with CA data
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-merge.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create ConfigMap resource")

			// Wait for secret to be created
			var createdSecret *corev1.Secret
			Eventually(func(g Gomega) bool {
				secret := &corev1.Secret{}
				err := test.K8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-merge",
					Namespace: "default",
				}, secret)
				if err == nil {
					createdSecret = secret
					return true
				}
				return false
			}).Should(BeTrue(), "should eventually create secret")

			// Add external annotations and labels to the secret (simulating external controller or user)
			createdSecret.Annotations["external-annotation"] = "external-value"
			createdSecret.Labels["external-label"] = "external-value"
			Expect(test.K8sClient.Update(test.Ctx, createdSecret)).To(Succeed(), "should update secret with external annotations and labels")

			// Trigger reconciliation by updating shoot
			shoot.Labels["trigger"] = "merge-test"
			Expect(test.GardenK8sClient.Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot to trigger reconciliation")

			// Verify that both controller-managed and external annotations/labels are preserved
			Eventually(func(g Gomega) bool {
				secret := &corev1.Secret{}
				err := test.K8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-merge",
					Namespace: "default",
				}, secret)
				g.Expect(err).NotTo(HaveOccurred(), "should get secret")

				// Check that controller-managed annotations are present
				g.Expect(secret.Annotations).To(HaveKeyWithValue("greenhouse.sap/propagate-labels", "foo,baz,quux,shoot-grafter.cloudoperators.dev/careinstruction"))
				g.Expect(secret.Annotations).To(HaveKeyWithValue(greenhouseapis.SecretAPIServerURLAnnotation, "https://api-server.test-shoot-merge.example.com"))

				// Check that external annotation is preserved
				g.Expect(secret.Annotations).To(HaveKeyWithValue("external-annotation", "external-value"), "should preserve external annotation")

				// Check that controller-managed labels are present
				g.Expect(secret.Labels).To(HaveKeyWithValue(v1alpha1.CareInstructionLabel, "test-careinstruction"))
				g.Expect(secret.Labels).To(HaveKeyWithValue("foo", "bar"))
				g.Expect(secret.Labels).To(HaveKeyWithValue("baz", "qux"))
				g.Expect(secret.Labels).To(HaveKeyWithValue("quux", "corge"))

				// Check that external label is preserved
				g.Expect(secret.Labels).To(HaveKeyWithValue("external-label", "external-value"), "should preserve external label")

				return true
			}).Should(BeTrue(), "should eventually preserve both controller and external annotations/labels")
		})

		It("should correctly propagate labels matching the wildcard", func() {
			// Set to only propagate labels matching the wildcard
			careInstruction.Spec.PropagateLabels = []string{"test/*"}

			// Create a shoot and set some test/* labels on it
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-merge",
					Namespace: "default",
					Labels: map[string]string{
						"foo":         "bar",
						"test/label1": "value1",
						"test/label2": "value2",
						"baz":         "qux",
						"test/label3": "value3",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-merge.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create ConfigMap with CA data
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-merge.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create ConfigMap resource")

			// Wait for secret to be created
			var createdSecret *corev1.Secret
			Eventually(func(g Gomega) bool {
				secret := &corev1.Secret{}
				err := test.K8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-merge",
					Namespace: "default",
				}, secret)
				if err == nil {
					createdSecret = secret
					return true
				}
				return false
			}).Should(BeTrue(), "should eventually create secret")

			// Add external annotations and labels to the secret (simulating external controller or user)
			createdSecret.Annotations["external-annotation"] = "external-value"
			createdSecret.Labels["external-label"] = "external-value"
			Expect(test.K8sClient.Update(test.Ctx, createdSecret)).To(Succeed(), "should update secret with external annotations and labels")

			// Trigger reconciliation by updating shoot
			shoot.Labels["trigger"] = "merge-test"
			Expect(test.GardenK8sClient.Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot to trigger reconciliation")

			// Verify that all labels matching the PropagateLabels have been propagated from Shoot to Secret
			Eventually(func(g Gomega) bool {
				secret := &corev1.Secret{}
				err := test.K8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-merge",
					Namespace: "default",
				}, secret)
				g.Expect(err).NotTo(HaveOccurred(), "should get secret")

				// Check that controller-managed annotations are present
				g.Expect(secret.Annotations).To(HaveKeyWithValue("greenhouse.sap/propagate-labels", "test/*,quux,shoot-grafter.cloudoperators.dev/careinstruction"))
				g.Expect(secret.Annotations).To(HaveKeyWithValue(greenhouseapis.SecretAPIServerURLAnnotation, "https://api-server.test-shoot-merge.example.com"))

				// Check that external annotation is preserved
				g.Expect(secret.Annotations).To(HaveKeyWithValue("external-annotation", "external-value"), "should preserve external annotation")

				// Check that controller-managed labels are present
				g.Expect(secret.Labels).To(HaveKeyWithValue(v1alpha1.CareInstructionLabel, "test-careinstruction"))
				g.Expect(secret.Labels).To(HaveKeyWithValue("quux", "corge"))
				g.Expect(secret.Labels).To(HaveKeyWithValue("test/label1", "value1"))
				g.Expect(secret.Labels).To(HaveKeyWithValue("test/label2", "value2"))
				g.Expect(secret.Labels).To(HaveKeyWithValue("test/label3", "value3"))

				// Check that external label is preserved
				g.Expect(secret.Labels).To(HaveKeyWithValue("external-label", "external-value"), "should preserve external label")

				return true
			}).Should(BeTrue(), "should eventually preserve both controller and external annotations/labels")
		})
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
					PropagateLabels: []string{
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
					"shoot-grafter.cloudoperators.dev/careinstruction": "test-careinstruction-empty-selector",
					"foo":  "bar",
					"baz":  "qux",
					"quux": "corge",
				}), "should have the expected labels")
				g.Expect(secret.Annotations).To(Equal(map[string]string{
					"greenhouse.sap/last-applied-propagator":    "{\"labelKeys\":[\"foo\",\"baz\"]}",
					"greenhouse.sap/propagate-labels":           "foo,baz,quux,shoot-grafter.cloudoperators.dev/careinstruction",
					greenhouseapis.SecretAPIServerURLAnnotation: "https://api-server.test-shoot.example.com",
				}), "should have the expected annotations")
				g.Expect(secret.Data).To(HaveKeyWithValue("ca.crt", []byte(base64.StdEncoding.EncodeToString([]byte("test-ca-data")))), "should have the expected data")
				return true
			}).Should(BeTrue(), "should eventually find the expected Secret")
		})
	})

	When("testing event recording", func() {
		BeforeEach(func() {
			careInstruction = &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-events",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					ShootSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "events",
						},
					},
				},
			}
		})

		It("should emit SecretCreated event for successful reconciliation", func() {
			// Create a shoot that matches the selector
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-events",
					Namespace: "default",
					Labels: map[string]string{
						"test": "events",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-events.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create ConfigMap with CA data
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-events.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create ConfigMap resource")

			// Eventually check for SecretCreated event
			Eventually(func(g Gomega) bool {
				events := &corev1.EventList{}
				g.Expect(test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))).To(Succeed(), "should list events")

				hasSecretCreatedEvent := false

				for _, event := range events.Items {
					if event.InvolvedObject.Name == careInstruction.Name &&
						event.InvolvedObject.Kind == "CareInstruction" {
						if event.Reason == "SecretCreated" && event.Type == corev1.EventTypeNormal {
							g.Expect(event.Message).To(ContainSubstring("Created Greenhouse secret test-shoot-events"))
							g.Expect(event.Message).To(ContainSubstring("https://api-server.test-shoot-events.example.com"))
							hasSecretCreatedEvent = true
						}
					}
				}

				return hasSecretCreatedEvent
			}).Should(BeTrue(), "should eventually find SecretCreated event")
		})

		It("should emit APIServerURLMissing warning event when API server URL is not found", func() {
			// Create a shoot without advertised addresses
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-no-url",
					Namespace: "default",
					Labels: map[string]string{
						"test": "events",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")
			// No advertised addresses set in status

			// Eventually check for warning event
			Eventually(func(g Gomega) bool {
				events := &corev1.EventList{}
				g.Expect(test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))).To(Succeed(), "should list events")

				hasWarningEvent := false
				for _, event := range events.Items {
					if event.InvolvedObject.Name == careInstruction.Name &&
						event.InvolvedObject.Kind == "CareInstruction" &&
						event.Reason == "APIServerURLMissing" &&
						event.Type == corev1.EventTypeWarning {
						g.Expect(event.Message).To(ContainSubstring("No external API server URL found for shoot default/test-shoot-no-url"))
						hasWarningEvent = true
					}
				}

				return hasWarningEvent
			}).Should(BeTrue(), "should eventually find APIServerURLMissing warning event")
		})

		It("should emit CAConfigMapFetchFailed warning event when CA ConfigMap is missing", func() {
			// Create a shoot with advertised address but without CA ConfigMap
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-no-cm",
					Namespace: "default",
					Labels: map[string]string{
						"test": "events",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-no-cm.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Don't create the ConfigMap - this will cause the fetch to fail

			// Eventually check for warning event
			Eventually(func(g Gomega) bool {
				events := &corev1.EventList{}
				g.Expect(test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))).To(Succeed(), "should list events")

				hasWarningEvent := false
				for _, event := range events.Items {
					if event.InvolvedObject.Name == careInstruction.Name &&
						event.InvolvedObject.Kind == "CareInstruction" &&
						event.Reason == "CAConfigMapFetchFailed" &&
						event.Type == corev1.EventTypeWarning {
						g.Expect(event.Message).To(ContainSubstring("Failed to fetch CA ConfigMap for shoot default/test-shoot-no-cm"))
						hasWarningEvent = true
					}
				}

				return hasWarningEvent
			}).Should(BeTrue(), "should eventually find CAConfigMapFetchFailed warning event")
		})

		It("should emit CADataMissing warning event when CA data is empty", func() {
			// Create a shoot with advertised address
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-empty-ca",
					Namespace: "default",
					Labels: map[string]string{
						"test": "events",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-empty-ca.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create ConfigMap without CA data
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-empty-ca.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					// Empty data
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create ConfigMap resource")

			// Eventually check for warning event
			Eventually(func(g Gomega) bool {
				events := &corev1.EventList{}
				g.Expect(test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))).To(Succeed(), "should list events")

				hasWarningEvent := false
				for _, event := range events.Items {
					if event.InvolvedObject.Name == careInstruction.Name &&
						event.InvolvedObject.Kind == "CareInstruction" &&
						event.Reason == "CADataMissing" &&
						event.Type == corev1.EventTypeWarning {
						g.Expect(event.Message).To(ContainSubstring("No CA data found in ConfigMap"))
						g.Expect(event.Message).To(ContainSubstring("for shoot default/test-shoot-empty-ca"))
						hasWarningEvent = true
					}
				}

				return hasWarningEvent
			}).Should(BeTrue(), "should eventually find CADataMissing warning event")
		})

		It("should emit SecretUpdated event when secret is updated", func() {
			// Create a shoot with all required resources
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-update",
					Namespace: "default",
					Labels: map[string]string{
						"test": "events",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-update.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create ConfigMap with CA data
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-update.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data-v1",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create ConfigMap resource")

			// Wait for secret to be created
			Eventually(func(g Gomega) bool {
				secret := &corev1.Secret{}
				err := test.K8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-update",
					Namespace: "default",
				}, secret)
				return err == nil
			}).Should(BeTrue(), "should eventually create secret")

			// Update the ConfigMap
			cm.Data["ca.crt"] = "test-ca-data-v2"
			Expect(test.GardenK8sClient.Update(test.Ctx, cm)).To(Succeed(), "should update ConfigMap resource")

			// Trigger reconciliation by updating shoot
			shoot.Labels["trigger"] = "update"
			Expect(test.GardenK8sClient.Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot to trigger reconciliation")

			// Eventually check for SecretUpdated event
			Eventually(func(g Gomega) bool {
				events := &corev1.EventList{}
				g.Expect(test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))).To(Succeed(), "should list events")

				hasUpdatedEvent := false
				for _, event := range events.Items {
					if event.InvolvedObject.Name == careInstruction.Name &&
						event.InvolvedObject.Kind == "CareInstruction" &&
						event.Reason == "SecretUpdated" &&
						event.Type == corev1.EventTypeNormal {
						g.Expect(event.Message).To(ContainSubstring("Updated Greenhouse secret test-shoot-update"))
						g.Expect(event.Message).To(ContainSubstring("https://api-server.test-shoot-update.example.com"))
						hasUpdatedEvent = true
					}
				}

				return hasUpdatedEvent
			}).Should(BeTrue(), "should eventually find SecretUpdated event")
		})

		It("should emit ShootDeleted event when shoot is deleted", func() {
			// Create and then delete a shoot
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-delete",
					Namespace: "default",
					Labels: map[string]string{
						"test": "events",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-delete.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create ConfigMap with CA data
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-delete.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create ConfigMap resource")

			// Wait for initial reconciliation
			Eventually(func(g Gomega) bool {
				secret := &corev1.Secret{}
				err := test.K8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-delete",
					Namespace: "default",
				}, secret)
				return err == nil
			}).Should(BeTrue(), "should eventually create secret")

			// Delete the shoot
			Expect(test.GardenK8sClient.Delete(test.Ctx, shoot)).To(Succeed(), "should delete Shoot resource")

			// Eventually check for ShootDeleted event
			Eventually(func(g Gomega) bool {
				events := &corev1.EventList{}
				g.Expect(test.K8sClient.List(test.Ctx, events, client.InNamespace("default"))).To(Succeed(), "should list events")

				hasDeletedEvent := false
				for _, event := range events.Items {
					if event.InvolvedObject.Name == careInstruction.Name &&
						event.InvolvedObject.Kind == "CareInstruction" &&
						event.Reason == "ShootDeleted" &&
						event.Type == corev1.EventTypeNormal {
						g.Expect(event.Message).To(ContainSubstring("Shoot default/test-shoot-delete was deleted"))
						hasDeletedEvent = true
					}
				}

				return hasDeletedEvent
			}).Should(BeTrue(), "should eventually find ShootDeleted event")
		})
	})

	When("testing OIDC configuration", func() {
		var greenhouseAuthConfigMap *corev1.ConfigMap

		BeforeEach(func() {
			careInstruction = &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-oidc",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					ShootSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "oidc",
						},
					},
					AuthenticationConfigMapName: "greenhouse-auth-config",
				},
			}

			// Create the Greenhouse AuthenticationConfiguration ConfigMap with the label
			greenhouseAuthConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "greenhouse-auth-config",
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.AuthConfigMapLabel: "true",
					},
				},
				Data: map[string]string{
					"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://greenhouse.test.example.com
    audiences:
    - greenhouse
  claimMappings:
    username:
      claim: sub
      prefix: 'greenhouse:'
`,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, greenhouseAuthConfigMap)).To(Succeed(), "should create Greenhouse auth ConfigMap")
		})

		It("should create AuthenticationConfiguration ConfigMap for shoot", func() {
			// Create a shoot that matches the selector
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc",
					Namespace: "default",
					Labels: map[string]string{
						"test": "oidc",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-oidc.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create CA ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create CA ConfigMap resource")

			// Eventually check that the OIDC AuthenticationConfiguration ConfigMap is created
			Eventually(func(g Gomega) bool {
				authConfigMap := &corev1.ConfigMap{}
				err := test.GardenK8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-careinstruction-oidc-greenhouse-auth",
					Namespace: "default",
				}, authConfigMap)
				if err != nil {
					return false
				}

				// Verify ConfigMap has config.yaml data
				g.Expect(authConfigMap.Data).To(HaveKey("config.yaml"))

				// Parse and verify the configuration
				var authConfig apiserverv1beta1.AuthenticationConfiguration
				err = yaml.Unmarshal([]byte(authConfigMap.Data["config.yaml"]), &authConfig)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(authConfig.JWT).To(HaveLen(1))
				g.Expect(authConfig.JWT[0].Issuer.URL).To(Equal("https://greenhouse.test.example.com"))
				g.Expect(authConfig.JWT[0].Issuer.Audiences).To(ConsistOf("greenhouse"))
				g.Expect(authConfig.JWT[0].ClaimMappings.Username.Claim).To(Equal("sub"))
				g.Expect(authConfig.JWT[0].ClaimMappings.Username.Prefix).NotTo(BeNil())
				g.Expect(*authConfig.JWT[0].ClaimMappings.Username.Prefix).To(Equal("greenhouse:"))

				return true
			}).Should(BeTrue(), "should eventually create OIDC AuthenticationConfiguration ConfigMap")

			// Verify shoot spec was updated with ConfigMap reference
			Eventually(func(g Gomega) bool {
				updatedShoot := &gardenerv1beta1.Shoot{}
				err := test.GardenK8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-oidc",
					Namespace: "default",
				}, updatedShoot)
				if err != nil {
					return false
				}

				g.Expect(updatedShoot.Spec.Kubernetes.KubeAPIServer).NotTo(BeNil())
				g.Expect(updatedShoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication).NotTo(BeNil())
				g.Expect(updatedShoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName).To(Equal("test-careinstruction-oidc-greenhouse-auth"))

				return true
			}).Should(BeTrue(), "should eventually update shoot spec with ConfigMap reference")
		})

		It("should update existing OIDC configuration when greenhouse issuer changes", func() {
			// Create a shoot with existing OIDC config
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc-update",
					Namespace: "default",
					Labels: map[string]string{
						"test": "oidc",
					},
				},
				Spec: gardenerv1beta1.ShootSpec{
					Kubernetes: gardenerv1beta1.Kubernetes{
						KubeAPIServer: &gardenerv1beta1.KubeAPIServerConfig{
							StructuredAuthentication: &gardenerv1beta1.StructuredAuthentication{
								ConfigMapName: "test-shoot-oidc-update-auth",
							},
						},
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-oidc-update.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create existing auth ConfigMap with old greenhouse config
			existingAuthCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc-update-auth",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://greenhouse.test.example.com
    audiences:
    - old-audience
  claimMappings:
    username:
      claim: email
`,
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, existingAuthCM)).To(Succeed(), "should create existing auth ConfigMap")

			// Create CA ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc-update.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create CA ConfigMap resource")

			// Eventually verify the auth ConfigMap was updated with correct greenhouse config
			Eventually(func(g Gomega) bool {
				authConfigMap := &corev1.ConfigMap{}
				err := test.GardenK8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-oidc-update-auth",
					Namespace: "default",
				}, authConfigMap)
				if err != nil {
					return false
				}

				// Parse and verify the configuration was updated
				var authConfig apiserverv1beta1.AuthenticationConfiguration
				err = yaml.Unmarshal([]byte(authConfigMap.Data["config.yaml"]), &authConfig)
				g.Expect(err).NotTo(HaveOccurred())

				// Should have one issuer (greenhouse, updated)
				g.Expect(authConfig.JWT).To(HaveLen(1))
				g.Expect(authConfig.JWT[0].Issuer.URL).To(Equal("https://greenhouse.test.example.com"))
				g.Expect(authConfig.JWT[0].Issuer.Audiences).To(ConsistOf("greenhouse"))
				g.Expect(authConfig.JWT[0].ClaimMappings.Username.Claim).To(Equal("sub"))
				g.Expect(authConfig.JWT[0].ClaimMappings.Username.Prefix).NotTo(BeNil())
				g.Expect(*authConfig.JWT[0].ClaimMappings.Username.Prefix).To(Equal("greenhouse:"))

				return true
			}).Should(BeTrue(), "should eventually update existing OIDC configuration")
		})

		It("should preserve other issuers when adding greenhouse issuer", func() {
			// Create a shoot with existing OIDC config containing other issuers
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc-preserve",
					Namespace: "default",
					Labels: map[string]string{
						"test": "oidc",
					},
				},
				Spec: gardenerv1beta1.ShootSpec{
					Kubernetes: gardenerv1beta1.Kubernetes{
						KubeAPIServer: &gardenerv1beta1.KubeAPIServerConfig{
							StructuredAuthentication: &gardenerv1beta1.StructuredAuthentication{
								ConfigMapName: "test-shoot-oidc-preserve-auth",
							},
						},
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-oidc-preserve.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create existing auth ConfigMap with other issuers
			existingAuthCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc-preserve-auth",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://other-issuer1.example.com
    audiences:
    - issuer1
  claimMappings:
    username:
      claim: sub
- issuer:
    url: https://other-issuer2.example.com
    audiences:
    - issuer2
  claimMappings:
    username:
      claim: sub
`,
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, existingAuthCM)).To(Succeed(), "should create existing auth ConfigMap")

			// Create CA ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-oidc-preserve.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create CA ConfigMap resource")

			// Eventually verify the auth ConfigMap was updated with greenhouse issuer added
			Eventually(func(g Gomega) bool {
				authConfigMap := &corev1.ConfigMap{}
				err := test.GardenK8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-oidc-preserve-auth",
					Namespace: "default",
				}, authConfigMap)
				if err != nil {
					return false
				}

				// Parse and verify the configuration
				var authConfig apiserverv1beta1.AuthenticationConfiguration
				err = yaml.Unmarshal([]byte(authConfigMap.Data["config.yaml"]), &authConfig)
				g.Expect(err).NotTo(HaveOccurred())

				// Should have three issuers now (two original + greenhouse)
				g.Expect(authConfig.JWT).To(HaveLen(3))

				// Verify other issuers are preserved
				foundIssuer1 := false
				foundIssuer2 := false
				foundGreenhouse := false

				for _, issuer := range authConfig.JWT {
					switch issuer.Issuer.URL {
					case "https://other-issuer1.example.com":
						foundIssuer1 = true
						g.Expect(issuer.Issuer.Audiences).To(ConsistOf("issuer1"))
					case "https://other-issuer2.example.com":
						foundIssuer2 = true
						g.Expect(issuer.Issuer.Audiences).To(ConsistOf("issuer2"))
					case "https://greenhouse.test.example.com":
						foundGreenhouse = true
						g.Expect(issuer.Issuer.Audiences).To(ConsistOf("greenhouse"))
						g.Expect(issuer.ClaimMappings.Username.Claim).To(Equal("sub"))
						g.Expect(issuer.ClaimMappings.Username.Prefix).NotTo(BeNil())
						g.Expect(*issuer.ClaimMappings.Username.Prefix).To(Equal("greenhouse:"))
					}
				}

				g.Expect(foundIssuer1).To(BeTrue(), "should preserve issuer1")
				g.Expect(foundIssuer2).To(BeTrue(), "should preserve issuer2")
				g.Expect(foundGreenhouse).To(BeTrue(), "should add greenhouse issuer")

				return true
			}).Should(BeTrue(), "should eventually add greenhouse issuer while preserving others")
		})

		It("should preserve all user issuers and add greenhouse issuer in real-world complex ConfigMap scenario", func() {
			// This test reproduces a real-world scenario where:
			// - User creates a ConfigMap with 3 issuers (no greenhouse issuer)
			// - shoot-grafter should ADD the greenhouse issuer
			// - All 3 original user issuers must be preserved
			// - Final result: 4 issuers total (3 original + 1 greenhouse)
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-complex",
					Namespace: "default",
					Labels: map[string]string{
						"test": "oidc",
					},
				},
				Spec: gardenerv1beta1.ShootSpec{
					Kubernetes: gardenerv1beta1.Kubernetes{
						KubeAPIServer: &gardenerv1beta1.KubeAPIServerConfig{
							StructuredAuthentication: &gardenerv1beta1.StructuredAuthentication{
								ConfigMapName: "authentication-config-complex",
							},
						},
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot resource")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-complex.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create existing auth ConfigMap with only 3 user-defined issuers (NO greenhouse issuer yet)
			existingAuthCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "authentication-config-complex",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- claimMappings:
    groups:
      claim: groups
      prefix: ""
    username:
      claim: name
      prefix: ""
  issuer:
    audiences:
    - aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
    url: https://issuer1.example.com
- claimMappings:
    groups:
      expression: |-
        [
          "group1:" + claims.name.replace('prefix-','').split('-')[0],
          "group2:" + claims.name.replace('prefix-','').split('-')[1]
        ]
    username:
      expression: claims.name.replace('prefix-', '')
  issuer:
    audiences:
    - system-audience
    url: https://issuer2.example.com/v1/identity/oidc
- claimMappings:
    username:
      claim: sub
      prefix: 'cluster-1:'
  issuer:
    audiences:
    - kubernetes
    certificateAuthority: my-ca-certificate-data
    url: https://issuer3.example.com
`,
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, existingAuthCM)).To(Succeed(), "should create existing complex auth ConfigMap with 3 user issuers")

			// Create CA ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-complex.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create CA ConfigMap resource")

			// Eventually verify: 3 original user issuers + 1 greenhouse issuer = 4 total
			Eventually(func(g Gomega) bool {
				authConfigMap := &corev1.ConfigMap{}
				err := test.GardenK8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "authentication-config-complex",
					Namespace: "default",
				}, authConfigMap)
				if err != nil {
					return false
				}

				// Parse and verify the configuration
				var authConfig apiserverv1beta1.AuthenticationConfiguration
				err = yaml.Unmarshal([]byte(authConfigMap.Data["config.yaml"]), &authConfig)
				g.Expect(err).NotTo(HaveOccurred())

				// Should have 4 issuers total: 3 original user issuers + 1 greenhouse issuer added by shoot-grafter
				g.Expect(authConfig.JWT).To(HaveLen(4), "should have 4 issuers: 3 original + 1 greenhouse")

				// Track which issuers we found
				foundIssuerURLs := make(map[string]bool)
				for _, jwt := range authConfig.JWT {
					foundIssuerURLs[jwt.Issuer.URL] = true
				}

				// Verify all 3 original user issuers are preserved
				g.Expect(foundIssuerURLs).To(HaveKey("https://issuer1.example.com"), "should preserve issuer1")
				g.Expect(foundIssuerURLs).To(HaveKey("https://issuer2.example.com/v1/identity/oidc"), "should preserve issuer2")
				g.Expect(foundIssuerURLs).To(HaveKey("https://issuer3.example.com"), "should preserve issuer3")

				// Verify greenhouse issuer was added by shoot-grafter
				g.Expect(foundIssuerURLs).To(HaveKey("https://greenhouse.test.example.com"), "should add greenhouse issuer from greenhouse-auth-config")

				// Verify greenhouse issuer has correct configuration
				var greenhouseIssuer *apiserverv1beta1.JWTAuthenticator
				for i := range authConfig.JWT {
					if authConfig.JWT[i].Issuer.URL == "https://greenhouse.test.example.com" {
						greenhouseIssuer = &authConfig.JWT[i]
						break
					}
				}
				g.Expect(greenhouseIssuer).NotTo(BeNil(), "should find greenhouse issuer")
				g.Expect(greenhouseIssuer.Issuer.Audiences).To(ConsistOf("greenhouse"), "greenhouse issuer should have correct audience")
				g.Expect(greenhouseIssuer.ClaimMappings.Username.Claim).To(Equal("sub"), "greenhouse issuer should have correct username claim")
				g.Expect(greenhouseIssuer.ClaimMappings.Username.Prefix).NotTo(BeNil(), "greenhouse issuer should have username prefix")
				g.Expect(*greenhouseIssuer.ClaimMappings.Username.Prefix).To(Equal("greenhouse:"), "greenhouse issuer should have correct prefix")

				return true
			}).Should(BeTrue(), "should eventually have 4 issuers: 3 original user issuers preserved + 1 greenhouse issuer added")

			// Verify shoot spec references the correct ConfigMap
			Eventually(func(g Gomega) bool {
				updatedShoot := &gardenerv1beta1.Shoot{}
				err := test.GardenK8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "test-shoot-complex",
					Namespace: "default",
				}, updatedShoot)
				if err != nil {
					return false
				}

				g.Expect(updatedShoot.Spec.Kubernetes.KubeAPIServer).NotTo(BeNil())
				g.Expect(updatedShoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication).NotTo(BeNil())
				g.Expect(updatedShoot.Spec.Kubernetes.KubeAPIServer.StructuredAuthentication.ConfigMapName).To(Equal("authentication-config-complex"))

				return true
			}).Should(BeTrue(), "shoot should reference the correct ConfigMap")
		})
	})

	When("testing OIDC configuration with label auto-addition", func() {
		var greenhouseAuthConfigMapNoLabel *corev1.ConfigMap

		BeforeEach(func() {
			careInstruction = &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-label",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					ShootSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "label",
						},
					},
					AuthenticationConfigMapName: "greenhouse-auth-config-no-label",
				},
			}

			// Create the Greenhouse AuthenticationConfiguration ConfigMap WITHOUT the label
			greenhouseAuthConfigMapNoLabel = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "greenhouse-auth-config-no-label",
					Namespace: "default",
					// No labels initially
				},
				Data: map[string]string{
					"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://greenhouse-no-label.test.example.com
    audiences:
    - greenhouse
  claimMappings:
    username:
      claim: sub
      prefix: 'greenhouse:'
`,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, greenhouseAuthConfigMapNoLabel)).To(Succeed(), "should create Greenhouse auth ConfigMap without label")
		})

		It("should add auth ConfigMap label when not initially present", func() {
			// Create a shoot to trigger reconciliation
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-label",
					Namespace: "default",
					Labels: map[string]string{
						"test": "label",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create Shoot")

			shoot.Status = gardenerv1beta1.ShootStatus{
				AdvertisedAddresses: []gardenerv1beta1.ShootAdvertisedAddress{
					{
						Name: "external",
						URL:  "https://api-server.test-shoot-label.example.com",
					},
				},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shoot)).To(Succeed(), "should update Shoot status")

			// Create CA ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-label.ca-cluster",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca.crt": "test-ca-data",
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, cm)).To(Succeed(), "should create CA ConfigMap")

			// Eventually verify the label was added by the controller
			Eventually(func(g Gomega) bool {
				var updatedConfigMap corev1.ConfigMap
				err := test.K8sClient.Get(test.Ctx, client.ObjectKey{
					Name:      "greenhouse-auth-config-no-label",
					Namespace: "default",
				}, &updatedConfigMap)
				if err != nil {
					return false
				}
				// Verify the label was added
				g.Expect(updatedConfigMap.Labels).To(HaveKeyWithValue(v1alpha1.AuthConfigMapLabel, "true"))
				return true
			}).Should(BeTrue(), "controller should add auth ConfigMap label when not initially present")
		})
	})
})

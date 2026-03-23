// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction_test

import (
	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/internal/test"

	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
)

var _ = Describe("CareInstruction Controller", func() {
	BeforeEach(func(SpecContext) {

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

		clusters := &greenhousev1alpha1.ClusterList{}
		Expect(test.K8sClient.List(test.Ctx, clusters)).To(Succeed(), "should list Clusters")
		for _, cluster := range clusters.Items {
			// Do not clean up the garden cluster created in BeforeSuite
			if cluster.Name != test.GardenClusterName {
				Expect(client.IgnoreNotFound(test.K8sClient.Delete(test.Ctx, &cluster))).To(Succeed(), "should delete Cluster resource")
			}
		}
		Eventually(func(g Gomega) bool {
			clusters := &greenhousev1alpha1.ClusterList{}
			err := test.K8sClient.List(test.Ctx, clusters)
			if err != nil {
				return false
			}
			return len(clusters.Items) == 1 // Only the garden cluster should remain
		}).Should(BeTrue(), "should eventually not find Cluster resources")

	})
	Context("when a CareInstruction is created", func() {

		It("should show the correct status if the Garden cluster is not accessible", func() {
			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-1",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: "some-non-existing-cluster",
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction resource")

			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the GardenClusterAccessible condition to be false
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.GardenClusterAccessReady {
						g.Expect(condition.Status).To(Equal(metav1.ConditionFalse), "should have GardenClusterAccessible condition set to false")
					}
					if condition.Type == greenhousemetav1alpha1.ReadyCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionFalse), "should have Ready condition set to false")
					}
					if condition.Type == v1alpha1.ShootControllerStartedCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionFalse), "should have ShootControllerStarted condition set to false")
					}
				}
				return true
			}).Should(BeTrue(), "should eventually have GardenClusterAccessible condition set to false")
		})

		It("should show the correct status if the Garden cluster is accessible", func() {
			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-2",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
				},
			}

			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction resource")

			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the GardenClusterAccessible condition to be true
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.GardenClusterAccessReady {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have GardenClusterAccessible condition set to true")
					}
				}
				return true
			}).Should(BeTrue(), "should eventually have GardenClusterAccessible condition set to true")
		})

		It("should show the correct status if the Shoot controller has been started", func() {
			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-3",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
				},
			}

			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction resource")

			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the ShootControllerStarted and the overall Ready condition to be true
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.ShootControllerStartedCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have ShootControllerStarted condition set to true")
					}
					if condition.Type == greenhousemetav1alpha1.ReadyCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have Ready condition set to true")
					}
				}
				return true
			}).Should(BeTrue(), "should eventually have ShootControllerStarted condition set to true")
		})

		It("should show the correct status if the shoots targeted by the CareInstruction have been reconciled", func() {
			By("creating a Shoot object to be reconciled on the Garden cluster")
			shoot := gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot",
					Namespace: "default",
					Labels: map[string]string{
						"gardenClusterName": test.GardenClusterName,
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, &shoot)).To(Succeed(), "should create shoot object on garden cluster")
			By("creating a CareInstruction resource targeting the Shoot")
			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-4",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
				},
			}

			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction resource")

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")

				// Find and check the ShootsReconciled condition
				shootsReconciledCondition := careInstruction.Status.GetConditionByType(v1alpha1.ShootsReconciledCondition)
				g.Expect(shootsReconciledCondition).ToNot(BeNil(), "ShootsReconciled condition should exist")
				g.Expect(shootsReconciledCondition.Status).To(Equal(metav1.ConditionFalse), "should have ShootsReconciled condition set to false")

				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(1), "should have total shoot count set to 1")
				g.Expect(careInstruction.Status.CreatedClusters).To(Equal(0), "should have created clusters count set to 0")
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(0), "should have failed clusters count set to 0")
				return true
			}).Should(BeTrue(), "should eventually have ShootsReconciled condition set to false")

			By("creating a Cluster object for the Shoot")
			cluster := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster)).To(Succeed(), "should create Cluster resource for the shoot")

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(1), "should have total shoot count set to 1")
				g.Expect(careInstruction.Status.CreatedClusters).To(Equal(1), "should have created clusters count set to 1")
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(1), "should have failed clusters count set to 1")
				return true
			}).Should(BeTrue(), "should eventually have correct conditions in CareInstruction status")

			By("updating the Cluster status to ready")
			cluster.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster)).To(Succeed(), "should update Cluster status to ready")

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the ShootsReconciled condition to be true
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.ShootsReconciledCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have ShootsReconciled condition set to true")
					}
				}
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(1), "should have total shoot count set to 1")
				g.Expect(careInstruction.Status.CreatedClusters).To(Equal(1), "should have created clusters count set to 1")
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(0), "should have failed clusters count set to 0")
				return true
			}).Should(BeTrue(), "should eventually have ShootsReconciled condition set to true")

			By("Creating a second Shoot object to be reconciled on the Garden cluster")
			shoot2 := gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-2",
					Namespace: "default",
					Labels: map[string]string{
						"gardenClusterName": test.GardenClusterName,
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, &shoot2)).To(Succeed(), "should create second shoot object on garden cluster")

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the ShootsReconciled condition to be true
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.ShootsReconciledCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionFalse), "should have ShootsReconciled condition set to false")
					}
				}
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(2), "should have total shoot count set to 2")
				g.Expect(careInstruction.Status.CreatedClusters).To(Equal(1), "should have created clusters count set to 1")
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(0), "should have failed clusters count set to 0")
				return true
			}).Should(BeTrue(), "should eventually have ShootsReconciled condition set to true")
		})

	})

	Context("When two CareInstructions with different ShootSelectors targeting the same Garden cluster are created", func() {
		It("should reconcile the shoots and clusters correctly", func() {
			By("Creating three different shoots with respective labels on the garden cluster	")
			shoot1 := gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-1",
					Namespace: "default",
					Labels: map[string]string{
						"gardenClusterName": test.GardenClusterName,
						"shootSelector":     "selector1",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, &shoot1)).To(Succeed(), "should create first shoot object on garden cluster")

			shoot2 := gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-2",
					Namespace: "default",
					Labels: map[string]string{
						"gardenClusterName": test.GardenClusterName,
						"shootSelector":     "selector1",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, &shoot2)).To(Succeed(), "should create second shoot object on garden cluster")

			shoot3 := gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-3",
					Namespace: "default",
					Labels: map[string]string{
						"gardenClusterName": test.GardenClusterName,
						"shootSelector":     "selector2",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, &shoot3)).To(Succeed(), "should create third shoot object on garden cluster")
			By("Creating two CareInstructions with different ShootSelectors targeting the same Garden cluster")
			careInstruction1 := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-5",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"shootSelector": "selector1",
							},
						},
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction1)).To(Succeed(), "should create first CareInstruction resource")

			careInstruction2 := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-6",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"shootSelector": "selector2",
							},
						},
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction2)).To(Succeed(), "should create second CareInstruction resource")

			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction1), careInstruction1)).To(Succeed(), "should get first CareInstruction resource")
				g.Expect(careInstruction1.Status.Conditions).ToNot(BeEmpty(), "should have conditions in first CareInstruction status")
				g.Expect(careInstruction1.Status.TotalTargetShoots).To(Equal(2), "should have total shoot count set to 2 for first CareInstruction")
				g.Expect(careInstruction1.Status.CreatedClusters).To(Equal(0), "should have created clusters count set to 0 for first CareInstruction")
				g.Expect(careInstruction1.Status.FailedClusters).To(Equal(0), "should have failed clusters count set to 0 for first CareInstruction")

				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction2), careInstruction2)).To(Succeed(), "should get second CareInstruction resource")
				g.Expect(careInstruction2.Status.Conditions).ToNot(BeEmpty(), "should have conditions in second CareInstruction status")
				g.Expect(careInstruction2.Status.TotalTargetShoots).To(Equal(1), "should have total shoot count set to 1 for second CareInstruction")
				g.Expect(careInstruction2.Status.CreatedClusters).To(Equal(0), "should have created clusters count set to 0 for second CareInstruction")
				g.Expect(careInstruction2.Status.FailedClusters).To(Equal(0), "should have failed clusters count set to 0 for second CareInstruction")

				return true
			}).Should(BeTrue(), "should eventually have correct status for both CareInstructions")

			By("Creating clusters for the shoots targeted by the CareInstructions and setting them to ready")
			cluster1 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot1.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction1.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster1)).To(Succeed(), "should create Cluster resource for first shoot")
			cluster1.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster1)).To(Succeed(), "should update first Cluster status to ready")

			cluster2 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot2.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction1.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster2)).To(Succeed(), "should create Cluster resource for second shoot")

			cluster3 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot3.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction2.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster3)).To(Succeed(), "should create Cluster resource for third shoot")
			cluster3.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster3)).To(Succeed(), "should update third Cluster status to ready")

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction1)
					test.ReconcileObject(careInstruction2)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction1), careInstruction1)).To(Succeed(), "should get first CareInstruction resource")
				g.Expect(careInstruction1.Status.Conditions).ToNot(BeEmpty(), "should have conditions in first CareInstruction status")
				g.Expect(careInstruction1.Status.TotalTargetShoots).To(Equal(2), "should have total shoot count set to 2 for first CareInstruction")
				g.Expect(careInstruction1.Status.CreatedClusters).To(Equal(2), "should have created clusters count set to 2 for first CareInstruction")
				g.Expect(careInstruction1.Status.FailedClusters).To(Equal(1), "should have failed clusters count set to 0 for first CareInstruction")

				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction2), careInstruction2)).To(Succeed(), "should get second CareInstruction resource")
				g.Expect(careInstruction2.Status.Conditions).ToNot(BeEmpty(), "should have conditions in second CareInstruction status")
				g.Expect(careInstruction2.Status.TotalTargetShoots).To(Equal(1), "should have total shoot count set to 1 for second CareInstruction")
				g.Expect(careInstruction2.Status.CreatedClusters).To(Equal(1), "should have created clusters count set to 1 for second CareInstruction")
				g.Expect(careInstruction2.Status.FailedClusters).To(Equal(0), "should have failed clusters count set to 0 for second CareInstruction")

				return true
			}).Should(BeTrue(), "should eventually have correct status for both CareInstructions after creating clusters")
		})
	})

	Context("When a CareInstruction is updated", func() {
		It("should update the status accordingly", func() {
			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-8",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction resource")

			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the overall Ready condition to be true
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == greenhousemetav1alpha1.ReadyCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have Ready condition set to true")
					}
				}
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(0), "should have total shoot count set to 0")
				g.Expect(careInstruction.Status.CreatedClusters).To(Equal(0), "should have created clusters count set to 0")
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(0), "should have failed clusters count set to 0")
				return true
			}).Should(BeTrue(), "should eventually have GardenClusterAccessible condition set to true")

			// Update the CareInstruction
			careInstruction.Spec.GardenClusterName = "updated-garden-cluster"
			Expect(test.K8sClient.Update(test.Ctx, careInstruction)).To(Succeed(), "should update CareInstruction resource")

			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get updated CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the GardenClusterAccessible condition to be false since the cluster does not exist
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.GardenClusterAccessReady {
						g.Expect(condition.Status).To(Equal(metav1.ConditionFalse), "should have GardenClusterAccessible condition set to false after update")
					}
				}
				return true
			}).Should(BeTrue(), "should eventually have GardenClusterAccessible condition set to false after update")

			By("Creating a new garden cluster with secret to test the update")
			newGardenCluster := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "updated-garden-cluster",
					Namespace: "default",
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, newGardenCluster)).To(Succeed(), "should create new garden cluster")
			newGardenCluster.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, newGardenCluster)).To(Succeed(), "should update new garden cluster status to ready")

			gardenClusterSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "updated-garden-cluster",
					Namespace: "default",
				},
				Data: map[string][]byte{
					greenhouseapis.GreenHouseKubeConfigKey: test.KubeConfig,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, gardenClusterSecret)).To(Succeed(), "should create garden cluster secret")
			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get updated CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the GardenClusterAccessible and the ShootControllerStarted condition to be true after the update
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.GardenClusterAccessReady {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have GardenClusterAccessible condition set to true after update")
					}
					if condition.Type == v1alpha1.ShootControllerStartedCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have ShootCreated condition set to true after update")
					}
					if condition.Type == greenhousemetav1alpha1.ReadyCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have Ready condition set to true after update")
					}
				}
				return true
			}).Should(BeTrue(), "should eventually have GardenClusterAccessible and ShootCreated conditions set to true after update")
		})
	})

	Context("When clusters have different ready states", func() {
		It("should correctly populate ReadyClusterNames and NotReadyClusterNames status fields", func() {
			By("Creating three shoots on the garden cluster")
			shoot1 := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ready-shoot-1",
					Namespace: "default",
					Labels: map[string]string{
						"test": "cluster-names",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot1)).To(Succeed(), "should create first shoot")

			shoot2 := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ready-shoot-2",
					Namespace: "default",
					Labels: map[string]string{
						"test": "cluster-names",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot2)).To(Succeed(), "should create second shoot")

			shoot3 := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-ready-shoot",
					Namespace: "default",
					Labels: map[string]string{
						"test": "cluster-names",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot3)).To(Succeed(), "should create third shoot")

			By("Creating a CareInstruction targeting these shoots")
			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-names",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "cluster-names",
							},
						},
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction")

			By("Creating clusters - two ready and one not ready")
			cluster1 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot1.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster1)).To(Succeed(), "should create first cluster")
			cluster1.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster1)).To(Succeed(), "should set first cluster to ready")

			cluster2 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot2.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster2)).To(Succeed(), "should create second cluster")
			cluster2.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster2)).To(Succeed(), "should set second cluster to ready")

			cluster3 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot3.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster3)).To(Succeed(), "should create third cluster")
			// Don't set cluster3 to ready - leave it in not-ready state

			By("Verifying the status fields are populated correctly")
			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction")

				// Verify counts
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(3), "should have 3 total shoots")
				g.Expect(careInstruction.Status.CreatedClusters).To(Equal(3), "should have 3 created clusters")
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(1), "should have 1 failed cluster")

				// Verify Clusters list is populated correctly
				g.Expect(careInstruction.Status.Shoots).To(HaveLen(3), "should have 3 clusters in status")

				// Count ready and failed clusters
				readyCount := 0
				failedCount := 0
				for _, shootStatus := range careInstruction.Status.Shoots {
					switch shootStatus.Status {
					case v1alpha1.ShootStatusOnboarded:
						readyCount++
						g.Expect([]string{shoot1.Name, shoot2.Name}).To(ContainElement(shootStatus.Name), "ready cluster should be shoot1 or shoot2")
					case v1alpha1.ShootStatusFailed:
						failedCount++
						g.Expect(shootStatus.Name).To(Equal(shoot3.Name), "failed cluster should be shoot3")
					}
				}
				g.Expect(readyCount).To(Equal(2), "should have 2 ready clusters")
				g.Expect(failedCount).To(Equal(1), "should have 1 failed cluster")

				return true
			}).Should(BeTrue(), "should eventually have correct cluster status in Clusters list")

			By("Setting the third cluster to ready and verifying status updates")
			cluster3.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster3)).To(Succeed(), "should set third cluster to ready")

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction")

				// Verify all clusters are now ready
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(0), "should have 0 failed clusters")
				g.Expect(careInstruction.Status.Shoots).To(HaveLen(3), "should have 3 clusters in status")

				// Verify all clusters have Ready status
				readyCount := 0
				for _, shootStatus := range careInstruction.Status.Shoots {
					if shootStatus.Status == v1alpha1.ShootStatusOnboarded {
						readyCount++
					}
				}
				g.Expect(readyCount).To(Equal(3), "should have 3 ready clusters")

				// Verify ShootsReconciled condition is true
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.ShootsReconciledCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have ShootsReconciled condition set to true")
					}
				}

				return true
			}).Should(BeTrue(), "should eventually have all clusters ready")
		})
	})

	Context("When a shoot cluster is managed by a different CareInstruction", func() {
		It("should detect and report the ownership conflict in status", func() {
			By("Creating a shoot on the garden cluster")
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "conflict-shoot",
					Namespace: "default",
					Labels: map[string]string{
						"test": "ownership-conflict",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create shoot on garden cluster")

			By("Creating first CareInstruction targeting the shoot")
			careInstruction1 := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-owner",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "ownership-conflict",
							},
						},
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction1)).To(Succeed(), "should create first CareInstruction")

			By("Creating a cluster owned by the first CareInstruction")
			cluster := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shoot.Name,
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.CareInstructionLabel: careInstruction1.Name,
					},
				},
				Spec: greenhousev1alpha1.ClusterSpec{
					AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster)).To(Succeed(), "should create cluster owned by first CareInstruction")
			cluster.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionTrue,
					"ClusterReady",
					"Cluster is ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster)).To(Succeed(), "should set cluster to ready")

			By("Creating second CareInstruction targeting the same shoot")
			careInstruction2 := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-conflict",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "ownership-conflict",
							},
						},
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction2)).To(Succeed(), "should create second CareInstruction")

			By("Verifying second CareInstruction detects the ownership conflict")
			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction2)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction2), careInstruction2)).To(Succeed(), "should get second CareInstruction")

				// Should show count mismatch (1 shoot, 0 clusters owned by this CareInstruction)
				g.Expect(careInstruction2.Status.TotalTargetShoots).To(Equal(1), "should have 1 total shoot")
				g.Expect(careInstruction2.Status.CreatedClusters).To(Equal(0), "should have 0 created clusters")

				// Should have detected the conflict and added it to status
				g.Expect(careInstruction2.Status.Shoots).To(HaveLen(1), "should have 1 cluster in status")
				if len(careInstruction2.Status.Shoots) > 0 {
					conflictShoot := careInstruction2.Status.Shoots[0]
					g.Expect(conflictShoot.Name).To(Equal(shoot.Name), "cluster name should match shoot name")
					g.Expect(conflictShoot.Status).To(Equal(v1alpha1.ShootStatusOnboarded), "cluster status should be Ready (reflecting actual cluster state)")
					g.Expect(conflictShoot.Message).To(ContainSubstring("Cluster managed by different CareInstruction"), "message should indicate ownership conflict")
					g.Expect(conflictShoot.Message).To(ContainSubstring(careInstruction1.Name), "message should mention the actual owner")
				}

				// ShootsReconciled condition should be false due to mismatch
				shootsReconciledCondition := careInstruction2.Status.GetConditionByType(v1alpha1.ShootsReconciledCondition)
				g.Expect(shootsReconciledCondition).ToNot(BeNil(), "ShootsReconciled condition should exist")
				g.Expect(shootsReconciledCondition.Status).To(Equal(metav1.ConditionFalse), "ShootsReconciled should be false")

				return true
			}).Should(BeTrue(), "should eventually detect ownership conflict")

			By("Verifying first CareInstruction still shows correct status")
			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction1)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction1), careInstruction1)).To(Succeed(), "should get first CareInstruction")

				g.Expect(careInstruction1.Status.TotalTargetShoots).To(Equal(1), "should have 1 total shoot")
				g.Expect(careInstruction1.Status.CreatedClusters).To(Equal(1), "should have 1 created cluster")
				g.Expect(careInstruction1.Status.FailedClusters).To(Equal(0), "should have 0 failed clusters")

				// ShootsReconciled condition should be true for the owner
				shootsReconciledCondition := careInstruction1.Status.GetConditionByType(v1alpha1.ShootsReconciledCondition)
				g.Expect(shootsReconciledCondition).ToNot(BeNil(), "ShootsReconciled condition should exist")
				g.Expect(shootsReconciledCondition.Status).To(Equal(metav1.ConditionTrue), "ShootsReconciled should be true for owner")

				return true
			}).Should(BeTrue(), "first CareInstruction should maintain correct status")

			By("Verifying conflict is reported when cluster is not ready")
			// Update cluster to not ready
			cluster.Status.SetConditions(
				greenhousemetav1alpha1.NewCondition(
					greenhousemetav1alpha1.ReadyCondition,
					metav1.ConditionFalse,
					"ClusterNotReady",
					"Cluster is not ready",
				),
			)
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster)).To(Succeed(), "should set cluster to not ready")

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction2)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction2), careInstruction2)).To(Succeed(), "should get second CareInstruction")

				// Should still detect the conflict
				g.Expect(careInstruction2.Status.Shoots).To(HaveLen(1), "should have 1 cluster in status")
				if len(careInstruction2.Status.Shoots) > 0 {
					conflictShoot := careInstruction2.Status.Shoots[0]
					g.Expect(conflictShoot.Status).To(Equal(v1alpha1.ShootStatusFailed), "cluster status should be Failed (reflecting actual cluster state)")
					g.Expect(conflictShoot.Message).To(ContainSubstring("Cluster managed by different CareInstruction"), "message should indicate ownership conflict")
				}

				// Failed clusters should be incremented
				g.Expect(careInstruction2.Status.FailedClusters).To(Equal(1), "should have 1 failed cluster")

				return true
			}).Should(BeTrue(), "should report conflict with correct status when cluster is not ready")
		})

		It("should still populate status for owned shoots when there is a partial ownership conflict", func() {
			By("Creating two shoots on the garden cluster with the same label")
			shootOwned := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "partial-owned-shoot",
					Namespace: "default",
					Labels:    map[string]string{"test": "partial-conflict"},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shootOwned)).To(Succeed())

			shootConflict := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "partial-conflict-shoot",
					Namespace: "default",
					Labels:    map[string]string{"test": "partial-conflict"},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shootConflict)).To(Succeed())

			By("Creating a cluster for the conflict shoot owned by a different CareInstruction")
			conflictCluster := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shootConflict.Name,
					Namespace: "default",
					Labels:    map[string]string{v1alpha1.CareInstructionLabel: "some-other-ci"},
				},
				Spec: greenhousev1alpha1.ClusterSpec{AccessMode: greenhousev1alpha1.ClusterAccessModeDirect},
			}
			Expect(test.K8sClient.Create(test.Ctx, conflictCluster)).To(Succeed())
			conflictCluster.Status.SetConditions(greenhousemetav1alpha1.NewCondition(
				greenhousemetav1alpha1.ReadyCondition, metav1.ConditionTrue, "ClusterReady", "Cluster is ready"))
			Expect(test.K8sClient.Status().Update(test.Ctx, conflictCluster)).To(Succeed())

			By("Creating the CareInstruction under test targeting both shoots")
			ci := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ci-partial-conflict",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"test": "partial-conflict"},
						},
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, ci)).To(Succeed())

			By("Creating a cluster owned by this CareInstruction for the owned shoot")
			ownedCluster := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shootOwned.Name,
					Namespace: "default",
					Labels:    map[string]string{v1alpha1.CareInstructionLabel: ci.Name},
				},
				Spec: greenhousev1alpha1.ClusterSpec{AccessMode: greenhousev1alpha1.ClusterAccessModeDirect},
			}
			Expect(test.K8sClient.Create(test.Ctx, ownedCluster)).To(Succeed())
			ownedCluster.Status.SetConditions(greenhousemetav1alpha1.NewCondition(
				greenhousemetav1alpha1.ReadyCondition, metav1.ConditionTrue, "ClusterReady", "Cluster is ready"))
			Expect(test.K8sClient.Status().Update(test.Ctx, ownedCluster)).To(Succeed())

			By("Verifying status contains both owned and conflicting shoots")
			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(ci)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(ci), ci)).To(Succeed())

				g.Expect(ci.Status.Shoots).To(HaveLen(2), "should have 2 shoots in status (owned + conflict)")

				shootNames := map[string]string{}
				for _, s := range ci.Status.Shoots {
					shootNames[s.Name] = s.Status
				}
				g.Expect(shootNames).To(HaveKeyWithValue(shootOwned.Name, v1alpha1.ShootStatusOnboarded),
					"owned shoot should have Onboarded status")
				g.Expect(shootNames).To(HaveKeyWithValue(shootConflict.Name, v1alpha1.ShootStatusOnboarded),
					"conflict shoot should have Onboarded status")

				return true
			}).Should(BeTrue(), "should have complete status for both owned and conflicting shoots")
		})
	})

	Context("When a CareInstruction is deleted", func() {
		It("should stop the Shoot controller", func() {
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shoot-4",
					Namespace: "default",
					Labels: map[string]string{
						"gardenClusterName": test.GardenClusterName,
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed(), "should create shoot object on garden cluster")

			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-careinstruction-7",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed(), "should create CareInstruction resource")

			Eventually(func(g Gomega) bool {
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed(), "should get CareInstruction resource")
				g.Expect(careInstruction.Status.Conditions).ToNot(BeEmpty(), "should have conditions in CareInstruction status")
				// Expect the ShootControllerStarted condition to be true
				for _, condition := range careInstruction.Status.Conditions {
					if condition.Type == v1alpha1.ShootControllerStartedCondition {
						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "should have ShootControllerStarted condition set to true")
					}
				}
				return true
			}).Should(BeTrue(), "should eventually have ShootControllerStarted condition set to true")

			Expect(test.K8sClient.Delete(test.Ctx, careInstruction)).To(Succeed(), "should delete CareInstruction resource")

			Eventually(func(g Gomega) bool {
				err := test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)
				return client.IgnoreNotFound(err) == nil
			}).Should(BeTrue(), "should eventually not find CareInstruction resource")

			// Check if the Shoot controller has been stopped
			// Eventually(func(g Gomega) bool {
			// 	shootControllerName := shoot.Name + "-" + shoot.ShootControllerSuffix
			// 	return true
			// }).Should(BeTrue(), "should eventually stop the Shoot controller for the deleted CareInstruction")

		})
	})

	Context("When using CEL expression filtering", func() {
		It("should filter shoots and populate ExcludedShoots status", func() {
			shootSucceeded1 := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-shoot-succeeded-1",
					Namespace: "default",
					Labels:    map[string]string{"test": "cel-filter"},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shootSucceeded1)).To(Succeed())
			shootSucceeded1.Status = gardenerv1beta1.ShootStatus{
				LastOperation: &gardenerv1beta1.LastOperation{State: gardenerv1beta1.LastOperationStateSucceeded},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shootSucceeded1)).To(Succeed())

			shootSucceeded2 := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-shoot-succeeded-2",
					Namespace: "default",
					Labels:    map[string]string{"test": "cel-filter"},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shootSucceeded2)).To(Succeed())
			shootSucceeded2.Status = gardenerv1beta1.ShootStatus{
				LastOperation: &gardenerv1beta1.LastOperation{State: gardenerv1beta1.LastOperationStateSucceeded},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shootSucceeded2)).To(Succeed())

			shootFailed := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-shoot-failed",
					Namespace: "default",
					Labels:    map[string]string{"test": "cel-filter"},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shootFailed)).To(Succeed())
			shootFailed.Status = gardenerv1beta1.ShootStatus{
				LastOperation: &gardenerv1beta1.LastOperation{State: gardenerv1beta1.LastOperationStateFailed},
			}
			Expect(test.GardenK8sClient.Status().Update(test.Ctx, shootFailed)).To(Succeed())

			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cel-filter",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"test": "cel-filter"},
						},
						Expression: `object.status.lastOperation.state == "Succeeded"`,
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed())

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed())
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(3))
				// Verify excluded shoot appears in Shoots list with Excluded status
				excludedCount := 0
				for _, s := range careInstruction.Status.Shoots {
					if s.Status == v1alpha1.ShootStatusExcluded {
						excludedCount++
						g.Expect(s.Name).To(Equal("cel-shoot-failed"))
						g.Expect(s.Message).To(ContainSubstring("filtered out by CEL expression"))
					}
				}
				g.Expect(excludedCount).To(Equal(1))
				return true
			}).Should(BeTrue())

			cluster1 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shootSucceeded1.Name,
					Namespace: "default",
					Labels:    map[string]string{v1alpha1.CareInstructionLabel: careInstruction.Name},
				},
				Spec: greenhousev1alpha1.ClusterSpec{AccessMode: greenhousev1alpha1.ClusterAccessModeDirect},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster1)).To(Succeed())
			cluster1.Status.SetConditions(greenhousemetav1alpha1.NewCondition(
				greenhousemetav1alpha1.ReadyCondition, metav1.ConditionTrue, "ClusterReady", "Cluster is ready"))
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster1)).To(Succeed())

			cluster2 := &greenhousev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shootSucceeded2.Name,
					Namespace: "default",
					Labels:    map[string]string{v1alpha1.CareInstructionLabel: careInstruction.Name},
				},
				Spec: greenhousev1alpha1.ClusterSpec{AccessMode: greenhousev1alpha1.ClusterAccessModeDirect},
			}
			Expect(test.K8sClient.Create(test.Ctx, cluster2)).To(Succeed())
			cluster2.Status.SetConditions(greenhousemetav1alpha1.NewCondition(
				greenhousemetav1alpha1.ReadyCondition, metav1.ConditionTrue, "ClusterReady", "Cluster is ready"))
			Expect(test.K8sClient.Status().Update(test.Ctx, cluster2)).To(Succeed())

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed())
				g.Expect(careInstruction.Status.CreatedClusters).To(Equal(2))
				g.Expect(careInstruction.Status.FailedClusters).To(Equal(0))
				shootsReconciledCondition := careInstruction.Status.GetConditionByType(v1alpha1.ShootsReconciledCondition)
				g.Expect(shootsReconciledCondition).ToNot(BeNil())
				g.Expect(shootsReconciledCondition.Status).To(Equal(metav1.ConditionTrue))
				return true
			}).Should(BeTrue())
		})

		It("should handle invalid CEL expression gracefully", func() {
			shoot := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-invalid-shoot",
					Namespace: "default",
					Labels: map[string]string{
						"test": "cel-invalid",
					},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot)).To(Succeed())

			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cel-invalid",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "cel-invalid",
							},
						},
						Expression: `invalid syntax here`,
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed())

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed())
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(1))
				// Verify excluded shoot appears in Shoots list with Excluded status
				excludedCount := 0
				for _, s := range careInstruction.Status.Shoots {
					if s.Status == v1alpha1.ShootStatusExcluded {
						excludedCount++
						g.Expect(s.Name).To(Equal("cel-invalid-shoot"))
						g.Expect(s.Message).To(ContainSubstring("CEL evaluation failed"))
					}
				}
				g.Expect(excludedCount).To(Equal(1))
				return true
			}).Should(BeTrue())
		})

		It("should not filter when CEL expression is empty", func() {
			shoot1 := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-empty-shoot-1",
					Namespace: "default",
					Labels:    map[string]string{"test": "cel-empty"},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot1)).To(Succeed())

			shoot2 := &gardenerv1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cel-empty-shoot-2",
					Namespace: "default",
					Labels:    map[string]string{"test": "cel-empty"},
				},
			}
			Expect(test.GardenK8sClient.Create(test.Ctx, shoot2)).To(Succeed())

			careInstruction := &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cel-empty",
					Namespace: "default",
				},
				Spec: v1alpha1.CareInstructionSpec{
					GardenClusterName: test.GardenClusterName,
					ShootSelector: &v1alpha1.ShootSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"test": "cel-empty"},
						},
					},
				},
			}
			Expect(test.K8sClient.Create(test.Ctx, careInstruction)).To(Succeed())

			Eventually(func(g Gomega) bool {
				defer func() {
					test.ReconcileObject(careInstruction)
				}()
				g.Expect(test.K8sClient.Get(test.Ctx, client.ObjectKeyFromObject(careInstruction), careInstruction)).To(Succeed())
				g.Expect(careInstruction.Status.TotalTargetShoots).To(Equal(2))
				// No shoots should have Excluded status
				for _, s := range careInstruction.Status.Shoots {
					g.Expect(s.Status).ToNot(Equal(v1alpha1.ShootStatusExcluded))
				}
				return true
			}).Should(BeTrue())
		})
	})

})

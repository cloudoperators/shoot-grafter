// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot_test

import (
	"context"

	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/controller/shoot"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// authScheme builds a minimal runtime.Scheme for the fake clients used in auth tests.
func authScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		panic(err)
	}
	if err := gardenerv1beta1.AddToScheme(s); err != nil {
		panic(err)
	}
	return s
}

// makeAuthController returns a ShootController wired with a Greenhouse fake client and a Garden
// fake client that already holds the provided objects.
func makeAuthController(careInstructionName string, greenhouseObjs, gardenObjs []client.Object) *shoot.ShootController {
	s := authScheme()
	return &shoot.ShootController{
		GreenhouseClient: fake.NewClientBuilder().WithScheme(s).WithObjects(greenhouseObjs...).Build(),
		GardenClient:     fake.NewClientBuilder().WithScheme(s).WithObjects(gardenObjs...).Build(),
		Logger:           logr.Discard(),
		CareInstruction: &v1alpha1.CareInstruction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      careInstructionName,
				Namespace: "default",
			},
			Spec: v1alpha1.CareInstructionSpec{
				AuthenticationConfigMapName: "greenhouse-auth",
			},
		},
	}
}

const authYAML = `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://greenhouse.example.com
    audiences:
    - greenhouse
  claimMappings:
    username:
      claim: sub
      prefix: 'greenhouse:'
`

var _ = Describe("enqueueShoots", func() {
	It("enqueues only shoots labeled with the CareInstruction name, not unrelated shoots", func() {
		ctx := context.Background()
		ciName := "my-ci"

		labeled := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "labeled-shoot",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.CareInstructionLabel: ciName},
			},
		}
		unrelated := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unrelated-shoot",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.CareInstructionLabel: "other-ci"},
			},
		}
		unlabeled := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "unlabeled-shoot", Namespace: "default"},
		}

		s := authScheme()
		sc := &shoot.ShootController{
			GardenClient: fake.NewClientBuilder().WithScheme(s).WithObjects(labeled, unrelated, unlabeled).
				WithIndex(&gardenerv1beta1.Shoot{}, v1alpha1.CareInstructionLabel, func(o client.Object) []string {
					if v := o.GetLabels()[v1alpha1.CareInstructionLabel]; v != "" {
						return []string{v}
					}
					return nil
				}).Build(),
			Logger: logr.Discard(),
			CareInstruction: &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{Name: ciName, Namespace: "default"},
			},
		}

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-auth-cm",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.CareInstructionLabel: ciName},
			},
		}

		reqs := sc.EnqueueShoots(ctx, cm)
		Expect(reqs).To(HaveLen(1))
		Expect(reqs[0].Name).To(Equal("labeled-shoot"))
	})
})

var _ = Describe("configureOIDCAuthentication", func() {
	var (
		ctx              = context.Background()
		greenhouseAuthCM *corev1.ConfigMap
	)

	BeforeEach(func() {
		greenhouseAuthCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "greenhouse-auth",
				Namespace: "default",
			},
			Data: map[string]string{
				"config.yaml": authYAML,
			},
		}
	})

	It("creates the garden CM with the Greenhouse content verbatim on first encounter", func() {
		shoot := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "my-shoot", Namespace: "default"},
		}
		ctrl := makeAuthController("my-ci", []client.Object{greenhouseAuthCM}, []client.Object{shoot})

		Expect(ctrl.ConfigureOIDCAuthentication(ctx, shoot)).To(Succeed())

		var gardenCM corev1.ConfigMap
		Expect(ctrl.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: "default", Name: "my-ci-greenhouse-auth",
		}, &gardenCM)).To(Succeed())

		Expect(gardenCM.Data["config.yaml"]).To(Equal(authYAML))
		Expect(gardenCM.Labels).To(HaveKeyWithValue(v1alpha1.CareInstructionLabel, "my-ci"))
	})

	It("adds CareInstructionLabel to the Shoot", func() {
		shoot := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "my-shoot", Namespace: "default"},
		}
		ctrl := makeAuthController("my-ci", []client.Object{greenhouseAuthCM}, []client.Object{shoot})

		Expect(ctrl.ConfigureOIDCAuthentication(ctx, shoot)).To(Succeed())

		var updatedShoot gardenerv1beta1.Shoot
		Expect(ctrl.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: "default", Name: "my-shoot",
		}, &updatedShoot)).To(Succeed())
		Expect(updatedShoot.Labels).To(HaveKeyWithValue(v1alpha1.CareInstructionLabel, "my-ci"))
	})

	It("uses the default CM name (<ci-name>-greenhouse-auth) when Shoot has no existing reference", func() {
		shoot := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "my-shoot", Namespace: "default"},
		}
		ctrl := makeAuthController("ci-default", []client.Object{greenhouseAuthCM}, []client.Object{shoot})

		Expect(ctrl.ConfigureOIDCAuthentication(ctx, shoot)).To(Succeed())

		var gardenCM corev1.ConfigMap
		Expect(ctrl.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: "default", Name: "ci-default-greenhouse-auth",
		}, &gardenCM)).To(Succeed())
	})

	It("overwrites an existing garden CM that already has different content", func() {
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-auth", Namespace: "default"},
			Data: map[string]string{
				"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://other-issuer.example.com
    audiences:
    - other
  claimMappings:
    username:
      claim: email
`,
			},
		}
		shoot := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "my-shoot", Namespace: "default"},
			Spec: gardenerv1beta1.ShootSpec{
				Kubernetes: gardenerv1beta1.Kubernetes{
					KubeAPIServer: &gardenerv1beta1.KubeAPIServerConfig{
						StructuredAuthentication: &gardenerv1beta1.StructuredAuthentication{
							ConfigMapName: "existing-auth",
						},
					},
				},
			},
		}
		ctrl := makeAuthController("my-ci", []client.Object{greenhouseAuthCM}, []client.Object{existingCM, shoot})

		Expect(ctrl.ConfigureOIDCAuthentication(ctx, shoot)).To(Succeed())

		var gardenCM corev1.ConfigMap
		Expect(ctrl.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: "default", Name: "existing-auth",
		}, &gardenCM)).To(Succeed())

		// Content must be exactly the Greenhouse content - old issuer must be gone
		Expect(gardenCM.Data["config.yaml"]).To(Equal(authYAML))
		Expect(gardenCM.Labels).To(HaveKeyWithValue(v1alpha1.CareInstructionLabel, "my-ci"))
	})

	It("updates the garden CM when Greenhouse CM content changes", func() {
		updatedYAML := `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://greenhouse-new.example.com
    audiences:
    - greenhouse-new
  claimMappings:
    username:
      claim: sub
      prefix: 'new:'
`
		// Greenhouse CM already has the new content; garden CM has the old content
		updatedGreenhouseCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "greenhouse-auth", Namespace: "default",
			},
			Data: map[string]string{"config.yaml": updatedYAML},
		}
		gardenCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-ci-greenhouse-auth",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.CareInstructionLabel: "my-ci"},
			},
			Data: map[string]string{"config.yaml": authYAML},
		}
		shoot := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "my-shoot", Namespace: "default"},
			Spec: gardenerv1beta1.ShootSpec{
				Kubernetes: gardenerv1beta1.Kubernetes{
					KubeAPIServer: &gardenerv1beta1.KubeAPIServerConfig{
						StructuredAuthentication: &gardenerv1beta1.StructuredAuthentication{
							ConfigMapName: "my-ci-greenhouse-auth",
						},
					},
				},
			},
		}

		ctrl := makeAuthController("my-ci", []client.Object{updatedGreenhouseCM}, []client.Object{gardenCM, shoot})

		Expect(ctrl.ConfigureOIDCAuthentication(ctx, shoot)).To(Succeed())

		var result corev1.ConfigMap
		Expect(ctrl.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: "default", Name: "my-ci-greenhouse-auth",
		}, &result)).To(Succeed())
		Expect(result.Data["config.yaml"]).To(Equal(updatedYAML))
	})

	It("updates the garden CM content when the CI's AuthenticationConfigMapName is changed to a different Greenhouse CM", func() {
		// Scenario: CI.Spec.AuthenticationConfigMapName was "greenhouse-auth" and is now
		// changed to "greenhouse-auth-v2". The garden CM name is unchanged (<ci>-greenhouse-auth),
		// but its content must reflect the new Greenhouse CM.
		newGreenhouseCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "greenhouse-auth-v2",
				Namespace: "default",
			},
			Data: map[string]string{
				"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://greenhouse-v2.example.com
    audiences:
    - greenhouse-v2
  claimMappings:
    username:
      claim: sub
      prefix: 'v2:'
`,
			},
		}
		gardenCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-ci-greenhouse-auth",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.CareInstructionLabel: "my-ci"},
			},
			Data: map[string]string{"config.yaml": authYAML},
		}
		shootObj := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "my-shoot", Namespace: "default"},
			Spec: gardenerv1beta1.ShootSpec{
				Kubernetes: gardenerv1beta1.Kubernetes{
					KubeAPIServer: &gardenerv1beta1.KubeAPIServerConfig{
						StructuredAuthentication: &gardenerv1beta1.StructuredAuthentication{
							ConfigMapName: "my-ci-greenhouse-auth",
						},
					},
				},
			},
		}

		s := authScheme()
		ctrl := &shoot.ShootController{
			GreenhouseClient: fake.NewClientBuilder().WithScheme(s).WithObjects(newGreenhouseCM).Build(),
			GardenClient:     fake.NewClientBuilder().WithScheme(s).WithObjects(gardenCM, shootObj).Build(),
			Logger:           logr.Discard(),
			CareInstruction: &v1alpha1.CareInstruction{
				ObjectMeta: metav1.ObjectMeta{Name: "my-ci", Namespace: "default"},
				Spec: v1alpha1.CareInstructionSpec{
					// CI now references the new Greenhouse CM
					AuthenticationConfigMapName: "greenhouse-auth-v2",
				},
			},
		}

		Expect(ctrl.ConfigureOIDCAuthentication(ctx, shootObj)).To(Succeed())

		var result corev1.ConfigMap
		Expect(ctrl.GardenClient.Get(ctx, client.ObjectKey{
			Namespace: "default", Name: "my-ci-greenhouse-auth",
		}, &result)).To(Succeed())
		// Garden CM name is unchanged; content reflects the new Greenhouse CM
		Expect(result.Data["config.yaml"]).To(Equal(newGreenhouseCM.Data["config.yaml"]))
	})
})

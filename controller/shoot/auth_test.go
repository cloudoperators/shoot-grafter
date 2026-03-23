// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"shoot-grafter/api/v1alpha1"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiserverv1beta1 "k8s.io/apiserver/pkg/apis/apiserver/v1beta1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Auth", func() {
	Describe("mergeAuthenticationConfigurations", func() {
		var controller *ShootController

		BeforeEach(func() {
			controller = &ShootController{
				Logger: logr.Discard(),
				CareInstruction: &v1alpha1.CareInstruction{
					Spec: v1alpha1.CareInstructionSpec{
						AuthenticationConfigMapName: "greenhouse-auth-config",
					},
				},
			}
		})

		DescribeTable("should correctly merge authentication configurations",
			func(
				initialGardenConfigMap *corev1.ConfigMap,
				greenhouseAuthConfig apiserverv1beta1.AuthenticationConfiguration,
				expectedConfigMap *corev1.ConfigMap,
			) {
				// Call the merge function
				err := controller.mergeAuthenticationConfigurations(initialGardenConfigMap, &greenhouseAuthConfig)
				Expect(err).NotTo(HaveOccurred())

				// Verify ConfigMap data was updated
				Expect(initialGardenConfigMap.Data).NotTo(BeNil())
				Expect(initialGardenConfigMap.Data).To(HaveKey("config.yaml"))

				// Verify other data keys are preserved (keys that are not "config.yaml")
				for key, value := range expectedConfigMap.Data {
					if key != "config.yaml" {
						Expect(initialGardenConfigMap.Data).To(HaveKeyWithValue(key, value))
					}
				}

				// Unmarshal the actual result
				var actualConfig apiserverv1beta1.AuthenticationConfiguration
				err = yaml.Unmarshal([]byte(initialGardenConfigMap.Data["config.yaml"]), &actualConfig)
				Expect(err).NotTo(HaveOccurred())

				// Unmarshal the expected configuration
				var expectedConfig apiserverv1beta1.AuthenticationConfiguration
				err = yaml.Unmarshal([]byte(expectedConfigMap.Data["config.yaml"]), &expectedConfig)
				Expect(err).NotTo(HaveOccurred())

				// Compare configurations
				Expect(actualConfig.APIVersion).To(Equal(expectedConfig.APIVersion))
				Expect(actualConfig.Kind).To(Equal(expectedConfig.Kind))
				Expect(actualConfig.JWT).To(HaveLen(len(expectedConfig.JWT)))

				// Compare each issuer
				for i := range expectedConfig.JWT {
					Expect(actualConfig.JWT[i].Issuer.URL).To(Equal(expectedConfig.JWT[i].Issuer.URL))
					Expect(actualConfig.JWT[i].Issuer.Audiences).To(Equal(expectedConfig.JWT[i].Issuer.Audiences))
					Expect(actualConfig.JWT[i].ClaimMappings.Username.Claim).To(Equal(expectedConfig.JWT[i].ClaimMappings.Username.Claim))

					if expectedConfig.JWT[i].ClaimMappings.Username.Prefix != nil {
						Expect(actualConfig.JWT[i].ClaimMappings.Username.Prefix).NotTo(BeNil())
						Expect(*actualConfig.JWT[i].ClaimMappings.Username.Prefix).To(Equal(*expectedConfig.JWT[i].ClaimMappings.Username.Prefix))
					} else {
						Expect(actualConfig.JWT[i].ClaimMappings.Username.Prefix).To(BeNil())
					}
				}
			},
			Entry("with empty Garden ConfigMap and one Greenhouse issuer",
				&corev1.ConfigMap{
					Data: map[string]string{},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					JWT: []apiserverv1beta1.JWTAuthenticator{
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://greenhouse.example.com",
								Audiences: []string{"greenhouse"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim:  "sub",
									Prefix: stringPtr("greenhouse:"),
								},
							},
						},
					},
				},
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
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
`,
					},
				}),
			Entry("with Garden ConfigMap having other data keys (should preserve them)",
				&corev1.ConfigMap{
					Data: map[string]string{
						"other-key":   "other-value",
						"another-key": "another-value",
					},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					JWT: []apiserverv1beta1.JWTAuthenticator{
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://greenhouse.example.com",
								Audiences: []string{"greenhouse"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim:  "sub",
									Prefix: stringPtr("greenhouse:"),
								},
							},
						},
					},
				},
				&corev1.ConfigMap{
					Data: map[string]string{
						"other-key":   "other-value",
						"another-key": "another-value",
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
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
`,
					},
				}),
			Entry("with Garden ConfigMap having one different issuer (should add Greenhouse issuer)",
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://other.example.com
    audiences:
    - other
  claimMappings:
    username:
      claim: sub
`,
					},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					JWT: []apiserverv1beta1.JWTAuthenticator{
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://greenhouse.example.com",
								Audiences: []string{"greenhouse"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim:  "sub",
									Prefix: stringPtr("greenhouse:"),
								},
							},
						},
					},
				},
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://other.example.com
    audiences:
    - other
  claimMappings:
    username:
      claim: sub
- issuer:
    url: https://greenhouse.example.com
    audiences:
    - greenhouse
  claimMappings:
    username:
      claim: sub
      prefix: 'greenhouse:'
`,
					},
				}),
			Entry("with Garden ConfigMap having the same issuer (Greenhouse should update it)",
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://greenhouse.example.com
    audiences:
    - old-audience
  claimMappings:
    username:
      claim: email
`,
					},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					JWT: []apiserverv1beta1.JWTAuthenticator{
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://greenhouse.example.com",
								Audiences: []string{"greenhouse"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim:  "sub",
									Prefix: stringPtr("greenhouse:"),
								},
							},
						},
					},
				},
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
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
`,
					},
				}),
			Entry("with multiple Greenhouse issuers and multiple Garden issuers",
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://garden-issuer1.example.com
    audiences:
    - garden1
  claimMappings:
    username:
      claim: sub
- issuer:
    url: https://greenhouse.example.com
    audiences:
    - old-audience
  claimMappings:
    username:
      claim: email
- issuer:
    url: https://garden-issuer2.example.com
    audiences:
    - garden2
  claimMappings:
    username:
      claim: sub
`,
					},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					JWT: []apiserverv1beta1.JWTAuthenticator{
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://greenhouse.example.com",
								Audiences: []string{"greenhouse"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim:  "sub",
									Prefix: stringPtr("greenhouse:"),
								},
							},
						},
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://another-greenhouse.example.com",
								Audiences: []string{"another"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim:  "email",
									Prefix: stringPtr("other:"),
								},
							},
						},
					},
				},
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://garden-issuer1.example.com
    audiences:
    - garden1
  claimMappings:
    username:
      claim: sub
- issuer:
    url: https://greenhouse.example.com
    audiences:
    - greenhouse
  claimMappings:
    username:
      claim: sub
      prefix: 'greenhouse:'
- issuer:
    url: https://garden-issuer2.example.com
    audiences:
    - garden2
  claimMappings:
    username:
      claim: sub
- issuer:
    url: https://another-greenhouse.example.com
    audiences:
    - another
  claimMappings:
    username:
      claim: email
      prefix: 'other:'
`,
					},
				}),
		)

		It("should return error for invalid YAML in Garden ConfigMap", func() {
			configMap := &corev1.ConfigMap{
				Data: map[string]string{
					"config.yaml": "invalid: yaml: content: [",
				},
			}

			greenhouseAuthConfig := apiserverv1beta1.AuthenticationConfiguration{
				JWT: []apiserverv1beta1.JWTAuthenticator{
					{
						Issuer: apiserverv1beta1.Issuer{
							URL:       "https://greenhouse.example.com",
							Audiences: []string{"greenhouse"},
						},
					},
				},
			}

			err := controller.mergeAuthenticationConfigurations(configMap, &greenhouseAuthConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse existing Garden AuthenticationConfiguration"))
		})
	})
})

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}

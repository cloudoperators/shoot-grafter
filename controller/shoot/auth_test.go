// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"shoot-grafter/api/v1alpha1"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiserverv1beta1 "k8s.io/apiserver/pkg/apis/apiserver/v1beta1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Auth", func() {
	Describe("createGreenhouseIssuerConfig", func() {
		DescribeTable("should create a valid JWT authenticator config",
			func(issuerURL string) {
				controller := &ShootController{
					Logger: logr.Discard(),
					CareInstruction: &v1alpha1.CareInstruction{
						Spec: v1alpha1.CareInstructionSpec{
							GreenhouseIssuerUrl: issuerURL,
						},
					},
				}

				config := controller.createGreenhouseIssuerConfig()

				// Verify it returns proper type
				var _ apiserverv1beta1.JWTAuthenticator = config

				// Verify issuer configuration
				Expect(config.Issuer.URL).To(Equal(issuerURL))
				Expect(config.Issuer.Audiences).To(ConsistOf("greenhouse"))
				Expect(config.Issuer.CertificateAuthority).To(Equal(""))
				Expect(config.Issuer.DiscoveryURL).To(BeNil())

				// Verify claim mappings
				Expect(config.ClaimMappings.Username.Claim).To(Equal("sub"))
				Expect(config.ClaimMappings.Username.Prefix).NotTo(BeNil())
				Expect(*config.ClaimMappings.Username.Prefix).To(Equal("greenhouse:"))

				// Verify optional fields are not set
				Expect(config.ClaimMappings.Groups.Claim).To(BeEmpty())
				Expect(config.ClaimMappings.Groups.Expression).To(BeEmpty())
				Expect(config.ClaimMappings.UID.Claim).To(BeEmpty())
				Expect(config.ClaimMappings.UID.Expression).To(BeEmpty())
				Expect(config.ClaimMappings.Extra).To(BeEmpty())
				Expect(config.ClaimValidationRules).To(BeEmpty())
				Expect(config.UserValidationRules).To(BeEmpty())
			},
			Entry("with standard HTTPS URL", "https://greenhouse.example.com"),
			Entry("with URL containing path", "https://auth.example.com/greenhouse"),
			Entry("with localhost URL", "https://localhost:8080/oidc"),
			Entry("with URL containing port and path", "https://greenhouse.example.com:8443/auth/realms/greenhouse"),
		)
	})

	Describe("upsertGreenhouseIssuer", func() {
		var controller *ShootController

		BeforeEach(func() {
			controller = &ShootController{
				Logger: logr.Discard(),
				CareInstruction: &v1alpha1.CareInstruction{
					Spec: v1alpha1.CareInstructionSpec{
						GreenhouseIssuerUrl: "https://greenhouse.example.com",
					},
				},
			}
		})

		DescribeTable("should correctly upsert greenhouse issuer",
			func(initialConfigMap *corev1.ConfigMap, expectedConfig apiserverv1beta1.AuthenticationConfiguration, expectOtherKeys map[string]string) {
				err := controller.upsertGreenhouseIssuer(initialConfigMap)
				Expect(err).NotTo(HaveOccurred())

				// Verify ConfigMap data was updated
				Expect(initialConfigMap.Data).NotTo(BeNil())
				Expect(initialConfigMap.Data).To(HaveKey("config.yaml"))

				// Verify other keys are preserved
				for key, value := range expectOtherKeys {
					Expect(initialConfigMap.Data).To(HaveKeyWithValue(key, value))
				}

				// Parse the resulting configuration
				var actualConfig apiserverv1beta1.AuthenticationConfiguration
				err = yaml.Unmarshal([]byte(initialConfigMap.Data["config.yaml"]), &actualConfig)
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
			Entry("with empty ConfigMap",
				&corev1.ConfigMap{
					Data: map[string]string{},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apiserver.k8s.io/v1beta1",
						Kind:       "AuthenticationConfiguration",
					},
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
				nil),
			Entry("with ConfigMap having no config.yaml key but other data",
				&corev1.ConfigMap{
					Data: map[string]string{
						"other-key":   "some-value",
						"another-key": "another-value",
					},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apiserver.k8s.io/v1beta1",
						Kind:       "AuthenticationConfiguration",
					},
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
				map[string]string{
					"other-key":   "some-value",
					"another-key": "another-value",
				}),
			Entry("with ConfigMap having empty config.yaml",
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": "",
					},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apiserver.k8s.io/v1beta1",
						Kind:       "AuthenticationConfiguration",
					},
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
				nil),
			Entry("with ConfigMap having one different issuer",
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
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apiserver.config.k8s.io/v1beta1",
						Kind:       "AuthenticationConfiguration",
					},
					JWT: []apiserverv1beta1.JWTAuthenticator{
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://other.example.com",
								Audiences: []string{"other"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim: "sub",
								},
							},
						},
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
				nil),
			Entry("with ConfigMap already having greenhouse issuer (should update it)",
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
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apiserver.config.k8s.io/v1beta1",
						Kind:       "AuthenticationConfiguration",
					},
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
				nil),
			Entry("with ConfigMap having multiple issuers including greenhouse (should update greenhouse)",
				&corev1.ConfigMap{
					Data: map[string]string{
						"config.yaml": `apiVersion: apiserver.config.k8s.io/v1beta1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: https://issuer1.example.com
    audiences:
    - issuer1
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
    url: https://issuer3.example.com
    audiences:
    - issuer3
  claimMappings:
    username:
      claim: sub
`,
					},
				},
				apiserverv1beta1.AuthenticationConfiguration{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apiserver.config.k8s.io/v1beta1",
						Kind:       "AuthenticationConfiguration",
					},
					JWT: []apiserverv1beta1.JWTAuthenticator{
						{
							Issuer: apiserverv1beta1.Issuer{
								URL:       "https://issuer1.example.com",
								Audiences: []string{"issuer1"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim: "sub",
								},
							},
						},
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
								URL:       "https://issuer3.example.com",
								Audiences: []string{"issuer3"},
							},
							ClaimMappings: apiserverv1beta1.ClaimMappings{
								Username: apiserverv1beta1.PrefixedClaimOrExpression{
									Claim: "sub",
								},
							},
						},
					},
				},
				nil),
		)

		It("should return error for invalid YAML", func() {
			configMap := &corev1.ConfigMap{
				Data: map[string]string{
					"config.yaml": "invalid: yaml: content: [",
				},
			}

			err := controller.upsertGreenhouseIssuer(configMap)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse existing AuthenticationConfiguration"))
		})
	})
})

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}

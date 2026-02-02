// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	shootgrafterv1alpha1 "shoot-grafter/api/v1alpha1"

	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func init() {
	RegisterFailHandler(Fail)
}

const (
	// GardenClusterName is the name of the garden cluster used in tests.
	GardenClusterName = "garden-cluster"
)

var (
	// Cfg is the rest.Config to access the cluster the tests are running against.
	Cfg *rest.Config
	// K8sClient is the client.Client to access the cluster the tests are running against.
	K8sClient client.Client
	// TestEnv is the envtest.Environment for the Greenhouse cluster.
	TestEnv *envtest.Environment
	// KubeConfig is the raw kubeconfig to access the cluster the tests are running against.
	KubeConfig []byte
	// Ctx is the context to use for the tests.
	Ctx context.Context

	cancel context.CancelFunc
	// GardenCfg is the rest.Config to access the garden cluster.
	GardenCfg *rest.Config
	// GardenK8sClient is the client.Client to access the garden cluster.
	GardenK8sClient client.Client
	// GardenKubeConfig is the raw kubeconfig to access the garden cluster.
	GardenKubeConfig []byte
	// GardenTestEnv is the envtest.Environment for the garden cluster.
	GardenTestEnv *envtest.Environment
	// TestBeforeSuite configures the default test suite.
	TestBeforeSuite = func() {
		log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
		SetDefaultEventuallyPollingInterval(1 * time.Second)
		SetDefaultEventuallyTimeout(30 * time.Second)
		Ctx, cancel = context.WithCancel(context.Background())
		fmt.Println("Starting controller cluster")
		Cfg, K8sClient, TestEnv, KubeConfig = StartControlPlane("")
		fmt.Println("Starting gardener cluster")
		GardenCfg, GardenK8sClient, GardenTestEnv, GardenKubeConfig = StartControlPlane("")

		// create a Greenhouse Cluster object and a Secret
		gardenCluster := &greenhousev1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GardenClusterName,
				Namespace: "default",
			},
			Spec: greenhousev1alpha1.ClusterSpec{
				AccessMode: greenhousev1alpha1.ClusterAccessModeDirect,
			},
		}
		Expect(K8sClient.Create(Ctx, gardenCluster)).To(Succeed(), "should create garden cluster object")
		// Set Cluster to Ready
		gardenCluster.Status.SetConditions(
			greenhousemetav1alpha1.NewCondition(
				greenhousemetav1alpha1.ReadyCondition,
				metav1.ConditionTrue,
				"ClusterReady",
				"Garden cluster is ready",
			),
		)
		Expect(K8sClient.Status().Update(Ctx, gardenCluster)).To(Succeed(), "should update garden cluster status to ready")
		gardenClusterSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GardenClusterName,
				Namespace: "default",
			},
			Data: map[string][]byte{
				greenhouseapis.GreenHouseKubeConfigKey: GardenKubeConfig,
			},
		}
		Expect(K8sClient.Create(Ctx, gardenClusterSecret)).To(Succeed(), "should create garden cluster secret")

	}

	// TestAfterSuite configures the test suite.
	TestAfterSuite = func() {
		cancel()
		By("tearing down the test environment")
		Eventually(func() error {
			return TestEnv.Stop()
		}).Should(Succeed(), "there should be no error stopping the test environment")
		Eventually(func() error {
			return GardenTestEnv.Stop()
		}).Should(Succeed(), "there should be no error stopping the garden test environment")

	}
)

// Starts a envTest control plane and returns the config, client, envtest.Environment and raw kubeconfig.
func StartControlPlane(port string) (*rest.Config, client.Client, *envtest.Environment, []byte) {
	// Configure control plane
	var testEnv = &envtest.Environment{}

	testEnv.CRDDirectoryPaths = []string{
		// shoot-grafter.cloudoperators
		filepath.Join("..", "..", "crd"),
		// crds needed in testing:
		// clusters.greenhouse.sap
		// and a representation of github.com/gardener/gardener/pkg/apis/core/v1beta1/shoot as CRD
		filepath.Join("..", "..", "internal", "test", "crd"),
	}
	testEnv.ErrorIfCRDPathMissing = true

	testEnv.WebhookInstallOptions = envtest.WebhookInstallOptions{
		Paths: []string{filepath.Join("..", "..", "webhook", "manifests.yaml")},
	}

	testEnv.ControlPlane.GetAPIServer().Port = port
	testEnv.ControlPlane.GetAPIServer().Configure().Append("enable-admission-plugins", "MutatingAdmissionWebhook", "ValidatingAdmissionWebhook")

	Expect(greenhousev1alpha1.AddToScheme(scheme.Scheme)).
		To(Succeed(), "there must be no error adding the greenhouse api v1alpha1 to the scheme")
	Expect(clientgoscheme.AddToScheme(scheme.Scheme)).
		To(Succeed(), "there must no error adding the clientgo api to the scheme")
	Expect(gardenerv1beta1.AddToScheme(scheme.Scheme)).
		To(Succeed(), "there must be no error adding the gardenerv1beta1 api to the scheme")
	Expect(shootgrafterv1alpha1.AddToScheme(scheme.Scheme)).
		To(Succeed(), "there must be no error adding the clusterplantoutv1alpha1 api to the scheme")

	// Make sure all schemes are added before starting the envtest. This will enable conversion webhooks.
	testEnv.CRDInstallOptions = envtest.CRDInstallOptions{
		Scheme: scheme.Scheme,
	}

	// Start control plane
	cfg, err := testEnv.Start()
	Expect(err).
		NotTo(HaveOccurred(), "there must be no error starting the test environment")
	Expect(cfg).
		NotTo(BeNil(), "the configuration of the test environment must not be nil")

	// Create k8s client
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).
		NotTo(HaveOccurred(), "there must be no error creating a new client")
	Expect(k8sClient).
		NotTo(BeNil(), "the kubernetes client must not be nil")

	// create raw kubeconfig
	var kubeConfig []byte
	// we add a user to the control plane to easily get a kubeconfig
	user, err := testEnv.ControlPlane.AddUser(envtest.User{
		Name:   "test-admin",
		Groups: []string{"system:masters"},
	}, nil)
	Expect(err).NotTo(HaveOccurred(), "there must be no error adding a user to the control plane")
	kubeConfig, err = user.KubeConfig()
	Expect(err).NotTo(HaveOccurred(), "there must be no error getting the kubeconfig for the user")

	// utility to export kubeconfig and use it e.g. on a breakpoint to inspect resources during testing
	if os.Getenv("TEST_EXPORT_KUBECONFIG") == "true" {
		dir := GinkgoT().TempDir()
		kubeCfgFile, err := os.CreateTemp(dir, "*-kubeconfig.yaml")
		Expect(err).NotTo(HaveOccurred())
		_, err = kubeCfgFile.Write(kubeConfig)
		Expect(err).NotTo(HaveOccurred())
		fmt.Printf("export KUBECONFIG=%s\n", kubeCfgFile.Name())
	}
	return cfg, k8sClient, testEnv, kubeConfig
}

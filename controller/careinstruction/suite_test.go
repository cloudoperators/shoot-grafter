// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction_test

import (
	"context"
	"testing"

	"shoot-grafter/controller/careinstruction"
	"shoot-grafter/internal/test"

	webhookv1alpha1 "shoot-grafter/webhook/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/kubectl/pkg/scheme"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestCareInstruction(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ClusterControllerSuite")
}

var _ = BeforeSuite(func() {
	test.TestBeforeSuite()

	host := test.TestEnv.WebhookInstallOptions.LocalServingHost
	port := test.TestEnv.WebhookInstallOptions.LocalServingPort
	// register controllers
	mgr, err := ctrl.NewManager(test.Cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    host,
			Port:    port,
			CertDir: test.TestEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
		LeaderElection: false,
	})
	Expect(err).NotTo(HaveOccurred(), "there must be no error creating the manager")
	Expect((&careinstruction.CareInstructionReconciler{
		Client:  test.K8sClient,
		Logger:  ctrl.Log.WithName("controllers").WithName("CareInstruction"),
		Manager: mgr,
		GardenMgrContextFunc: func() context.Context {
			return test.Ctx
		},
	}).SetupWithManager(mgr)).To(Succeed(), "there must be no error setting up the controller with the manager")

	careInstructionWebhook := &webhookv1alpha1.CareInstructionWebhook{}
	Expect(careInstructionWebhook.SetupWebhookWithManager(mgr)).To(Succeed(), "there must be no error setting up the webhook with the manager")

	// start the manager
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(test.Ctx)).To(Succeed(), "there must be no error starting the manager")
	}()
	test.WaitForWebhookServerReady(host, port)
})

var _ = AfterSuite(func() {
	test.TestAfterSuite()
})

// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

// ReconcileObject adds a label "reconcile" with the current timestamp as epoch string to trigger reconciliation of the object.
// Get the latest version of the object from the cluster to ensure the label is set correctly.
// It then updates the object in the cluster and retrieves it to ensure the update was successful.
func ReconcileObject(obj client.Object) {
	now := time.Now()
	if obj == nil {
		return
	}

	Eventually(func(g Gomega) error {
		g.Expect(K8sClient.Get(Ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed(), "should get resource before reconciliation")
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["reconcile"] = strconv.FormatInt(now.Unix(), 10)
		obj.SetLabels(labels)
		return K8sClient.Update(Ctx, obj)
	}).Should(Succeed(), "should update resource to force reconciliation")
}

func WaitForWebhookServerReady(host string, port int) {
	// wait for the webhook server to get ready
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", host, port)
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}, updateTimeout, pollInterval).Should(Succeed(), "there should be no error dialing the webhook server")
}

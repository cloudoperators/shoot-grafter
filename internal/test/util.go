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

func WaitForPortFree(host string, port int) {
	// wait for the port to be free (not in use)
	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
	addrPort := net.JoinHostPort(host, strconv.Itoa(port))
	Eventually(func() error {
		conn, err := dialer.Dial("tcp", addrPort)
		if err != nil {
			// Port is free (connection failed)
			return nil //nolint:nilerr // returning nil when port is free is intentional
		}
		// Port is in use, close connection and retry
		if closeErr := conn.Close(); closeErr != nil {
			return closeErr
		}
		return fmt.Errorf("port %d is still in use", port)
	}).WithTimeout(2*time.Minute).WithPolling(100*time.Millisecond).Should(Succeed(), "port should eventually be free")
}

func WaitForWebhookServerReady(host string, port int) {
	// wait for the webhook server to get ready
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := net.JoinHostPort(host, strconv.Itoa(port))
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if err != nil {
			return err
		}
		return conn.Close()
	}).WithTimeout(30*time.Second).WithPolling(1*time.Second).Should(Succeed(), "there should be no error dialing the webhook server")
}

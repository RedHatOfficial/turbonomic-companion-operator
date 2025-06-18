/*
Copyright 2025 Marek Paterczyk

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	//+kubebuilder:scaffold:imports
)

// Define utility constants for object names and testing timeouts/durations and intervals.
const (
	// Webhook configuration
	webhookPath = "/mutate-v1-obj"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	// Global test variables
	cfg            *rest.Config
	k8sClient      client.Client
	k8sClientTurbo client.Client
	testEnv        *envtest.Environment
	ctx            context.Context
	cancel         context.CancelFunc

	// Test configuration
	testConfig *TestConfig
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	// Initialize test configuration from environment
	testConfig = LoadTestConfigFromEnv()

	// Validate configuration
	Expect(testConfig.Validate()).To(Succeed())

	// Parse command line flags
	opts := zap.Options{
		Development: testConfig.Development,
		Level:       testConfig.LogLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Set up logging
	testLogger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(testLogger)

	By("bootstrapping test environment")
	By("using configuration: " + testConfig.String())

	// Configure test environment
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")},
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
		// Add additional configuration for better test isolation
		UseExistingCluster: func() *bool {
			val := false
			return &val
		}(),
		AttachControlPlaneOutput: true,
	}

	// Start the test environment
	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Create Turbonomic service account user for testing
	turboUserInfo := envtest.User{Name: turboSA, Groups: []string{"system:masters"}}
	turboUser, err := testEnv.AddUser(turboUserInfo, cfg)
	Expect(err).NotTo(HaveOccurred())
	turboCfg := turboUser.Config()

	// Create Turbonomic client
	k8sClientTurbo, err = client.New(turboCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClientTurbo).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme

	// Set up webhook server
	webhookInstallOptions := &testEnv.WebhookInstallOptions
	webhookServer := webhook.NewServer(webhook.Options{
		Host:    webhookInstallOptions.LocalServingHost,
		Port:    webhookInstallOptions.LocalServingPort,
		CertDir: webhookInstallOptions.LocalServingCertDir,
	})

	// Create and configure manager
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme.Scheme,
		WebhookServer:  webhookServer,
		LeaderElection: false,
	})
	Expect(err).ToNot(HaveOccurred())

	// Set up context with cancellation
	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	// Register webhook handler
	hookServer := k8sManager.GetWebhookServer()
	decoder := admission.NewDecoder(testEnv.Scheme)
	hookServer.Register(webhookPath, &webhook.Admission{
		Handler: &WorkloadResourcesMutator{
			Client:                       k8sManager.GetClient(),
			Log:                          ctrl.Log.WithName("mutatingwebhook").WithName("WorkloadResourcesMutator"),
			Decoder:                      &decoder,
			IgnoreArgoCDManagedResources: testConfig.IgnoreArgoCDResources,
		}})

	// Start manager in background
	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	// Get client from manager
	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	// Wait for webhook server to be ready
	waitForWebhookServer(webhookInstallOptions)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	// Cancel context to stop manager
	if cancel != nil {
		cancel()
	}

	// Wait for manager to stop
	Eventually(func() error {
		return testEnv.Stop()
	}, testConfig.CleanupTimeout, time.Second).ShouldNot(HaveOccurred())
})

// waitForWebhookServer waits for the webhook server to be ready
func waitForWebhookServer(webhookInstallOptions *envtest.WebhookInstallOptions) {
	By("waiting for webhook server to be ready")

	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)

	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return err
		}
		if err := conn.Close(); err != nil {
			return err
		}
		return nil
	}, testConfig.WebhookReadyTimeout, time.Second).Should(Succeed())
}

// Test utilities

// CreateTestNamespace creates a namespace for testing
func CreateTestNamespace(name string) *corev1.Namespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"turbo.ibm.com/override": "true",
			},
		},
	}
	Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
	return ns
}

// CleanupTestNamespace cleans up a test namespace
func CleanupTestNamespace(name string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())

	// Wait for namespace to be deleted
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)
	}, testConfig.TestTimeout, time.Second).ShouldNot(Succeed())
}

// WaitForObject waits for an object to exist
func WaitForObject(obj client.Object, timeout time.Duration) {
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		}, obj)
	}, timeout, time.Second).Should(Succeed())
}

// WaitForObjectDeletion waits for an object to be deleted
func WaitForObjectDeletion(obj client.Object, timeout time.Duration) {
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		}, obj)
	}, timeout, time.Second).ShouldNot(Succeed())
}

// GetTestConfig returns the current test configuration
func GetTestConfig() *TestConfig {
	return testConfig
}

// SetTestConfig updates the test configuration
func SetTestConfig(config *TestConfig) {
	testConfig = config
}

// GetTestContext returns the test context
func GetTestContext() context.Context {
	return ctx
}

// GetTestClient returns the main test client
func GetTestClient() client.Client {
	return k8sClient
}

// GetTurboClient returns the Turbonomic test client
func GetTurboClient() client.Client {
	return k8sClientTurbo
}

/*
Copyright 2025 isola.

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
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const (
	testNamespace = "test-sandbox"
	testTimeout   = time.Second * 10
	testInterval  = time.Millisecond * 250
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client // Direct client for test reads/writes (no cache delay)
	k8sCache  client.Client // Cached client for reconciler field index queries

	// testRecorder captures events for test assertions
	testRecorder *record.FakeRecorder
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error

	// Register sandbox types
	err = sandboxv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Register RuntimeClass for snapshotting tests
	err = nodev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Register NetworkPolicy for network policy tests
	err = networkingv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Create direct client for test reads/writes (no cache delay issues)
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Create manager with cache for field indexing support (used by reconciler)
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server in tests
		},
	})
	Expect(err).NotTo(HaveOccurred())

	// Register field index for sandbox templateRef lookups
	err = mgr.GetFieldIndexer().IndexField(
		ctx,
		&sandboxv1alpha1.Sandbox{},
		sandboxTemplateRefField,
		extractTemplateRefName,
	)
	Expect(err).NotTo(HaveOccurred())

	// Register field index for sandbox networkTemplateRef lookups
	err = mgr.GetFieldIndexer().IndexField(
		ctx,
		&sandboxv1alpha1.Sandbox{},
		sandboxNetworkTemplateRefField,
		extractNetworkTemplateRefName,
	)
	Expect(err).NotTo(HaveOccurred())

	// Start manager cache in background (needed for field indexing)
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// Cache client for reconciler's field index queries
	k8sCache = mgr.GetClient()
	Expect(k8sCache).NotTo(BeNil())

	Expect(mgr.GetCache().WaitForCacheSync(ctx)).To(BeTrue())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	// Create and reconcile the default NetworkTemplate that all sandboxes use
	defaultNetworkTemplate := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxv1alpha1.DefaultNetworkTemplate,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{},
	}
	Expect(k8sClient.Create(ctx, defaultNetworkTemplate)).To(Succeed())

	// Reconcile to make it ready
	ntReconciler := &NetworkTemplateReconciler{
		Client: k8sClient,
		Scheme: scheme.Scheme,
	}
	_, err = ntReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      sandboxv1alpha1.DefaultNetworkTemplate,
			Namespace: testNamespace,
		},
	})
	Expect(err).NotTo(HaveOccurred())

	// Create fake event recorder for test assertions
	testRecorder = record.NewFakeRecorder(100)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

// newTestReconciler creates a SandboxReconciler configured for testing.
// Uses direct k8sClient for immediate consistency in tests.
// ControllerNamespace is not set, so it defaults to sandbox's namespace (single-namespace deployment).
func newTestReconciler(clock Clock) *SandboxReconciler {
	rec := record.NewFakeRecorder(100)
	return &SandboxReconciler{
		Client:     k8sClient,
		Scheme:     scheme.Scheme,
		Recorder:   rec,
		AgentImage: "isola-agent:test",
		Clock:      clock,
		// ControllerNamespace not set - defaults to sandbox namespace
		// ControllerLabels not set - defaults to {"app.kubernetes.io/name": "isola-controller"}
	}
}

// newTestReconcilerWithRecorder creates a SandboxReconciler with a specific recorder for event testing.
// Uses direct k8sClient for immediate consistency in tests.
func newTestReconcilerWithRecorder(clock Clock, recorder record.EventRecorder) *SandboxReconciler {
	return &SandboxReconciler{
		Client:     k8sClient,
		Scheme:     scheme.Scheme,
		Recorder:   recorder,
		AgentImage: "isola-agent:test",
		Clock:      clock,
	}
}

// newTestReconcilerWithCache creates a SandboxReconciler using the cached client.
// Required for testing field index queries like findSandboxesForTemplate.
func newTestReconcilerWithCache(clock Clock) *SandboxReconciler {
	rec := record.NewFakeRecorder(100)
	return &SandboxReconciler{
		Client:     k8sCache,
		Scheme:     scheme.Scheme,
		Recorder:   rec,
		AgentImage: "isola-agent:test",
		Clock:      clock,
	}
}

// newTestNetworkTemplateReconciler creates a NetworkTemplateReconciler for testing.
// Uses k8sCache (manager's client) because field indexing is required for efficient sandbox lookups.
func newTestNetworkTemplateReconciler() *NetworkTemplateReconciler {
	return &NetworkTemplateReconciler{
		Client: k8sCache,
		Scheme: scheme.Scheme,
		// IsolaGatewayNamespace defaults to NetworkTemplate's namespace
		// IsolaGatewayLabels defaults to {"app.kubernetes.io/name": "isola-controller"}
	}
}

// reconcileNetworkTemplate reconciles a NetworkTemplate, creating the NetworkPolicy and setting Ready condition.
// This simulates what happens in production when NetworkTemplateReconciler runs.
// Waits for cache to sync the NetworkTemplate before reconciling.
func reconcileNetworkTemplate(ctx context.Context, templateName string) {
	rec := newTestNetworkTemplateReconciler()

	// Wait for cache to sync the NetworkTemplate (created with direct client)
	Eventually(func() error {
		nt := &sandboxv1alpha1.NetworkTemplate{}
		return k8sCache.Get(ctx, types.NamespacedName{Name: templateName, Namespace: testNamespace}, nt)
	}, "5s", "100ms").Should(Succeed())

	_, err := rec.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      templateName,
			Namespace: testNamespace,
		},
	})
	Expect(err).NotTo(HaveOccurred())
}

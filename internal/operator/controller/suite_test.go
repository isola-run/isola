// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const (
	testNamespace = "test-sandbox"
	testTimeout   = time.Second * 10
	testInterval  = time.Millisecond * 10
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client // Direct client for test reads/writes (no cache delay)
	k8sCache  client.Client // Cached client for reconciler field index queries

	// testRecorder captures events for test assertions
	testRecorder *events.FakeRecorder
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
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "charts", "isola", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Create direct client for test reads/writes (no cache delay issues)
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	RegisterRunningCollector(k8sClient)

	// Create manager with cache for field indexing support (used by reconciler)
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server in tests
		},
	})
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

	// Create PriorityClass for sandbox pods
	priorityClass := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "isola-sandbox",
		},
		Value:            1000000000,
		GlobalDefault:    false,
		PreemptionPolicy: func() *corev1.PreemptionPolicy { p := corev1.PreemptLowerPriority; return &p }(),
		Description:      "High priority for isola sandbox pods",
	}
	Expect(k8sClient.Create(ctx, priorityClass)).To(Succeed())

	// Create fake event recorder for test assertions
	testRecorder = events.NewFakeRecorder(100)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// newTestReconciler creates a SandboxReconciler configured for testing.
// Uses direct k8sClient for immediate consistency in tests.
// ControllerNamespace is not set, so it defaults to sandbox's namespace (single-namespace deployment).
func newTestReconciler(clock Clock) *SandboxReconciler {
	rec := events.NewFakeRecorder(100)
	return &SandboxReconciler{
		Client:              k8sClient,
		Scheme:              scheme.Scheme,
		Recorder:            rec,
		SandboxSidecarImage: "sandbox-sidecar:test",
		Clock:               clock,
		// ControllerNamespace not set - defaults to sandbox namespace
		// ControllerLabels not set - defaults to {"app.kubernetes.io/name": "isola-controller"}
	}
}

// newTestReconcilerWithRecorder creates a SandboxReconciler with a specific recorder for event testing.
// Uses direct k8sClient for immediate consistency in tests.
func newTestReconcilerWithRecorder(clock Clock, recorder events.EventRecorder) *SandboxReconciler {
	return &SandboxReconciler{
		Client:              k8sClient,
		Scheme:              scheme.Scheme,
		Recorder:            recorder,
		SandboxSidecarImage: "sandbox-sidecar:test",
		Clock:               clock,
	}
}

// newTestReconcilerWithRuntimeClass creates a SandboxReconciler with RuntimeClassName set.
// Used for testing gvisor-specific features like overlay2 annotation.
func newTestReconcilerWithRuntimeClass(clock Clock, runtimeClassName string) *SandboxReconciler {
	rec := events.NewFakeRecorder(100)
	return &SandboxReconciler{
		Client:              k8sClient,
		Scheme:              scheme.Scheme,
		Recorder:            rec,
		SandboxSidecarImage: "sandbox-sidecar:test",
		Clock:               clock,
		RuntimeClassName:    runtimeClassName,
	}
}

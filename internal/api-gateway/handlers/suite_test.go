package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

const testNamespace = "test-sandbox"

var (
	ctx        context.Context
	cancel     context.CancelFunc
	testEnv    *envtest.Environment
	k8sClient  client.Client
	testServer *httptest.Server
	testClient *http.Client
)

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Handlers Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	// Register sandbox types
	err := sandboxv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "charts", "isola-operator", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Create manager with cache
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server in tests
		},
	})
	Expect(err).NotTo(HaveOccurred())

	// Start manager in background
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	k8sClient = mgr.GetClient()
	Expect(k8sClient).NotTo(BeNil())

	Expect(mgr.GetCache().WaitForCacheSync(ctx)).To(BeTrue())

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	// Set up HTTP test server with gin
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))

	r := gin.New()
	r.Use(gin.Recovery())

	handler := NewHandler(logger, k8sClient)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", handler.GetHealth)
		v1.GET("/ready", handler.GetReady)
	}

	testServer = httptest.NewServer(r)
	DeferCleanup(testServer.Close)

	testClient = testServer.Client()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// doGet performs a GET request and returns the response.
// Caller is responsible for closing the response body.
func doGet(path string) *http.Response {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, testServer.URL+path, nil)
	Expect(err).NotTo(HaveOccurred())

	resp, err := testClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

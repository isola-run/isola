package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	testServer *httptest.Server
	testClient *http.Client
)

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sidecar Handlers Suite")
}

var _ = BeforeSuite(func() {
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))

	r := gin.New()
	r.Use(gin.Recovery())

	handler := NewHandler(logger)
	handler.RegisterRoutes(r)

	testServer = httptest.NewServer(r)
	DeferCleanup(testServer.Close)

	testClient = testServer.Client()
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

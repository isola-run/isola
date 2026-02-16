package health

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var testAPI humatest.TestAPI

func TestHealth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sidecar Health Suite")
}

var _ = BeforeSuite(func() {
	_, testAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))

	h := New()
	Register(testAPI, h)
})

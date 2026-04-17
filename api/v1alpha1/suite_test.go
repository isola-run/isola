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

// Package v1alpha1_test holds CRD validation tests that exercise the
// generated CRDs against a real kube-apiserver via envtest. The pattern
// is the one used by sigs.k8s.io/karpenter (pkg/apis/v1/suite_test.go,
// pkg/apis/v1/ec2nodeclass_validation_cel_test.go): Create/Update against
// installed CRDs, assert Succeed/NotSucceed, no error-shape assertions.
//
// Why no error-shape assertions: CEL error wording and cause layout drift
// across K8s versions and are not a contract. See the karpenter CEL suite
// for the same choice.
package v1alpha1_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client

	testNamespace string
	nsCounter     atomic.Int32
)

func TestAPIValidation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "api/v1alpha1 CRD Validation Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(sandboxv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	cancel()
	Expect(testEnv.Stop()).To(Succeed())
})

// Each It runs in its own namespace so created objects don't collide.
var _ = BeforeEach(func() {
	n := nsCounter.Add(1)
	testNamespace = fmt.Sprintf("cel-test-%d", n)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
})

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

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

var (
	sandboxCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "sandbox",
		Name:      "created_total",
		Help:      "Total number of sandbox pods created by the operator.",
	})

	sandboxTimedOutTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "sandbox",
		Name:      "timed_out_total",
		Help:      "Total number of sandboxes that hit their activeDeadlineSeconds timeout.",
	})

	sandboxReadyDurationSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "isola",
		Subsystem: "sandbox",
		Name:      "ready_duration_seconds",
		Help:      "Time from sandbox creation to Ready condition becoming True.",
		// ExponentialBuckets(0.5, 2, 9) → 0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 9),
	})

	rootfsSnapshotCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "rootfssnapshot",
		Name:      "created_total",
		Help:      "Total number of rootfs snapshot jobs created.",
	})

	rootfsSnapshotCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "rootfssnapshot",
		Name:      "completed_total",
		Help:      "Total number of rootfs snapshots that reached a terminal state.",
	}, []string{"result"})

	sandboxRunningDesc = prometheus.NewDesc(
		"isola_sandbox_running",
		"Current number of sandboxes with Ready condition True.",
		nil, nil,
	)
)

// sandboxRunningCollector computes the running sandbox count at scrape time
// by listing from the controller-runtime cache. Restart- and multi-replica-safe.
type sandboxRunningCollector struct {
	client client.Reader
}

func (c *sandboxRunningCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- sandboxRunningDesc
}

func (c *sandboxRunningCollector) Collect(ch chan<- prometheus.Metric) {
	var sandboxes sandboxv1alpha1.SandboxList
	if err := c.client.List(context.Background(), &sandboxes); err != nil {
		ch <- prometheus.NewInvalidMetric(sandboxRunningDesc, err)
		return
	}

	var count float64
	for i := range sandboxes.Items {
		cond := meta.FindStatusCondition(sandboxes.Items[i].Status.Conditions, SandboxReadyCondition)
		if cond != nil && cond.Status == metav1.ConditionTrue {
			count++
		}
	}
	ch <- prometheus.MustNewConstMetric(sandboxRunningDesc, prometheus.GaugeValue, count)
}

// RegisterMetrics registers all custom metrics with the controller-runtime registry.
// Must be called after the manager is created (the cache is only read at scrape time).
func RegisterMetrics(reader client.Reader) {
	metrics.Registry.MustRegister(
		sandboxCreatedTotal,
		sandboxTimedOutTotal,
		sandboxReadyDurationSeconds,
		rootfsSnapshotCreatedTotal,
		rootfsSnapshotCompletedTotal,
		&sandboxRunningCollector{client: reader},
	)
}

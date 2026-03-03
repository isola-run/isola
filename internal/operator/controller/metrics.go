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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var factory = promauto.With(metrics.Registry)

var (
	sandboxCreatedTotal = factory.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "sandbox",
		Name:      "created_total",
		Help:      "Total number of sandbox pods created by the operator.",
	})

	sandboxTimedOutTotal = factory.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "sandbox",
		Name:      "timed_out_total",
		Help:      "Total number of sandboxes that hit their activeDeadlineSeconds timeout.",
	})

	sandboxReadyDurationSeconds = factory.NewHistogram(prometheus.HistogramOpts{
		Namespace: "isola",
		Subsystem: "sandbox",
		Name:      "ready_duration_seconds",
		Help:      "Time from sandbox creation to Ready condition becoming True.",
		// ExponentialBuckets(0.5, 2, 9) → 0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 9),
	})

	rootfsSnapshotCreatedTotal = factory.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "rootfssnapshot",
		Name:      "created_total",
		Help:      "Total number of rootfs snapshot jobs created.",
	})

	rootfsSnapshotCompletedTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "rootfssnapshot",
		Name:      "completed_total",
		Help:      "Total number of rootfs snapshots that reached a terminal state.",
	}, []string{"result"})
)

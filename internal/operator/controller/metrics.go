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
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	sandboxCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "isola_sandbox_created_total",
		Help: "Total number of sandbox pods created by the operator.",
	})

	sandboxDeletedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "isola_sandbox_deleted_total",
		Help: "Total number of sandboxes deleted (finalizer completed).",
	})

	sandboxTimedOutTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "isola_sandbox_timed_out_total",
		Help: "Total number of sandboxes that hit their activeDeadlineSeconds timeout.",
	})

	sandboxReadyDurationSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "isola_sandbox_ready_duration_seconds",
		Help:    "Time from sandbox creation to Ready condition becoming True.",
		Buckets: []float64{1, 2, 5, 10, 15, 30, 60, 120},
	})

	rootfsSnapshotCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "isola_rootfssnapshot_created_total",
		Help: "Total number of rootfs snapshot jobs created.",
	})

	rootfsSnapshotCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "isola_rootfssnapshot_completed_total",
		Help: "Total number of rootfs snapshots that reached a terminal state.",
	}, []string{"result"})
)

func init() {
	metrics.Registry.MustRegister(
		sandboxCreatedTotal,
		sandboxDeletedTotal,
		sandboxTimedOutTotal,
		sandboxReadyDurationSeconds,
		rootfsSnapshotCreatedTotal,
		rootfsSnapshotCompletedTotal,
	)
}

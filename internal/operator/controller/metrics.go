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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconcileTotal counts total reconciliations by controller and result.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "isola",
			Subsystem: "operator",
			Name:      "reconcile_total",
			Help:      "Total reconciliations by controller and result",
		},
		[]string{"controller", "result"},
	)

	// ReconcileDuration tracks reconciliation latency by controller.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "isola",
			Subsystem: "operator",
			Name:      "reconcile_duration_seconds",
			Help:      "Reconciliation latency in seconds",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		},
		[]string{"controller"},
	)

	// PodCreationDuration tracks time from sandbox creation to pod ready.
	PodCreationDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "isola",
		Subsystem: "operator",
		Name:      "pod_creation_duration_seconds",
		Help:      "Time from sandbox creation to pod ready in seconds",
		Buckets:   []float64{.5, 1, 2, 5, 10, 30, 60, 120},
	})

	// SandboxesTotal counts sandboxes by state.
	SandboxesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "isola",
			Subsystem: "operator",
			Name:      "sandboxes_total",
			Help:      "Current number of sandboxes by state",
		},
		[]string{"state"},
	)

	// NetworkPolicyCreationsTotal counts network policy creations.
	NetworkPolicyCreationsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "operator",
		Name:      "network_policy_creations_total",
		Help:      "Total custom network policies created",
	})

	// SnapshotJobsTotal counts snapshot jobs by status.
	SnapshotJobsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "isola",
			Subsystem: "operator",
			Name:      "snapshot_jobs_total",
			Help:      "Total snapshot jobs by status",
		},
		[]string{"status"},
	)

	// SnapshotDuration tracks snapshot operation duration.
	SnapshotDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "isola",
		Subsystem: "operator",
		Name:      "snapshot_duration_seconds",
		Help:      "Snapshot operation duration in seconds",
		Buckets:   []float64{1, 5, 10, 30, 60, 120, 300},
	})

	// TimeoutEnforcementsTotal counts sandbox timeout enforcements.
	TimeoutEnforcementsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "operator",
		Name:      "timeout_enforcements_total",
		Help:      "Total sandbox timeout enforcements",
	})

	// K8sAPICallDuration tracks K8s API call latency from operator.
	K8sAPICallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "isola",
			Subsystem: "operator",
			Name:      "k8s_api_call_duration_seconds",
			Help:      "K8s API call latency in seconds",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"operation", "resource"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ReconcileTotal,
		ReconcileDuration,
		PodCreationDuration,
		SandboxesTotal,
		NetworkPolicyCreationsTotal,
		SnapshotJobsTotal,
		SnapshotDuration,
		TimeoutEnforcementsTotal,
		K8sAPICallDuration,
	)
}

// RecordReconcile records metrics for a reconciliation.
func RecordReconcile(controller, result string, duration time.Duration) {
	ReconcileTotal.WithLabelValues(controller, result).Inc()
	ReconcileDuration.WithLabelValues(controller).Observe(duration.Seconds())
}

// RecordPodCreation records the duration from sandbox creation to pod ready.
func RecordPodCreation(duration time.Duration) {
	PodCreationDuration.Observe(duration.Seconds())
}

// RecordSnapshot records metrics for a snapshot operation.
func RecordSnapshot(status string, duration time.Duration) {
	SnapshotJobsTotal.WithLabelValues(status).Inc()
	if duration > 0 {
		SnapshotDuration.Observe(duration.Seconds())
	}
}

// RecordTimeoutEnforcement records a sandbox timeout enforcement.
func RecordTimeoutEnforcement() {
	TimeoutEnforcementsTotal.Inc()
}

// RecordNetworkPolicyCreation records a network policy creation.
func RecordNetworkPolicyCreation() {
	NetworkPolicyCreationsTotal.Inc()
}

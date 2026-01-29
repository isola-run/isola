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

// Condition types for Sandbox status.
const (
	// SandboxReadyCondition is the aggregate ready condition.
	SandboxReadyCondition = "Ready"

	SandboxTemplateReadyCondition  = "TemplateReady"
	SandboxPodReadyCondition       = "PodReady"
	SandboxNetworkReadyCondition   = "NetworkConfigured"
	SandboxRootfsSnapshotCondition = "RootfsSnapshot"
)

// Condition reasons for Sandbox status.
const (
	CondReasonTemplateNotFound = "TemplateNotFound"
	CondReasonTemplateResolved = "TemplateResolved"

	CondReasonPodPending        = "PodPending"
	CondReasonPodRunning        = "PodRunning"
	CondReasonPodFailed         = "PodFailed"
	CondReasonPodSucceeded      = "PodSucceeded"
	CondReasonPodCreating       = "PodCreating"
	CondReasonPodCreationFailed = "PodCreationFailed"
	CondReasonSandboxTimedOut   = "TimedOut"
	CondReasonDeleting          = "Deleting"
	CondReasonReconciling       = "Reconciling"

	// Snapshot-related reasons
	CondReasonSnapshottingInProgress = "SnapshottingInProgress"
	CondReasonSnapshotComplete       = "SnapshotComplete"
	CondReasonSnapshotFailed         = "SnapshotFailed"
	CondReasonSnapshotTimeout        = "SnapshotTimeout"
	CondReasonInvalidRuntime         = "InvalidRuntime"

	// NetworkPolicy-related reasons
	CondReasonNetworkPolicyApplied = "NetworkPolicyApplied"
	CondReasonNetworkPolicyFailed  = "NetworkPolicyFailed"
)

const (
	defaultActiveDeadlineSeconds int64 = 300

	SandboxFinalizer = "sandbox.isola.run/cleanup"

	sandboxSidecarContainerName = "sandbox-sidecar"

	// Field index for efficient lookup of sandboxes by template references
	sandboxTemplateRefField = ".spec.templateRef.name"

	// Network labels for pod selection by Helm-installed NetworkPolicies
	LabelAllowInternet   = "isola.run/allow-internet"
	LabelAllowClusterDNS = "isola.run/allow-cluster-dns"
)

// CleanupTrigger indicates what triggered sandbox cleanup.
type CleanupTrigger string

const (
	CleanupTriggerTimeout  CleanupTrigger = "Timeout"
	CleanupTriggerDeletion CleanupTrigger = "Deletion"
)

package v1alpha1

// Sandbox condition type strings used by the controller and gateway.
const (
	ConditionReady             = "Ready"
	ConditionPodReady          = "PodReady"
	ConditionNetworkConfigured = "NetworkConfigured"
	ConditionRootfsSnapshot    = "RootfsSnapshot"
)

// Condition reasons for the Ready and PodReady conditions.
const (
	ReasonPodPending        = "PodPending"
	ReasonPodRunning        = "PodRunning"
	ReasonPodFailed         = "PodFailed"
	ReasonPodSucceeded      = "PodSucceeded"
	ReasonPodCreating       = "PodCreating"
	ReasonPodCreationFailed = "PodCreationFailed"
	ReasonDeleting          = "Deleting"
	ReasonReconciling       = "Reconciling"
	ReasonInvalidRuntime    = "InvalidRuntime"
)

// Condition reasons for the NetworkConfigured condition.
const (
	ReasonNetworkPolicyApplied = "NetworkPolicyApplied"
	ReasonNetworkPolicyFailed  = "NetworkPolicyFailed"
)

// Condition reasons for the RootfsSnapshot condition on a Sandbox.
// These track snapshot progress from the Sandbox's perspective.
// Not to be confused with RootfsSnapshot CR condition reasons
// (ReasonRootfsSnapshotSucceeded, etc.) which use simpler reason strings.
const (
	ReasonSnapshottingInProgress = "RootfsSnapshottingInProgress"
	ReasonSnapshotComplete       = "RootfsSnapshotComplete"
	ReasonSnapshotFailed         = "RootfsSnapshotFailed"
	ReasonSnapshotTimeout        = "RootfsSnapshotTimeout"
)

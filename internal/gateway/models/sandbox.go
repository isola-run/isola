// Package models provides data structures for the isola-gw API.
package models

import "time"

type SandboxState string

const (
	SandboxStatePending     SandboxState = "pending"
	SandboxStateStarting    SandboxState = "starting"
	SandboxStateRunning     SandboxState = "running"
	SandboxStateTerminating SandboxState = "terminating"
	SandboxStateStopped     SandboxState = "stopped"
	SandboxStateError       SandboxState = "error"
	SandboxStateUnknown     SandboxState = "unknown"
)

type AttachedVolume struct {
	VolumeID  string `json:"volumeId"`
	MountPath string `json:"mountPath"`
}

type Sandbox struct {
	// Name is the unique identifier for the sandbox.
	// It is a DNS-safe string used as the Kubernetes resource name.
	Name         string            `json:"name"`
	State        SandboxState      `json:"state"`
	DesiredState *SandboxState     `json:"desiredState,omitempty"`
	Env          map[string]string `json:"env"`
	Labels       map[string]string `json:"labels"`
	ErrorReason  *string           `json:"errorReason,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

type SandboxList struct {
	Items  []Sandbox `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}

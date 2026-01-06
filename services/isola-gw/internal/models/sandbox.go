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
	ID           string            `json:"id"`
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


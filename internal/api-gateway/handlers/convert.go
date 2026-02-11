package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

const containerName = "sandbox"

func sandboxToResponse(sb *sandboxv1alpha1.Sandbox) SandboxResponse {
	resp := SandboxResponse{
		ID:                sb.Name,
		Status:            conditionsToStatus(sb.Status.Conditions),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}

	// todo benl: change to support multiple containers
	if len(sb.Spec.PodTemplate.Spec.Containers) > 0 {
		c := sb.Spec.PodTemplate.Spec.Containers[0]
		resp.PodTemplate.Container.Image = c.Image
		resp.PodTemplate.Container.Command = c.Command
		resp.PodTemplate.Container.Resources = containerResourcesToSpec(c.Resources)
	}

	resp.ActiveDeadlineSeconds = sb.Spec.ActiveDeadlineSeconds
	resp.Network = crdNetworkToREST(sb.Spec.Network)

	return resp
}

func sandboxToSummary(sb *sandboxv1alpha1.Sandbox) SandboxSummary {
	return SandboxSummary{
		ID:                sb.Name,
		Status:            conditionsToStatus(sb.Status.Conditions),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}
}

func conditionsToStatus(conditions []metav1.Condition) string {
	ready := meta.FindStatusCondition(conditions, "Ready")
	if ready == nil {
		return "unknown"
	}

	if ready.Status == metav1.ConditionTrue {
		return "running"
	}

	// TODO: remove snapshot-related reasons from Sandbox CRD — they should be
	// encapsulated in the RootfsSnapshot CRD only.
	// TODO benl: make them as constants and share them with routes.go openapi enum generation
	switch ready.Reason {
	case "PodPending", "PodCreating", "Reconciling", "NetworkPolicyApplied":
		return "creating"
	case "PodRunning", "RootfsSnapshottingInProgress":
		return "running"
	case "Deleting":
		return "shuttingDown"
	case "PodFailed", "PodCreationFailed", "InvalidRuntime",
		"NetworkPolicyFailed", "RootfsSnapshotFailed", "RootfsSnapshotTimeout":
		return "failed"
	case "PodSucceeded", "RootfsSnapshotComplete":
		return "stopped"
	default:
		return "unknown"
	}
}

func mapToEnvVars(m map[string]string) []corev1.EnvVar {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	envVars := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: m[k]})
	}
	return envVars
}

func containerResourcesToSpec(r corev1.ResourceRequirements) *ResourcesSpec {
	spec := &ResourcesSpec{
		Limits:   resourceListToREST(r.Limits),
		Requests: resourceListToREST(r.Requests),
	}
	if spec.Limits == nil && spec.Requests == nil {
		return nil
	}
	return spec
}

func resourceListToREST(rl corev1.ResourceList) *ResourceList {
	if len(rl) == 0 {
		return nil
	}

	out := &ResourceList{}
	hasContent := false

	if q, ok := rl[corev1.ResourceCPU]; ok {
		out.CPU = q.String()
		hasContent = true
	}
	if q, ok := rl[corev1.ResourceMemory]; ok {
		out.Memory = q.String()
		hasContent = true
	}
	if q, ok := rl[corev1.ResourceEphemeralStorage]; ok {
		out.EphemeralStorage = q.String()
		hasContent = true
	}

	if !hasContent {
		return nil
	}
	return out
}

func crdNetworkToREST(n *sandboxv1alpha1.NetworkSpec) *NetworkSpec {
	if n == nil {
		return nil
	}

	return &NetworkSpec{
		AllowAllInternet:   n.AllowAllInternet,
		AllowClusterDNS:    n.AllowClusterDNS,
		AllowedEgressCIDRs: n.AllowedEgressCIDRs,
		Nameservers:        n.Nameservers,
	}
}

func requestToSandboxCR(req CreateSandboxRequest, name, namespace string) (*sandboxv1alpha1.Sandbox, error) {
	c := req.PodTemplate.Container
	container := corev1.Container{
		Name:    containerName,
		Image:   c.Image,
		Command: c.Command,
	}

	if c.Resources != nil {
		limits, err := restResourceListToK8s(c.Resources.Limits)
		if err != nil {
			return nil, fmt.Errorf("invalid resource limits: %w", err)
		}
		requests, err := restResourceListToK8s(c.Resources.Requests)
		if err != nil {
			return nil, fmt.Errorf("invalid resource requests: %w", err)
		}
		container.Resources = corev1.ResourceRequirements{
			Limits:   limits,
			Requests: requests,
		}
	}

	container.Env = mapToEnvVars(c.Env)

	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
				},
			},
			ActiveDeadlineSeconds: req.ActiveDeadlineSeconds,
		},
	}

	sb.Spec.Network = restNetworkToCRD(req.Network)

	return sb, nil
}

// restResourceListToK8s parses a REST ResourceList into a K8s ResourceList.
// Returns nil if src is nil. Returns error on invalid quantities.
func restResourceListToK8s(src *ResourceList) (corev1.ResourceList, error) {
	if src == nil {
		return nil, nil
	}

	rl := corev1.ResourceList{}

	if src.CPU != "" {
		q, err := resource.ParseQuantity(src.CPU)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu %q: %w", src.CPU, err)
		}
		rl[corev1.ResourceCPU] = q
	}
	if src.Memory != "" {
		q, err := resource.ParseQuantity(src.Memory)
		if err != nil {
			return nil, fmt.Errorf("invalid memory %q: %w", src.Memory, err)
		}
		rl[corev1.ResourceMemory] = q
	}
	if src.EphemeralStorage != "" {
		q, err := resource.ParseQuantity(src.EphemeralStorage)
		if err != nil {
			return nil, fmt.Errorf("invalid ephemeralStorage %q: %w", src.EphemeralStorage, err)
		}
		rl[corev1.ResourceEphemeralStorage] = q
	}

	if len(rl) == 0 {
		return nil, nil
	}
	return rl, nil
}

func restNetworkToCRD(n *NetworkSpec) *sandboxv1alpha1.NetworkSpec {
	if n == nil {
		return nil
	}

	return &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet:   n.AllowAllInternet,
		AllowClusterDNS:    n.AllowClusterDNS,
		AllowedEgressCIDRs: n.AllowedEgressCIDRs,
		Nameservers:        n.Nameservers,
	}
}

func k8sErrorToHuma(err error, fallbackMsg string) error {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) && statusErr.ErrStatus.Code > 0 {
		return huma.NewError(int(statusErr.ErrStatus.Code), statusErr.ErrStatus.Message)
	}
	return huma.Error500InternalServerError(fallbackMsg)
}

func getReadySandbox(ctx context.Context, k8sClient client.Client, namespace, id string, logger *slog.Logger) (*sandboxv1alpha1.Sandbox, error) {
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Warn("sandbox not found", "id", id)
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", id))
		}
		logger.Error("failed to get sandbox", "error", err, "id", id)
		return nil, k8sErrorToHuma(err, "failed to get sandbox")
	}

	if conditionsToStatus(sb.Status.Conditions) != "running" {
		logger.Warn("sandbox is not ready", "id", id, "status", conditionsToStatus(sb.Status.Conditions))
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	if sb.Status.PodIP == "" {
		logger.Warn("sandbox is not ready", "id", id)
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	return sb, nil
}

func handleSidecarError(resp *http.Response, sandboxID string, logger *slog.Logger) error {
	if resp.StatusCode >= 500 {
		logger.Error("sidecar returned server error", "id", sandboxID, "status", resp.StatusCode)
		return huma.Error502BadGateway("sidecar internal error")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		logger.Warn("failed to read sidecar error body", "error", err, "id", sandboxID, "status", resp.StatusCode)
	}

	detail := http.StatusText(resp.StatusCode)
	var sidecarErr huma.ErrorModel
	if json.Unmarshal(body, &sidecarErr) == nil && sidecarErr.Detail != "" {
		detail = sidecarErr.Detail
	}

	logger.Debug("forwarding sidecar client error", "id", sandboxID, "status", resp.StatusCode, "detail", detail)
	return huma.NewError(resp.StatusCode, detail)
}

package apigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

// HTTPDoer abstracts HTTP request execution (satisfied by *http.Client), for faking it in tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// BodyStream provides streaming access to request body via Huma's Resolver pattern.
// See https://github.com/danielgtaylor/huma/issues/749
type BodyStream struct {
	Stream io.Reader
}

func (b *BodyStream) Resolve(ctx huma.Context) []error {
	b.Stream = ctx.BodyReader()
	return nil
}

func ConditionsToStatus(conditions []metav1.Condition) string {
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

func K8sErrorToHuma(err error, fallbackMsg string) error {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) && statusErr.ErrStatus.Code > 0 {
		return huma.NewError(int(statusErr.ErrStatus.Code), statusErr.ErrStatus.Message)
	}
	return huma.Error500InternalServerError(fallbackMsg)
}

func GetReadySandbox(ctx context.Context, k8sClient client.Client, namespace, id string, logger *slog.Logger) (*sandboxv1alpha1.Sandbox, error) {
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Warn("sandbox not found", "id", id)
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", id))
		}
		logger.Error("failed to get sandbox", "error", err, "id", id)
		return nil, K8sErrorToHuma(err, "failed to get sandbox")
	}

	// todo benl: stop using raw strings for sandbox status
	if ConditionsToStatus(sb.Status.Conditions) != "running" {
		logger.Warn("sandbox is not ready", "id", id, "status", ConditionsToStatus(sb.Status.Conditions))
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	if sb.Status.PodIP == "" { // should not happen if sandbox is ready ^
		logger.Warn("sandbox is not ready", "id", id)
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	return sb, nil
}

func HandleSidecarError(resp *http.Response, sandboxID string, logger *slog.Logger) error {
	// Read limited error body to avoid unbounded reads to memory from untrusted sandbox
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		logger.Warn("failed to read sidecar error body", "error", err, "id", sandboxID, "status", resp.StatusCode)
	}

	detail := http.StatusText(resp.StatusCode)
	var sidecarErr huma.ErrorModel
	if json.Unmarshal(body, &sidecarErr) == nil && sidecarErr.Detail != "" {
		detail = sidecarErr.Detail
	}

	if resp.StatusCode >= 500 {
		logger.Error("sidecar returned server error", "id", sandboxID, "status", resp.StatusCode, "detail", detail)
		return huma.Error502BadGateway("sidecar internal error")
	}

	logger.Debug("forwarding sidecar client error", "id", sandboxID, "status", resp.StatusCode, "detail", detail)
	return huma.NewError(resp.StatusCode, detail)
}

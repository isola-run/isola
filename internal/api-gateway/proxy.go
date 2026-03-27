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

package apigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	"github.com/isola-ai/isola/internal/constants"
	"github.com/isola-ai/isola/internal/httputil"
)

// HTTPDoer abstracts HTTP request execution (satisfied by *http.Client), for faking it in tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// BodyStream provides streaming access to request body via Huma's Resolver pattern.
// See https://github.com/danielgtaylor/huma/issues/749
type BodyStream struct {
	Stream             io.Reader
	ResponseController *http.ResponseController
}

func (b *BodyStream) Resolve(ctx huma.Context) []error {
	b.Stream = ctx.BodyReader()
	b.ResponseController = httputil.ResponseController(ctx)
	return nil
}

func ConditionsToStatus(conditions []metav1.Condition) string {
	ready := meta.FindStatusCondition(conditions, "Ready")
	if ready == nil {
		return constants.SandboxStatusUnknown
	}

	if ready.Status == metav1.ConditionTrue {
		return constants.SandboxStatusRunning
	}

	// TODO: remove snapshot-related reasons from Sandbox CRD — they should be
	// encapsulated in the RootfsSnapshot CRD only.
	switch ready.Reason {
	case constants.CondReasonPodPending, constants.CondReasonPodCreating,
		constants.CondReasonReconciling, constants.CondReasonNetworkPolicyApplied:
		return constants.SandboxStatusCreating
	case constants.CondReasonPodRunning, constants.CondReasonRootfsSnapshottingInProgress:
		return constants.SandboxStatusRunning
	case constants.CondReasonDeleting:
		return constants.SandboxStatusShuttingDown
	case constants.CondReasonPodFailed, constants.CondReasonPodCreationFailed,
		constants.CondReasonInvalidRuntime, constants.CondReasonNetworkPolicyFailed,
		constants.CondReasonRootfsSnapshotFailed, constants.CondReasonRootfsSnapshotTimeout,
		constants.CondReasonRootfsRestoreConfigError, constants.CondReasonStartupTimeoutExceeded:
		return constants.SandboxStatusFailed
	case constants.CondReasonPodSucceeded, constants.CondReasonRootfsSnapshotComplete:
		return constants.SandboxStatusStopped
	default:
		return constants.SandboxStatusUnknown
	}
}

func K8sErrorToHuma(err error, fallbackMsg string) error {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) && statusErr.ErrStatus.Code > 0 {
		humaErr := huma.NewError(int(statusErr.ErrStatus.Code), statusErr.ErrStatus.Message)
		if seconds, ok := apierrors.SuggestsClientDelay(err); ok && seconds > 0 {
			return huma.ErrorWithHeaders(humaErr, http.Header{
				"Retry-After": {strconv.Itoa(seconds)},
			})
		}
		return humaErr
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

	if ConditionsToStatus(sb.Status.Conditions) != constants.SandboxStatusRunning {
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

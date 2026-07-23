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

package gvisorinstaller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type criStatus struct {
	RuntimeReady bool
	Handlers     []string
}

var errNoRuntimeHandlers = errors.New(
	"containerd reports no runtime handlers over CRI (neither the RuntimeHandlers status field of " +
		"containerd 2.x nor the verbose CRI plugin config of containerd 1.x). " +
		"gVisor auto-install requires containerd 1.6 or newer")

// CRIClient judges containerd over the same surface the kubelet does.
type CRIClient interface {
	// withHandlers costs a second round trip on containerd 1.x.
	Status(ctx context.Context, withHandlers bool) (criStatus, error)
}

type criClient struct {
	target string
}

func NewCRIClient(socketPath string) CRIClient {
	return &criClient{target: "unix://" + socketPath}
}

func (c *criClient) Status(ctx context.Context, withHandlers bool) (criStatus, error) {
	// A fresh connection per probe keeps each check independent of stale
	// connection state across containerd restarts.
	conn, err := grpc.NewClient(c.target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return criStatus{}, fmt.Errorf("connecting to CRI socket: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rt := runtimeapi.NewRuntimeServiceClient(conn)
	return statusFromRPC(ctx, func(ctx context.Context, verbose bool) (*runtimeapi.StatusResponse, error) {
		return rt.Status(ctx, &runtimeapi.StatusRequest{Verbose: verbose})
	}, withHandlers)
}

// statusRPC is the seam that makes statusFromRPC testable without containerd.
type statusRPC func(ctx context.Context, verbose bool) (*runtimeapi.StatusResponse, error)

// statusFromRPC reads handlers from StatusResponse.RuntimeHandlers, which
// only containerd 2.0+ populates, and falls back to the verbose Info config
// for 1.x. Equal evidence: both are the running daemon's loaded config, and
// 1.x resolves RunPodSandbox against that very map. Verbose is requested only
// when the field came back empty, so 2.x never pays for it.
func statusFromRPC(ctx context.Context, call statusRPC, withHandlers bool) (criStatus, error) {
	resp, err := call(ctx, false)
	if err != nil {
		return criStatus{}, fmt.Errorf("CRI runtime status: %w", err)
	}

	var st criStatus
	for _, cond := range resp.GetStatus().GetConditions() {
		if cond.GetType() == runtimeapi.RuntimeReady {
			st.RuntimeReady = cond.GetStatus()
		}
	}
	for _, h := range resp.GetRuntimeHandlers() {
		st.Handlers = append(st.Handlers, h.GetName())
	}
	if !withHandlers || len(st.Handlers) > 0 {
		return st, nil
	}

	verbose, err := call(ctx, true)
	if err != nil {
		return st, fmt.Errorf("CRI runtime status (verbose): %w", err)
	}
	st.Handlers, err = handlersFromPluginConfig(verbose.GetInfo()["config"])
	if err != nil {
		return st, err
	}
	return st, nil
}

// criPluginConfigInfo mirrors containerd 1.x's verbose Info["config"], where
// runtimes sit at containerd.runtimes.<handler>, NOT under the
// plugins."io.containerd..." nesting containerd.go parses from `config dump`.
type criPluginConfigInfo struct {
	Containerd struct {
		Runtimes map[string]json.RawMessage `json:"runtimes"`
	} `json:"containerd"`
}

// handlersFromPluginConfig distinguishes absent (no handlers, caller fails
// closed) from unparsable (an error), so a regression is not hidden behind a
// version complaint.
func handlersFromPluginConfig(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var cfg criPluginConfigInfo
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parsing the CRI plugin config reported by containerd: %w", err)
	}
	if len(cfg.Containerd.Runtimes) == 0 {
		return nil, nil
	}
	handlers := make([]string, 0, len(cfg.Containerd.Runtimes))
	for name := range cfg.Containerd.Runtimes {
		handlers = append(handlers, name)
	}
	// Sorted so error messages are stable.
	slices.Sort(handlers)
	return handlers, nil
}

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
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// criStatus is the subset of the CRI runtime status the installer cares
// about. Handlers is nil when the runtime predates the RuntimeHandlers field
// (CRI v1, containerd < 1.7.15) — verification then fails closed; the
// on-disk config is never accepted as proof of serving.
type criStatus struct {
	RuntimeReady bool
	Handlers     []string
}

// CRIClient checks containerd health through its CRI socket — the same
// surface the kubelet judges the runtime by.
type CRIClient interface {
	Status(ctx context.Context) (criStatus, error)
}

type criClient struct {
	// target is a gRPC unix target, e.g. "unix:///host/run/containerd/containerd.sock".
	target string
}

// NewCRIClient returns a CRI client for the containerd socket as mounted
// inside the installer container.
func NewCRIClient(socketPath string) CRIClient {
	return &criClient{target: "unix://" + socketPath}
}

func (c *criClient) Status(ctx context.Context) (criStatus, error) {
	// A fresh connection per probe keeps each check independent of stale
	// connection state across containerd restarts.
	conn, err := grpc.NewClient(c.target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return criStatus{}, fmt.Errorf("connecting to CRI socket: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := runtimeapi.NewRuntimeServiceClient(conn).Status(ctx, &runtimeapi.StatusRequest{})
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
	return st, nil
}

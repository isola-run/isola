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

package sandboxsidecar

import (
	"io"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/isola-run/isola/internal/httputil"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
)

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

// PIDResolver caches container PID lookups to avoid repeated /proc scans.
// Shared across handler types so the cache is unified.
type PIDResolver struct {
	procFS     proc.ProcFS
	pidMu      sync.RWMutex
	cachedPIDs map[string]int
}

func NewPIDResolver(procFS proc.ProcFS) *PIDResolver {
	return &PIDResolver{
		procFS:     procFS,
		cachedPIDs: make(map[string]int),
	}
}

func (r *PIDResolver) FindCachedContainerPID(containerName string) (int, error) {
	if containerName != "" {
		r.pidMu.RLock()
		pid, ok := r.cachedPIDs[containerName]
		r.pidMu.RUnlock()

		if ok {
			if name, found := proc.GetContainerName(pid); found && name == containerName {
				return pid, nil
			}
		}
	}

	newPID, err := r.procFS.FindMarkedPID(containerName)
	if err != nil {
		return 0, err
	}

	if containerName != "" {
		r.pidMu.Lock()
		r.cachedPIDs[containerName] = newPID
		r.pidMu.Unlock()
	}

	return newPID, nil
}

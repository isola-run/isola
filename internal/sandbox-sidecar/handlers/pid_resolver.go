package handlers

import (
	"sync"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

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
	r.pidMu.RLock()
	pid, ok := r.cachedPIDs[containerName]
	r.pidMu.RUnlock()

	if ok {
		if name, found := proc.GetContainerName(pid); found && (containerName == "" || name == containerName) {
			return pid, nil
		}
	}

	newPID, err := r.procFS.FindMarkedPID(containerName)
	if err != nil {
		return 0, err
	}

	r.pidMu.Lock()
	r.cachedPIDs[containerName] = newPID
	r.pidMu.Unlock()

	return newPID, nil
}

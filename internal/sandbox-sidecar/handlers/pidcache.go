package handlers

import (
	"sync"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

// PIDCache caches container name → PID mappings to avoid repeated /proc scans.
type PIDCache struct {
	procFS proc.ProcFS
	mu     sync.RWMutex
	pids   map[string]int
}

func NewPIDCache(procFS proc.ProcFS) *PIDCache {
	return &PIDCache{
		procFS: procFS,
		pids:   make(map[string]int),
	}
}

func (c *PIDCache) FindPID(containerName string) (int, error) {
	c.mu.RLock()
	pid, ok := c.pids[containerName]
	c.mu.RUnlock()

	if ok {
		if name, found := proc.GetContainerName(pid); found && (containerName == "" || name == containerName) {
			return pid, nil
		}
	}

	// Cache miss or stale — rescan
	newPID, err := c.procFS.FindMarkedPID(containerName)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.pids[containerName] = newPID
	c.mu.Unlock()

	return newPID, nil
}

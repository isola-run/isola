package handlers

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

// ErrContainerNotRunning indicates the container was previously running but can no longer be found.
// This typically happens when a container crashes and hasn't restarted yet (e.g. CrashLoopBackOff).
var ErrContainerNotRunning = errors.New("container not running")

const (
	defaultMaxRetries = 10
	defaultRetryDelay = 200 * time.Millisecond
)

// PIDResolver caches container PID lookups to avoid repeated /proc scans.
// When a cached PID goes stale (container crashed), it retries discovery
// to bridge the gap during container restarts.
// Shared across handler types so the cache is unified.
type PIDResolver struct {
	procFS     proc.ProcFS
	pidMu      sync.RWMutex
	cachedPIDs map[string]int

	// Retry parameters for crash recovery. Configurable for testing.
	maxRetries int
	retryDelay time.Duration
}

func NewPIDResolver(procFS proc.ProcFS) *PIDResolver {
	return &PIDResolver{
		procFS:     procFS,
		cachedPIDs: make(map[string]int),
		maxRetries: defaultMaxRetries,
		retryDelay: defaultRetryDelay,
	}
}

// FindCachedContainerPID resolves a container name to its PID, using a cache for repeated lookups.
//
// When a previously-cached PID goes stale (container crashed) and a fresh /proc scan also fails,
// it retries with short delays to bridge the window during container restarts. If the container
// was never seen before, it fails immediately without retries.
//
// Returns ErrContainerNotRunning when a previously-known container can't be found after retries
// (distinguishing crashed containers from containers that never existed).
func (r *PIDResolver) FindCachedContainerPID(ctx context.Context, containerName string) (int, error) {
	r.pidMu.RLock()
	cachedPID, wasCached := r.cachedPIDs[containerName]
	r.pidMu.RUnlock()

	if wasCached {
		if name, found := r.procFS.GetContainerName(cachedPID); found && (containerName == "" || name == containerName) {
			return cachedPID, nil
		}
	}

	// Fresh scan
	newPID, err := r.procFS.FindMarkedPID(containerName)
	if err == nil {
		r.pidMu.Lock()
		r.cachedPIDs[containerName] = newPID
		r.pidMu.Unlock()
		return newPID, nil
	}

	// If we had a cached PID but the container is gone, it likely crashed and may
	// be restarting. Retry a few times to bridge the restart window.
	if wasCached && errors.Is(err, proc.ErrContainerNotFound) {
		return r.retryFindPID(ctx, containerName)
	}

	return 0, err
}

// retryFindPID retries /proc scanning with delays for crash recovery.
func (r *PIDResolver) retryFindPID(ctx context.Context, containerName string) (int, error) {
	for range r.maxRetries {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(r.retryDelay):
		}

		newPID, err := r.procFS.FindMarkedPID(containerName)
		if err == nil {
			r.pidMu.Lock()
			r.cachedPIDs[containerName] = newPID
			r.pidMu.Unlock()
			return newPID, nil
		}
		if !errors.Is(err, proc.ErrContainerNotFound) {
			return 0, err
		}
	}

	return 0, ErrContainerNotRunning
}

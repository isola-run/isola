// Package handlers provides HTTP request handlers for the isola-agent API.
package handlers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var ErrMainContainerNotFound = errors.New("main container PID not found")

// MainContainerMarker is the environment variable injected by isola-operator
// into main containers for reliable PID discovery.
const MainContainerMarker = "ISOLA_MAIN_CONTAINER=true"

type ProcFS struct {
	procPath string

	// Use sync.Once to ensure discovery happens only once
	once             sync.Once
	mainContainerPID int
	discoverErr      error
}

func NewProcFS() *ProcFS {
	return &ProcFS{
		procPath: "/proc",
	}
}

// DiscoverMainContainerPID scans /proc to find the main container's PID.
// It looks for processes with the ISOLA_MAIN_CONTAINER=true environment variable,
func (p *ProcFS) DiscoverMainContainerPID() (int, error) {
	p.once.Do(func() {
		p.mainContainerPID, p.discoverErr = p.findMarkedProcess()
	})

	// handles container restarts and PID reuse
	if p.mainContainerPID != 0 && p.discoverErr == nil {
		if !p.hasMainContainerMarker(p.mainContainerPID) {
			p.mainContainerPID, p.discoverErr = p.findMarkedProcess()
		}
	}

	return p.mainContainerPID, p.discoverErr
}

func (p *ProcFS) findMarkedProcess() (int, error) {
	entries, err := os.ReadDir(p.procPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // Not a PID directory
		}

		if p.hasMainContainerMarker(pid) {
			return pid, nil
		}
	}

	return 0, ErrMainContainerNotFound
}

func (p *ProcFS) hasMainContainerMarker(pid int) bool {
	environPath := filepath.Join(p.procPath, strconv.Itoa(pid), "environ")
	data, err := os.ReadFile(environPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), MainContainerMarker)
}

// This uses /proc/<pid>/root to access the main container's root filesystem.
func (p *ProcFS) GetMainContainerRootPath() (string, error) {
	pid, err := p.DiscoverMainContainerPID()
	if err != nil {
		return "", err
	}

	return filepath.Join(p.procPath, strconv.Itoa(pid), "root"), nil
}

func (p *ProcFS) ResolvePath(requestedPath string) (string, error) {
	rootPath, err := p.GetMainContainerRootPath()
	if err != nil {
		return "", err
	}

	// Ensure the requested path is absolute
	if !strings.HasPrefix(requestedPath, "/") {
		requestedPath = "/" + requestedPath
	}

	return filepath.Join(rootPath, requestedPath), nil
}

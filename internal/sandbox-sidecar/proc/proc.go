package proc

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrContainerNotFound is returned when the marked container PID cannot be found.
var ErrContainerNotFound = errors.New("container not found: no process with ISOLA_MAIN_CONTAINER=true")

// ProcFS abstracts /proc filesystem operations for finding container processes.
type ProcFS interface {
	// FindMarkedPID scans /proc/*/environ for a process with ISOLA_MAIN_CONTAINER=true.
	FindMarkedPID() (int, error)
	// GetCwd reads the /proc/<pid>/cwd symlink to get the process working directory.
	GetCwd(pid int) (string, error)
	// GetRoot returns the path to /proc/<pid>/root for the given PID.
	GetRoot(pid int) string
}

// RealProcFS implements ProcFS using the actual /proc filesystem.
type RealProcFS struct{}

// FindMarkedPID scans /proc for a process with ISOLA_MAIN_CONTAINER=true in its environment.
func (r *RealProcFS) FindMarkedPID() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("read /proc: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		if hasMarker(pid) {
			return pid, nil
		}
	}

	return 0, ErrContainerNotFound
}

// hasMarker checks if a process has ISOLA_MAIN_CONTAINER=true in its environment.
func hasMarker(pid int) bool {
	environPath := fmt.Sprintf("/proc/%d/environ", pid)
	f, err := os.Open(environPath) //nolint:gosec // path is constructed from trusted PID
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	// environ is null-byte separated
	scanner := bufio.NewScanner(f)
	scanner.Split(splitNullBytes)

	for scanner.Scan() {
		if scanner.Text() == "ISOLA_MAIN_CONTAINER=true" {
			return true
		}
	}

	return false
}

// splitNullBytes is a bufio.SplitFunc that splits on null bytes.
func splitNullBytes(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := strings.IndexByte(string(data), 0); i >= 0 {
		return i + 1, data[0:i], nil
	}

	if atEOF {
		return len(data), data, nil
	}

	return 0, nil, nil
}

// GetCwd reads the cwd symlink for the given PID.
func (r *RealProcFS) GetCwd(pid int) (string, error) {
	cwdPath := fmt.Sprintf("/proc/%d/cwd", pid)
	target, err := os.Readlink(cwdPath)
	if err != nil {
		return "", fmt.Errorf("read cwd symlink: %w", err)
	}
	return target, nil
}

// GetRoot returns the path to /proc/<pid>/root.
func (r *RealProcFS) GetRoot(pid int) string {
	return filepath.Join("/proc", strconv.Itoa(pid), "root")
}

package proc

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/isola-ai/isola-sb/internal/constants"
)

// ErrContainerNotFound is returned when the marked container PID cannot be found.
var ErrContainerNotFound = errors.New("container not found: no process with ISOLA_CONTAINER_NAME")

// ProcFS abstracts /proc filesystem operations for finding container processes.
type ProcFS interface {
	// FindMarkedPID scans /proc/*/environ for a process with ISOLA_CONTAINER_NAME=<containerName>.
	// If containerName is empty and there is only one container, it will return the PID of that container.
	// Otherwise, return an error.
	FindMarkedPID(containerName string) (int, error)
	// GetCwd reads the /proc/<pid>/cwd symlink to get the process working directory.
	GetCwd(pid int) (string, error)
	// GetRoot returns the path to /proc/<pid>/root for the given PID.
	GetRoot(pid int) string
	// GetUIDGID reads the real UID and GID from /proc/<pid>/status.
	GetUIDGID(pid int) (uid, gid int, err error)
	// ReadEnviron reads all environment variables from /proc/<pid>/environ.
	// Returns []string{"KEY=value", ...} format (same as os.Environ()).
	ReadEnviron(pid int) ([]string, error)
}

// RealProcFS implements ProcFS using the actual /proc filesystem.
type RealProcFS struct{}

// FindMarkedPID scans /proc for a process with ISOLA_CONTAINER_NAME in its environment.
// If containerName is specified, it finds the process with that exact container name.
// If containerName is empty and there is exactly one container, it returns that container's PID.
func (r *RealProcFS) FindMarkedPID(containerName string) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("read /proc: %w", err)
	}

	var foundPID int
	var foundCount int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		name, ok := GetContainerName(pid)
		if !ok {
			continue
		}

		if containerName != "" {
			if name == containerName {
				return pid, nil
			}
		} else {
			foundPID = pid
			foundCount++
		}
	}

	if containerName != "" {
		return 0, ErrContainerNotFound
	}

	if foundCount == 0 {
		return 0, ErrContainerNotFound
	}
	if foundCount > 1 {
		return 0, ErrContainerNotFound
	}

	// if no container name is specified and only one container is found, return that container's PID
	return foundPID, nil
}

// GetContainerName returns the ISOLA_CONTAINER_NAME value if present in the process environment.
func GetContainerName(pid int) (string, bool) {
	environPath := fmt.Sprintf("/proc/%d/environ", pid)
	f, err := os.Open(environPath) //nolint:gosec // path is constructed from trusted PID
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()

	// environ is null-byte separated
	scanner := bufio.NewScanner(f)
	scanner.Split(splitOnNullBytes)

	prefix := constants.IsolaContainerNameEnv + "="
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, prefix) {
			return text[len(prefix):], true
		}
	}

	return "", false
}

func splitOnNullBytes(data []byte, atEOF bool) (advance int, token []byte, err error) {
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

// GetUIDGID returns the UID and GID of the process by stat'ing /proc/<pid>.
// The /proc/<pid> directory is owned by the user running the process.
func (r *RealProcFS) GetUIDGID(pid int) (uid, gid int, err error) {
	procPath := fmt.Sprintf("/proc/%d", pid)
	var stat unix.Stat_t
	if err := unix.Stat(procPath, &stat); err != nil {
		return 0, 0, fmt.Errorf("stat %s: %w", procPath, err)
	}
	return int(stat.Uid), int(stat.Gid), nil
}

// ReadEnviron reads all environment variables from /proc/<pid>/environ.
func (r *RealProcFS) ReadEnviron(pid int) ([]string, error) {
	environPath := fmt.Sprintf("/proc/%d/environ", pid)
	f, err := os.Open(environPath) //nolint:gosec // path is constructed from trusted PID
	if err != nil {
		return nil, fmt.Errorf("open environ: %w", err)
	}
	defer func() { _ = f.Close() }()

	var env []string
	scanner := bufio.NewScanner(f)
	scanner.Split(splitOnNullBytes)
	for scanner.Scan() {
		if text := scanner.Text(); text != "" {
			env = append(env, text)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read environ: %w", err)
	}
	return env, nil
}

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

// runsc-launcher execs the runsc a given sandbox was created with, rather
// than whichever release is newest on the node. The gvisor-installer keeps
// every installed release, and running a sandbox's control operations with a
// different release than its sentry is unsupported by gVisor.
//
// It has to be a separate exec step because a Pod's hostPath sources are
// fixed before any container reads containerd's state.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const shimBinaryPathFile = "shim-binary-path"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "runsc-launcher:", err)
		os.Exit(1)
	}
}

type options struct {
	bundleRoot  string
	namespace   string
	containerID string
	installDir  string
	fallback    string
	args        []string
}

func run(argv []string) error {
	opts, err := parseArgs(argv)
	if err != nil {
		return err
	}
	runsc, err := resolveRunsc(opts)
	if err != nil {
		return err
	}
	fi, err := os.Stat(runsc)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("resolved runsc %q is not an executable file", runsc)
	}
	return syscall.Exec(runsc, append([]string{runsc}, opts.args...), os.Environ()) //nolint:gosec // execing the resolved runsc is the whole job, and the path is validated above
}

func parseArgs(argv []string) (options, error) {
	var o options
	rest := argv
	for len(rest) > 0 {
		arg := rest[0]
		if arg == "--" {
			o.args = rest[1:]
			break
		}
		name, value, ok := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !ok {
			return o, fmt.Errorf("expected --flag=value, got %q", arg)
		}
		switch name {
		case "bundle-root":
			o.bundleRoot = value
		case "namespace":
			o.namespace = value
		case "container-id":
			o.containerID = value
		case "install-dir":
			o.installDir = value
		case "fallback":
			o.fallback = value
		default:
			return o, fmt.Errorf("unknown flag %q", name)
		}
		rest = rest[1:]
	}
	for _, f := range []struct{ name, value string }{
		{"bundle-root", o.bundleRoot},
		{"namespace", o.namespace},
		{"container-id", o.containerID},
		{"install-dir", o.installDir},
	} {
		if f.value == "" {
			return o, fmt.Errorf("--%s is required", f.name)
		}
	}
	if len(o.args) == 0 {
		return o, errors.New("no runsc arguments given after --")
	}
	return o, nil
}

// The container ID becomes a path component, so it must not be able to walk
// out of the bundle root.
func bundleDir(o options) (string, error) {
	for _, part := range []string{o.namespace, o.containerID} {
		if part == "" || part == "." || part == ".." || strings.ContainsRune(part, '/') {
			return "", fmt.Errorf("unsafe path component %q", part)
		}
	}
	return filepath.Join(o.bundleRoot, o.namespace, o.containerID), nil
}

// resolveRunsc uses what containerd recorded for this sandbox. Both records
// are consulted before falling back, so losing one of them cannot silently
// downgrade a managed sandbox to the fallback release.
func resolveRunsc(o options) (string, error) {
	dir, err := bundleDir(o)
	if err != nil {
		return "", err
	}
	fromShim, shimErr := generationFromShimPath(dir)
	fromState, haveState := generationFromShimState(dir)

	// Disagreement means something rewrote one of the two records, and
	// guessing which is authoritative could pair a sentry with the wrong
	// release.
	if shimErr == nil && haveState && fromShim != fromState {
		return "", fmt.Errorf("containerd records shim generation %q but the shim state records %q", fromShim, fromState)
	}

	releases := filepath.Join(o.installDir, "releases")
	for _, gen := range []string{fromShim, fromState} {
		if gen != "" && filepath.Dir(gen) == releases {
			return filepath.Join(gen, "runsc"), nil
		}
	}
	if shimErr != nil && !haveState {
		return o.unmanaged(fmt.Errorf("reading the containerd bundle: %w", shimErr))
	}
	return o.unmanaged(fmt.Errorf("sandbox runs outside %q", releases))
}

// Nodes with a pre-existing runsc have no generation to resolve, so they must
// configure the path explicitly.
func (o options) unmanaged(cause error) (string, error) {
	if o.fallback == "" {
		return "", fmt.Errorf("cannot determine the runsc this sandbox was created with and no fallback is configured: %w", cause)
	}
	return o.fallback, nil
}

// containerd writes shim-binary-path into every bundle at task creation, so
// its directory is the generation the sandbox was created under.
func generationFromShimPath(bundle string) (string, error) {
	data, err := os.ReadFile(filepath.Join(bundle, shimBinaryPathFile)) //nolint:gosec // path built from validated components
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(string(data))
	if !filepath.IsAbs(p) || p != filepath.Clean(p) {
		return "", fmt.Errorf("shim binary path %q is not a clean absolute path", p)
	}
	return filepath.Dir(p), nil
}

// The gVisor shim persists the runsc it was told to use. Treated as a
// cross-check rather than the source of truth, because its on-disk shape is
// the shim's internal state format.
func generationFromShimState(bundle string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(bundle, "state.json")) //nolint:gosec // path built from validated components
	if err != nil {
		return "", false
	}
	var st struct {
		Options map[string]json.RawMessage `json:"Options"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return "", false
	}
	for key, raw := range st.Options {
		if !strings.EqualFold(key, "binaryname") && !strings.EqualFold(key, "binary_name") {
			continue
		}
		var p string
		if err := json.Unmarshal(raw, &p); err != nil || p == "" {
			return "", false
		}
		if !filepath.IsAbs(p) || p != filepath.Clean(p) {
			return "", false
		}
		return filepath.Dir(p), true
	}
	return "", false
}

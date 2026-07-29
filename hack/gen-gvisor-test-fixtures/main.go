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

// Generates the committed gvisor.tar.bz2 test fixtures under
// internal/gvisor-installer/testdata, mirroring the layout of real gVisor
// release archives with tiny shell scripts as payload. Deterministic output
// (fixed versions, timestamps, ordering, USTAR) so regeneration only changes
// the files when the layout itself changes. Needs bzip2 on PATH; run rarely:
//
//	go run ./hack/gen-gvisor-test-fixtures
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var versions = []string{"20260101.0", "20260202.0"}

func main() {
	outDir := filepath.Join("internal", "gvisor-installer", "testdata")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		log.Fatal(err)
	}
	for _, v := range versions {
		data, err := archive(v)
		if err != nil {
			log.Fatal(err)
		}
		out := filepath.Join(outDir, "release-"+v+".tar.bz2")
		if err := os.WriteFile(out, data, 0o644); err != nil { //nolint:gosec // committed fixture, must be world-readable
			log.Fatal(err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", out, len(data))
	}
}

func archive(version string) ([]byte, error) {
	script := func(body string) []byte { return []byte("#!/bin/sh\n" + body) }
	entries := []struct {
		name string
		dir  bool
		data []byte
	}{
		{name: "containerd-shim-runsc-v1", data: script("exit 0\n")},
		{name: "gvisor-bin", dir: true},
		{name: "gvisor-bin/checkpointgofer", data: script("echo checkpointgofer " + version + "\n")},
		{name: "gvisor-bin/runsc-metric-server", data: script("echo runsc-metric-server " + version + "\n")},
		{name: "runsc", data: script("echo 'runsc version release-" + version + "'\necho 'spec: 1.2.0'\n")},
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:    e.name,
			Mode:    0o755,
			ModTime: time.Unix(0, 0).UTC(),
			Format:  tar.FormatUSTAR,
		}
		if e.dir {
			hdr.Name += "/"
			hdr.Typeflag = tar.TypeDir
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.data))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(context.Background(), "bzip2", "-9")
	cmd.Stdin = &buf
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("bzip2: %w", err)
	}
	return out.Bytes(), nil
}

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
	"fmt"
	"os"
	"path"
)

const stateSchemaVersion = 1

// ManagedBlock is the exact block text, not a hash: after a handler or
// installDir change the previous block cannot be reconstructed from config.
type generationRecord struct {
	// Host-view absolute, so the record survives installDir changes.
	GenerationPath string `json:"generationPath"`
	// Sandboxes created under this activation keep reading this exact path.
	ConfigPath    string `json:"configPath"`
	Version       string `json:"version"`
	ArchiveSHA512 string `json:"archiveSHA512"`
	Handler       string `json:"handler"`
	ManagedBlock  string `json:"managedBlock"`
}

// Written before the managed block is modified, cleared only after a restart
// settled the daemon on a known block. While present, on-disk config cannot
// be trusted to match what containerd has loaded.
type pendingActivation struct {
	Target generationRecord `json:"target"`
	// Empty when no block existed; rollback then removes the block.
	PreviousManagedBlock string `json:"previousManagedBlock"`
	// Empty means no isola-managed handler to expect after rolling back.
	PreviousHandler string `json:"previousHandler"`
}

type installerState struct {
	SchemaVersion int                `json:"schemaVersion"`
	Active        *generationRecord  `json:"active,omitempty"`
	Pending       *pendingActivation `json:"pending,omitempty"`
}

func (i *Installer) statePath() string { return i.cfg.hostPath(stateFilePath) }

// Missing, legacy or malformed state degrades to a full activation
// transaction: one possibly redundant restart, never a trusted bad record.
func (i *Installer) readState() installerState {
	var st installerState
	if err := readJSONFile(i.statePath(), &st); err != nil {
		if !os.IsNotExist(err) {
			i.log.Warn("unreadable installer state, treating as absent", "path", stateFilePath, "error", err)
		}
		return installerState{SchemaVersion: stateSchemaVersion}
	}
	if st.SchemaVersion != stateSchemaVersion || !st.valid() {
		i.log.Warn("ignoring installer state with unknown schema or invalid contents", "path", stateFilePath, "schemaVersion", st.SchemaVersion)
		return installerState{SchemaVersion: stateSchemaVersion}
	}
	return st
}

func (i *Installer) writeState(st installerState) error {
	st.SchemaVersion = stateSchemaVersion
	if err := writeJSONFile(i.statePath(), st); err != nil {
		return fmt.Errorf("writing installer state: %w", err)
	}
	return nil
}

func (st installerState) valid() bool {
	for _, r := range []*generationRecord{st.Active, st.pendingTarget()} {
		if r == nil {
			continue
		}
		if !path.IsAbs(r.GenerationPath) || !path.IsAbs(r.ConfigPath) ||
			r.Version == "" || r.Handler == "" || r.ManagedBlock == "" {
			return false
		}
		if len(r.ArchiveSHA512) != sha512HexLen || !isHex(r.ArchiveSHA512) {
			return false
		}
	}
	return true
}

func (st installerState) pendingTarget() *generationRecord {
	if st.Pending == nil {
		return nil
	}
	return &st.Pending.Target
}

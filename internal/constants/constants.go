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

package constants

// IsolaContainerNameEnv is the environment variable used to mark containers
// with their name for discovery by the sidecar via /proc/<pid>/environ.
const IsolaContainerNameEnv = "ISOLA_CONTAINER_NAME"

// SidecarPort is the HTTP port the sandbox-sidecar listens on.
const SidecarPort = 10032

const SidecarCommandOutputDir = "/run/isola/commands"

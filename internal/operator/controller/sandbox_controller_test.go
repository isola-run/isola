/*
Copyright 2025 isola.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package controller contains tests for the sandbox controller.
// Tests are split across multiple files for maintainability:
//   - sandbox_controller_helpers_test.go: Helper functions
//   - sandbox_controller_template_test.go: Template lifecycle tests
//   - sandbox_controller_pod_test.go: Pod creation and condition state machine tests
//   - sandbox_controller_timeout_test.go: Timeout behavior tests
//   - sandbox_controller_snapshot_test.go: Snapshotting tests
//   - sandbox_controller_events_test.go: Event recording and error handling tests
//   - sandbox_controller_finalizer_test.go: Finalizer behavior tests
//   - sandbox_controller_network_test.go: Network configuration tests
package controller

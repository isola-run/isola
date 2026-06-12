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
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

// Node labels stamped by the installer. LabelGVisorReady gates scheduling:
// the chart-created RuntimeClass selects it via scheduling.nodeSelector, so
// sandbox pods only land on nodes with a verified-healthy runtime.
const (
	LabelGVisorReady   = "isola.run/gvisor"
	LabelGVisorVersion = "isola.run/gvisor-version"

	// LabelGVisorInstall is the per-node opt-out: nodes labeled
	// isola.run/gvisor-install=disabled are excluded by the DaemonSet's
	// node affinity (enforced in the Helm template, not here).
	LabelGVisorInstall = "isola.run/gvisor-install"

	// VersionUnmanaged is the version label value for nodes where a
	// pre-existing (non-isola) runsc handler was detected and adopted as-is.
	VersionUnmanaged = "unmanaged"
)

// NodeClient reports per-node installation state to the cluster: labels for
// scheduling/observability, events for humans.
type NodeClient interface {
	// SetNodeLabels patches the installer-owned labels on the node.
	// An empty value removes the label.
	SetNodeLabels(ctx context.Context, labels map[string]string) error
	// Event records a Kubernetes event on the Node object.
	Event(eventType, reason, message string)
}

type nodeClient struct {
	clientset *kubernetes.Clientset
	nodeName  string
	recorder  record.EventRecorder
}

// NewNodeClient builds the production NodeClient using in-cluster credentials.
func NewNodeClient(clientset *kubernetes.Clientset, nodeName string) NodeClient {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "isola-gvisor-installer", Host: nodeName})
	return &nodeClient{clientset: clientset, nodeName: nodeName, recorder: recorder}
}

func (n *nodeClient) SetNodeLabels(ctx context.Context, labels map[string]string) error {
	// Strategic-merge semantics on labels: null deletes the key.
	patchLabels := make(map[string]*string, len(labels))
	for k, v := range labels {
		if v == "" {
			patchLabels[k] = nil
		} else {
			patchLabels[k] = &v
		}
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": patchLabels}})
	if err != nil {
		return err
	}
	if _, err := n.clientset.CoreV1().Nodes().Patch(ctx, n.nodeName, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patching node labels: %w", err)
	}
	return nil
}

func (n *nodeClient) Event(eventType, reason, message string) {
	ref := &corev1.ObjectReference{
		Kind: "Node",
		Name: n.nodeName,
		UID:  types.UID(n.nodeName),
	}
	n.recorder.Event(ref, eventType, reason, message)
}

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
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

func TestNodeEventReferencesCurrentNode(t *testing.T) {
	client := fake.NewClientset()
	broadcaster := record.NewBroadcaster()
	t.Cleanup(broadcaster.Shutdown)
	events := make(chan *corev1.Event, 2)
	watcher := broadcaster.StartEventWatcher(func(event *corev1.Event) { events <- event })
	t.Cleanup(watcher.Stop)
	n := &nodeClient{
		clientset: client,
		nodeName:  "worker",
		recorder:  broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "isola-gvisor-installer"}),
		log:       slog.New(slog.DiscardHandler),
	}

	for _, uid := range []types.UID{"original-node-uid", "replacement-node-uid"} {
		_, err := client.CoreV1().Nodes().Create(t.Context(), &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: n.nodeName, UID: uid},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		n.Event(corev1.EventTypeNormal, "GVisorReady", "runtime ready")
		select {
		case event := <-events:
			ref := event.InvolvedObject
			if ref.APIVersion != "v1" || ref.Kind != "Node" || ref.Name != n.nodeName || ref.UID != uid || ref.Namespace != "" {
				t.Fatalf("incorrect node reference: %+v", ref)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("event was not recorded")
		}
		if err := client.CoreV1().Nodes().Delete(t.Context(), n.nodeName, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNodeEventRetriesFailedLookup(t *testing.T) {
	client := fake.NewClientset()
	recorder := record.NewFakeRecorder(2)
	var logs bytes.Buffer
	n := &nodeClient{
		clientset: client,
		nodeName:  "worker",
		recorder:  recorder,
		log:       slog.New(slog.NewTextHandler(&logs, nil)),
	}

	n.Event(corev1.EventTypeWarning, "GVisorInstallFailed", "installation failed")
	select {
	case event := <-recorder.Events:
		t.Fatalf("recorded event without a Node: %s", event)
	default:
	}
	if !strings.Contains(logs.String(), "could not read node for installer event") {
		t.Fatal("lookup failure was not logged")
	}

	_, err := client.CoreV1().Nodes().Create(t.Context(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: n.nodeName, UID: "node-uid"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n.Event(corev1.EventTypeNormal, "GVisorReady", "runtime ready")
	select {
	case <-recorder.Events:
	default:
		t.Fatal("event lookup did not recover")
	}
}

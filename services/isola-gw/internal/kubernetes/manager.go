// Package kubernetes provides Kubernetes client functionality for managing Sandbox CRs and pods.
package kubernetes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/isola-ai/isola-sb/services/isola-gw/internal/models"
)

const (
	sandboxGroup    = "sandbox.isola.run"
	sandboxVersion  = "v1alpha1"
	sandboxPlural   = "sandboxes"
	templatePlural  = "sandboxtemplates"
	defaultTimeout  = 600 // seconds
	defaultShutdown = "Delete"
)

type Manager struct {
	namespace     string
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
	restConfig    *rest.Config
	initOnce      sync.Once
	initErr       error
}

func NewManager(namespace string) *Manager {
	return &Manager{
		namespace: namespace,
	}
}

// Initialize initializes the Kubernetes client (thread-safe)
func (m *Manager) Initialize() error {
	m.initOnce.Do(func() {
		m.initErr = m.doInitialize()
	})
	return m.initErr
}

func (m *Manager) doInitialize() error {
	log.Printf("Initializing KubernetesManager for namespace '%s'", m.namespace)

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Failed to load in-cluster config, trying kubeconfig: %v", err)
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return fmt.Errorf("failed to load Kubernetes config: %w", err)
		}
		log.Printf("Loaded kubeconfig from file")
	} else {
		log.Printf("Loaded in-cluster Kubernetes config")
	}

	m.restConfig = config

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}
	m.clientset = clientset

	// Create dynamic client for CRDs
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}
	m.dynamicClient = dynamicClient

	return nil
}

func (m *Manager) CreateSandboxCR(ctx context.Context, sandboxID string, req models.CreateSandboxRequest, templateName string) (bool, *string) {
	if err := m.Initialize(); err != nil {
		errorMsg := fmt.Sprintf("Failed to initialize: %v", err)
		return false, &errorMsg
	}

	// First create the SandboxTemplate CR
	templateSpec := map[string]interface{}{
		"podTemplate": map[string]interface{}{
			"spec": map[string]interface{}{
				"restartPolicy": "Never",
				"containers": []map[string]interface{}{
					{
						"name":    "sandbox",
						"image":   getImage(req.Image),
						"command": []string{"sleep", "3600"},
						"env":     envToK8sEnv(req.Env),
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu":    fmt.Sprintf("%dm", int(getCPU(req.CPU)*1000)),
								"memory": fmt.Sprintf("%dMi", int(getMemory(req.Memory)*1024)),
							},
							"limits": map[string]interface{}{
								"cpu":    fmt.Sprintf("%dm", int(getCPU(req.CPU)*1000)),
								"memory": fmt.Sprintf("%dMi", int(getMemory(req.Memory)*1024)),
							},
						},
					},
				},
			},
		},
		"timeoutSeconds": defaultTimeout,
		"shutdownPolicy": map[string]interface{}{
			"policy": defaultShutdown,
		},
	}

	err := m.createSandboxTemplateCR(ctx, templateName, templateSpec)
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to create SandboxTemplate: %v", err)
		return false, &errorMsg
	}

	// Create Sandbox CR
	sandboxName := fmt.Sprintf("sandbox-%s", sandboxID[:min(8, len(sandboxID))])
	log.Printf("Creating Sandbox CR '%s' (template=%s) in namespace '%s'",
		sandboxName, templateName, m.namespace)

	sandboxBody := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", sandboxGroup, sandboxVersion),
			"kind":       "Sandbox",
			"metadata": map[string]interface{}{
				"name":      sandboxName,
				"namespace": m.namespace,
				"labels": map[string]interface{}{
					"sandbox-id": sandboxID,
					"managed-by": "isola-gw",
				},
				"annotations": map[string]interface{}{
					"isola.run/sandbox-name": req.Name,
				},
			},
			"spec": map[string]interface{}{
				"templateRef": map[string]interface{}{
					"name": templateName,
				},
			},
		},
	}

	gvr := schema.GroupVersionResource{
		Group:    sandboxGroup,
		Version:  sandboxVersion,
		Resource: sandboxPlural,
	}

	_, err = m.dynamicClient.Resource(gvr).Namespace(m.namespace).Create(ctx, sandboxBody, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Printf("Sandbox CR '%s' already exists", sandboxName)
			return true, nil
		}
		log.Printf("Failed to create Sandbox CR for %s: %v", sandboxID, err)
		errorMsg := fmt.Sprintf("Kubernetes API error: %v", err)
		return false, &errorMsg
	}

	log.Printf("Created Sandbox CR 'sandbox-%s' for sandbox %s", sandboxID[:min(8, len(sandboxID))], sandboxID)
	return true, nil
}

func (m *Manager) createSandboxTemplateCR(ctx context.Context, templateName string, templateSpec map[string]interface{}) error {
	log.Printf("Creating SandboxTemplate '%s' in namespace '%s'", templateName, m.namespace)

	templateBody := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", sandboxGroup, sandboxVersion),
			"kind":       "SandboxTemplate",
			"metadata": map[string]interface{}{
				"name":      templateName,
				"namespace": m.namespace,
				"labels": map[string]interface{}{
					"managed-by": "isola-gw",
				},
			},
			"spec": templateSpec,
		},
	}

	gvr := schema.GroupVersionResource{
		Group:    sandboxGroup,
		Version:  sandboxVersion,
		Resource: templatePlural,
	}

	_, err := m.dynamicClient.Resource(gvr).Namespace(m.namespace).Create(ctx, templateBody, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Printf("SandboxTemplate '%s' already exists, updating", templateName)
			// Try to update if it already exists
			existing, getErr := m.dynamicClient.Resource(gvr).Namespace(m.namespace).Get(ctx, templateName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing template: %w", getErr)
			}
			templateBody.SetResourceVersion(existing.GetResourceVersion())
			_, updateErr := m.dynamicClient.Resource(gvr).Namespace(m.namespace).Update(ctx, templateBody, metav1.UpdateOptions{})
			if updateErr != nil {
				return fmt.Errorf("failed to update template: %w", updateErr)
			}
			return nil
		}
		return fmt.Errorf("failed to create SandboxTemplate: %w", err)
	}

	log.Printf("Created SandboxTemplate '%s' in namespace '%s'", templateName, m.namespace)
	return nil
}

func (m *Manager) GetSandboxCR(ctx context.Context, sandboxID string) (*unstructured.Unstructured, error) {
	if err := m.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}

	sandboxName := fmt.Sprintf("sandbox-%s", sandboxID[:min(8, len(sandboxID))])
	gvr := schema.GroupVersionResource{
		Group:    sandboxGroup,
		Version:  sandboxVersion,
		Resource: sandboxPlural,
	}

	sandbox, err := m.dynamicClient.Resource(gvr).Namespace(m.namespace).Get(ctx, sandboxName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get Sandbox CR: %w", err)
	}

	return sandbox, nil
}

func (m *Manager) ListSandboxCRs(ctx context.Context) ([]*unstructured.Unstructured, error) {
	if err := m.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    sandboxGroup,
		Version:  sandboxVersion,
		Resource: sandboxPlural,
	}

	sandboxes, err := m.dynamicClient.Resource(gvr).Namespace(m.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("Failed to list Sandbox CRs: %v", err)
		return nil, fmt.Errorf("failed to list Sandbox CRs: %w", err)
	}

	result := make([]*unstructured.Unstructured, 0, len(sandboxes.Items))
	for i := range sandboxes.Items {
		result = append(result, &sandboxes.Items[i])
	}

	return result, nil
}

func (m *Manager) DeleteSandboxCR(ctx context.Context, sandboxID string) error {
	if err := m.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	sandboxName := fmt.Sprintf("sandbox-%s", sandboxID[:min(8, len(sandboxID))])
	log.Printf("Deleting Sandbox CR '%s'", sandboxName)

	gvr := schema.GroupVersionResource{
		Group:    sandboxGroup,
		Version:  sandboxVersion,
		Resource: sandboxPlural,
	}

	err := m.dynamicClient.Resource(gvr).Namespace(m.namespace).Delete(ctx, sandboxName, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Printf("Sandbox CR '%s' already deleted", sandboxName)
			return nil // Idempotent: already gone is success
		}
		log.Printf("Failed to delete Sandbox CR '%s': %v", sandboxName, err)
		return err // Preserve original error for apierrors.IsNotFound() etc.
	}

	log.Printf("Deleted Sandbox CR '%s'", sandboxName)
	return nil
}

const agentServiceName = "sandbox-agents"

type SandboxStatus struct {
	State        models.SandboxState
	ErrorReason  *string
	AgentAddress string
}

func (m *Manager) GetSandboxStatus(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
	if err := m.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}

	log.Printf("Fetching sandbox status for '%s' from CR conditions", sandboxID)

	// Get the Sandbox CR
	sandbox, err := m.GetSandboxCR(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox CR: %w", err)
	}
	if sandbox == nil {
		return nil, nil // Sandbox not found
	}

	// Parse state from CR conditions
	state, errorReason := m.parseStateFromConditions(sandbox)

	// Construct DNS address for the agent
	agentAddress := m.getAgentAddress(sandboxID)

	return &SandboxStatus{
		State:        state,
		ErrorReason:  errorReason,
		AgentAddress: agentAddress,
	}, nil
}

// getAgentAddress constructs the DNS-resolvable address for the sandbox agent.
// Format: <pod-name>.<headless-service>.<namespace>.svc.cluster.local
func (m *Manager) getAgentAddress(sandboxID string) string {
	sandboxName := fmt.Sprintf("sandbox-%s", sandboxID[:min(8, len(sandboxID))])
	podName := sandboxName + "-pod"
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", podName, agentServiceName, m.namespace)
}

// parseStateFromConditions derives the SandboxState from the Sandbox CR conditions.
// This mirrors what the controller sets and avoids duplicating pod-inspection logic.
func (m *Manager) parseStateFromConditions(sandbox *unstructured.Unstructured) (models.SandboxState, *string) {
	status, found, _ := unstructured.NestedMap(sandbox.Object, "status")
	if !found {
		return models.SandboxStatePending, nil
	}

	conditions, found, _ := unstructured.NestedSlice(status, "conditions")
	if !found {
		return models.SandboxStatePending, nil
	}

	var readyCondition map[string]interface{}
	var podReadyCondition map[string]interface{}

	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := condMap["type"].(string)
		switch condType {
		case "Ready":
			readyCondition = condMap
		case "PodReady":
			podReadyCondition = condMap
		}
	}

	// Use Ready condition as primary indicator
	if readyCondition != nil {
		condStatus, _ := readyCondition["status"].(string)
		reason, _ := readyCondition["reason"].(string)
		message, _ := readyCondition["message"].(string)

		if condStatus == "True" {
			return models.SandboxStateRunning, nil
		}

		// Map controller reasons to states
		switch reason {
		case "PodPending", "PodCreating", "Reconciling":
			return models.SandboxStatePending, nil
		case "PodFailed", "PodCreationFailed":
			errorReason := message
			return models.SandboxStateError, &errorReason
		case "PodSucceeded", "TimedOut", "Deleting":
			return models.SandboxStateStopped, nil
		case "NetworkConfigNotApplied", "NetworkTemplateNotFound", "NetworkTemplateDeleting":
			// Network not ready yet, but pod might be pending/running
			if podReadyCondition != nil {
				podStatus, _ := podReadyCondition["status"].(string)
				if podStatus == "True" {
					// Pod is ready but network isn't - still consider it "pending" from user perspective
					return models.SandboxStatePending, nil
				}
			}
			return models.SandboxStatePending, nil
		default:
			return models.SandboxStatePending, nil
		}
	}

	return models.SandboxStatePending, nil
}

// Executes a command in a pod
// Will be implemented in the future in the sidecar
// TODO: simplify / make more readable return value
func (m *Manager) ExecuteCommand(ctx context.Context, sandboxID string, command string) (string, string, int, error) {
	if err := m.Initialize(); err != nil {
		return "", fmt.Sprintf("Failed to initialize: %v", err), 1, err
	}

	labelSelector := labels.SelectorFromSet(map[string]string{
		"sandbox-id": sandboxID,
	}).String()

	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		log.Printf("Failed to list pods for sandbox %s: %v", sandboxID, err)
		return "", fmt.Sprintf("API error: %v", err), 1, err
	}

	if len(pods.Items) == 0 {
		errorMsg := fmt.Sprintf("Pod not found for sandbox %s", sandboxID)
		log.Print(errorMsg)
		return "", errorMsg, 1, errors.New(errorMsg)
	}

	pod := pods.Items[0]
	podName := pod.Name

	// Execute command in pod using /bin/sh -c
	execCommand := []string{"/bin/sh", "-c", command}

	parameterCodec := runtime.NewParameterCodec(scheme.Scheme)

	req := m.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(m.namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: execCommand,
			Stdout:  true,
			Stderr:  true,
		}, parameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(m.restConfig, "POST", req.URL())
	if err != nil {
		log.Printf("Failed to create executor for pod %s: %v", podName, err)
		return "", fmt.Sprintf("Failed to create executor: %v", err), 1, err
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	exitCode := 0
	if err != nil {
		if e, ok := err.(interface{ ExitStatus() int }); ok {
			exitCode = e.ExitStatus()
		} else {
			log.Printf("Failed to execute command in pod %s: %v", podName, err)
			return stdout.String(), stderr.String(), 1, err
		}
	}

	log.Printf("Executed command in pod %s: %s... (exit_code=%d)", podName, command[:min(50, len(command))], exitCode)
	return stdout.String(), stderr.String(), exitCode, nil
}

// Helper functions
func getImage(img *string) string {
	if img != nil && *img != "" {
		return *img
	}
	return "python:3.11"
}

func getCPU(cpu *float64) float64 {
	if cpu != nil {
		return *cpu
	}
	return 0.25 // 250m - reasonable default that won't exhaust cluster resources
}

func getMemory(mem *float64) float64 {
	if mem != nil {
		return *mem
	}
	return 0.5 // 512Mi - reasonable default for most sandbox workloads
}

func envToK8sEnv(env map[string]string) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(env))
	for k, v := range env {
		result = append(result, map[string]interface{}{
			"name":  k,
			"value": v,
		})
	}
	return result
}

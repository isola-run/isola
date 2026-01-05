// Package kubernetes provides Kubernetes client functionality for managing Sandbox CRs and pods.
package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

const (
	sandboxGroup      = "sandbox.isola.run"
	sandboxVersion    = "v1alpha1"
	sandboxPlural     = "sandboxes"
	templatePlural    = "sandboxtemplates"
	defaultTimeout    = 600 // seconds
	defaultShutdown   = "Delete"
	runtimeClassName  = "gvisor"
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
	log.Printf("Initializing KubernetesManager for namespace '%s' (runtime_class=%s)",
		m.namespace, runtimeClassName)

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

	podSpec := templateSpec["podTemplate"].(map[string]interface{})["spec"].(map[string]interface{})
	podSpec["runtimeClassName"] = runtimeClassName

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
		if errors.IsAlreadyExists(err) {
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
		if errors.IsAlreadyExists(err) {
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
		if errors.IsNotFound(err) {
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

func (m *Manager) DeleteSandboxCR(ctx context.Context, sandboxID string) (bool, *string) {
	if err := m.Initialize(); err != nil {
		errorMsg := fmt.Sprintf("Failed to initialize: %v", err)
		return false, &errorMsg
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
		if errors.IsNotFound(err) {
			log.Printf("Sandbox CR '%s' already deleted", sandboxName)
			return true, nil
		}
		log.Printf("Failed to delete Sandbox CR '%s': %v", sandboxName, err)
		errorMsg := fmt.Sprintf("API error: %v", err)
		return false, &errorMsg
	}

	log.Printf("Deleted Sandbox CR '%s'", sandboxName)
	return true, nil
}

func (m *Manager) GetPodStatus(ctx context.Context, sandboxID string) (*models.SandboxState, *string, *string) {
	if err := m.Initialize(); err != nil {
		errorMsg := fmt.Sprintf("Failed to initialize: %v", err)
		state := models.SandboxStateError
		return &state, nil, &errorMsg
	}

	log.Printf("Fetching pod status for sandbox '%s'", sandboxID)

	labelSelector := labels.SelectorFromSet(map[string]string{
		"sandbox-id": sandboxID,
	}).String()

	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		log.Printf("Failed to list pods for sandbox %s: %v", sandboxID, err)
		errorMsg := fmt.Sprintf("API error: %v", err)
		state := models.SandboxStateError
		return &state, nil, &errorMsg
	}

	if len(pods.Items) == 0 {
		errorMsg := "Pod not found"
		return nil, nil, &errorMsg
	}

	pod := pods.Items[0]
	phase := pod.Status.Phase
	var ipAddress *string
	if pod.Status.PodIP != "" {
		ipAddress = &pod.Status.PodIP
	}

	// Map Kubernetes phase to SandboxState
	var state models.SandboxState
	switch phase {
	case corev1.PodPending:
		state = models.SandboxStatePending
	case corev1.PodRunning:
		state = models.SandboxStateRunning
	case corev1.PodSucceeded:
		state = models.SandboxStateStopped
	case corev1.PodFailed:
		state = models.SandboxStateError
	default:
		state = models.SandboxStateError
	}

	// Get error reason if failed
	var errorReason *string
	if phase == corev1.PodFailed && len(pod.Status.ContainerStatuses) > 0 {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.State.Terminated != nil {
				reason := containerStatus.State.Terminated.Reason
				errorReason = &reason
				break
			}
		}
	}

	return &state, ipAddress, errorReason
}

// Executes a command in a pod
// Will be implemented in the future in the sidecar
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
		log.Printf(errorMsg)
		return "", errorMsg, 1, fmt.Errorf(errorMsg)
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
			Stdout:   true,
			Stderr:   true,
		}, parameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(m.restConfig, "POST", req.URL())
	if err != nil {
		log.Printf("Failed to create executor for pod %s: %v", podName, err)
		return "", fmt.Sprintf("Failed to create executor: %v", err), 1, err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		log.Printf("Failed to execute command in pod %s: %v", podName, err)
		return stdout.String(), stderr.String(), 1, err
	}

	exitCode := 0

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
	return 1.0
}

func getMemory(mem *float64) float64 {
	if mem != nil {
		return *mem
	}
	return 1.0
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


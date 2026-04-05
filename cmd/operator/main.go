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

package main

import (
	"flag"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/logging"
	"github.com/isola-run/isola/internal/operator/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(sandboxv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var sandboxSidecarImage string
	var runtimeClassName string
	var priorityClassName string
	var rootfssnapshotBucketURL string
	var rootfssnapshotCredentialSecret string
	var rootfssnapshotUploaderImage string
	var rootfssnapshotServiceAccount string
	var imagePullSecretsStr string
	var rootfssnapshotEnabled bool
	var gvisorRunscPath string
	var gvisorRunscRoot string
	var rootfssnapshotHostMountPath string
	var sandboxSidecarImagePullPolicy string
	var rootfssnapshotUploaderImagePullPolicy string
	var logLevel string
	var devMode bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to. Set to 0 to disable.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&sandboxSidecarImage, "sidecar-image", "", "Container image for the sandbox-sidecar (required)")
	flag.StringVar(&runtimeClassName, "runtime-class", "", "Required. Must reference a gVisor/runsc RuntimeClass (e.g. 'gvisor').")
	flag.StringVar(&priorityClassName, "priority-class", "", "PriorityClassName to use for sandbox pods. Empty means use cluster default.")
	flag.StringVar(&rootfssnapshotBucketURL, "rootfssnapshot-bucket-url", "", "Bucket URL for rootfs snapshot storage (e.g., s3://bucket?region=us-east-1)")
	flag.StringVar(&rootfssnapshotCredentialSecret, "rootfssnapshot-credential-secret", "", "Secret name for bucket credentials (optional, uses pod identity if not set)")
	flag.StringVar(&rootfssnapshotUploaderImage, "rootfssnapshot-uploader-image", "", "Container image for the rootfs snapshot uploader")
	flag.StringVar(&rootfssnapshotServiceAccount, "rootfssnapshot-service-account", "", "ServiceAccount for rootfs snapshot jobs")
	flag.StringVar(&imagePullSecretsStr, "image-pull-secrets", "", "Comma-separated list of imagePullSecret names for sandbox pods and rootfs snapshot jobs")
	flag.BoolVar(&rootfssnapshotEnabled, "rootfssnapshot-enabled", false, "Enable rootfs snapshot capability (requires gVisor runtime and privileged operations)")
	flag.StringVar(&gvisorRunscPath, "gvisor-runsc-path", "", "Path to the runsc binary on cluster nodes (for gVisor snapshot support)")
	flag.StringVar(&gvisorRunscRoot, "gvisor-runsc-root", "", "Root directory where runsc stores runtime state (for gVisor snapshot support)")
	flag.StringVar(&rootfssnapshotHostMountPath, "rootfssnapshot-host-mount-path", "", "Host path where snapshot-mounter NFS-mounts snapshot tars (readable by runsc on the node)")
	flag.StringVar(&sandboxSidecarImagePullPolicy, "sidecar-image-pull-policy", "", "ImagePullPolicy for the sandbox-sidecar container (Always, IfNotPresent, Never)")
	flag.StringVar(&rootfssnapshotUploaderImagePullPolicy, "rootfssnapshot-uploader-image-pull-policy", "", "ImagePullPolicy for the rootfs snapshot uploader container (Always, IfNotPresent, Never)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.BoolVar(&devMode, "dev-mode", false, "Enable development mode (text logging)")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   logLevel,
		DevMode: devMode,
	})
	logrLogger := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(logrLogger)
	klog.SetLogger(logrLogger)

	// Fall back to env vars for image refs so Tilt's match_in_env_vars can rewrite them.
	if sandboxSidecarImage == "" {
		sandboxSidecarImage = os.Getenv("ISOLA_SANDBOX_SIDECAR_IMAGE")
	}
	if rootfssnapshotUploaderImage == "" {
		rootfssnapshotUploaderImage = os.Getenv("ISOLA_SNAPSHOT_UPLOADER_IMAGE")
	}

	if runtimeClassName == "" {
		setupLog.Error(nil, "--runtime-class is required")
		os.Exit(1)
	}
	if sandboxSidecarImage == "" {
		setupLog.Error(nil, "--sidecar-image or ISOLA_SANDBOX_SIDECAR_IMAGE env var is required")
		os.Exit(1)
	}
	if rootfssnapshotEnabled {
		if rootfssnapshotUploaderImage == "" {
			setupLog.Error(nil, "--rootfssnapshot-uploader-image or ISOLA_SNAPSHOT_UPLOADER_IMAGE env var is required when --rootfssnapshot-enabled is set")
			os.Exit(1)
		}
		if rootfssnapshotBucketURL == "" {
			setupLog.Error(nil, "--rootfssnapshot-bucket-url is required when --rootfssnapshot-enabled is set")
			os.Exit(1)
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "3e5ad6c4.isola.run",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Parse imagePullSecrets from comma-separated string
	var imagePullSecrets []corev1.LocalObjectReference
	if imagePullSecretsStr != "" {
		for _, name := range strings.Split(imagePullSecretsStr, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				imagePullSecrets = append(imagePullSecrets, corev1.LocalObjectReference{Name: name})
			}
		}
	}

	// SandboxReconciler manages Sandbox resources.
	if err := (&controller.SandboxReconciler{
		Client:                        mgr.GetClient(),
		Scheme:                        mgr.GetScheme(),
		SandboxSidecarImage:           sandboxSidecarImage,
		SandboxSidecarImagePullPolicy: corev1.PullPolicy(sandboxSidecarImagePullPolicy),
		RuntimeClassName:              runtimeClassName,
		PriorityClassName:             priorityClassName,
		ImagePullSecrets:              imagePullSecrets,
		Clock:                         controller.RealClock{},
		RootfsSnapshotHostMountPath:   rootfssnapshotHostMountPath,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Sandbox")
		os.Exit(1)
	}

	// RootfsSnapshotReconciler manages RootfsSnapshot resources.
	// It creates Jobs to snapshot container rootfs and upload to bucket storage.
	if err := (&controller.RootfsSnapshotReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		Clock:                   controller.RealClock{},
		BucketURL:               rootfssnapshotBucketURL,
		CredentialSecretName:    rootfssnapshotCredentialSecret,
		UploaderImage:           rootfssnapshotUploaderImage,
		UploaderImagePullPolicy: corev1.PullPolicy(rootfssnapshotUploaderImagePullPolicy),
		SnapshotServiceAccount:  rootfssnapshotServiceAccount,
		ImagePullSecrets:        imagePullSecrets,
		Enabled:                 rootfssnapshotEnabled,
		GvisorRunscPath:         gvisorRunscPath,
		GvisorRunscRoot:         gvisorRunscRoot,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RootfsSnapshot")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

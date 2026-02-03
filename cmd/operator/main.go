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

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/operator/controller"
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

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	var sandboxSidecarImage string
	var runtimeClassName string
	var priorityClassName string
	var isolaAPINamespace string
	var isolaAPILabelName string
	var snapshotBucketURL string
	var snapshotCredentialSecret string
	var snapshotUploaderImage string
	var snapshotServiceAccount string
	var imagePullSecretsStr string
	var runtimeType string
	var snapshotEnabled bool
	var gvisorRunscPath string
	var gvisorRunscRoot string
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&sandboxSidecarImage, "sidecar-image", "sandbox-sidecar:latest", "Container image for the sandbox-sidecar")
	flag.StringVar(&runtimeClassName, "runtime-class", "gvisor", "RuntimeClassName to use for sandbox pods (e.g. 'gvisor'). Empty means use cluster default.")
	flag.StringVar(&priorityClassName, "priority-class", "", "PriorityClassName to use for sandbox pods. Empty means use cluster default.")
	flag.StringVar(&isolaAPINamespace, "api-namespace", "isola-system", "Namespace where api-gateway runs (for NetworkPolicy ingress rules)")
	flag.StringVar(&isolaAPILabelName, "api-label-name", "api-gateway", "Value of app.kubernetes.io/name label for api-gateway pods")
	flag.StringVar(&snapshotBucketURL, "snapshot-bucket-url", os.Getenv("ISOLA_SNAPSHOT_BUCKET_URL"), "Bucket URL for snapshot storage (e.g., s3://bucket?region=us-east-1)")
	flag.StringVar(&snapshotCredentialSecret, "snapshot-credential-secret", os.Getenv("ISOLA_SNAPSHOT_CREDENTIAL_SECRET"), "Secret name for bucket credentials (optional, uses pod identity if not set)")
	flag.StringVar(&snapshotUploaderImage, "snapshot-uploader-image", os.Getenv("ISOLA_UPLOADER_IMAGE"), "Container image for the snapshot uploader")
	flag.StringVar(&snapshotServiceAccount, "snapshot-service-account", os.Getenv("ISOLA_SNAPSHOT_SERVICE_ACCOUNT"), "ServiceAccount for snapshot jobs")
	flag.StringVar(&imagePullSecretsStr, "image-pull-secrets", os.Getenv("ISOLA_IMAGE_PULL_SECRETS"), "Comma-separated list of imagePullSecret names for sandbox pods and snapshot jobs")
	flag.StringVar(&runtimeType, "runtime-type", os.Getenv("ISOLA_RUNTIME_TYPE"), "Runtime type: 'gvisor' or 'clusterDefault'")
	flag.BoolVar(&snapshotEnabled, "snapshot-enabled", os.Getenv("ISOLA_SNAPSHOT_ENABLED") == "true", "Enable rootfs snapshot capability (requires gVisor runtime and privileged operations)")
	flag.StringVar(&gvisorRunscPath, "gvisor-runsc-path", os.Getenv("ISOLA_GVISOR_RUNSC_PATH"), "Path to the runsc binary on cluster nodes (for gVisor snapshot support)")
	flag.StringVar(&gvisorRunscRoot, "gvisor-runsc-root", os.Getenv("ISOLA_GVISOR_RUNSC_ROOT"), "Root directory where runsc stores runtime state (for gVisor snapshot support)")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if runtimeType != "" && runtimeType != "gvisor" && runtimeType != "clusterDefault" {
		setupLog.Error(nil, "Invalid runtime type", "runtimeType", runtimeType, "allowed", []string{"gvisor", "clusterDefault"})
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics server options
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider protects the metrics endpoint with authn/authz
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If certificate path not specified, controller-runtime generates self-signed certs
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
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
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		SandboxSidecarImage: sandboxSidecarImage,
		RuntimeClassName:    runtimeClassName,
		PriorityClassName:   priorityClassName,
		ImagePullSecrets:    imagePullSecrets,
		Clock:               controller.RealClock{},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Sandbox")
		os.Exit(1)
	}

	// RootfsSnapshotReconciler manages RootfsSnapshot resources.
	// It creates Jobs to snapshot container rootfs and upload to bucket storage.
	if err := (&controller.RootfsSnapshotReconciler{
		Client:                 mgr.GetClient(),
		Scheme:                 mgr.GetScheme(),
		Clock:                  controller.RealClock{},
		BucketURL:              snapshotBucketURL,
		CredentialSecretName:   snapshotCredentialSecret,
		UploaderImage:          snapshotUploaderImage,
		SnapshotServiceAccount: snapshotServiceAccount,
		ImagePullSecrets:       imagePullSecrets,
		Enabled:                snapshotEnabled,
		GvisorRunscPath:        gvisorRunscPath,
		GvisorRunscRoot:        gvisorRunscRoot,
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

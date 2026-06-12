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

// The gvisor-installer runs as a privileged DaemonSet pod and installs the
// gVisor runtime (runsc + containerd shim) on the node it is scheduled to,
// registering it with containerd and labeling the node when healthy.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/isola-run/isola/internal/env"
	gvisorinstaller "github.com/isola-run/isola/internal/gvisor-installer"
	"github.com/isola-run/isola/internal/logging"
	"github.com/isola-run/isola/internal/version"
)

func main() {
	logger := logging.New(logging.Config{
		Level:   env.GetOrDefault("LOG_LEVEL", "info"),
		DevMode: env.GetOrDefault("LOG_DEV_MODE", "false") == "true",
	})

	cfg, err := gvisorinstaller.ConfigFromEnv()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logger.Info("starting gvisor-installer",
		"version", version.Get(),
		"node", cfg.NodeName,
		"gvisorVersion", cfg.Version,
		"handler", cfg.Handler,
		"installDir", cfg.InstallDir)

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Error("loading in-cluster config", "error", err)
		os.Exit(1)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		logger.Error("creating kubernetes client", "error", err)
		os.Exit(1)
	}

	installer := gvisorinstaller.New(
		cfg,
		logger,
		gvisorinstaller.NewNsenterExec(),
		gvisorinstaller.NewCRIClient(cfg.CRISocketPath()),
		gvisorinstaller.NewNodeClient(clientset, cfg.NodeName),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go gvisorinstaller.ServeHealth(ctx, cfg.HealthAddr, installer, logger)
	installer.Run(ctx)
	logger.Info("shutting down")
}

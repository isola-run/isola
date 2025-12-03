#!/bin/bash

set -euo pipefail


install_minikube() {

  if command -v minikube >/dev/null 2>&1; then
    local minikube_version="$(minikube version --short 2>/dev/null || true)"
    echo "minikube already installed (version ${minikube_version})" >&2
    return
  fi

  if ! command -v curl >/dev/null 2>&1; then
    echo "error: curl is required to download minikube" >&2
    exit 1
  fi

  os="$(uname -s)"
  arch="$(uname -m)"
  artifact=""

  case "${os}" in
    Linux)
      case "${arch}" in
        x86_64|amd64) artifact="minikube-linux-amd64" ;;
        arm64|aarch64) artifact="minikube-linux-arm64" ;;
        armv7l|armv7) artifact="minikube-linux-arm" ;;
        *)
          echo "error: unsupported Linux architecture: ${arch}" >&2
          exit 1
          ;;
      esac
      ;;
    Darwin)
      case "${arch}" in
        x86_64) artifact="minikube-darwin-amd64" ;;
        arm64) artifact="minikube-darwin-arm64" ;;
        *)
          echo "error: unsupported macOS architecture: ${arch}" >&2
          exit 1
          ;;
      esac
      ;;
    *)
      echo "error: unsupported operating system: ${os}" >&2
      exit 1
      ;;
  esac

  url="https://github.com/kubernetes/minikube/releases/latest/download/${artifact}"

  echo "Downloading ${artifact}..."
  cd /tmp
  curl -Lo "${artifact}" "${url}"

  echo "Installing minikube to /usr/local/bin (sudo may prompt for your password)..."
  sudo install "${artifact}" /usr/local/bin/minikube

  echo "minikube has been installed successfully."
}

install_helm() {
  if command -v helm >/dev/null 2>&1; then
    local helm_version="$(helm version --short 2>/dev/null || true)"
    echo "Helm already installed (version ${helm_version})" >&2
    return
  else
    echo "Installing Helm..."
    curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-4 | bash
  fi
}

install_cilium() {
  helm repo add cilium https://helm.cilium.io/
  helm repo update
  helm upgrade --install cilium cilium/cilium --version 1.18.4 --namespace kube-system --reuse-values --set operator.replicas=1 --set hubble.relay.enabled=true  --set hubble.ui.enabled=true
}


install_minikube

install_helm

minikube stop || true

minikube start --network-plugin=cni --container-runtime=containerd --docker-opt containerd=/var/run/containerd/containerd.sock --extra-config=kubelet.register-with-taints=node.cilium.io/agent-not-ready=true:NoExecute

install_cilium

cilium hubble enable --ui

minikube addons enable gvisor



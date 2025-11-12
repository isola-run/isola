#!/bin/bash

set -euo pipefail


install_minikube() {

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
  curl -Lo "${artifact}" "${url}"

  echo "Installing minikube to /usr/local/bin (sudo may prompt for your password)..."
  sudo install "${artifact}" /usr/local/bin/minikube

  echo "minikube has been installed successfully."
}

cleanup() {
  rm -f "${artifact}"
}

if command -v minikube >/dev/null 2>&1; then
  echo "minikube already installed" >&2
else
  trap cleanup EXIT
  install_minikube
fi

minikube stop

minikube start --container-runtime=containerd --docker-opt containerd=/var/run/containerd/containerd.sock

minikube addons enable gvisor










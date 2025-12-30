#!/bin/bash

set -euo pipefail

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required to download minikube" >&2
  exit 1
fi

if command -v minikube >/dev/null 2>&1; then
  echo "minikube already installed" >&2
  exit 0
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
tmp_artifact=$(mktemp)

cleanup() {
  rm -f "${tmp_artifact}"
}
trap cleanup EXIT

echo "Downloading ${artifact}..."
curl -Lo "${tmp_artifact}" "${url}"

echo "Installing minikube to /usr/local/bin (sudo may prompt for your password)..."
sudo install "${tmp_artifact}" /usr/local/bin/minikube

echo "minikube has been installed successfully. Start the local cluster with ./deploy.sh"


#!/usr/bin/env bash
# Wraps controller-gen CRD output with Helm conditional templating.
#   $1 - staging dir (raw controller-gen output)
#   $2 - target dir (chart templates)
set -euo pipefail

SRC=$1
DST=$2

rm -rf "$DST"
mkdir -p "$DST"

for f in "$SRC"/*.yaml; do
    name=$(basename "$f")
    {
        echo '{{- if .Values.crds.enabled }}'
        awk '
            /controller-gen\.kubebuilder\.io\/version:/ && !injected {
                print
                print "    {{- if .Values.crds.keep }}"
                print "    \"helm.sh/resource-policy\": keep"
                print "    {{- end }}"
                injected = 1
                next
            }
            { print }
        ' "$f"
        echo '{{- end }}'
    } > "$DST/$name"
done

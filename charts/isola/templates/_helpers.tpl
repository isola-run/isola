{{/*
Expand the name of the chart.
*/}}
{{- define "isola.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "isola.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "isola.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "isola.labels" -}}
helm.sh/chart: {{ include "isola.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end }}

{{/*
Sandbox namespace
*/}}
{{- define "isola.sandboxNamespace" -}}
{{- .Values.sandboxNamespace }}
{{- end }}

{{/*
Build a full image reference from registry, repository, and tag/digest
*/}}
{{- define "isola.image" -}}
{{- $registry := .imageConfig.registry -}}
{{- if .global.imageRegistry -}}
{{- $registry = .global.imageRegistry -}}
{{- end -}}
{{- if .imageConfig.digest -}}
{{- if $registry -}}
{{- printf "%s/%s@%s" $registry .imageConfig.repository .imageConfig.digest -}}
{{- else -}}
{{- printf "%s@%s" .imageConfig.repository .imageConfig.digest -}}
{{- end -}}
{{- else -}}
{{- $tag := .imageConfig.tag | default .appVersion -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry .imageConfig.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .imageConfig.repository $tag -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
Return imagePullSecrets
*/}}
{{- define "isola.imagePullSecrets" -}}
{{- $secrets := .Values.global.imagePullSecrets -}}
{{- if $secrets -}}
imagePullSecrets:
{{- range $secrets }}
  - name: {{ .name }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Return imagePullSecret names as comma-separated string
*/}}
{{- define "isola.imagePullSecretNames" -}}
{{- $secrets := .Values.global.imagePullSecrets -}}
{{- $names := list -}}
{{- range $secrets -}}
{{- $names = append $names .name -}}
{{- end -}}
{{- join "," $names -}}
{{- end }}

{{/* ==========================================================================
   Operator Helpers
   ========================================================================== */}}

{{/*
Operator fullname
*/}}
{{- define "isola.operator.fullname" -}}
{{- printf "%s-operator" (include "isola.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Operator labels
*/}}
{{- define "isola.operator.labels" -}}
{{ include "isola.labels" . }}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end }}

{{/*
Operator selector labels
*/}}
{{- define "isola.operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end }}

{{/*
Operator service account name
*/}}
{{- define "isola.operator.serviceAccountName" -}}
{{- if .Values.operator.serviceAccount.create }}
{{- default (include "isola.operator.fullname" .) .Values.operator.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.operator.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Operator image
*/}}
{{- define "isola.operator.image" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Sidecar image
*/}}
{{- define "isola.operator.sidecarImage" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sidecar.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Runtime type selector
*/}}
{{- define "isola.operator.runtimeType" -}}
{{- .Values.operator.sandboxRuntime.type -}}
{{- end }}

{{/*
RuntimeClassName for sandbox pods (conditional on runtime type)
*/}}
{{- define "isola.operator.runtimeClassName" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.runtimeClassName -}}
{{- else -}}
{{- "" -}}
{{- end -}}
{{- end }}

{{/*
Snapshot enabled flag (only true for gvisor with snapshot.enabled)
*/}}
{{- define "isola.operator.snapshotEnabled" -}}
{{- if and (eq .Values.operator.sandboxRuntime.type "gvisor") (.Values.operator.sandboxRuntime.gvisor.snapshot.enabled) -}}
{{- "true" -}}
{{- else -}}
{{- "false" -}}
{{- end -}}
{{- end }}

{{/*
gVisor runsc binary path (only used when snapshot enabled)
*/}}
{{- define "isola.operator.gvisorRunscPath" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.snapshot.runsc.binaryPath -}}
{{- end -}}
{{- end }}

{{/*
gVisor runsc root directory (only used when snapshot enabled)
*/}}
{{- define "isola.operator.gvisorRunscRoot" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.snapshot.runsc.rootDir -}}
{{- end -}}
{{- end }}

{{/*
Snapshot bucket URL (from gvisor snapshot config)
*/}}
{{- define "isola.operator.storageBucketUrl" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.snapshot.storage.bucketUrl -}}
{{- end -}}
{{- end }}

{{/*
Uploader image (from gvisor snapshot config)
*/}}
{{- define "isola.operator.uploaderImage" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxRuntime.gvisor.snapshot.uploader.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end -}}
{{- end }}

{{/*
Storage credential secret name
*/}}
{{- define "isola.operator.snapshotCredentialSecretName" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- if .Values.operator.sandboxRuntime.gvisor.snapshot.storage.credentials.existingSecret -}}
{{- .Values.operator.sandboxRuntime.gvisor.snapshot.storage.credentials.existingSecret -}}
{{- else if and .Values.operator.sandboxRuntime.gvisor.snapshot.storage.credentials.accessKeyId .Values.operator.sandboxRuntime.gvisor.snapshot.storage.credentials.secretAccessKey -}}
{{- printf "%s-snapshot-credentials" (include "isola.operator.fullname" .) -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
Snapshot service account name (from gvisor snapshot config)
*/}}
{{- define "isola.operator.snapshotServiceAccountName" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- if .Values.operator.sandboxRuntime.gvisor.snapshot.serviceAccount.create -}}
{{- .Values.operator.sandboxRuntime.gvisor.snapshot.serviceAccount.name | default (printf "%s-snapshot" (include "isola.operator.fullname" .)) -}}
{{- else -}}
{{- .Values.operator.sandboxRuntime.gvisor.snapshot.serviceAccount.name -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/* ==========================================================================
   API Gateway Helpers
   ========================================================================== */}}

{{/*
API Gateway fullname
*/}}
{{- define "isola.apiGateway.fullname" -}}
{{- printf "%s-api-gateway" (include "isola.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
API Gateway labels
*/}}
{{- define "isola.apiGateway.labels" -}}
{{ include "isola.labels" . }}
app.kubernetes.io/name: {{ include "isola.name" . }}-api-gateway
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api-gateway
{{- end }}

{{/*
API Gateway selector labels
*/}}
{{- define "isola.apiGateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola.name" . }}-api-gateway
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api-gateway
{{- end }}

{{/*
API Gateway service account name
*/}}
{{- define "isola.apiGateway.serviceAccountName" -}}
{{- if .Values.apiGateway.serviceAccount.create }}
{{- default (include "isola.apiGateway.fullname" .) .Values.apiGateway.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.apiGateway.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
API Gateway image
*/}}
{{- define "isola.apiGateway.image" -}}
{{- include "isola.image" (dict "imageConfig" .Values.apiGateway.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

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
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Operator selector labels
*/}}
{{- define "isola.operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end }}

{{/*
Operator labels (common + selector)
*/}}
{{- define "isola.operator.labels" -}}
{{ include "isola.labels" . }}
{{ include "isola.operator.selectorLabels" . }}
{{- end }}

{{/*
API selector labels
*/}}
{{- define "isola.api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api
{{- end }}

{{/*
API labels (common + selector)
*/}}
{{- define "isola.api.labels" -}}
{{ include "isola.labels" . }}
{{ include "isola.api.selectorLabels" . }}
{{- end }}

{{/*
Operator service account name
*/}}
{{- define "isola.operator.serviceAccountName" -}}
{{- if .Values.operator.serviceAccount.create }}
{{- default (printf "%s-operator" (include "isola.fullname" .)) .Values.operator.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.operator.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
API service account name
*/}}
{{- define "isola.api.serviceAccountName" -}}
{{- if .Values.api.serviceAccount.create }}
{{- default (printf "%s-api" (include "isola.fullname" .)) .Values.api.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.api.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Snapshot service account name
*/}}
{{- define "isola.snapshot.serviceAccountName" -}}
{{- .Values.operator.snapshot.serviceAccount.name | default "isola-snapshot-job" }}
{{- end }}

{{/*
Storage credential secret name
Returns empty string if no credentials are configured (pod identity mode)
*/}}
{{- define "isola.storageCredentialSecretName" -}}
{{- if .Values.storage.credentials.existingSecret }}
{{- .Values.storage.credentials.existingSecret }}
{{- else if and .Values.storage.credentials.accessKeyId .Values.storage.credentials.secretAccessKey }}
{{- printf "%s-storage-credentials" (include "isola.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Build a full image reference from registry, repository, and tag
Usage: {{ include "isola.image" (dict "imageConfig" .Values.operator.image "global" .Values.global "appVersion" .Chart.AppVersion) }}
*/}}
{{- define "isola.image" -}}
{{- $registry := .imageConfig.registry -}}
{{- if .global.imageRegistry -}}
{{- $registry = .global.imageRegistry -}}
{{- end -}}
{{- $tag := .imageConfig.tag | default .appVersion -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry .imageConfig.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .imageConfig.repository $tag -}}
{{- end -}}
{{- end }}

{{/*
Operator image reference
*/}}
{{- define "isola.operator.image" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Sidecar image reference
*/}}
{{- define "isola.sidecar.image" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sidecar.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Uploader image reference
*/}}
{{- define "isola.uploader.image" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.snapshot.uploader.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
API image reference
*/}}
{{- define "isola.api.image" -}}
{{- include "isola.image" (dict "imageConfig" .Values.api.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
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
Return imagePullSecret names as comma-separated string (for passing to operator)
*/}}
{{- define "isola.imagePullSecretNames" -}}
{{- $secrets := .Values.global.imagePullSecrets -}}
{{- $names := list -}}
{{- range $secrets -}}
{{- $names = append $names .name -}}
{{- end -}}
{{- join "," $names -}}
{{- end }}

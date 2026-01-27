{{/*
Expand the name of the chart.
*/}}
{{- define "isola-api.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "isola-api.fullname" -}}
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
{{- define "isola-api.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "isola-api.labels" -}}
helm.sh/chart: {{ include "isola-api.chart" . }}
{{ include "isola-api.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "isola-api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola-api.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "isola-api.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "isola-api.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Determine the credential secret name
Returns empty string if no credentials are configured (pod identity mode)
*/}}
{{- define "isola-api.credentialSecretName" -}}
{{- if .Values.storage.credentials.existingSecret }}
{{- .Values.storage.credentials.existingSecret }}
{{- else if and .Values.storage.credentials.accessKeyId .Values.storage.credentials.secretAccessKey }}
{{- printf "%s-storage-credentials" (include "isola-api.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Build a full image reference from registry, repository, and tag
Usage: {{ include "isola-api.image" (dict "imageConfig" .Values.image "global" .Values.global "appVersion" .Chart.AppVersion) }}
*/}}
{{- define "isola-api.image" -}}
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
Build the API image reference
*/}}
{{- define "isola-api.apiImage" -}}
{{- include "isola-api.image" (dict "imageConfig" .Values.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Return imagePullSecrets (global takes precedence)
*/}}
{{- define "isola-api.imagePullSecrets" -}}
{{- $secrets := .Values.global.imagePullSecrets -}}
{{- if $secrets -}}
imagePullSecrets:
{{- range $secrets }}
  - name: {{ .name }}
{{- end }}
{{- end }}
{{- end }}

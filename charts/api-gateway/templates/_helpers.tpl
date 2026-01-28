{{/*
Expand the name of the chart.
*/}}
{{- define "api-gateway.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "api-gateway.fullname" -}}
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
{{- define "api-gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "api-gateway.labels" -}}
helm.sh/chart: {{ include "api-gateway.chart" . }}
{{ include "api-gateway.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "api-gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "api-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "api-gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "api-gateway.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Determine the credential secret name
Returns empty string if no credentials are configured (pod identity mode)
*/}}
{{- define "api-gateway.credentialSecretName" -}}
{{- if .Values.storage.credentials.existingSecret }}
{{- .Values.storage.credentials.existingSecret }}
{{- else if and .Values.storage.credentials.accessKeyId .Values.storage.credentials.secretAccessKey }}
{{- printf "%s-storage-credentials" (include "api-gateway.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Build a full image reference from registry, repository, and tag
Usage: {{ include "api-gateway.image" (dict "imageConfig" .Values.image "global" .Values.global "appVersion" .Chart.AppVersion) }}
*/}}
{{- define "api-gateway.image" -}}
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
{{- define "api-gateway.apiImage" -}}
{{- include "api-gateway.image" (dict "imageConfig" .Values.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Return imagePullSecrets (global takes precedence)
*/}}
{{- define "api-gateway.imagePullSecrets" -}}
{{- $secrets := .Values.global.imagePullSecrets -}}
{{- if $secrets -}}
imagePullSecrets:
{{- range $secrets }}
  - name: {{ .name }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Get sandboxNamespace (global takes precedence over local for umbrella chart support)
*/}}
{{- define "api-gateway.sandboxNamespace" -}}
{{- if .Values.global.sandboxNamespace -}}
{{- .Values.global.sandboxNamespace -}}
{{- else -}}
{{- .Values.sandboxNamespace -}}
{{- end -}}
{{- end }}

{{/*
Get storage bucket URL (global takes precedence over local for umbrella chart support)
*/}}
{{- define "api-gateway.storageBucketUrl" -}}
{{- if .Values.global.storage -}}
{{- .Values.global.storage.bucketUrl -}}
{{- else -}}
{{- .Values.storage.bucketUrl -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials existingSecret (global takes precedence)
*/}}
{{- define "api-gateway.storageCredentialsExistingSecret" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.existingSecret -}}
{{- else -}}
{{- .Values.storage.credentials.existingSecret -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials accessKeyId (global takes precedence)
*/}}
{{- define "api-gateway.storageCredentialsAccessKeyId" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.accessKeyId -}}
{{- else -}}
{{- .Values.storage.credentials.accessKeyId -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials secretAccessKey (global takes precedence)
*/}}
{{- define "api-gateway.storageCredentialsSecretAccessKey" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.secretAccessKey -}}
{{- else -}}
{{- .Values.storage.credentials.secretAccessKey -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials region (global takes precedence)
*/}}
{{- define "api-gateway.storageCredentialsRegion" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.region -}}
{{- else -}}
{{- .Values.storage.credentials.region -}}
{{- end -}}
{{- end }}

{{/*
Determine the credential secret name (updated for global support)
Returns empty string if no credentials are configured (pod identity mode)
*/}}
{{- define "api-gateway.credentialSecretNameResolved" -}}
{{- $existingSecret := include "api-gateway.storageCredentialsExistingSecret" . -}}
{{- $accessKeyId := include "api-gateway.storageCredentialsAccessKeyId" . -}}
{{- $secretAccessKey := include "api-gateway.storageCredentialsSecretAccessKey" . -}}
{{- if $existingSecret -}}
{{- $existingSecret -}}
{{- else if and $accessKeyId $secretAccessKey -}}
{{- printf "%s-storage-credentials" (include "api-gateway.fullname" .) -}}
{{- end -}}
{{- end }}

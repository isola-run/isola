{{/*
Expand the name of the chart.
*/}}
{{- define "isola-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "isola-operator.fullname" -}}
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
{{- define "isola-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "isola-operator.labels" -}}
helm.sh/chart: {{ include "isola-operator.chart" . }}
{{ include "isola-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "isola-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "isola-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "isola-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Build a full image reference from registry, repository, and tag
Usage: {{ include "isola-operator.image" (dict "imageConfig" .Values.image "global" .Values.global "appVersion" .Chart.AppVersion) }}
*/}}
{{- define "isola-operator.image" -}}
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
Build the operator image reference
*/}}
{{- define "isola-operator.operatorImage" -}}
{{- include "isola-operator.image" (dict "imageConfig" .Values.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Build the sidecar image reference
*/}}
{{- define "isola-operator.sidecarImage" -}}
{{- include "isola-operator.image" (dict "imageConfig" .Values.sidecar.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Build the uploader image reference
*/}}
{{- define "isola-operator.uploaderImage" -}}
{{- include "isola-operator.image" (dict "imageConfig" .Values.snapshot.uploader.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Return imagePullSecrets (global takes precedence)
*/}}
{{- define "isola-operator.imagePullSecrets" -}}
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
{{- define "isola-operator.imagePullSecretNames" -}}
{{- $secrets := .Values.global.imagePullSecrets -}}
{{- $names := list -}}
{{- range $secrets -}}
{{- $names = append $names .name -}}
{{- end -}}
{{- join "," $names -}}
{{- end }}

{{/*
Get sandboxNamespace (global takes precedence over local for umbrella chart support)
*/}}
{{- define "isola-operator.sandboxNamespace" -}}
{{- if .Values.global.sandboxNamespace -}}
{{- .Values.global.sandboxNamespace -}}
{{- else -}}
{{- .Values.sandboxNamespace -}}
{{- end -}}
{{- end }}

{{/*
Get storage bucket URL (global takes precedence over local for umbrella chart support)
*/}}
{{- define "isola-operator.storageBucketUrl" -}}
{{- if .Values.global.storage -}}
{{- .Values.global.storage.bucketUrl -}}
{{- else -}}
{{- .Values.snapshot.storage.bucketUrl -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials existingSecret (global takes precedence)
*/}}
{{- define "isola-operator.storageCredentialsExistingSecret" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.existingSecret -}}
{{- else -}}
{{- .Values.snapshot.storage.credentials.existingSecret -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials accessKeyId (global takes precedence)
*/}}
{{- define "isola-operator.storageCredentialsAccessKeyId" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.accessKeyId -}}
{{- else -}}
{{- .Values.snapshot.storage.credentials.accessKeyId -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials secretAccessKey (global takes precedence)
*/}}
{{- define "isola-operator.storageCredentialsSecretAccessKey" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.secretAccessKey -}}
{{- else -}}
{{- .Values.snapshot.storage.credentials.secretAccessKey -}}
{{- end -}}
{{- end }}

{{/*
Get storage credentials region (global takes precedence)
*/}}
{{- define "isola-operator.storageCredentialsRegion" -}}
{{- if and .Values.global.storage .Values.global.storage.credentials -}}
{{- .Values.global.storage.credentials.region -}}
{{- else -}}
{{- .Values.snapshot.storage.credentials.region -}}
{{- end -}}
{{- end }}

{{/*
Determine the credential secret name for snapshots (updated for global support)
Returns empty string if no credentials are configured (pod identity mode)
*/}}
{{- define "isola-operator.snapshotCredentialSecretNameResolved" -}}
{{- $existingSecret := include "isola-operator.storageCredentialsExistingSecret" . -}}
{{- $accessKeyId := include "isola-operator.storageCredentialsAccessKeyId" . -}}
{{- $secretAccessKey := include "isola-operator.storageCredentialsSecretAccessKey" . -}}
{{- if $existingSecret -}}
{{- $existingSecret -}}
{{- else if and $accessKeyId $secretAccessKey -}}
{{- printf "%s-snapshot-credentials" (include "isola-operator.fullname" .) -}}
{{- end -}}
{{- end }}

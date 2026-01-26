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
Determine the credential secret name for snapshots
Returns empty string if no credentials are configured (pod identity mode)
*/}}
{{- define "isola-operator.snapshotCredentialSecretName" -}}
{{- if .Values.snapshot.storage.credentials.existingSecret }}
{{- .Values.snapshot.storage.credentials.existingSecret }}
{{- else if and .Values.snapshot.storage.credentials.accessKeyId .Values.snapshot.storage.credentials.secretAccessKey }}
{{- printf "%s-snapshot-credentials" (include "isola-operator.fullname" .) }}
{{- end }}
{{- end }}

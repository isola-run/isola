{{/*
Create a default fully qualified app name.
*/}}
{{- define "isola-gw.fullname" -}}
{{- if contains "isola-gw" .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name "isola-gw" | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Selector labels for matching pods
*/}}
{{- define "isola-gw.selectorLabels" -}}
app.kubernetes.io/name: isola-gw
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}


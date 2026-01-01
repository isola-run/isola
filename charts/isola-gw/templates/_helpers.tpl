{{/*
Selector labels for matching pods
*/}}
{{- define "isola-gw.selectorLabels" -}}
app.kubernetes.io/name: isola-gw
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}


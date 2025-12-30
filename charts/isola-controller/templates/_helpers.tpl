{{/*
Selector labels for matching pods
*/}}
{{- define "isola-controller.selectorLabels" -}}
app.kubernetes.io/name: isola-controller
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

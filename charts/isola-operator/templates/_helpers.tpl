{{/*
Selector labels for matching pods
*/}}
{{- define "isola-operator.selectorLabels" -}}
app.kubernetes.io/name: isola-operator
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

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
app.kubernetes.io/part-of: isola
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end }}

{{/*
Sandbox namespace
*/}}
{{- define "isola.sandboxNamespace" -}}
{{- .Values.sandboxNamespace.name }}
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
   Values Validation
   ========================================================================== */}}

{{/*
Aggregate all validation errors and fail with a single message.
Invoked from the operator deployment template to catch misconfigurations at install time.
*/}}
{{- define "isola.validateValues" -}}
{{- $messages := list -}}
{{- $messages = append $messages (include "isola.validateValues.sandboxNamespace" .) -}}
{{- $messages = append $messages (include "isola.validateValues.runtimeType" .) -}}
{{- $messages = append $messages (include "isola.validateValues.rootfssnapshot" .) -}}
{{- $messages = append $messages (include "isola.validateValues.rootfssnapshotCredentials" .) -}}
{{- $messages = without $messages "" -}}
{{- $message := join "\n" $messages -}}
{{- if $message -}}
{{-   printf "\nVALUES VALIDATION:\n%s" $message | fail -}}
{{- end -}}
{{- end -}}

{{- define "isola.validateValues.sandboxNamespace" -}}
{{- if not .Values.sandboxNamespace.name -}}
isola: sandboxNamespace.name
    sandboxNamespace.name must not be empty.
    This is the namespace where sandbox pods, network policies, and related resources are created.
{{- end -}}
{{- end -}}

{{- define "isola.validateValues.runtimeType" -}}
{{- $validTypes := list "gvisor" "clusterDefault" -}}
{{- if not (has .Values.operator.sandboxRuntime.type $validTypes) -}}
isola: operator.sandboxRuntime.type
    Invalid runtime type "{{ .Values.operator.sandboxRuntime.type }}".
    Must be one of: gvisor, clusterDefault
{{- end -}}
{{- end -}}

{{- define "isola.validateValues.rootfssnapshot" -}}
{{- if eq (include "isola.operator.rootfssnapshotEnabled" .) "true" -}}
{{- if not (include "isola.operator.storageBucketUrl" .) -}}
isola: operator.sandboxRuntime.gvisor.rootfssnapshot.storage.bucketUrl
    When rootfssnapshot is enabled (operator.sandboxRuntime.gvisor.rootfssnapshot.enabled=true),
    you must provide a storage bucket URL.
    Set operator.sandboxRuntime.gvisor.rootfssnapshot.storage.bucketUrl to a valid bucket URL.
    Examples: s3://bucket-name?region=us-east-1, gs://bucket-name, azblob://container-name
    Alternatively, disable rootfssnapshot: operator.sandboxRuntime.gvisor.rootfssnapshot.enabled=false
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "isola.validateValues.rootfssnapshotCredentials" -}}
{{- if eq (include "isola.operator.rootfssnapshotEnabled" .) "true" -}}
{{- $creds := .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.storage.credentials -}}
{{- if or (and $creds.accessKeyId (not $creds.secretAccessKey)) (and (not $creds.accessKeyId) $creds.secretAccessKey) -}}
isola: operator.sandboxRuntime.gvisor.rootfssnapshot.storage.credentials
    Only one of accessKeyId/secretAccessKey is set. You must provide both together,
    or use existingSecret, or leave both empty to use pod/workload identity (e.g. IRSA).
{{- end -}}
{{- end -}}
{{- end -}}

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
Operator labels (component labels are applied per-resource and not here)
*/}}
{{- define "isola.operator.labels" -}}
{{ include "isola.labels" . }}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Operator selector labels
*/}}
{{- define "isola.operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
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
Sandbox sidecar image
*/}}
{{- define "isola.operator.sandboxSidecarImage" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxSidecar.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
RuntimeClassName for sandbox pods (pod.spec.runtimeClassName).
Returns the gVisor runtimeClassName when type is "gvisor".
Empty for "clusterDefault" (pods use cluster default runtime).
*/}}
{{- define "isola.operator.runtimeClassName" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.runtimeClassName -}}
{{- end -}}
{{- end }}

{{/*
RootfsSnapshot enabled flag (only true for gvisor with rootfssnapshot.enabled)
*/}}
{{- define "isola.operator.rootfssnapshotEnabled" -}}
{{- if and (eq .Values.operator.sandboxRuntime.type "gvisor") (.Values.operator.sandboxRuntime.gvisor.rootfssnapshot.enabled) -}}
{{- "true" -}}
{{- else -}}
{{- "false" -}}
{{- end -}}
{{- end }}

{{/*
gVisor runsc binary path (only used when rootfssnapshot enabled)
*/}}
{{- define "isola.operator.gvisorRunscPath" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.runsc.binaryPath -}}
{{- end -}}
{{- end }}

{{/*
gVisor runsc root directory (only used when rootfssnapshot enabled)
*/}}
{{- define "isola.operator.gvisorRunscRoot" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.runsc.rootDir -}}
{{- end -}}
{{- end }}

{{/*
RootfsSnapshot bucket URL (from gvisor rootfssnapshot config)
*/}}
{{- define "isola.operator.storageBucketUrl" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.storage.bucketUrl -}}
{{- end -}}
{{- end }}

{{/*
Uploader image (from gvisor rootfssnapshot config)
*/}}
{{- define "isola.operator.uploaderImage" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.uploader.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end -}}
{{- end }}

{{/*
Storage credential secret name
*/}}
{{- define "isola.operator.rootfssnapshotCredentialSecretName" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- if .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.storage.credentials.existingSecret -}}
{{- .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.storage.credentials.existingSecret -}}
{{- else if and .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.storage.credentials.accessKeyId .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.storage.credentials.secretAccessKey -}}
{{- printf "%s-rootfssnapshot-credentials" (include "isola.operator.fullname" .) -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
RootfsSnapshot service account name (from gvisor rootfssnapshot config)
*/}}
{{- define "isola.operator.rootfssnapshotServiceAccountName" -}}
{{- if eq .Values.operator.sandboxRuntime.type "gvisor" -}}
{{- if .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.serviceAccount.create -}}
{{- .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.serviceAccount.name | default (printf "%s-rootfssnapshot" (include "isola.operator.fullname" .)) -}}
{{- else -}}
{{- default "default" .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.serviceAccount.name -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
Snapshot mounter service account name (DaemonSet in release namespace, separate from snapshot job SA)
*/}}
{{- define "isola.operator.snapshotMounterServiceAccountName" -}}
{{- if eq (include "isola.operator.rootfssnapshotEnabled" .) "true" -}}
{{- $sa := .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.snapshotMounter.serviceAccount -}}
{{- if $sa.create -}}
{{- $sa.name | default (printf "%s-snapshot-mounter" (include "isola.operator.fullname" .)) -}}
{{- else -}}
{{- default "default" $sa.name -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
RootfsSnapshot host mount path for restore (only when rootfssnapshot enabled)
*/}}
{{- define "isola.operator.rootfssnapshotHostMountPath" -}}
{{- if eq (include "isola.operator.rootfssnapshotEnabled" .) "true" -}}
{{- .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.snapshotMounter.hostMountPath -}}
{{- end -}}
{{- end }}

{{/*
rclone image for rootfssnapshot NFS server container (unprivileged)
*/}}
{{- define "isola.operator.rootfssnapshotRcloneImage" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.snapshotMounter.rclone.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Mounter image for rootfssnapshot NFS mount container (privileged, minimal)
*/}}
{{- define "isola.operator.rootfssnapshotMounterImage" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxRuntime.gvisor.rootfssnapshot.snapshotMounter.mounter.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/* ==========================================================================
   Snapshot Mounter Helpers
   ========================================================================== */}}

{{/*
Snapshot mounter fullname
*/}}
{{- define "isola.snapshotMounter.fullname" -}}
{{- printf "%s-snapshot-mounter" (include "isola.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Snapshot mounter labels
*/}}
{{- define "isola.snapshotMounter.labels" -}}
{{ include "isola.labels" . }}
app.kubernetes.io/name: {{ include "isola.name" . }}-snapshot-mounter
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: snapshot-mounter
{{- end }}

{{/*
Snapshot mounter selector labels
*/}}
{{- define "isola.snapshotMounter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola.name" . }}-snapshot-mounter
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: snapshot-mounter
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

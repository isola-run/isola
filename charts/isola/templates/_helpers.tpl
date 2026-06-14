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
{{- $messages = append $messages (include "isola.validateValues.runtimeClassName" .) -}}
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

{{- define "isola.validateValues.runtimeClassName" -}}
{{- if not .Values.operator.sandboxRuntime.runtimeClassName -}}
isola: operator.sandboxRuntime.runtimeClassName
    runtimeClassName must not be empty. It must reference a gVisor/runsc RuntimeClass in your cluster.
{{- end -}}
{{- end -}}

{{- define "isola.validateValues.rootfssnapshot" -}}
{{- if eq (include "isola.operator.rootfssnapshotEnabled" .) "true" -}}
{{- $storage := .Values.operator.sandboxRuntime.rootfssnapshot.storage -}}
{{- $validTypes := list "s3" "gcs" "azure" -}}
{{- if not (has $storage.type $validTypes) -}}
isola: operator.sandboxRuntime.rootfssnapshot.storage.type
    When rootfssnapshot is enabled, storage.type must be one of: s3, gcs, azure.
    Current value: "{{ $storage.type }}"
{{- else if and (eq $storage.type "s3") (not $storage.s3.bucket) -}}
isola: operator.sandboxRuntime.rootfssnapshot.storage.s3.bucket
    storage.type is "s3" but s3.bucket is empty.
{{- else if and (eq $storage.type "gcs") (not $storage.gcs.bucket) -}}
isola: operator.sandboxRuntime.rootfssnapshot.storage.gcs.bucket
    storage.type is "gcs" but gcs.bucket is empty.
{{- else if and (eq $storage.type "azure") (not $storage.azure.container) -}}
isola: operator.sandboxRuntime.rootfssnapshot.storage.azure.container
    storage.type is "azure" but azure.container is empty.
{{- else if and (eq $storage.type "azure") (not $storage.azure.storageAccount) -}}
isola: operator.sandboxRuntime.rootfssnapshot.storage.azure.storageAccount
    storage.type is "azure" but azure.storageAccount is empty.
    Azure requires the storage account name even with workload identity.
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "isola.validateValues.rootfssnapshotCredentials" -}}
{{- if eq (include "isola.operator.rootfssnapshotEnabled" .) "true" -}}
{{- $storage := .Values.operator.sandboxRuntime.rootfssnapshot.storage -}}
{{- if eq $storage.type "s3" -}}
{{- if or (and $storage.s3.accessKeyId (not $storage.s3.secretAccessKey)) (and (not $storage.s3.accessKeyId) $storage.s3.secretAccessKey) -}}
isola: operator.sandboxRuntime.rootfssnapshot.storage.s3
    Only one of s3.accessKeyId/s3.secretAccessKey is set. You must provide both together,
    or use uploader.existingSecret/snapshotMounter.existingSecret,
    or leave both empty to use pod/workload identity (e.g. IRSA).
{{- end -}}
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
Operator labels
*/}}
{{- define "isola.operator.labels" -}}
{{ include "isola.labels" . }}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end }}

{{/*
Operator selector labels
*/}}
{{- define "isola.operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end }}

{{/*
Sandbox infrastructure labels (network policies, namespace)
*/}}
{{- define "isola.sandbox.labels" -}}
{{ include "isola.labels" . }}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: sandbox
{{- end }}

{{/*
RootfsSnapshot labels (snapshot jobs, credentials)
*/}}
{{- define "isola.rootfssnapshot.labels" -}}
{{ include "isola.labels" . }}
app.kubernetes.io/name: {{ include "isola.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: rootfssnapshot
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
*/}}
{{- define "isola.operator.runtimeClassName" -}}
{{- .Values.operator.sandboxRuntime.runtimeClassName -}}
{{- end }}

{{/*
RootfsSnapshot enabled flag
*/}}
{{- define "isola.operator.rootfssnapshotEnabled" -}}
{{- if .Values.operator.sandboxRuntime.rootfssnapshot.enabled -}}
{{- "true" -}}
{{- else -}}
{{- "false" -}}
{{- end -}}
{{- end }}

{{/*
gVisor runsc binary path (only used when rootfssnapshot enabled)
*/}}
{{- define "isola.operator.gvisorRunscPath" -}}
{{- .Values.operator.sandboxRuntime.rootfssnapshot.runsc.binaryPath -}}
{{- end }}

{{/*
gVisor runsc root directory (only used when rootfssnapshot enabled)
*/}}
{{- define "isola.operator.gvisorRunscRoot" -}}
{{- .Values.operator.sandboxRuntime.rootfssnapshot.runsc.rootDir -}}
{{- end }}

{{/*
Construct gocloud.dev bucket URL from typed storage config.
  s3    -> s3://bucket?region=...&endpoint=...&use_path_style=true
  gcs   -> gs://bucket
  azure -> azblob://container?storage_account=...
*/}}
{{- define "isola.operator.storageBucketUrl" -}}
{{- $storage := .Values.operator.sandboxRuntime.rootfssnapshot.storage -}}
{{- if eq $storage.type "s3" -}}
{{- $params := list -}}
{{- if $storage.s3.region -}}
{{- $params = append $params (printf "region=%s" $storage.s3.region) -}}
{{- end -}}
{{- if $storage.s3.endpoint -}}
{{- $params = append $params (printf "endpoint=%s" $storage.s3.endpoint) -}}
{{- end -}}
{{- if $storage.s3.forcePathStyle -}}
{{- $params = append $params "use_path_style=true" -}}
{{- end -}}
{{- if $params -}}
{{- printf "s3://%s?%s" $storage.s3.bucket (join "&" $params) -}}
{{- else -}}
{{- printf "s3://%s" $storage.s3.bucket -}}
{{- end -}}
{{- else if eq $storage.type "gcs" -}}
{{- printf "gs://%s" $storage.gcs.bucket -}}
{{- else if eq $storage.type "azure" -}}
{{- printf "azblob://%s?storage_account=%s" $storage.azure.container $storage.azure.storageAccount -}}
{{- end -}}
{{- end }}

{{/*
Snapshot-uploader image (from rootfssnapshot config)
*/}}
{{- define "isola.operator.uploaderImage" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxRuntime.rootfssnapshot.uploader.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Rclone remote string for the snapshot mounter (e.g. ":s3:bucket/rootfssnapshots/")
*/}}
{{- define "isola.operator.rcloneRemote" -}}
{{- $storage := .Values.operator.sandboxRuntime.rootfssnapshot.storage -}}
{{- if eq $storage.type "s3" -}}
{{- printf ":s3:%s/rootfssnapshots/" $storage.s3.bucket -}}
{{- else if eq $storage.type "gcs" -}}
{{- printf ":gcs:%s/rootfssnapshots/" $storage.gcs.bucket -}}
{{- else if eq $storage.type "azure" -}}
{{- printf ":azureblob:%s/rootfssnapshots/" $storage.azure.container -}}
{{- end -}}
{{- end }}

{{/*
Whether chart-managed credentials are provided for the current storage type.
S3: both accessKeyId and secretAccessKey set. Azure: accountKey set. GCS: never (no inline creds).
*/}}
{{- define "isola.operator.hasChartManagedCredentials" -}}
{{- $storage := .Values.operator.sandboxRuntime.rootfssnapshot.storage -}}
{{- if or (and (eq $storage.type "s3") $storage.s3.accessKeyId $storage.s3.secretAccessKey) (and (eq $storage.type "azure") $storage.azure.accountKey) -}}
{{- "true" -}}
{{- else -}}
{{- "false" -}}
{{- end -}}
{{- end }}

{{/*
Snapshot-uploader credential secret name (sandbox namespace).
Resolution: uploader.existingSecret > chart-managed > empty (workload identity)
*/}}
{{- define "isola.operator.uploaderCredentialSecretName" -}}
{{- $uploader := .Values.operator.sandboxRuntime.rootfssnapshot.uploader -}}
{{- if $uploader.existingSecret -}}
{{- $uploader.existingSecret -}}
{{- else if eq (include "isola.operator.hasChartManagedCredentials" .) "true" -}}
{{- printf "%s-snapshot-uploader-credentials" (include "isola.operator.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Mounter credential secret name (release namespace).
Resolution: snapshotMounter.existingSecret > chart-managed > empty (workload identity)
*/}}
{{- define "isola.operator.mounterCredentialSecretName" -}}
{{- $mounter := .Values.operator.sandboxRuntime.rootfssnapshot.snapshotMounter -}}
{{- if $mounter.existingSecret -}}
{{- $mounter.existingSecret -}}
{{- else if eq (include "isola.operator.hasChartManagedCredentials" .) "true" -}}
{{- printf "%s-mounter-credentials" (include "isola.operator.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
RootfsSnapshot service account name (from gvisor rootfssnapshot config)
*/}}
{{- define "isola.operator.rootfssnapshotServiceAccountName" -}}
{{- $sa := .Values.operator.sandboxRuntime.rootfssnapshot.uploader.serviceAccount -}}
{{- if $sa.create -}}
{{- $sa.name | default (printf "%s-rootfssnapshot" (include "isola.operator.fullname" .)) -}}
{{- else -}}
{{- default "default" $sa.name -}}
{{- end -}}
{{- end }}

{{/*
Snapshot mounter service account name (DaemonSet in release namespace, separate from snapshot job SA)
*/}}
{{- define "isola.operator.snapshotMounterServiceAccountName" -}}
{{- if eq (include "isola.operator.rootfssnapshotEnabled" .) "true" -}}
{{- $sa := .Values.operator.sandboxRuntime.rootfssnapshot.snapshotMounter.serviceAccount -}}
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
{{- .Values.operator.sandboxRuntime.rootfssnapshot.snapshotMounter.hostMountPath -}}
{{- end -}}
{{- end }}

{{/*
rclone image for rootfssnapshot NFS server container (unprivileged)
*/}}
{{- define "isola.operator.rootfssnapshotRcloneImage" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxRuntime.rootfssnapshot.snapshotMounter.rclone.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
{{- end }}

{{/*
Mounter image for rootfssnapshot NFS mount container (privileged, minimal)
*/}}
{{- define "isola.operator.rootfssnapshotMounterImage" -}}
{{- include "isola.image" (dict "imageConfig" .Values.operator.sandboxRuntime.rootfssnapshot.snapshotMounter.mounter.image "global" .Values.global "appVersion" .Chart.AppVersion) -}}
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

{{/*
API Gateway auth secret name.
Resolution: apiGateway.auth.existingSecret > chart-managed ("<fullname>-auth")
*/}}
{{- define "isola.apiGateway.authSecretName" -}}
{{- if .Values.apiGateway.auth.existingSecret -}}
{{- .Values.apiGateway.auth.existingSecret -}}
{{- else -}}
{{- printf "%s-auth" (include "isola.apiGateway.fullname" .) -}}
{{- end -}}
{{- end }}

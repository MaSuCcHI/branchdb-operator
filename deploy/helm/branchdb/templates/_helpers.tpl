{{/* チャート名 */}}
{{- define "branchdb.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* フルネーム */}}
{{- define "branchdb.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* 共通ラベル */}}
{{- define "branchdb.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "branchdb.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* セレクターラベル */}}
{{- define "branchdb.selectorLabels" -}}
app.kubernetes.io/name: {{ include "branchdb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* ServiceAccount 名 */}}
{{- define "branchdb.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "branchdb.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* branch リソースを作成する namespace */}}
{{- define "branchdb.branchNamespace" -}}
{{- default .Release.Namespace .Values.branchNamespace -}}
{{- end -}}

{{/* API サーバーのフルネーム */}}
{{- define "branchdb.apiServer.fullname" -}}
{{- printf "%s-api" (include "branchdb.fullname" .) -}}
{{- end -}}

{{/* API サーバーのセレクターラベル */}}
{{- define "branchdb.apiServer.selectorLabels" -}}
app.kubernetes.io/name: {{ include "branchdb.name" . }}-api
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* ZFS Agent トークンを保持する Secret 名 */}}
{{- define "branchdb.secretName" -}}
{{- if .Values.zfsAgent.existingSecret -}}
{{- .Values.zfsAgent.existingSecret -}}
{{- else -}}
{{- printf "%s-zfsagent" (include "branchdb.fullname" .) -}}
{{- end -}}
{{- end -}}

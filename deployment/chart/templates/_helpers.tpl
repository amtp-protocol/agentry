{{/*
Expand the name of the chart.
*/}}
{{- define "agentry.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this.
*/}}
{{- define "agentry.fullname" -}}
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
{{- define "agentry.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "agentry.labels" -}}
helm.sh/chart: {{ include "agentry.chart" . }}
{{ include "agentry.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "agentry.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agentry.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "agentry.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "agentry.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Generate TLS secret name
*/}}
{{- define "agentry.tlsSecretName" -}}
{{- if .Values.tls.existingSecret }}
{{- .Values.tls.existingSecret }}
{{- else }}
{{- printf "%s-tls" (include "agentry.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Generate admin key secret name
*/}}
{{- define "agentry.adminKeySecretName" -}}
{{- if .Values.auth.adminKeyExistingSecret }}
{{- .Values.auth.adminKeyExistingSecret }}
{{- else }}
{{- printf "%s-admin-key" (include "agentry.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Generate database connection string secret name
*/}}
{{- define "agentry.dbSecretName" -}}
{{- if .Values.storage.database.connectionStringExistingSecret }}
{{- .Values.storage.database.connectionStringExistingSecret }}
{{- else }}
{{- printf "%s-db" (include "agentry.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Generate API key salt secret name
*/}}
{{- define "agentry.saltSecretName" -}}
{{- if .Values.auth.apiKeySaltExistingSecret }}
{{- .Values.auth.apiKeySaltExistingSecret }}
{{- else }}
{{- printf "%s-salt" (include "agentry.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Whether an admin key file should be mounted (existing secret or inline key provided)
*/}}
{{- define "agentry.adminKeyEnabled" -}}
{{- if and .Values.auth.requireAuth (or .Values.auth.adminKeyExistingSecret .Values.auth.adminKey) }}true{{- end }}
{{- end }}

{{/*
Return the environment variable style name for container
*/}}
{{- define "agentry.envVarPrefix" -}}
AMTP
{{- end }}

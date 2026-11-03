{{/*
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "gv-factory.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "gv-factory.fullname" -}}
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
{{- define "gv-factory.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "gv-factory.labels" -}}
helm.sh/chart: {{ include "gv-factory.chart" . }}
{{ include "gv-factory.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "gv-factory.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gv-factory.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Component-specific selector labels
*/}}
{{- define "gv-factory.componentSelectorLabels" -}}
app.kubernetes.io/name: {{ include "gv-factory.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Server full name
*/}}
{{- define "gv-factory.server.fullname" -}}
{{- printf "%s-server" (include "gv-factory.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Worker full name
*/}}
{{- define "gv-factory.worker.fullname" -}}
{{- printf "%s-worker" (include "gv-factory.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Web full name
*/}}
{{- define "gv-factory.web.fullname" -}}
{{- printf "%s-web" (include "gv-factory.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Postgres full name
*/}}
{{- define "gv-factory.postgres.fullname" -}}
{{- printf "%s-postgres" (include "gv-factory.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
NATS full name
*/}}
{{- define "gv-factory.nats.fullname" -}}
{{- printf "%s-nats" (include "gv-factory.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Secret name
*/}}
{{- define "gv-factory.secretName" -}}
{{- default (printf "%s-secrets" (include "gv-factory.fullname" .)) .Values.secret.existingSecret }}
{{- end }}

{{/*
ConfigMap name
*/}}
{{- define "gv-factory.configMapName" -}}
{{- printf "%s-config" (include "gv-factory.fullname" .) }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "gv-factory.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "gv-factory.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

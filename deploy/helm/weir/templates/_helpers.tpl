{{- define "weir.name" -}}{{ .Release.Name | trunc 50 | trimSuffix "-" }}{{- end }}

{{- define "weir.labels" -}}
app.kubernetes.io/name: weir
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{- define "weir.selector" -}}
app.kubernetes.io/name: weir
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "weir.image" -}}{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}{{- end }}

{{- define "weir.agentNamespace" -}}{{ .Values.agents.namespace | default .Release.Namespace }}{{- end }}

{{/* The settings every Weir container shares: the Secret, and anything in env. */}}
{{- define "weir.env" -}}
{{- if .Values.existingSecret }}
envFrom:
  - secretRef:
      name: {{ .Values.existingSecret }}
{{- end }}
env:
  {{- range $name, $value := .Values.env }}
  - name: {{ $name }}
    value: {{ $value | quote }}
  {{- end }}
  {{- if .Values.databaseCA.configMap }}
  - name: WEIR_DATABASE_SSLROOTCERT
    value: /etc/weir/db-ca/{{ .Values.databaseCA.key }}
  {{- end }}
{{- end }}

{{/* Where a container finds the database's certificate authorities. */}}
{{- define "weir.caMount" -}}
{{- if .Values.databaseCA.configMap }}
volumeMounts:
  - name: database-ca
    mountPath: /etc/weir/db-ca
    readOnly: true
{{- end }}
{{- end }}

{{- define "weir.caVolume" -}}
{{- if .Values.databaseCA.configMap }}
volumes:
  - name: database-ca
    configMap:
      name: {{ .Values.databaseCA.configMap }}
{{- end }}
{{- end }}

{{/*
Settings that cannot work together. The Job that migrates before an install
runs before the chart's ServiceAccount exists, so it has no cloud identity to
sign in to the database with.
*/}}
{{- define "weir.check" -}}
{{- if and (index .Values.env "WEIR_DATABASE_IAM") .Values.migrate.enabled (eq .Values.migrate.when "before") }}
{{- fail "env.WEIR_DATABASE_IAM is set, so the database is signed in to with the server's cloud identity. The Job that migrates before an install has no such identity. Set migrate.when to start, which migrates in the server's own pod." }}
{{- end }}
{{- end }}

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
{{- end }}

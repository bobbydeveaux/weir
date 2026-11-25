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
  {{- if .Values.secretFiles.volume }}
  - name: WEIR_SECRETS_DIR
    value: {{ .Values.secretFiles.mountPath | quote }}
  {{- end }}
  {{- if .Values.secretFiles.adminKey }}
  - name: WEIR_ADMIN_KEY_FILE
    value: {{ printf "%s/%s" (trimSuffix "/" .Values.secretFiles.mountPath) .Values.secretFiles.adminKey | quote }}
  {{- end }}
{{- end }}

{{/*
The volumes a container mounts: the database's certificate authorities, and
the secret files. One list, so that a container never has two.
*/}}
{{- define "weir.volumeMounts" -}}
{{- if or .Values.databaseCA.configMap .Values.secretFiles.volume }}
volumeMounts:
  {{- if .Values.databaseCA.configMap }}
  - name: database-ca
    mountPath: /etc/weir/db-ca
    readOnly: true
  {{- end }}
  {{- if .Values.secretFiles.volume }}
  - name: secret-files
    mountPath: {{ .Values.secretFiles.mountPath }}
    readOnly: true
  {{- end }}
{{- end }}
{{- end }}

{{/* The pod's volumes, as one list for the same reason. */}}
{{- define "weir.volumes" -}}
{{- if or .Values.databaseCA.configMap .Values.secretFiles.volume }}
volumes:
  {{- if .Values.databaseCA.configMap }}
  - name: database-ca
    configMap:
      name: {{ .Values.databaseCA.configMap }}
  {{- end }}
  {{- with .Values.secretFiles.volume }}
  - name: secret-files
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}
{{- end }}

{{/*
Settings that cannot work together. The Job that migrates before an install
runs before the chart's ServiceAccount exists, so it has no cloud identity to
sign in to the database with.

The image refuses to start without an admin key unless WEIR_REQUIRE_ADMIN_KEY
is set and empty, so the chart refuses a release that gives it none. It cannot
see inside existingSecret, so a Secret is taken to hold WEIR_ADMIN_KEY.

The admin key file must be at the top of the volume. A file: reference reads
<mountPath>/<workspace>/<name>, and neither part may hold a slash or a dot, so
no workspace can name a file there. A file one directory down could be read
by a workspace of that directory's name.
*/}}
{{- define "weir.check" -}}
{{- if and (index .Values.env "WEIR_DATABASE_IAM") .Values.migrate.enabled (eq .Values.migrate.when "before") }}
{{- fail "env.WEIR_DATABASE_IAM is set, so the database is signed in to with the server's cloud identity. The Job that migrates before an install has no such identity. Set migrate.when to start, which migrates in the server's own pod." }}
{{- end }}
{{- $files := .Values.secretFiles }}
{{- if and $files.adminKey (not $files.volume) }}
{{- fail "secretFiles.adminKey names a file, but secretFiles.volume is not set, so there is nothing to find it in. Set secretFiles.volume to the volume that holds it." }}
{{- end }}
{{- if contains "/" (toString $files.adminKey) }}
{{- fail "secretFiles.adminKey must be a file at the top of the volume, with no directory: a file in a directory could be read by the workspace of that name. Set it to a plain file name, such as admin-key." }}
{{- end }}
{{- $required := not (and (hasKey .Values.env "WEIR_REQUIRE_ADMIN_KEY") (empty (index .Values.env "WEIR_REQUIRE_ADMIN_KEY"))) }}
{{- if and $required (not .Values.existingSecret) (not $files.adminKey) }}
{{- fail "The server will not start without an admin key, and none is given. Set existingSecret to a Secret that holds WEIR_ADMIN_KEY, or secretFiles.adminKey to the file in secretFiles.volume that holds it." }}
{{- end }}
{{- end }}

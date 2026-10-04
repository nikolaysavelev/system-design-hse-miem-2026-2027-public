{{- define "soldout.labels" -}}
app.kubernetes.io/part-of: soldout
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "soldout.secretName" -}}
{{- if .Values.secrets.create -}}soldout-secrets{{- else -}}{{ required "secrets.existingSecret обязателен при secrets.create=false" .Values.secrets.existingSecret }}{{- end -}}
{{- end -}}

{{/* Общий securityContext контейнера: образ distroless nonroot, запись на диск не нужна. */}}
{{- define "soldout.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end -}}

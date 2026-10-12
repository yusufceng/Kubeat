{{- define "kubeat.labels" -}}
app.kubernetes.io/name: kubeat
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{- define "kubeat.selector" -}}
app.kubernetes.io/name: kubeat
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Restricted-profile container security context, valid under OpenShift's restricted SCC. */}}
{{- define "kubeat.containerSecurityContext" -}}
allowPrivilegeEscalation: false
runAsNonRoot: true
capabilities:
  drop: ["ALL"]
seccompProfile:
  type: RuntimeDefault
{{- end }}

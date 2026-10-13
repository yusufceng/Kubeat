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
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{/* The Secret holding the API token, and the env entry that reads it. */}}
{{- define "kubeat.apiTokenSecret" -}}
{{ .Values.apiToken.existingSecret | default (printf "%s-api-token" .Release.Name) }}
{{- end }}

{{/*
The URL the identity provider sends the browser back to: as given, or made
from the Route or Ingress host.
*/}}
{{- define "kubeat.oidcRedirectURL" -}}
{{- if .Values.oidc.redirectURL -}}
{{ .Values.oidc.redirectURL }}
{{- else if and .Values.route.enabled .Values.route.host -}}
https://{{ .Values.route.host }}/auth/callback
{{- else if and .Values.ingress.enabled .Values.ingress.host -}}
https://{{ .Values.ingress.host }}/auth/callback
{{- else -}}
{{- fail "oidc.redirectURL is required when neither route.host nor ingress.host is set" -}}
{{- end -}}
{{- end -}}

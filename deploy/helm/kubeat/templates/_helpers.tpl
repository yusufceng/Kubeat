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
The Secret holding the token a scrape of /metrics presents: the backend
expects it and the ServiceMonitor sends it.
*/}}
{{- define "kubeat.metricsTokenSecret" -}}
{{ .Values.metrics.auth.existingSecret | default (printf "%s-metrics-token" .Release.Name) }}
{{- end }}

{{/*
The Ingress host as a URL: HTTPS only when the Ingress serves TLS itself.
When TLS ends in front of the ingress controller, at a load balancer, the
address users really reach is given in publicURL and oidc.redirectURL.
*/}}
{{- define "kubeat.ingressURL" -}}
{{ if .Values.ingress.tls.enabled }}https{{ else }}http{{ end }}://{{ .Values.ingress.host }}
{{- end -}}

{{/*
Where users reach Kubeat, for the links in notifications, the OIDC redirect
and the frontend's HTTPS settings: as given, or made from the Route or
Ingress host.
*/}}
{{- define "kubeat.publicURL" -}}
{{- if .Values.publicURL -}}
{{ .Values.publicURL }}
{{- else if and .Values.route.enabled .Values.route.host -}}
https://{{ .Values.route.host }}
{{- else if and .Values.ingress.enabled .Values.ingress.host -}}
{{ include "kubeat.ingressURL" . }}
{{- end -}}
{{- end -}}

{{/*
The URL the identity provider sends the browser back to: as given, or the
/auth/callback of the public URL. Empty while neither is known (a Route
whose host the router picks): SSO then needs publicURL set to that host.
*/}}
{{- define "kubeat.oidcRedirectURL" -}}
{{- if .Values.oidc.redirectURL -}}
{{ .Values.oidc.redirectURL }}
{{- else if include "kubeat.publicURL" . -}}
{{ include "kubeat.publicURL" . | trimSuffix "/" }}/auth/callback
{{- end -}}
{{- end -}}

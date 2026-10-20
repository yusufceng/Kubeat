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
Values that came after 0.37.0 are read with dig and the defaults of
values.yaml, never as .Values.a.b: "helm upgrade --reuse-values" from 0.37.0
renders with that release's values, where these keys do not exist, and
reading a key of a missing map fails the upgrade. kubeat.metricsAuth and
kubeat.ingressTLS say "true" or nothing.
*/}}
{{- define "kubeat.metricsAuth" -}}
{{- if dig "auth" "enabled" true .Values.metrics }}true{{ end -}}
{{- end -}}

{{- define "kubeat.ingressTLS" -}}
{{- if dig "tls" "enabled" true .Values.ingress }}true{{ end -}}
{{- end -}}

{{/*
Fails the render on values no template reads, which would otherwise be
dropped without a word: backend.env and frontend.env, which values files
of 0.37.0 and before have, unless they are empty. extraEnv took their
place.
*/}}
{{- define "kubeat.checkValues" -}}
{{- range $part := list "backend" "frontend" -}}
{{- if dig "env" nil (index $.Values $part) -}}
{{- fail (printf "%s.env is no longer read: move its variables to %s.extraEnv" $part $part) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* The kubernetes.io/tls Secret with the Ingress's certificate. */}}
{{- define "kubeat.ingressTLSSecret" -}}
{{ dig "tls" "secretName" "" .Values.ingress | default (printf "%s-tls" .Release.Name) }}
{{- end -}}

{{/*
The Secret holding the token a scrape of /metrics presents: the backend
expects it and the ServiceMonitor sends it.
*/}}
{{- define "kubeat.metricsTokenSecret" -}}
{{ dig "auth" "existingSecret" "" .Values.metrics | default (printf "%s-metrics-token" .Release.Name) }}
{{- end }}

{{/*
The Ingress host as a URL: HTTPS only when the Ingress serves TLS itself.
When TLS ends in front of the ingress controller, at a load balancer, the
address users really reach is given in publicURL.
*/}}
{{- define "kubeat.ingressURL" -}}
{{ if include "kubeat.ingressTLS" . }}https{{ else }}http{{ end }}://{{ .Values.ingress.host }}
{{- end -}}

{{/*
Where users reach Kubeat, for the links in notifications and the
frontend's HTTPS settings: as given, or made from the Route or Ingress host.
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
The URL the identity provider sends the browser back to: oidc.redirectURL
as given; or the /auth/callback of publicURL when that is given; or that of
the Route or Ingress host, over HTTPS whatever ingress.tls says, for when
TLS ends in front of the ingress controller (as 0.37.0 did; SSO cannot work
without HTTPS somewhere). Empty while none is known (a Route whose host the
router picks): SSO then needs publicURL set to that host.
*/}}
{{- define "kubeat.oidcRedirectURL" -}}
{{- if .Values.oidc.redirectURL -}}
{{ .Values.oidc.redirectURL }}
{{- else if .Values.publicURL -}}
{{ .Values.publicURL | trimSuffix "/" }}/auth/callback
{{- else if and .Values.route.enabled .Values.route.host -}}
https://{{ .Values.route.host }}/auth/callback
{{- else if and .Values.ingress.enabled .Values.ingress.host -}}
https://{{ .Values.ingress.host }}/auth/callback
{{- end -}}
{{- end -}}

{{/* A dotted IPv4 address as Go's netip reads it: no leading zeros. */}}
{{- define "kubeat.ipv4Pattern" -}}
(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])(\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])){3}
{{- end -}}

{{/*
An IPv6 address (no brackets, no zone) as its eight groups joined by ":",
the way Go's netip reads it: "::" stands for one or more groups of zeros,
and a trailing IPv4 address for the last two groups. Empty for anything
netip does not take as an IPv6 address.
*/}}
{{- define "kubeat.ipv6Groups" -}}
{{- $addr := lower . -}}
{{- with regexFind (printf ":%s$" (include "kubeat.ipv4Pattern" .)) $addr -}}
{{- $o := splitList "." (trimPrefix ":" .) -}}
{{- $addr = printf "%s:%x:%x" (trimSuffix . $addr) (add (mul (atoi (index $o 0)) 256) (atoi (index $o 1))) (add (mul (atoi (index $o 2)) 256) (atoi (index $o 3))) -}}
{{- end -}}
{{- $halves := splitList "::" $addr -}}
{{- $left := list -}}
{{- $right := list -}}
{{- with first $halves }}{{ $left = splitList ":" . }}{{ end -}}
{{- if eq (len $halves) 2 }}{{ with last $halves }}{{ $right = splitList ":" . }}{{ end }}{{ end -}}
{{- $n := add (len $left) (len $right) | int -}}
{{- $ok := and (le (len $halves) 2) (ternary (le $n 7) (eq $n 8) (eq (len $halves) 2)) -}}
{{- range concat $left $right }}{{ if not (regexMatch "^[0-9a-f]{1,4}$" .) }}{{ $ok = false }}{{ end }}{{ end -}}
{{- if $ok -}}
{{- $zeros := list -}}
{{- range until (sub 8 $n | int) }}{{ $zeros = append $zeros "0" }}{{ end -}}
{{- join ":" (concat $left $zeros $right) -}}
{{- end -}}
{{- end -}}

{{/*
"true" when a URL's hostname names this machine as the backend's
loopback() reads it (net.ParseIP): localhost, an IPv4 address in
127.0.0.0/8, or an IPv6 address without a zone that is ::1 or an
IPv4-mapped one in 127.0.0.0/8.
*/}}
{{- define "kubeat.loopbackHost" -}}
{{- $host := lower . -}}
{{- if or (eq $host "localhost") (and (hasPrefix "127." $host) (regexMatch (printf "^%s$" (include "kubeat.ipv4Pattern" .)) $host)) -}}
true
{{- else if not (contains "%" $host) -}}
{{- if regexMatch "^0+:0+:0+:0+:0+:(0+:0+:0*1|ffff:7f[0-9a-f]{2}:[0-9a-f]+)$" (include "kubeat.ipv6Groups" $host) }}true{{ end -}}
{{- end -}}
{{- end -}}

{{/*
Why the backend would refuse this redirect URL (checkCallbackURL in
internal/config), or nothing: it must be absolute, https (plain http only
to this machine), with the path /auth/callback and no user info, query or
fragment. Its host is held to what Go's URL parser takes for http and
https: an IPv6 address in brackets, and a host with at most a port after
its first colon. One without "<scheme>://" has no host, and is refused
before urlParse, which fails the render on some of them with an error of
its own.
*/}}
{{- define "kubeat.oidcRedirectProblem" -}}
{{- $notAbsolute := "is not an absolute URL such as https://kubeat.apps.example/auth/callback" -}}
{{- if not (regexMatch "^[A-Za-z][A-Za-z0-9+.-]*://" .) -}}
{{ $notAbsolute }}
{{- else -}}
{{- /* The host is read from the text itself, before urlParse: a Helm built
with a newer Go refuses some hosts in urlParse (a bracketed IPv4 address, a
second port) with an error that does not say which value to fix. */ -}}
{{- $authority := regexReplaceAll "^[A-Za-z][A-Za-z0-9+.-]*://([^/?#]*).*$" . "${1}" -}}
{{- $host := regexReplaceAll "^.*@" $authority "" -}}
{{- $literal := regexFind "^\\[.*\\]" $host | trimPrefix "[" | trimSuffix "]" -}}
{{- $hostOK := false -}}
{{- if hasPrefix "[" $host -}}
{{- $address := regexReplaceAll "%.*$" $literal "" -}}
{{- $hostOK = and (regexMatch "^\\[.*\\](:[0-9]*)?$" $host) (include "kubeat.ipv6Groups" $address) (ne (trimPrefix $address $literal) "%") -}}
{{- else -}}
{{- $hostOK = regexMatch "^[^\\[:]*(:[0-9]*)?$" $host -}}
{{- end -}}
{{- if not (and $host $hostOK) -}}
{{ $notAbsolute }}
{{- else -}}
{{- $url := urlParse . -}}
{{- if not (or (eq $url.scheme "https") (and (eq $url.scheme "http") (include "kubeat.loopbackHost" $url.hostname))) -}}
is plain http, which the backend accepts only for localhost
{{- else if or (ne $url.path "/auth/callback") (regexMatch "^[^/?#]*//[^/?#]*@" .) $url.query $url.fragment -}}
is not the UI's /auth/callback alone, without a path before it, user info, query or fragment
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Fails the render when SSO would send users back to an address the backend
refuses (see kubeat.oidcRedirectProblem): the backend would turn SSO off
after the identity provider was given the address the install notes print.
The message names the value the address came from.
*/}}
{{- define "kubeat.checkOIDCRedirect" -}}
{{- $redirect := include "kubeat.oidcRedirectURL" . -}}
{{- if and .Values.oidc.issuer $redirect -}}
{{- with include "kubeat.oidcRedirectProblem" $redirect -}}
{{- $from := "oidc.redirectURL" -}}
{{- $fix := "set oidc.redirectURL to the https:// address users open, followed by /auth/callback" -}}
{{- if and (not $.Values.oidc.redirectURL) $.Values.publicURL -}}
{{- $from = "made from publicURL" -}}
{{- $fix = "set publicURL to the https:// address users open, without a path, or set oidc.redirectURL" -}}
{{- else if not $.Values.oidc.redirectURL -}}
{{- $from = ternary "made from route.host" "made from ingress.host" (and $.Values.route.enabled $.Values.route.host | empty | not) -}}
{{- $fix = "set publicURL to the https:// address users open, or set oidc.redirectURL" -}}
{{- end -}}
{{- fail (printf "oidc.issuer is set, but the SSO redirect URL %s (%s) %s, and the backend would turn SSO off: %s" $redirect $from . $fix) -}}
{{- end -}}
{{- end -}}
{{- end -}}

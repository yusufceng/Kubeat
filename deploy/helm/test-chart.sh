#!/usr/bin/env bash
# Renders the chart with values it must take and values it must refuse, and
# checks what the install notes say for some of them. Needs only helm; it
# never talks to a cluster. Usage: deploy/helm/test-chart.sh
set -u

CHART="$(cd "$(dirname "$0")/kubeat" && pwd)"
# Nothing here may reach a cluster, whatever the caller's kubeconfig says.
export KUBECONFIG=/dev/null
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
FAILED=0

# helm template does not print NOTES.txt: a copy of the chart renders the
# notes into a ConfigMap instead.
NOTES_CHART="$WORK/kubeat"
cp -R "$CHART" "$NOTES_CHART"
{
  printf '{{- define "test.notes" -}}\n'
  cat "$CHART/templates/NOTES.txt"
  printf '\n{{- end -}}\n'
} >"$NOTES_CHART/templates/_test_notes.tpl"
rm "$NOTES_CHART/templates/NOTES.txt"
printf 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test-notes\ndata:\n  notes: |\n{{ include "test.notes" . | indent 4 }}\n' \
  >"$NOTES_CHART/templates/zz_test_notes.yaml"

failed() {
  echo "FAIL: $*"
  FAILED=$((FAILED + 1))
}

# renders NAME VALUES...: the chart renders with these values.
renders() {
  local name="$1"
  shift
  helm template kubeat "$CHART" "$@" >/dev/null 2>"$WORK/err" || failed "$name: refused: $(cat "$WORK/err")"
}

# refuses NAME TEXT VALUES...: the chart refuses these values, saying TEXT.
refuses() {
  local name="$1" text="$2"
  shift 2
  if helm template kubeat "$CHART" "$@" >/dev/null 2>"$WORK/err"; then
    failed "$name: rendered"
  elif ! grep -qF -- "$text" "$WORK/err"; then
    failed "$name: refused without \"$text\": $(cat "$WORK/err")"
  fi
}

# notes NAME says|omits TEXT VALUES...: the install notes for these values
# say TEXT, or leave it out.
notes() {
  local name="$1" want="$2" text="$3"
  shift 3
  if ! helm template kubeat "$NOTES_CHART" --show-only templates/zz_test_notes.yaml "$@" >"$WORK/notes" 2>"$WORK/err"; then
    failed "$name: refused: $(cat "$WORK/err")"
  elif grep -qF -- "$text" "$WORK/notes"; then
    [ "$want" = says ] || failed "$name: the notes say \"$text\""
  else
    [ "$want" = omits ] || failed "$name: the notes do not say \"$text\""
  fi
}

SSO=(--set oidc.issuer=https://sso.example.com/realms/corp --set oidc.clientID=kubeat)
ROUTE=(--set route.enabled=true --set route.host=kubeat.apps.example)

renders "defaults"

# Values files of 0.37.0 and before have backend.env and frontend.env, which
# no template reads: empty, as helm upgrade --reuse-values renders them,
# they are fine; anything in them would be dropped without a word.
renders "empty env" --set-json 'backend.env={}' --set-json 'frontend.env={}'
refuses "backend.env" "move its variables to backend.extraEnv" --set backend.env.UPTIME_RETENTION=48h
refuses "frontend.env" "move its variables to frontend.extraEnv" --set frontend.env.REFRESH_INTERVAL_MS=10000

# The SSO redirect URL: the chart takes what the backend's checkCallbackURL
# takes, refuses the rest, and names the value a refused one came from.
renders "redirect from route.host" "${SSO[@]}" "${ROUTE[@]}"
renders "redirect from publicURL" "${SSO[@]}" --set publicURL=https://kubeat.apps.example/
for url in https://sso-entry.example/auth/callback https://10.0.0.5/auth/callback \
  https://kubeat.shop.svc:3000/auth/callback http://localhost:3000/auth/callback \
  http://127.0.0.1:3000/auth/callback 'http://[::1]:3000/auth/callback' 'http://[::ffff:127.0.0.1]/auth/callback'; do
  renders "redirect $url" "${SSO[@]}" --set-json "oidc.redirectURL=\"$url\""
done
for url in http://kubeat.apps.example/auth/callback http://10.0.0.5/auth/callback \
  kubeat.apps.example/auth/callback /auth/callback https:///auth/callback https://kubeat.apps.example/ \
  https://kubeat.apps.example/auth/callback/ 'https://kubeat.apps.example/auth/callback?x=1' \
  'https://kubeat.apps.example/auth/callback#x' https://user@kubeat.apps.example/auth/callback \
  'https://[10.0.0.5]/auth/callback' 'http://[::2]/auth/callback' 'https://kubeat.apps.example:80:80/auth/callback'; do
  refuses "redirect $url" "$url (oidc.redirectURL)" "${SSO[@]}" --set-json "oidc.redirectURL=\"$url\""
done
refuses "publicURL with a path" "(made from publicURL) is not the UI's /auth/callback alone" \
  "${SSO[@]}" --set publicURL=https://portal.example.com/kubeat
refuses "plain-HTTP publicURL" "(made from publicURL) is plain http" \
  "${SSO[@]}" "${ROUTE[@]}" --set publicURL=http://kubeat.corp.example

# The install notes say the redirect follows publicURL only when it does.
FOLLOWS="With publicURL set it follows publicURL"
notes "redirect from publicURL" says "$FOLLOWS" "${SSO[@]}" "${ROUTE[@]}" --set publicURL=https://kubeat.corp.example
notes "redirect pinned by oidc.redirectURL" omits "$FOLLOWS" "${SSO[@]}" "${ROUTE[@]}" \
  --set publicURL=https://kubeat.corp.example --set-json 'oidc.redirectURL="https://kubeat.apps.example/auth/callback"'
notes "redirect from route.host" omits "$FOLLOWS" "${SSO[@]}" "${ROUTE[@]}"

# An Ingress without TLS: SSO needs HTTPS in front of the controller.
NEEDS_HTTPS="SSO needs HTTPS in front of the ingress controller"
notes "TLS-off Ingress with SSO" says "$NEEDS_HTTPS" "${SSO[@]}" --set ingress.enabled=true --set ingress.tls.enabled=false
notes "TLS-off Ingress without SSO" omits "$NEEDS_HTTPS" --set ingress.enabled=true --set ingress.tls.enabled=false

# The license: mounted whole, so that a renewed Secret reaches the file.
helm template kubeat "$CHART" --set license.existingSecret=kubeat-license >"$WORK/license.yaml" 2>"$WORK/err" ||
  failed "license: refused: $(cat "$WORK/err")"
grep -qF "value: /etc/kubeat/license/license" "$WORK/license.yaml" || failed "license: UPTIME_LICENSE_FILE not set"
grep -qF "secretName: kubeat-license" "$WORK/license.yaml" || failed "license: the Secret is not mounted"
grep -q "subPath:" "$WORK/license.yaml" && failed "license: mounted with subPath, which a renewed Secret never reaches"
helm template kubeat "$CHART" | grep -q "UPTIME_LICENSE_FILE" && failed "license: set without a Secret"
NEEDS_LICENSE="needs a Kubeat Enterprise license"
notes "SSO without a license" says "$NEEDS_LICENSE" "${SSO[@]}" "${ROUTE[@]}"
notes "SSO with a license" omits "$NEEDS_LICENSE" "${SSO[@]}" "${ROUTE[@]}" --set license.existingSecret=kubeat-license
notes "LDAP without a license" says "$NEEDS_LICENSE" --set ldap.url=ldap://ldap.example.com --set ldap.baseDN=dc=example,dc=com
notes "no SSO" omits "$NEEDS_LICENSE"

if [ "$FAILED" -gt 0 ]; then
  echo "$FAILED chart check(s) failed"
  exit 1
fi
echo "chart: all checks passed"

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

# Removing the release leaves the database's and the backups' PVCs: helm
# uninstall skips what carries helm.sh/resource-policy, deleting an Argo CD
# application what carries Delete=false. Each claim has a switch of its
# own, read with a default of true: a value set to null, as a release
# upgraded with --reuse-values from 0.43 renders without it, keeps them too.
# The backups' claim exists only when the chart creates it.
# claim NAME CLAIM has|lacks|absent TEXT VALUES...: the PersistentVolumeClaim
# CLAIM rendered with these values has TEXT, lacks it, or is not rendered.
claim() {
  local name="$1" claim="$2" want="$3" text="$4"
  shift 4
  if ! helm template kubeat "$CHART" --show-only templates/backend.yaml "$@" >"$WORK/backend" 2>"$WORK/err"; then
    failed "$name: refused: $(cat "$WORK/err")"
    return
  fi
  awk -v claim="$claim" '
    function flush() {
      if (index(doc, "\nkind: PersistentVolumeClaim\n") && index(doc, "\n  name: " claim "\n")) printf "%s", doc
      doc = "\n"
    }
    BEGIN { doc = "\n" }
    /^---/ { flush(); next }
    { doc = doc $0 "\n" }
    END { flush() }
  ' "$WORK/backend" >"$WORK/claim"
  if [ ! -s "$WORK/claim" ]; then
    [ "$want" = absent ] || failed "$name: $claim is not rendered"
  elif [ "$want" = absent ]; then
    failed "$name: $claim is rendered"
  elif grep -qF -- "$text" "$WORK/claim"; then
    [ "$want" = has ] || failed "$name: $claim has \"$text\""
  else
    [ "$want" = lacks ] || failed "$name: $claim lacks \"$text\""
  fi
}
BACKUPS=(--set backup.enabled=true)
NO_KEEP_VALUES=(--set backend.persistence.keepOnUninstall=null --set backup.persistence.keepOnUninstall=null)
for text in "helm.sh/resource-policy: keep" "argocd.argoproj.io/sync-options: Delete=false"; do
  claim "defaults" kubeat-data has "$text"
  claim "backups" kubeat-data has "$text" "${BACKUPS[@]}"
  claim "backups" kubeat-backup has "$text" "${BACKUPS[@]}"
  claim "no keepOnUninstall values" kubeat-data has "$text" "${BACKUPS[@]}" "${NO_KEEP_VALUES[@]}"
  claim "no keepOnUninstall values" kubeat-backup has "$text" "${BACKUPS[@]}" "${NO_KEEP_VALUES[@]}"
  claim "data not kept" kubeat-data lacks "$text" "${BACKUPS[@]}" --set backend.persistence.keepOnUninstall=false
  claim "data not kept" kubeat-backup has "$text" "${BACKUPS[@]}" --set backend.persistence.keepOnUninstall=false
  claim "backups not kept" kubeat-backup lacks "$text" "${BACKUPS[@]}" --set backup.persistence.keepOnUninstall=false
  claim "backups not kept" kubeat-data has "$text" "${BACKUPS[@]}" --set backup.persistence.keepOnUninstall=false
  claim "backup existingClaim" kubeat-data has "$text" "${BACKUPS[@]}" --set backup.persistence.existingClaim=kubeat-old-backup
done
claim "defaults" kubeat-backup absent ""
claim "backup existingClaim" kubeat-backup absent "" "${BACKUPS[@]}" --set backup.persistence.existingClaim=kubeat-old-backup
# The install notes name the claims left behind, and the command that
# deletes them names those alone.
DELETE_PVC="kubectl -n kubeat delete pvc"
notes "kept claims" says "$DELETE_PVC kubeat-data kubeat-backup" --namespace kubeat "${BACKUPS[@]}"
notes "kept claims without the values" says "$DELETE_PVC kubeat-data kubeat-backup" --namespace kubeat "${BACKUPS[@]}" "${NO_KEEP_VALUES[@]}"
notes "kept data claim without backups" says "$DELETE_PVC kubeat-data" --namespace kubeat
notes "kept data claim without backups" omits "kubeat-backup" --namespace kubeat
notes "backups not kept" omits "kubeat-backup" --namespace kubeat "${BACKUPS[@]}" --set backup.persistence.keepOnUninstall=false
notes "backup existingClaim" omits "kubeat-backup" --namespace kubeat "${BACKUPS[@]}" --set backup.persistence.existingClaim=kubeat-old-backup
notes "data not kept" says "$DELETE_PVC kubeat-backup" --namespace kubeat "${BACKUPS[@]}" --set backend.persistence.keepOnUninstall=false
notes "data not kept" omits "kubeat-data" --namespace kubeat "${BACKUPS[@]}" --set backend.persistence.keepOnUninstall=false
notes "nothing kept" omits "delete pvc" --namespace kubeat "${BACKUPS[@]}" \
  --set backend.persistence.keepOnUninstall=false --set backup.persistence.keepOnUninstall=false
notes "nothing kept without backups" omits "delete pvc" --namespace kubeat --set backend.persistence.keepOnUninstall=false

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

# The alert rules: Kubeat's self-health rules render, the backup staleness
# rule only with backups on, with a threshold read from backup.interval.
RULES=(--set metrics.prometheusRule.enabled=true)
# rules NAME has|lacks TEXT VALUES...: the PrometheusRule rendered with these
# values (and the rules on) has TEXT, or lacks it.
rules() {
  local name="$1" want="$2" text="$3"
  shift 3
  if ! helm template kubeat "$CHART" --show-only templates/prometheusrule.yaml "${RULES[@]}" "$@" >"$WORK/rules" 2>"$WORK/err"; then
    failed "$name: refused: $(cat "$WORK/err")"
  elif grep -qF -- "$text" "$WORK/rules"; then
    [ "$want" = has ] || failed "$name: the rules have \"$text\""
  else
    [ "$want" = lacks ] || failed "$name: the rules lack \"$text\""
  fi
}
for alert in KubeatDown KubeatSchedulerStalled KubeatMetricsIncomplete KubeatResultsDropped \
  KubeatNotificationsFailing KubeatWorkersSaturated KubeatBackupFailing KubeatBackupStale KubeatLicenseExpiring; do
  rules "$alert with backups" has "alert: $alert" --set backup.enabled=true
done
rules "KubeatBackupStale without backups" lacks "alert: KubeatBackupStale"
rules "KubeatBackupFailing without backups" has "alert: KubeatBackupFailing"
# A failed attempt fires the notifications alert, not only a notification
# given up on: the latter stays silent while an outage notification is
# attempted again.
rules "KubeatNotificationsFailing counts every attempt" has 'increase(kubeat_notification_attempts_total{result="failed"}[15m]) > 0'
rules "KubeatNotificationsFailing counts the dropped" has 'increase(kubeat_notifications_dropped_total[15m]) > 0'
rules "KubeatNotificationsFailing does not wait for the engine to give up" lacks 'kubeat_notifications_total{result="failed"}'
rules "license warning at 30 days" has "< 30 * 86400"
rules "license critical at 7 days" has "< 7 * 86400"
for interval in 24h:86400 12h:43200 90m:5400 1h30m:5400 1.5h:5400 2h30m15s:9015 1h0m0s:3600 30s:30; do
  rules "stale threshold at ${interval%:*}" has "> 2 * ${interval#*:} and" --set backup.enabled=true --set backup.interval="${interval%:*}"
done
for interval in 1d 24 1.5 h 24H 24h30 ""; do
  refuses "backup.interval $interval" "backup.interval is \"$interval\", not a duration" \
    "${RULES[@]}" --set backup.enabled=true --set backup.interval="$interval"
done

if [ "$FAILED" -gt 0 ]; then
  echo "$FAILED chart check(s) failed"
  exit 1
fi
echo "chart: all checks passed"

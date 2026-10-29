#!/usr/bin/env bash
# Renders the chart with values it must take and values it must refuse, and
# checks what the install notes say for some of them. Needs helm; it never
# talks to a cluster. With promtool (PROMTOOL, or promtool on the PATH) it
# also checks the alert rules and runs prometheusrule-test.yaml against them;
# without it that part is skipped, except under CI, where it fails.
# Usage: deploy/helm/test-chart.sh, or PROMTOOL=/path/to/promtool deploy/helm/test-chart.sh
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

# Outside OpenShift, whose SCC assigns an fsGroup, the backend can write to
# volumes owned by root only with podSecurityContext.fsGroup: the install
# notes warn while it is empty. The cluster's APIs pick the text alone; the
# chart sets no fsGroup of its own.
UNWRITABLE="the backend may be unable to write to its volumes"
notes "no podSecurityContext" says "$UNWRITABLE"
notes "fsGroup given" omits "$UNWRITABLE" --set podSecurityContext.fsGroup=1001
notes "OpenShift" omits "$UNWRITABLE" --api-versions security.openshift.io/v1
notes "OpenShift with an Ingress" omits "$UNWRITABLE" --api-versions security.openshift.io/v1 --set ingress.enabled=true
helm template kubeat "$CHART" | grep -q "fsGroup" && failed "fsGroup: the chart sets one of its own"

# Removing the release leaves the database's and the backups' PVCs: helm
# uninstall skips what carries helm.sh/resource-policy, deleting an Argo CD
# application (2.7 or later) what carries either that or Delete=false. Each
# claim has a switch of its own, read with a default of true: a value set
# to null, as a release upgraded with --reuse-values from 0.43 renders
# without it, keeps them too.
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

# The backend's grace period, which must outlast UPTIME_SHUTDOWN_TIMEOUT:
# 30 seconds by default, as given, and the default for values without the
# key, as an upgrade with --reuse-values from before it renders.
# grace NAME SECONDS VALUES...: the backend renders with this grace period.
grace() {
  local name="$1" want="$2"
  shift 2
  if ! helm template kubeat "$CHART" --show-only templates/backend.yaml "$@" >"$WORK/backend.yaml" 2>"$WORK/err"; then
    failed "$name: refused: $(cat "$WORK/err")"
  elif ! grep -qx "      terminationGracePeriodSeconds: $want" "$WORK/backend.yaml"; then
    failed "$name: want terminationGracePeriodSeconds: $want, got: $(grep -F terminationGracePeriodSeconds: "$WORK/backend.yaml")"
  fi
}
grace "grace period by default" 30
grace "grace period given" 45 --set backend.terminationGracePeriodSeconds=45
grace "grace period missing from older values" 30 --set backend.terminationGracePeriodSeconds=null

# Kubeat's own failures: the install notes warn while neither a heartbeat
# nor the alert rules would tell of them, and say when no backups are taken.
UNHEARD="nobody hears when Kubeat itself fails"
notes "no heartbeat, no rules" says "$UNHEARD"
notes "no heartbeat, no rules" says '"Aracın kendi sağlığı"'
notes "heartbeat from its Secret" omits "$UNHEARD" --set heartbeat.urlSecret.name=kubeat-heartbeat
notes "heartbeat in extraEnv" omits "$UNHEARD" --set backend.extraEnv.UPTIME_HEARTBEAT_URL=https://hc-ping.example/x
notes "empty heartbeat URL in extraEnv" says "$UNHEARD" --set-string backend.extraEnv.UPTIME_HEARTBEAT_URL=
notes "alert rules" omits "$UNHEARD" --set metrics.prometheusRule.enabled=true
NO_BACKUPS="No backups of the database are taken"
notes "backups off" says "$NO_BACKUPS"
notes "backups off" says "Velero, OADP"
notes "backups on" omits "$NO_BACKUPS" "${BACKUPS[@]}"

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
for alert in KubeatDown KubeatSchedulerStalled KubeatUnhealthy KubeatMetricsIncomplete KubeatResultsDropped \
  KubeatNotificationsFailing KubeatWorkersSaturated KubeatBackupFailing KubeatBackupStale KubeatLicenseExpiring; do
  rules "$alert with backups" has "alert: $alert" --set backup.enabled=true
done
rules "KubeatBackupStale without backups" lacks "alert: KubeatBackupStale"
rules "KubeatBackupFailing without backups" has "alert: KubeatBackupFailing"
# The notifications alert is one per channel: the channel's last attempt
# failed, or an outage it did not take waits to be sent to it again, held
# over a scrape gap; notifications dropped from a full queue stay a separate
# expression. It no longer counts failed attempts by channel type, nor waits
# for the engine to give up. promtool below shows how it behaves.
rules "KubeatNotificationsFailing on a channel's last attempt" has 'max without (instance, pod, endpoint, container, service) (kubeat_channel_last_delivery_ok) == 0'
rules "KubeatNotificationsFailing while an outage waits for a channel" has 'max without (instance, pod, endpoint, container, service) (kubeat_channel_undelivered_outages) > 0'
rules "KubeatNotificationsFailing over a scrape gap" has 'last_over_time(kubeat_channel_undelivered_outages[10m])'
rules "KubeatNotificationsFailing counts the dropped" has 'increase(kubeat_notifications_dropped_total[15m]) > 0'
rules "KubeatNotificationsFailing per channel, not per channel type" lacks 'kubeat_notification_attempts_total'
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
# KubeatDown looks for the backend's up series in the release's namespace,
# where an absent one fires it too; the outage and backup alerts hold their
# state over a scrape gap. promtool below shows how they behave.
rules "KubeatDown without a backend target" has 'absent(up{job="kubeat-backend", namespace="default"})'
rules "KubeatMonitorDown over a scrape gap" has 'last_over_time(kubeat_monitor_incident_open[10m])'
rules "KubeatBackupFailing until a backup succeeds" has 'max_over_time(kubeat_backup_last_failure_timestamp_seconds[10m])'
# KubeatUnhealthy reads what the heartbeat judges, one alert across a
# Recreate; a loss of some results stays KubeatResultsDropped's warning.
rules "KubeatUnhealthy reads the self-health gauge" has 'expr: max without (instance, pod, endpoint, container, service) (kubeat_self_healthy) == 0'

# metrics.prometheusRule.clusterLabel, for rules evaluated over the series of
# several clusters in one store. Empty, the rules it changes render as 0.44
# rendered them, and KubeatUnhealthy and KubeatNotificationsFailing, whose
# expressions came with 0.45, without the scrape labels as those do; given,
# they group by it beside namespace and job and keep no other label a store
# adds, and every other rule renders the same either way. promtool below
# shows how they behave over two clusters' series.
CLUSTER=(--set metrics.prometheusRule.clusterLabel=cluster)
BY_CLUSTER='^(KubeatMonitorDown|KubeatCertificateExpiring|KubeatDown|KubeatUnhealthy|KubeatBackupFailing|KubeatNotificationsFailing): '
# exprs FILE: the expression of every rule in a rendered PrometheusRule, each
# of its lines after the rule's alert name.
exprs() {
  awk '
    /^        - alert: / { alert = $3; inexpr = 0; next }
    /^          expr: \|-$/ { inexpr = 1; next }
    /^          expr: / { sub(/^          expr: /, ""); print alert ": " $0; next }
    inexpr && /^            / { sub(/^            /, ""); print alert ": " $0; next }
    { inexpr = 0 }
  ' "$1"
}
# What 0.44 rendered for release "kubeat" in namespace "kubeat", with backups.
cat >"$WORK/exprs-0.44" <<'EOF'
KubeatMonitorDown: max without (instance, pod, endpoint, container, service) (kubeat_monitor_incident_open) == 1
KubeatMonitorDown: or (
KubeatMonitorDown:   max without (instance, pod, endpoint, container, service) (last_over_time(kubeat_monitor_incident_open[10m])) == 1
KubeatMonitorDown:   unless on (namespace, job) (kubeat_monitor_scrape_success == 1)
KubeatMonitorDown: )
KubeatCertificateExpiring: (
KubeatCertificateExpiring:   max without (instance, pod, endpoint, container, service) (kubeat_monitor_cert_expiry_timestamp_seconds)
KubeatCertificateExpiring:   or (
KubeatCertificateExpiring:     max without (instance, pod, endpoint, container, service) (last_over_time(kubeat_monitor_cert_expiry_timestamp_seconds[10m]))
KubeatCertificateExpiring:     unless on (namespace, job) (kubeat_monitor_scrape_success == 1)
KubeatCertificateExpiring:   )
KubeatCertificateExpiring: ) - time() < 14 * 86400
KubeatDown: max by (namespace, job) (up{job="kubeat-backend", namespace="kubeat"}) == 0
KubeatDown: or absent(up{job="kubeat-backend", namespace="kubeat"})
KubeatBackupFailing: max without (instance, pod, endpoint, container, service) (max_over_time(kubeat_backup_last_failure_timestamp_seconds[10m]))
KubeatBackupFailing: > max without (instance, pod, endpoint, container, service) (max_over_time(kubeat_backup_last_success_timestamp_seconds[10m]))
EOF
# KubeatNotificationsFailing, whose expression came with 0.45. Empty, its
# channel series drop the scrape labels and are held by namespace and job,
# as the outage alerts above; given, they group by the cluster beside
# namespace, job and the channel, and are held per cluster, so that an HA
# pair raises one alert per channel and the other cluster's scrape does not
# resolve it. Dropped notifications look at each series either way, as the
# rules that need no cluster do.
cat >"$WORK/channels-plain" <<'EOF'
KubeatNotificationsFailing: (
KubeatNotificationsFailing:   max without (instance, pod, endpoint, container, service) (kubeat_channel_last_delivery_ok) == 0
KubeatNotificationsFailing:   or max without (instance, pod, endpoint, container, service) (kubeat_channel_undelivered_outages) > 0
KubeatNotificationsFailing: )
KubeatNotificationsFailing: or (
KubeatNotificationsFailing:   (
KubeatNotificationsFailing:     max without (instance, pod, endpoint, container, service) (last_over_time(kubeat_channel_last_delivery_ok[10m])) == 0
KubeatNotificationsFailing:     or max without (instance, pod, endpoint, container, service) (last_over_time(kubeat_channel_undelivered_outages[10m])) > 0
KubeatNotificationsFailing:   )
KubeatNotificationsFailing:   unless on (namespace, job) (kubeat_monitor_scrape_success == 1)
KubeatNotificationsFailing: )
KubeatNotificationsFailing: or increase(kubeat_notifications_dropped_total[15m]) > 0
EOF
cat >"$WORK/channels-cluster" <<'EOF'
KubeatNotificationsFailing: (
KubeatNotificationsFailing:   max by (namespace, job, cluster, channel, channel_type) (kubeat_channel_last_delivery_ok) == 0
KubeatNotificationsFailing:   or max by (namespace, job, cluster, channel, channel_type) (kubeat_channel_undelivered_outages) > 0
KubeatNotificationsFailing: )
KubeatNotificationsFailing: or (
KubeatNotificationsFailing:   (
KubeatNotificationsFailing:     max by (namespace, job, cluster, channel, channel_type) (last_over_time(kubeat_channel_last_delivery_ok[10m])) == 0
KubeatNotificationsFailing:     or max by (namespace, job, cluster, channel, channel_type) (last_over_time(kubeat_channel_undelivered_outages[10m])) > 0
KubeatNotificationsFailing:   )
KubeatNotificationsFailing:   unless on (namespace, job, cluster) (kubeat_monitor_scrape_success == 1)
KubeatNotificationsFailing: )
KubeatNotificationsFailing: or increase(kubeat_notifications_dropped_total[15m]) > 0
EOF
if helm template kubeat "$CHART" --namespace kubeat --show-only templates/prometheusrule.yaml "${RULES[@]}" \
  --set backup.enabled=true >"$WORK/rules-plain" 2>"$WORK/err" &&
  helm template kubeat "$CHART" --namespace kubeat --show-only templates/prometheusrule.yaml "${RULES[@]}" \
    --set backup.enabled=true "${CLUSTER[@]}" >"$WORK/rules-cluster" 2>"$WORK/err"; then
  exprs "$WORK/rules-plain" | grep -E "$BY_CLUSTER" | grep -vE '^(KubeatUnhealthy|KubeatNotificationsFailing): ' |
    diff "$WORK/exprs-0.44" - >"$WORK/diff" ||
    failed "clusterLabel empty: the rules differ from 0.44's: $(cat "$WORK/diff")"
  exprs "$WORK/rules-plain" | grep '^KubeatNotificationsFailing: ' | diff "$WORK/channels-plain" - >"$WORK/diff" ||
    failed "clusterLabel empty: KubeatNotificationsFailing differs: $(cat "$WORK/diff")"
  exprs "$WORK/rules-cluster" | grep '^KubeatNotificationsFailing: ' | diff "$WORK/channels-cluster" - >"$WORK/diff" ||
    failed "clusterLabel: KubeatNotificationsFailing is not per cluster and channel: $(cat "$WORK/diff")"
  exprs "$WORK/rules-plain" | grep -vE "$BY_CLUSTER" >"$WORK/others-plain"
  exprs "$WORK/rules-cluster" | grep -vE "$BY_CLUSTER" >"$WORK/others-cluster"
  [ -s "$WORK/others-plain" ] || failed "clusterLabel: no other rule found in the render"
  diff "$WORK/others-plain" "$WORK/others-cluster" >"$WORK/diff" ||
    failed "clusterLabel changes rules that need no cluster: $(cat "$WORK/diff")"
else
  failed "clusterLabel: refused: $(cat "$WORK/err")"
fi
rules "clusterLabel missing from older values" lacks '[24h]' --set metrics.prometheusRule.clusterLabel=null
rules "KubeatDown per cluster" has 'max by (namespace, job, cluster) (up{job="kubeat-backend", namespace="default"}) == 0' "${CLUSTER[@]}"
rules "KubeatDown for a cluster whose backend is gone" has 'group by (namespace, job, cluster) (max_over_time(up{job="kubeat-backend", namespace="default"}[24h]))' "${CLUSTER[@]}"
rules "KubeatDown for a cluster whose backend is gone" has 'unless group by (namespace, job, cluster) (up{job="kubeat-backend", namespace="default"})' "${CLUSTER[@]}"
rules "KubeatDown without a backend in any cluster" has 'or absent(up{job="kubeat-backend", namespace="default"})' "${CLUSTER[@]}"
rules "outages held per cluster" has 'unless on (namespace, job, cluster) (kubeat_monitor_scrape_success == 1)' "${CLUSTER[@]}"
rules "outages per cluster and monitor" has 'max by (namespace, job, cluster, monitor, monitor_namespace, type) (kubeat_monitor_incident_open) == 1' "${CLUSTER[@]}"
rules "KubeatUnhealthy per cluster" has 'expr: max by (namespace, job, cluster) (kubeat_self_healthy) == 0' "${CLUSTER[@]}"
rules "no store label kept but the cluster's" lacks 'without (' "${CLUSTER[@]}"
for label in k8s-cluster 1cluster __cluster cluster.name namespace job pod monitor_namespace channel channel_type severity; do
  refuses "clusterLabel $label" "metrics.prometheusRule.clusterLabel is \"$label\"" "${RULES[@]}" --set-string metrics.prometheusRule.clusterLabel="$label"
done
for label in cluster k8s_cluster _cluster Cluster1; do
  renders "clusterLabel $label" "${RULES[@]}" --set-string metrics.prometheusRule.clusterLabel="$label"
done

# The rules as Prometheus reads them: promtool checks the rule groups and
# runs the scenarios of prometheusrule-test.yaml (no backend target, a failed
# scrape, a Recreate, an outage that closes, a failing backup, a backend that
# stores no result, a channel that does not take notifications) against the
# rules rendered for release "kubeat" in namespace "kubeat", and those of
# prometheusrule-cluster-test.yaml (two clusters, one of them failing, gone,
# in a scrape gap or recreated, and an HA pair the store does not
# deduplicate) against the rules rendered with clusterLabel=cluster.
PROMTOOL="${PROMTOOL:-promtool}"
# rulefile FILE VALUES...: writes the rule groups of the PrometheusRule
# rendered with these values (and the rules on) to FILE.
rulefile() {
  local file="$1"
  shift
  if ! helm template kubeat "$CHART" --namespace kubeat --show-only templates/prometheusrule.yaml "${RULES[@]}" "$@" >"$WORK/rendered" 2>"$WORK/err"; then
    failed "rule file: refused: $(cat "$WORK/err")"
    return 1
  fi
  # A rule file is the PrometheusRule's spec, which is indented under spec:.
  awk 'found { sub(/^  /, ""); print } /^spec:$/ { found = 1 }' "$WORK/rendered" >"$file"
}
if command -v "$PROMTOOL" >/dev/null 2>&1; then
  mkdir -p "$WORK/promtool"
  cp "$CHART/../prometheusrule-test.yaml" "$CHART/../prometheusrule-cluster-test.yaml" "$WORK/promtool/"
  if rulefile "$WORK/promtool/rules.yaml" --set backup.enabled=true &&
    rulefile "$WORK/promtool/rules-without-backups.yaml" &&
    rulefile "$WORK/promtool/rules-cluster.yaml" --set backup.enabled=true "${CLUSTER[@]}"; then
    "$PROMTOOL" check rules --lint-fatal "$WORK/promtool/rules.yaml" "$WORK/promtool/rules-without-backups.yaml" \
      "$WORK/promtool/rules-cluster.yaml" >"$WORK/out" 2>&1 ||
      failed "promtool check rules: $(cat "$WORK/out")"
    "$PROMTOOL" test rules "$WORK/promtool/prometheusrule-test.yaml" "$WORK/promtool/prometheusrule-cluster-test.yaml" >"$WORK/out" 2>&1 ||
      failed "promtool test rules: $(cat "$WORK/out")"
  fi
elif [ -n "${CI:-}" ]; then
  failed "promtool not found: under CI the alert rules must be checked with it"
else
  echo "WARN: promtool not found, the alert rules were not checked with it (set PROMTOOL or put promtool on the PATH)"
fi

if [ "$FAILED" -gt 0 ]; then
  echo "$FAILED chart check(s) failed"
  exit 1
fi
echo "chart: all checks passed"

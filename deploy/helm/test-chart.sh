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

# The chart's own Secrets (the admin password, the API token, the metrics
# token) are left behind on the same switch as the database's PVC, so that a
# reinstall with the same release name and namespace keeps the password and
# tokens instead of regenerating them (which would break sign-in and any
# script or Prometheus using the old token). They carry helm.sh/resource-policy:
# keep, like the encryption key's Secret.
# secret NAME FILE has|lacks|absent TEXT VALUES...: the Secret from
# templates/FILE rendered with these values has TEXT, lacks it, or is not
# rendered at all.
secret() {
  local name="$1" file="$2" want="$3" text="$4"
  shift 4
  if ! helm template kubeat "$CHART" --show-only "templates/$file" "$@" >"$WORK/secret" 2>"$WORK/err"; then
    if grep -q "could not find template" "$WORK/err"; then
      [ "$want" = absent ] || failed "$name: $file is not rendered"
    else
      failed "$name: refused: $(cat "$WORK/err")"
    fi
    return
  fi
  if [ "$want" = absent ]; then
    failed "$name: $file is rendered"
  elif grep -qF -- "$text" "$WORK/secret"; then
    [ "$want" = has ] || failed "$name: $file has \"$text\""
  else
    [ "$want" = lacks ] || failed "$name: $file lacks \"$text\""
  fi
}
for f in admin-secret.yaml api-token.yaml metrics-token.yaml frontend-token.yaml; do
  secret "defaults" "$f" has "helm.sh/resource-policy: keep"
  # A release upgraded with --reuse-values from before the switch renders it
  # as null; read with a default of true, the Secret is kept all the same.
  secret "no keepOnUninstall value" "$f" has "helm.sh/resource-policy: keep" --set backend.persistence.keepOnUninstall=null
  secret "data not kept" "$f" lacks "helm.sh/resource-policy: keep" --set backend.persistence.keepOnUninstall=false
done
# The metrics token's Secret exists only with metrics auth on; a Secret of
# one's own (existingSecret) the chart neither renders nor annotates.
secret "metrics auth off" metrics-token.yaml absent "" --set metrics.auth.enabled=false
secret "auth.existingSecret" admin-secret.yaml absent "" --set auth.existingSecret=my-admin
secret "apiToken.existingSecret" api-token.yaml absent "" --set apiToken.existingSecret=my-api-token
secret "frontendToken.existingSecret" frontend-token.yaml absent "" --set frontendToken.existingSecret=my-frontend-token
# A release upgraded with --reuse-values from 0.49 has no frontendToken: the
# chart generates the Secret all the same.
secret "frontendToken missing from older values" frontend-token.yaml has "token:" --set frontendToken=null

# The install notes name the kept Secrets and the command that deletes them,
# and that command names exactly the ones the chart generated and kept.
DELETE_SECRET="kubectl -n kubeat delete secret"
notes "kept secrets" says "$DELETE_SECRET kubeat-admin kubeat-api-token kubeat-metrics-token kubeat-frontend-token" --namespace kubeat
notes "kept secrets without the value" says "$DELETE_SECRET kubeat-admin kubeat-api-token kubeat-metrics-token kubeat-frontend-token" \
  --namespace kubeat --set backend.persistence.keepOnUninstall=null
notes "frontend token from frontendToken.existingSecret" says "$DELETE_SECRET kubeat-admin kubeat-api-token kubeat-metrics-token" \
  --namespace kubeat --set frontendToken.existingSecret=my-frontend-token
notes "frontend token from frontendToken.existingSecret" omits "kubeat-frontend-token" \
  --namespace kubeat --set frontendToken.existingSecret=my-frontend-token
notes "secrets not kept" omits "delete secret" --namespace kubeat --set backend.persistence.keepOnUninstall=false
notes "admin from auth.existingSecret" says "$DELETE_SECRET kubeat-api-token kubeat-metrics-token" --namespace kubeat --set auth.existingSecret=my-admin
notes "admin from auth.existingSecret" omits "kubeat-admin" --namespace kubeat --set auth.existingSecret=my-admin
notes "metrics auth off" says "$DELETE_SECRET kubeat-admin kubeat-api-token" --namespace kubeat --set metrics.auth.enabled=false
notes "metrics auth off" omits "kubeat-metrics-token" --namespace kubeat --set metrics.auth.enabled=false

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

# The sign-in throttle behind the Route or Ingress: the notes give README
# "Hesaplar ve giriş"'s hop values while UPTIME_TRUST_PROXY_HOPS is 0, and
# the frontend's network policy while it is off; without a Route or Ingress,
# neither. The backend's own policy is no longer what keeps a pod from
# picking its source: the backend reads X-Forwarded-For only from the
# frontend, which presents its token; a pod that reaches the frontend's
# Service is what the frontend's policy keeps out. Below: one value for the
# Ingress class the notes know, the router's forwardedHeaderPolicy with a
# Route, the frontend's policy once on, the README that gives what the
# notes suggest, and the pilot's values.
FRONTEND_POLICY=(--set frontend.networkPolicy.enabled=true)
THROTTLE="Sign-in throttle:"
HOPS_ZERO="UPTIME_TRUST_PROXY_HOPS is 0"
SET_HOPS="--set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1"
NEEDS_POLICY="--set frontend.networkPolicy.enabled=true"
OLD_POLICY="--set networkPolicy.enabled=true"
TOKEN_SECRET="with the Secret kubeat-frontend-token"
POLICY_ROUTER="which by default lets in only the OpenShift router"
POLICY_FROM="with frontend.networkPolicy.from set to your ingress controller's namespace"
POLICY_GUIDE='giriş", "Arayüzün ağ politikası")'
# With networkPolicy.enabled push jobs of other namespaces send to the
# frontend: the notes say to let them in.
POLICY_PUSH="list their namespaces in frontend.networkPolicy.from too"
# A value too high is no longer the safe side: behind a proxy that appends,
# a client lengthening the header picks its source, so the notes ask for the
# lower value when unsure.
HOPS_LOWER="If unsure, take the lower value and check the source it keys on"
INGRESS=(--set ingress.enabled=true)
notes "Route, hops unset" says "$HOPS_ZERO" "${ROUTE[@]}"
notes "Route, hops unset" says "a plain Route: 1; a Route behind a WAF" "${ROUTE[@]}"
notes "Route, hops unset" omits "ingress-nginx" "${ROUTE[@]}"
notes "Route, hops unset" says "$HOPS_LOWER" "${ROUTE[@]}"
notes "Route, hops unset" says "$SET_HOPS" "${ROUTE[@]}"
notes "Route, hops unset" says "$NEEDS_POLICY" "${ROUTE[@]}"
notes "Route, hops unset" omits "$OLD_POLICY" "${ROUTE[@]}"
notes "Route, hops unset" says "$TOKEN_SECRET" "${ROUTE[@]}"
notes "Route, hops unset" says "$POLICY_ROUTER" "${ROUTE[@]}"
notes "Route, hops unset" omits "$POLICY_FROM" "${ROUTE[@]}"
notes "Route, hops unset" says "$POLICY_GUIDE" "${ROUTE[@]}"
notes "Route, hops unset" omits "$POLICY_PUSH" "${ROUTE[@]}"
notes "Ingress, hops unset" says "$HOPS_ZERO" "${INGRESS[@]}"
notes "Ingress, hops unset" says "ingress-nginx with its default settings: 1; GKE's own Ingress (GCE): 2;" "${INGRESS[@]}"
notes "Ingress, hops unset" says "use-forwarded-headers and compute-full-forwarded-for: 3" "${INGRESS[@]}"
notes "Ingress, hops unset" omits "a plain Route" "${INGRESS[@]}"
notes "Ingress, hops unset" says "$HOPS_LOWER" "${INGRESS[@]}"
notes "Ingress, hops unset" says "$NEEDS_POLICY" "${INGRESS[@]}"
notes "Ingress, hops unset" says "$POLICY_FROM" "${INGRESS[@]}"
notes "Ingress, hops unset" omits "$POLICY_ROUTER" "${INGRESS[@]}"
notes "Ingress, hops unset" says "$POLICY_GUIDE" "${INGRESS[@]}"
notes "neither Route nor Ingress" omits "$THROTTLE"
notes "neither Route nor Ingress, policy off" omits "$NEEDS_POLICY"
notes "Route, hops 0 given" says "$HOPS_ZERO" "${ROUTE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=0
notes "Ingress, hops empty" says "$HOPS_ZERO" "${INGRESS[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=
notes "Route, hops 1" omits "$HOPS_ZERO" "${ROUTE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1
notes "Route, hops 1" omits "$HOPS_LOWER" "${ROUTE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1
notes "Route, hops 1" says "$NEEDS_POLICY" "${ROUTE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1
notes "Ingress, hops 2 as a number" omits "$HOPS_ZERO" "${INGRESS[@]}" --set backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
# The backend's policy is not the frontend's: on its own it still leaves the
# frontend open, and the notes still ask for the frontend's, saying that the
# push jobs it sends to the frontend must be let in.
notes "Route, hops 1, backend policy on" says "$NEEDS_POLICY" "${ROUTE[@]}" \
  --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1 --set networkPolicy.enabled=true
notes "Route, hops 1, backend policy on" says "$POLICY_PUSH" "${ROUTE[@]}" \
  --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1 --set networkPolicy.enabled=true
notes "Route, hops unset, frontend policy on" says "$HOPS_ZERO" "${ROUTE[@]}" --set frontend.networkPolicy.enabled=true
notes "Route, hops unset, frontend policy on" omits "$NEEDS_POLICY" "${ROUTE[@]}" --set frontend.networkPolicy.enabled=true
notes "frontend token of one's own" says "with the Secret my-frontend-token" "${ROUTE[@]}" \
  --set frontendToken.existingSecret=my-frontend-token

# One suggestion for the Ingress class the notes know: ingress-nginx's 1
# holds only with externalTrafficPolicy: Local on its Service; GKE's own
# Ingress, which reads its class from the kubernetes.io/ingress.class
# annotation rather than ingressClassName, takes 2. Any other class, or
# none, gets the three of before.
NGINX_ONE="ingress-nginx: 1, provided its LoadBalancer Service has"
NGINX_LOCAL="--set controller.service.externalTrafficPolicy=Local"
GCE_TWO="GKE's own Ingress (GCE): 2"
SET_HOPS_TWO="--set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2"
NGINX=(--set ingress.enabled=true --set ingress.className=nginx)
GCE=(--set ingress.enabled=true --set 'ingress.annotations.kubernetes\.io/ingress\.class=gce')
notes "Ingress nginx, hops unset" says "$NGINX_ONE" "${NGINX[@]}"
notes "Ingress nginx, hops unset" says "$NGINX_LOCAL" "${NGINX[@]}"
notes "Ingress nginx, hops unset" says "$SET_HOPS" "${NGINX[@]}"
notes "Ingress nginx, hops unset" omits "$GCE_TWO" "${NGINX[@]}"
notes "Ingress nginx, hops unset" omits "compute-full-forwarded-for: 3" "${NGINX[@]}"
notes "Ingress nginx, hops unset" says "$HOPS_LOWER" "${NGINX[@]}"
notes "Ingress gce annotation, hops unset" says "$GCE_TWO" "${GCE[@]}"
notes "Ingress gce annotation, hops unset" says "$SET_HOPS_TWO" "${GCE[@]}"
notes "Ingress gce annotation, hops unset" omits "$SET_HOPS" "${GCE[@]}"
notes "Ingress gce annotation, hops unset" omits "ingress-nginx" "${GCE[@]}"
notes "Ingress gce className, hops unset" says "$SET_HOPS_TWO" --set ingress.enabled=true --set ingress.className=gce
notes "Ingress nginx, hops 1" omits "$NGINX_ONE" "${NGINX[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1
for class in traefik ""; do
  notes "Ingress class \"$class\", hops unset" says "ingress-nginx with its default settings: 1; GKE's own Ingress (GCE): 2;" \
    --set ingress.enabled=true --set-string ingress.className="$class"
  notes "Ingress class \"$class\", hops unset" omits "$NGINX_LOCAL" --set ingress.enabled=true --set-string ingress.className="$class"
done
notes "Route with an Ingress class left in the values" says "a plain Route: 1" "${ROUTE[@]}" --set ingress.className=gce
notes "Route with an Ingress class left in the values" says "$SET_HOPS" "${ROUTE[@]}" --set ingress.className=gce

# A value given that is not the class's own gets a warning of its own
# (docs/agents/sprint-takimi.md, step 2: a contradiction gets a line too):
# GCE's other than 2, ingress-nginx's other than 1, with the class's value
# and the README section that gives it; 0 keeps the suggestion above. Any
# other class, and a Route, which keeps its own note, get none. The section
# shows for the contradiction alone, and with the alert rules on names
# KubeatSignInSourceDegraded, which tells of a wrong value where the notes
# are not read.
HOPS_CONFLICT="WARNING: UPTIME_TRUST_PROXY_HOPS is"
GCE_CONFLICT="but behind GKE's own Ingress"
NGINX_CONFLICT="but behind ingress-nginx it is 1,"
HOP_GUIDE='(README, "Hesaplar ve giriş", "Vekil-hop rehberi")'
GIVEN_ONE=(--set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1)
for hops in 1 3; do
  notes "Ingress gce annotation, hops $hops" says "WARNING: UPTIME_TRUST_PROXY_HOPS is $hops, $GCE_CONFLICT" "${GCE[@]}" \
    --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS="$hops"
done
# Read as the backend reads it: "02" is 2, "00" is 0.
notes "Ingress gce annotation, hops 02" omits "$HOPS_CONFLICT" "${GCE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=02
notes "Ingress gce annotation, hops 00" says "$HOPS_ZERO" "${GCE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=00
notes "Ingress gce annotation, hops 1" says "$SET_HOPS_TWO" "${GCE[@]}" "${GIVEN_ONE[@]}"
notes "Ingress gce annotation, hops 1" says "$HOP_GUIDE" "${GCE[@]}" "${GIVEN_ONE[@]}"
notes "Ingress gce annotation, hops 1" omits "$HOPS_ZERO" "${GCE[@]}" "${GIVEN_ONE[@]}"
notes "Ingress gce className, hops 1" says "$GCE_CONFLICT" --set ingress.enabled=true --set ingress.className=gce "${GIVEN_ONE[@]}"
notes "Ingress gce, hops 1, frontend policy on" says "$THROTTLE" "${GCE[@]}" "${GIVEN_ONE[@]}" "${FRONTEND_POLICY[@]}"
notes "Ingress gce annotation, hops 2" omits "$HOPS_CONFLICT" "${GCE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
notes "Ingress gce annotation, hops 2 as a number" omits "$HOPS_CONFLICT" "${GCE[@]}" --set backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
notes "Ingress gce annotation, hops unset" omits "$HOPS_CONFLICT" "${GCE[@]}"
notes "Ingress nginx, hops 2" says "WARNING: UPTIME_TRUST_PROXY_HOPS is 2, $NGINX_CONFLICT" "${NGINX[@]}" \
  --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
notes "Ingress nginx, hops 2" says "$SET_HOPS" "${NGINX[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
# 3 is right behind an L7 Google Cloud load balancer; the line says so and
# that the warning stays there, and points at the hop guide.
for text in "compute-full-forwarded-for on, it is 1 plus the" "3 behind an L7 Google Cloud load" \
  "Keep such a value; this warning stays" "$HOP_GUIDE"; do
  notes "Ingress nginx, hops 2" says "$text" "${NGINX[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
done
notes "Ingress nginx, hops 3" says "WARNING: UPTIME_TRUST_PROXY_HOPS is 3, $NGINX_CONFLICT" "${NGINX[@]}" \
  --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=3
notes "Ingress nginx, hops 1" omits "$HOPS_CONFLICT" "${NGINX[@]}" "${GIVEN_ONE[@]}"
notes "Ingress nginx, hops unset" omits "$HOPS_CONFLICT" "${NGINX[@]}"
for class in traefik ""; do
  notes "Ingress class \"$class\", hops 2" omits "$HOPS_CONFLICT" --set ingress.enabled=true --set-string ingress.className="$class" \
    --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
done
notes "Route with an Ingress class gce left in the values, hops 1" omits "$HOPS_CONFLICT" "${ROUTE[@]}" --set ingress.className=gce "${GIVEN_ONE[@]}"
notes "Route, hops 2" omits "$HOPS_CONFLICT" "${ROUTE[@]}" --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=2
RULE_LINE="With the alert rules on, KubeatSignInSourceDegraded tells of a wrong"
RULE_GUIDE='"Hesaplar ve giriş", "Sayaç ve kural")'
WITH_RULES=(--set metrics.prometheusRule.enabled=true)
notes "Route, hops unset, rules on" says "$RULE_LINE" "${ROUTE[@]}" "${WITH_RULES[@]}"
notes "Route, hops unset, rules on" says "at 0 it fires" "${ROUTE[@]}" "${WITH_RULES[@]}"
notes "Route, hops unset, rules on" says "$RULE_GUIDE" "${ROUTE[@]}" "${WITH_RULES[@]}"
notes "Route, hops unset, rules off" omits "$RULE_LINE" "${ROUTE[@]}"
notes "Ingress gce annotation, hops 1, rules on" says "$RULE_LINE" "${GCE[@]}" "${GIVEN_ONE[@]}" "${WITH_RULES[@]}"
notes "Ingress gce annotation, hops 1, rules on" omits "at 0 it fires" "${GCE[@]}" "${GIVEN_ONE[@]}" "${WITH_RULES[@]}"
notes "neither Route nor Ingress, rules on" omits "$RULE_LINE" "${WITH_RULES[@]}"
notes "Ingress, hops 1, frontend policy on, rules on" omits "$RULE_LINE" "${INGRESS[@]}" "${GIVEN_ONE[@]}" "${FRONTEND_POLICY[@]}" "${WITH_RULES[@]}"

# A Route's value holds under the router's forwardedHeaderPolicy Append or
# Replace; under IfNone or Never a client writes the header. With a Route
# the notes say so whatever the value, give the oc line that reads the
# policy (which needs cluster-wide read access) and, for whoever lacks it,
# the Route's own policy; once the Route pins append or replace, not.
ROUTER_POLICY_CMD="oc -n openshift-ingress-operator get ingresscontroller default -o jsonpath='{.spec.httpHeaders.forwardedHeaderPolicy}{\"\\n\"}'"
ROUTER_ACCESS="it needs cluster-wide read access"
# README's line that tells a router on the pod network from one on the host
# network, for the frontend policy's "from".
ROUTER_NETWORK_CMD="oc -n openshift-ingress-operator get ingresscontroller default -o jsonpath='{.status.endpointPublishingStrategy.type}{\"\\n\"}'"
ROUTE_OWN_POLICY="--set-string 'route.annotations.haproxy\\.router\\.openshift\\.io/set-forwarded-headers=append'"
ROUTE_PINNED=(--set-string 'route.annotations.haproxy\.router\.openshift\.io/set-forwarded-headers=append')
HOPS_ONE=(--set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1)
notes "Route, hops unset" says "IfNone or Never it passes on a header the client wrote" "${ROUTE[@]}"
notes "Route, hops unset" says "$ROUTER_POLICY_CMD" "${ROUTE[@]}"
notes "Route, hops unset" says "$ROUTER_ACCESS" "${ROUTE[@]}"
notes "Route, hops unset" says "$ROUTE_OWN_POLICY" "${ROUTE[@]}"
notes "Route, hops 1, frontend policy on" says "$THROTTLE" "${ROUTE[@]}" "${HOPS_ONE[@]}" "${FRONTEND_POLICY[@]}"
notes "Route, hops 1, frontend policy on" says "$ROUTER_POLICY_CMD" "${ROUTE[@]}" "${HOPS_ONE[@]}" "${FRONTEND_POLICY[@]}"
notes "Route, hops 1, frontend policy on" omits "$HOPS_ZERO" "${ROUTE[@]}" "${HOPS_ONE[@]}" "${FRONTEND_POLICY[@]}"
notes "Route, hops 1, frontend policy on" omits "$NEEDS_POLICY" "${ROUTE[@]}" "${HOPS_ONE[@]}" "${FRONTEND_POLICY[@]}"
notes "Route pinning append, hops 1, frontend policy on" omits "$THROTTLE" "${ROUTE[@]}" "${ROUTE_PINNED[@]}" "${HOPS_ONE[@]}" "${FRONTEND_POLICY[@]}"
notes "Route pinning replace" omits "$ROUTER_POLICY_CMD" "${ROUTE[@]}" \
  --set-string 'route.annotations.haproxy\.router\.openshift\.io/set-forwarded-headers=Replace'
notes "Route pinning if-none" says "$ROUTER_POLICY_CMD" "${ROUTE[@]}" \
  --set-string 'route.annotations.haproxy\.router\.openshift\.io/set-forwarded-headers=if-none'
notes "Ingress, hops unset" omits "$ROUTER_POLICY_CMD" "${INGRESS[@]}"
notes "Ingress, hops 1, frontend policy on" omits "$THROTTLE" "${INGRESS[@]}" "${HOPS_ONE[@]}" "${FRONTEND_POLICY[@]}"
# The annotation the notes give is the one the chart puts on the Route.
helm template kubeat "$CHART" --show-only templates/expose.yaml "${ROUTE[@]}" "${ROUTE_PINNED[@]}" >"$WORK/route" 2>"$WORK/err" ||
  failed "Route's own forwarded-headers policy: refused: $(cat "$WORK/err")"
grep -qF "haproxy.router.openshift.io/set-forwarded-headers: append" "$WORK/route" ||
  failed "Route's own forwarded-headers policy: the Route lacks the annotation the notes give"

# The frontend's policy on: the notes ask for a check that it is enforced,
# with a Route, an Ingress or neither; with the backend's policy on too, to
# let the push jobs of other namespaces in. Its default "from" selects the
# OpenShift router alone: where the render does not see OpenShift's API
# (another cluster, or helm template without --api-versions, as GitOps tools
# render), a warning, never a refused render.
POLICY_CHECK="frontend.networkPolicy is on: check that the cluster enforces it"
POLICY_CHECK_URL="http://kubeat-frontend.kubeat.svc:3000/login"
POLICY_PUSH_BOTH="frontend.networkPolicy is on too: list those jobs' namespaces in"
DEFAULT_FROM_WARN="WARNING: frontend.networkPolicy.from is left at its default"
OPENSHIFT=(--api-versions security.openshift.io/v1)
notes "frontend policy on, Route" says "$POLICY_CHECK" "${ROUTE[@]}" "${FRONTEND_POLICY[@]}" "${OPENSHIFT[@]}"
notes "frontend policy on, Ingress" says "$POLICY_CHECK" "${INGRESS[@]}" "${FRONTEND_POLICY[@]}"
notes "frontend policy on, neither Route nor Ingress" says "$POLICY_CHECK" "${FRONTEND_POLICY[@]}"
notes "frontend policy on" says "$POLICY_CHECK_URL" --namespace kubeat "${FRONTEND_POLICY[@]}"
notes "frontend policy off" omits "$POLICY_CHECK" "${ROUTE[@]}"
notes "both policies" says "$POLICY_PUSH_BOTH" "${INGRESS[@]}" "${FRONTEND_POLICY[@]}" --set networkPolicy.enabled=true
notes "both policies, hops 1" says "$POLICY_PUSH_BOTH" "${ROUTE[@]}" "${HOPS_ONE[@]}" "${FRONTEND_POLICY[@]}" --set networkPolicy.enabled=true
notes "frontend policy alone" omits "$POLICY_PUSH_BOTH" "${INGRESS[@]}" "${FRONTEND_POLICY[@]}"
notes "backend policy alone" omits "$POLICY_PUSH_BOTH" "${INGRESS[@]}" --set networkPolicy.enabled=true
notes "default from, no OpenShift API" says "$DEFAULT_FROM_WARN" "${INGRESS[@]}" "${FRONTEND_POLICY[@]}"
notes "default from, no OpenShift API, Route" says "$DEFAULT_FROM_WARN" "${ROUTE[@]}" "${FRONTEND_POLICY[@]}"
notes "default from, OpenShift" omits "$DEFAULT_FROM_WARN" "${ROUTE[@]}" "${FRONTEND_POLICY[@]}" "${OPENSHIFT[@]}"
notes "default from given by hand" says "$DEFAULT_FROM_WARN" "${INGRESS[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json 'frontend.networkPolicy.from=[{"namespaceSelector":{"matchLabels":{"policy-group.network.openshift.io/ingress":""}}}]'
notes "from of one's own, no OpenShift API" omits "$DEFAULT_FROM_WARN" "${INGRESS[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json 'frontend.networkPolicy.from=[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"ingress-nginx"}}}]'
notes "OpenShift's two-entry from" omits "$DEFAULT_FROM_WARN" "${ROUTE[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json 'frontend.networkPolicy.from=[{"namespaceSelector":{"matchLabels":{"policy-group.network.openshift.io/ingress":""}}},{"namespaceSelector":{"matchLabels":{"policy-group.network.openshift.io/host-network":""}}}]'
notes "frontend values missing" omits "$DEFAULT_FROM_WARN" "${INGRESS[@]}" --set frontend.networkPolicy=null
notes "frontend policy off, no OpenShift API" omits "$DEFAULT_FROM_WARN" "${INGRESS[@]}"
renders "default from, no OpenShift API" "${INGRESS[@]}" "${FRONTEND_POLICY[@]}"

# The README section the notes point at gives the values they suggest
# (docs/agents/sprint-takimi.md, step 2): in that section, not merely
# somewhere in the README, where another section may carry the same text.
# A pointer (README, "HEADING", "LEAD") names a heading and, inside it, the
# paragraphs that open with **LEAD.
README="$CHART/../../../README.md"
# readme_section HEADING [LEAD]: the README's section under the heading
# HEADING (of any level), up to the next heading of its level or above; with
# LEAD, only its paragraphs from the one that opens with **LEAD followed by
# ".", ":" or " (", up to the next paragraph that opens in bold or the next
# heading. Code blocks belong to the section they are in.
readme_section() {
  awk -v heading="$1" -v lead="${2:-}" '
    /^[ \t]*(```|~~~)/ { code = !code; if (out) print; next }
    !code && /^#+[ \t]/ {
      match($0, /^#+/)
      depth = RLENGTH
      title = $0
      sub(/^#+[ \t]+/, "", title)
      sub(/[ \t]+$/, "", title)
      if (insection && depth <= level) exit
      if (!insection) {
        if (title == heading) { insection = 1; level = depth; out = (lead == "") }
        next
      }
      if (lead != "" && out) exit
      if (out) print
      next
    }
    !code && insection && lead != "" && /^\*\*/ {
      if (out) exit
      rest = substr($0, length(lead) + 3)
      if (index($0, "**" lead) == 1 && rest ~ /^(\.|:| \()/) out = 1
    }
    out { print }
  ' "$README"
}
# readme_says HEADING LEAD TEXT...: the README section has each TEXT.
readme_says() {
  local heading="$1" lead="$2" where text
  shift 2
  where="\"$heading\"${lead:+, \"$lead\"}"
  readme_section "$heading" "$lead" >"$WORK/readme-section"
  if [ ! -s "$WORK/readme-section" ]; then
    failed "README has no section $where, which the notes point at"
    return
  fi
  for text in "$@"; do
    grep -qF -- "$text" "$WORK/readme-section" || failed "README $where lacks \"$text\", which the notes point at"
  done
}
# A paragraph ends where the next bold one opens.
readme_section "Hesaplar ve giriş" "Arayüzün ağ politikası" | grep -qF "**Vekil-hop rehberi" &&
  failed "README section \"Arayüzün ağ politikası\" runs on into the next paragraph"
readme_section "Cluster'a kurulum" "GKE" | grep -qF "**GKE kontrol listesi" &&
  failed "README section \"GKE\" runs on into \"GKE kontrol listesi\""
# The sign-in throttle's values, the router's policy and the alert rule:
# "Hesaplar ve giriş" and its paragraphs.
readme_says "Hesaplar ve giriş" "" "$SET_HOPS" "$SET_HOPS_TWO" "$NGINX_LOCAL" "externalTrafficPolicy: Local" \
  "use-forwarded-headers" "compute-full-forwarded-for" "$ROUTER_POLICY_CMD" "$ROUTE_OWN_POLICY" \
  "**Vekil-hop rehberi" "**Uydurma başlıkla negatif adım" "X-Forwarded-For: 192.0.2.99"
readme_says "Hesaplar ve giriş" "Vekil-hop rehberi" "| GKE'nin kendi Ingress'i (GCE) | \`2\` |" \
  "\`externalTrafficPolicy: Local\`, varsayılan ayarlar | \`1\` |" "\`compute-full-forwarded-for: \"true\"\`) | \`3\` |" \
  "haproxy.router.openshift.io/set-forwarded-headers: append" "GKE Gateway (\`gke-l7-global-external-managed\`"
readme_says "Hesaplar ve giriş" "Arayüzün ağ politikası" "frontend.networkPolicy.enabled=true" "frontend.networkPolicy.from" \
  "policy-group.network.openshift.io/ingress" "kubernetes.io/metadata.name: ingress-nginx" "35.191.0.0/16" "130.211.0.0/22" \
  "policy-group.network.openshift.io/host-network: \"\"" "$ROUTER_NETWORK_CMD"
readme_says "Hesaplar ve giriş" "Politikanın uygulandığını doğrulamak" "$POLICY_CHECK_URL" "curl: (28)"
readme_says "Hesaplar ve giriş" "Sayaç ve kural" "KubeatSignInSourceDegraded" "metrics.prometheusRule.enabled=true" \
  "hops_zero" "hops_too_high" "hops_too_low" "no_frontend_token"
# fsGroup, and GKE's own Ingress: TLS at the load balancer and the policy's
# two ranges. The checklist also says that behind an L7 Google Cloud load
# balancer ingress-nginx's 3 is right though the notes warn, and how the CI
# recipe counts only.
readme_says "Cluster'a kurulum" "" "--set podSecurityContext.fsGroup=1001"
readme_says "Cluster'a kurulum" "GKE" "ingress.tls.enabled=false" "publicURL=https://<host>" \
  "networking.gke.io/managed-certificates" "ingress.gcp.kubernetes.io/pre-shared-cert"
readme_says "Cluster'a kurulum" "GKE kontrol listesi" "$SET_HOPS_TWO" "frontend:" "networkPolicy:" \
  "cidr: 35.191.0.0/16" "cidr: 130.211.0.0/22" "KubeatSignInSourceDegraded" \
  "\`externalTrafficPolicy: Cluster\`'da sinyal susar; \`sign-in refused\`" \
  "Önde L7 GCLB olan \`ingress-nginx\`'te" "\`3\`'tür (\"Hesaplar ve giriş\" → \"Vekil-hop rehberi\")" \
  "Değeri değiştirmeyin: \`1\`'de bütün tarayıcılar" "\`&& [ \"\$n\" -eq 0 ]\`'ı silin"
# The heartbeat and the rules; the API.
readme_says "Aracın kendi sağlığı" "" "--set metrics.prometheusRule.enabled=true" \
  "kubectl -n kubeat create secret generic kubeat-heartbeat --from-literal=url=" \
  "--reset-then-reuse-values --set heartbeat.urlSecret.name=kubeat-heartbeat" "### Operator'süz cluster"
readme_says "API" "" "port-forward svc/<release>-backend 8090:8080" "localhost:8090/api/v1/monitors"
# The notes name those sections as the checks above read them.
notes "frontend policy on" says '"Politikanın uygulandığını doğrulamak")' "${FRONTEND_POLICY[@]}"
notes "no podSecurityContext" says '("Cluster'"'"'a kurulum")'

# The bank pilot's values (docs/pilot.md, "2. values-pilot.yaml") render,
# install 0.50's sign-in fix on (hops 1, so the notes do not say it is 0)
# and leave the proxied scope setting off, commented out.
PILOT="$CHART/../../../docs/pilot.md"
awk '/^### 2\. values-pilot\.yaml/ { section = 1; next }
  section && /^```yaml$/ { inblock = 1; next }
  inblock && /^```$/ { exit }
  inblock { print }' "$PILOT" >"$WORK/values-pilot.yaml"
if [ ! -s "$WORK/values-pilot.yaml" ]; then
  failed "pilot: no values block under \"2. values-pilot.yaml\" in docs/pilot.md"
else
  PILOT_VALUES=(-f "$WORK/values-pilot.yaml" "${OPENSHIFT[@]}" --api-versions route.openshift.io/v1)
  renders "pilot values" "${PILOT_VALUES[@]}"
  notes "pilot values" omits "$HOPS_ZERO" "${PILOT_VALUES[@]}"
  notes "pilot values" says "$THROTTLE" "${PILOT_VALUES[@]}"
  notes "pilot values" says "$ROUTER_POLICY_CMD" "${PILOT_VALUES[@]}"
  notes "pilot values" omits "$UNWRITABLE" "${PILOT_VALUES[@]}"
  if helm template kubeat "$CHART" --show-only templates/backend.yaml "${PILOT_VALUES[@]}" >"$WORK/pilot-backend" 2>"$WORK/err"; then
    grep -A1 -- "- name: UPTIME_TRUST_PROXY_HOPS\$" "$WORK/pilot-backend" | grep -qF 'value: "1"' ||
      failed "pilot values: the backend's UPTIME_TRUST_PROXY_HOPS is not \"1\""
    grep -qF "UPTIME_TARGET_SCOPE_TRUST_PROXY" "$WORK/pilot-backend" &&
      failed "pilot values: UPTIME_TARGET_SCOPE_TRUST_PROXY is set, not left commented out"
  else
    failed "pilot values: refused: $(cat "$WORK/err")"
  fi
fi

# GKE's own Ingress (GCE). A certificate of Google's named in the Ingress's
# annotations (a ManagedCertificate, or a pre-shared one) ends TLS at the
# load balancer: while ingress.tls.enabled is on the notes warn and give the
# two values that go with it (and leave out the TLS Secret's warning), and
# once it is off with an https:// publicURL they do not warn of an Ingress
# without TLS. Behind it the UI is reached from Google's two ranges, which
# the frontend's policy must let in: the notes warn while "from" lacks
# either, in place of the default "from"'s warning, and with the policy off
# name the two ranges.
GCE_HOST=(--set ingress.host=kubeat.example.com)
MANAGED_CERT=(--set 'ingress.annotations.networking\.gke\.io/managed-certificates=kubeat')
PRESHARED_CERT=(--set 'ingress.annotations.ingress\.gcp\.kubernetes\.io/pre-shared-cert=kubeat')
TLS_OFF=(--set ingress.tls.enabled=false)
HTTPS_URL=(--set publicURL=https://kubeat.example.com)
GCE_TLS_WARN="WARNING: the Ingress names a certificate of Google's"
GCE_TLS_SET="--set ingress.tls.enabled=false --set publicURL=https://kubeat.example.com"
GCE_TLS_GUIDE='(README, "Cluster'"'"'a kurulum", "GKE")'
GCE_TLS_ENDS="TLS ends at GKE's load balancer"
NO_TLS_WARN="WARNING: the Ingress has no TLS"
SECRET_WARN='WARNING: the Secret "kubeat-tls" does not exist yet'
notes "gce, managed certificate, TLS on" says "$GCE_TLS_WARN" "${GCE[@]}" "${GCE_HOST[@]}" "${MANAGED_CERT[@]}"
notes "gce, managed certificate, TLS on" says "$GCE_TLS_SET" "${GCE[@]}" "${GCE_HOST[@]}" "${MANAGED_CERT[@]}"
notes "gce, managed certificate, TLS on" says "$GCE_TLS_GUIDE" "${GCE[@]}" "${GCE_HOST[@]}" "${MANAGED_CERT[@]}"
notes "gce, managed certificate, TLS on" omits "$SECRET_WARN" "${GCE[@]}" "${GCE_HOST[@]}" "${MANAGED_CERT[@]}"
notes "gce, pre-shared certificate, TLS on" says "$GCE_TLS_SET" "${GCE[@]}" "${GCE_HOST[@]}" "${PRESHARED_CERT[@]}"
notes "gce className, managed certificate, TLS on" says "$GCE_TLS_WARN" --set ingress.enabled=true --set ingress.className=gce "${MANAGED_CERT[@]}"
notes "gce, managed certificate, TLS on, https publicURL" says "$GCE_TLS_WARN" "${GCE[@]}" "${MANAGED_CERT[@]}" "${HTTPS_URL[@]}"
notes "gce, managed certificate, TLS on, https publicURL" omits "--set publicURL=" "${GCE[@]}" "${MANAGED_CERT[@]}" "${HTTPS_URL[@]}"
notes "gce, no certificate annotation, TLS on" omits "$GCE_TLS_WARN" "${GCE[@]}"
notes "gce, no certificate annotation, TLS on" says "$SECRET_WARN" "${GCE[@]}"
notes "nginx, managed certificate annotation, TLS on" omits "$GCE_TLS_WARN" "${NGINX[@]}" "${MANAGED_CERT[@]}"
notes "Route, gce values left over" omits "$GCE_TLS_WARN" "${ROUTE[@]}" --set ingress.className=gce "${MANAGED_CERT[@]}"
notes "gce, managed certificate, TLS off, https publicURL" says "$GCE_TLS_ENDS" "${GCE[@]}" "${MANAGED_CERT[@]}" "${TLS_OFF[@]}" "${HTTPS_URL[@]}"
notes "gce, managed certificate, TLS off, https publicURL" omits "$NO_TLS_WARN" "${GCE[@]}" "${MANAGED_CERT[@]}" "${TLS_OFF[@]}" "${HTTPS_URL[@]}"
notes "gce, pre-shared certificate, TLS off, https publicURL" omits "$NO_TLS_WARN" "${GCE[@]}" "${PRESHARED_CERT[@]}" "${TLS_OFF[@]}" "${HTTPS_URL[@]}"
notes "gce, managed certificate, TLS off, no publicURL" says "$NO_TLS_WARN" "${GCE[@]}" "${GCE_HOST[@]}" "${MANAGED_CERT[@]}" "${TLS_OFF[@]}"
notes "gce, managed certificate, TLS off, no publicURL" omits "$GCE_TLS_ENDS" "${GCE[@]}" "${GCE_HOST[@]}" "${MANAGED_CERT[@]}" "${TLS_OFF[@]}"
notes "gce, no certificate annotation, TLS off, https publicURL" says "$NO_TLS_WARN" "${GCE[@]}" "${TLS_OFF[@]}" "${HTTPS_URL[@]}"
notes "nginx, managed certificate annotation, TLS off, https publicURL" says "$NO_TLS_WARN" "${NGINX[@]}" "${MANAGED_CERT[@]}" "${TLS_OFF[@]}" "${HTTPS_URL[@]}"
GCE_RANGES='[{"ipBlock":{"cidr":"35.191.0.0/16"}},{"ipBlock":{"cidr":"130.211.0.0/22"}}]'
NGINX_FROM='[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"ingress-nginx"}}}]'
GCE_FROM_WARN="WARNING: GKE's own Ingress (GCE) reaches the UI from Google's load"
GCE_FROM_GUIDE='(README, "Cluster'"'"'a kurulum", "GKE kontrol listesi")'
GCE_FROM_OFF="with frontend.networkPolicy.from set to GKE's load balancer ranges"
notes "gce, frontend policy, default from" says "$GCE_FROM_WARN" "${GCE[@]}" "${FRONTEND_POLICY[@]}"
notes "gce, frontend policy, default from" says "cidr: 130.211.0.0/22" "${GCE[@]}" "${FRONTEND_POLICY[@]}"
notes "gce, frontend policy, default from" says "$GCE_FROM_GUIDE" "${GCE[@]}" "${FRONTEND_POLICY[@]}"
notes "gce, frontend policy, default from" omits "$DEFAULT_FROM_WARN" "${GCE[@]}" "${FRONTEND_POLICY[@]}"
notes "gce className, frontend policy, default from" says "$GCE_FROM_WARN" --set ingress.enabled=true --set ingress.className=gce "${FRONTEND_POLICY[@]}"
notes "gce, frontend policy, ingress-nginx's namespace" says "$GCE_FROM_WARN" "${GCE[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json "frontend.networkPolicy.from=$NGINX_FROM"
notes "gce, frontend policy, one range" says "$GCE_FROM_WARN" "${GCE[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json 'frontend.networkPolicy.from=[{"ipBlock":{"cidr":"35.191.0.0/16"}}]'
notes "gce, frontend policy, the two ranges" omits "$GCE_FROM_WARN" "${GCE[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json "frontend.networkPolicy.from=$GCE_RANGES"
notes "gce, frontend policy, the two ranges" omits "$DEFAULT_FROM_WARN" "${GCE[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json "frontend.networkPolicy.from=$GCE_RANGES"
notes "gce, frontend policy, the two ranges and a namespace" omits "$GCE_FROM_WARN" "${GCE[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json 'frontend.networkPolicy.from=[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"probes"}}},{"ipBlock":{"cidr":"130.211.0.0/22"}},{"ipBlock":{"cidr":"35.191.0.0/16"}}]'
notes "nginx, frontend policy, ingress-nginx's namespace" omits "$GCE_FROM_WARN" "${NGINX[@]}" "${FRONTEND_POLICY[@]}" \
  --set-json "frontend.networkPolicy.from=$NGINX_FROM"
notes "Route, gce class left over, frontend policy" omits "$GCE_FROM_WARN" "${ROUTE[@]}" --set ingress.className=gce "${FRONTEND_POLICY[@]}" "${OPENSHIFT[@]}"
notes "gce, frontend policy off" says "$GCE_FROM_OFF" "${GCE[@]}" "${GIVEN_ONE[@]}"
notes "gce, frontend policy off" says "$GCE_FROM_GUIDE" "${GCE[@]}" "${GIVEN_ONE[@]}"
notes "gce, frontend policy off" omits "$POLICY_FROM" "${GCE[@]}" "${GIVEN_ONE[@]}"
notes "gce, frontend policy off" omits "$GCE_FROM_WARN" "${GCE[@]}" "${GIVEN_ONE[@]}"
notes "nginx, frontend policy off" says "$POLICY_FROM" "${NGINX[@]}" "${GIVEN_ONE[@]}"

# GKE's own Ingress as docs/gke.md gives it ("2. values-gke.yaml"), taken
# from the document as written (docs/agents/sprint-takimi.md, step 7: an
# install path recommended but not measured has a values recipe rendered in
# CI): it renders what it says, and its notes warn of nothing, the hop, TLS
# and the frontend's policy among it.
GKE_DOC="$CHART/../../../docs/gke.md"
awk '/^## 2\. values-gke\.yaml/ { section = 1; next }
  section && /^```yaml$/ { inblock = 1; next }
  inblock && /^```$/ { exit }
  inblock { print }' "$GKE_DOC" >"$WORK/values-gke.yaml"
if [ ! -s "$WORK/values-gke.yaml" ]; then
  failed "GKE: no values block under \"2. values-gke.yaml\" in docs/gke.md"
else
  GKE_VALUES=(-f "$WORK/values-gke.yaml")
  renders "GKE values" "${GKE_VALUES[@]}"
  for text in "$THROTTLE" "$HOPS_ZERO" "$HOPS_CONFLICT" "$GCE_TLS_WARN" "$NO_TLS_WARN" "$SECRET_WARN" \
    "$GCE_FROM_WARN" "$DEFAULT_FROM_WARN" "$NEEDS_POLICY" "$UNWRITABLE" "WARNING:"; do
    notes "GKE values" omits "$text" "${GKE_VALUES[@]}"
  done
  notes "GKE values" says "$GCE_TLS_ENDS" "${GKE_VALUES[@]}"
  notes "GKE values" says "$POLICY_CHECK" "${GKE_VALUES[@]}"
  if helm template kubeat "$CHART" "${GKE_VALUES[@]}" >"$WORK/gke-manifests" 2>"$WORK/err"; then
    for text in "kubernetes.io/ingress.class: gce" "networking.gke.io/managed-certificates: kubeat" \
      "cloud.google.com/neg: '{\"ingress\": true}'" "cidr: 35.191.0.0/16" "cidr: 130.211.0.0/22" \
      "fsGroup: 1001"; do
      grep -qF -- "$text" "$WORK/gke-manifests" || failed "GKE values: the manifests lack \"$text\""
    done
    grep -A1 -- "- name: UPTIME_TRUST_PROXY_HOPS\$" "$WORK/gke-manifests" | grep -qF 'value: "2"' ||
      failed "GKE values: the backend's UPTIME_TRUST_PROXY_HOPS is not \"2\""
    grep -A1 -- "- name: UPTIME_PUBLIC_URL\$" "$WORK/gke-manifests" | grep -qF 'value: "https://' ||
      failed "GKE values: the backend's UPTIME_PUBLIC_URL is not https://"
    grep -qF "secretName:" "$WORK/gke-manifests" && failed "GKE values: the Ingress names a TLS Secret"
  else
    failed "GKE values: refused: $(cat "$WORK/err")"
  fi
fi

# The README's recipe for reading the notes in CI, where a GitOps render
# shows none ("Kurulum notları görünmez"), run as written there with the
# helm this script runs with, as CI runs it with Helm 3 and Helm 4; only the
# chart it pulls is this one, and the values file one of these. It talks to
# no cluster, prints the notes and counts their warnings but the TLS
# Secret's, which a render without a cluster never finds; the step fails on
# any, and on notes that did not render, under sh as under bash -e.
# --api-versions as the README says: both for OpenShift with the
# PrometheusRule CRD, none for GKE.
awk '/^\*\*Kurulum notları görünmez\.\*\*/ { section = 1; next }
  section && /^```sh$/ { inblock = 1; next }
  inblock && /^```$/ { exit }
  inblock { print }' "$README" >"$WORK/ci-notes.sh"
# ci_notes NAME SHELL APIS EXIT COUNT VALUES [TEXT]: the recipe run with
# SHELL ("bash -e" or "sh"), keeping its two --api-versions (APIS all) or
# dropping them (none), with the values file VALUES, exits EXIT, prints the
# notes (and TEXT in them) and counts COUNT warnings; COUNT - for notes
# that do not render.
ci_notes() {
  local name="$1" shell="$2" apis="$3" want_exit="$4" want_count="$5" values="$6" text="${7:-Kubeat is installed.}" dir got
  dir="$(mktemp -d "$WORK/ci-notes.XXXXXX")"
  mkdir "$dir/ci"
  cp -R "$CHART" "$dir/ci/kubeat"
  sed -e '/^helm pull /d' -e "s|values-<cluster>\\.yaml|$values|" "$WORK/ci-notes.sh" |
    if [ "$apis" = none ]; then sed 's| --api-versions [^ ]*||g'; else cat; fi >"$dir/run.sh"
  # shellcheck disable=SC2086 # SHELL is a command and its flag
  (cd "$dir" && $shell run.sh) >"$dir/out" 2>"$dir/err"
  got=$?
  [ "$got" = "$want_exit" ] || failed "CI notes recipe, $name: exits $got, want $want_exit: $(cat "$dir/err")"
  [ "$want_count" = - ] && return
  grep -qF "Kubeat is installed." "$dir/out" || failed "CI notes recipe, $name: the notes are not printed"
  grep -qF -- "$text" "$dir/out" || failed "CI notes recipe, $name: the notes do not say \"$text\""
  grep -qx "WARNING: $want_count" "$dir/out" ||
    failed "CI notes recipe, $name: want \"WARNING: $want_count\", got: $(grep -E 'WARNING' "$dir/out")"
}
if [ ! -s "$WORK/ci-notes.sh" ]; then
  failed "README: no sh block under \"Kurulum notları görünmez\""
else
  [ "$(wc -l <"$WORK/ci-notes.sh")" -le 6 ] || failed "README's CI notes recipe: more than 6 lines"
  grep -qx 'helm pull oci://ghcr.io/yusufceng/charts/kubeat --version [0-9.]* --untar --untardir ci' "$WORK/ci-notes.sh" ||
    failed "README's CI notes recipe: no \"helm pull ... --untar --untardir ci\" line, which this check replaces with the chart"
  grep -qF -- "-f values-<cluster>.yaml" "$WORK/ci-notes.sh" || failed "README's CI notes recipe: no -f values-<cluster>.yaml"
  grep -qF -- "--api-versions security.openshift.io/v1 --api-versions monitoring.coreos.com/v1" "$WORK/ci-notes.sh" ||
    failed "README's CI notes recipe: not the two --api-versions"
  sed '/^helm pull /d' "$WORK/ci-notes.sh" | grep -Eq '(^|[;|&(] *)(kubectl|oc|helm (install|upgrade|pull))( |$)' &&
    failed "README's CI notes recipe: a command that may reach a cluster or the registry"
  if [ -s "$WORK/values-gke.yaml" ]; then
    ci_notes "GKE values" "bash -e" none 0 0 "$WORK/values-gke.yaml"
    ci_notes "GKE values under sh" sh none 0 0 "$WORK/values-gke.yaml"
    sed 's/UPTIME_TRUST_PROXY_HOPS: "2"/UPTIME_TRUST_PROXY_HOPS: "1"/' "$WORK/values-gke.yaml" >"$WORK/values-gke-hops-1.yaml"
    ci_notes "GKE values, hops 1" "bash -e" none 1 1 "$WORK/values-gke-hops-1.yaml"
    ci_notes "GKE values, hops 1, under sh" sh none 1 1 "$WORK/values-gke-hops-1.yaml"
  fi
  # Values the chart refuses: no notes, and the step fails without set -e too.
  printf 'backend:\n  env:\n    UPTIME_RETENTION: 48h\n' >"$WORK/values-refused.yaml"
  ci_notes "refused values" "bash -e" none 1 - "$WORK/values-refused.yaml"
  ci_notes "refused values under sh" sh none 1 - "$WORK/values-refused.yaml"
  # ingress-nginx with cert-manager's Secret, which the count leaves out.
  cat >"$WORK/values-nginx.yaml" <<'EOF'
podSecurityContext:
  fsGroup: 1001
ingress:
  enabled: true
  className: nginx
  host: kubeat.example.com
frontend:
  networkPolicy:
    enabled: true
    from:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: ingress-nginx
backend:
  extraEnv:
    UPTIME_TRUST_PROXY_HOPS: "1"
heartbeat:
  urlSecret:
    name: kubeat-heartbeat
backup:
  enabled: true
EOF
  ci_notes "ingress-nginx, the TLS Secret's line" "bash -e" none 0 0 "$WORK/values-nginx.yaml" "$SECRET_WARN"
  # OpenShift: the Route with its own forwarded-headers policy, the router's
  # default "from", no fsGroup. Without the OpenShift API the notes warn of
  # both.
  cat >"$WORK/values-openshift.yaml" <<'EOF'
route:
  enabled: true
  host: kubeat.apps.example
  annotations:
    haproxy.router.openshift.io/set-forwarded-headers: append
frontend:
  networkPolicy:
    enabled: true
backend:
  extraEnv:
    UPTIME_TRUST_PROXY_HOPS: "1"
metrics:
  prometheusRule:
    enabled: true
backup:
  enabled: true
EOF
  ci_notes "OpenShift values" "bash -e" all 0 0 "$WORK/values-openshift.yaml"
  ci_notes "OpenShift values without the OpenShift API" "bash -e" none 1 2 "$WORK/values-openshift.yaml"
fi

# The frontend's network policy: a switch of its own, off by default and
# not turned on by networkPolicy.enabled, whose push jobs it would cut off.
# On, it lets in the OpenShift router by default, or the sources given; an
# empty list, which would let every source in, is refused.
# policy NAME POLICY has|lacks|absent TEXT VALUES...: the NetworkPolicy
# POLICY rendered with these values has TEXT, lacks it, or is not rendered.
policy() {
  local name="$1" policy="$2" want="$3" text="$4"
  shift 4
  if ! helm template kubeat "$CHART" --show-only templates/networkpolicy.yaml "$@" >"$WORK/policies" 2>"$WORK/err"; then
    if grep -q "could not find template" "$WORK/err"; then
      [ "$want" = absent ] || failed "$name: no NetworkPolicy is rendered"
    else
      failed "$name: refused: $(cat "$WORK/err")"
    fi
    return
  fi
  awk -v policy="$policy" '
    function flush() {
      if (index(doc, "\nkind: NetworkPolicy\n") && index(doc, "\n  name: " policy "\n")) printf "%s", doc
      doc = "\n"
    }
    BEGIN { doc = "\n" }
    /^---/ { flush(); next }
    { doc = doc $0 "\n" }
    END { flush() }
  ' "$WORK/policies" >"$WORK/policy"
  if [ ! -s "$WORK/policy" ]; then
    [ "$want" = absent ] || failed "$name: $policy is not rendered"
  elif [ "$want" = absent ]; then
    failed "$name: $policy is rendered"
  elif grep -qF -- "$text" "$WORK/policy"; then
    [ "$want" = has ] || failed "$name: $policy has \"$text\""
  else
    [ "$want" = lacks ] || failed "$name: $policy lacks \"$text\""
  fi
}
policy "defaults" kubeat-frontend absent ""
policy "backend policy on" kubeat-frontend absent "" --set networkPolicy.enabled=true
policy "backend policy on" kubeat-backend has "app.kubernetes.io/component: backend" --set networkPolicy.enabled=true
policy "frontend policy on" kubeat-frontend has "app.kubernetes.io/component: frontend" "${FRONTEND_POLICY[@]}"
policy "frontend policy on" kubeat-frontend has "policy-group.network.openshift.io/ingress: \"\"" "${FRONTEND_POLICY[@]}"
policy "frontend policy on" kubeat-frontend has "- port: http" "${FRONTEND_POLICY[@]}"
policy "frontend policy on" kubeat-backend absent "" "${FRONTEND_POLICY[@]}"
policy "both policies" kubeat-frontend has "policy-group.network.openshift.io/ingress" "${FRONTEND_POLICY[@]}" --set networkPolicy.enabled=true
policy "both policies" kubeat-backend has "app.kubernetes.io/component: backend" "${FRONTEND_POLICY[@]}" --set networkPolicy.enabled=true
# A release upgraded with --reuse-values from 0.49 has no such values.
policy "frontend values missing" kubeat-frontend absent "" --set frontend.networkPolicy=null
INGRESS_NGINX='[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"ingress-nginx"}}},{"ipBlock":{"cidr":"35.191.0.0/16"}}]'
policy "sources given" kubeat-frontend has "kubernetes.io/metadata.name: ingress-nginx" "${FRONTEND_POLICY[@]}" \
  --set-json "frontend.networkPolicy.from=$INGRESS_NGINX"
policy "sources given" kubeat-frontend has "cidr: 35.191.0.0/16" "${FRONTEND_POLICY[@]}" \
  --set-json "frontend.networkPolicy.from=$INGRESS_NGINX"
policy "sources given" kubeat-frontend lacks "policy-group.network.openshift.io/ingress" "${FRONTEND_POLICY[@]}" \
  --set-json "frontend.networkPolicy.from=$INGRESS_NGINX"
refuses "frontend policy letting everyone in" "frontend.networkPolicy.from is empty" "${FRONTEND_POLICY[@]}" \
  --set-json 'frontend.networkPolicy.from=[]'

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

# The internal CA recipe (README, "OpenShift", "Kurum CA'sı"): its values,
# taken from the README, mount the ConfigMap whole, read-only and optional
# (a ConfigMap not there leaves the directory empty), and point
# SSL_CERT_DIR, which adds to the system bundle file where SSL_CERT_FILE
# replaces it, at the mount; the defaults have none of it. The README gives
# OpenShift's injection label and the restart the bundle needs in both
# CLIs, and values.yaml's example mounts where the README's does.
awk '/^\*\*Kurum CA.sı\.\*\*/ { section = 1; next }
  section && /^\*\*Benzetim\.\*\*/ { exit }
  section && /^```yaml$/ { inblock = 1; block = ""; next }
  inblock && /^```$/ { inblock = 0; if (block ~ /^backend:/) { printf "%s", block; exit }; next }
  inblock { block = block $0 "\n" }' "$README" >"$WORK/values-trusted-ca.yaml"
if [ ! -s "$WORK/values-trusted-ca.yaml" ]; then
  failed "internal CA: no backend values block under \"Kurum CA'sı\" in the README"
elif ! helm template kubeat "$CHART" --show-only templates/backend.yaml -f "$WORK/values-trusted-ca.yaml" \
  >"$WORK/trusted-ca.yaml" 2>"$WORK/err"; then
  failed "internal CA: refused: $(cat "$WORK/err")"
else
  # list_item FILE TEXT: the item of a YAML list, one without lists of its
  # own, that has a line with TEXT; toYaml writes a value's keys sorted.
  list_item() {
    awk -v want="$2" '
      /^ *- / { if (found) exit; item = ""; match($0, /^ */); indent = RLENGTH }
      found && match($0, /^ */) && RLENGTH <= indent { exit }
      { item = item $0 "\n" }
      index($0, want) { found = 1 }
      END { if (found) printf "%s", item }' "$1"
  }
  CA_DIR="$(awk '/^ *- name: SSL_CERT_DIR$/ { getline; sub(/^ *value: /, ""); gsub(/"/, ""); print }' "$WORK/trusted-ca.yaml")"
  if [ -z "$CA_DIR" ]; then
    failed "internal CA: SSL_CERT_DIR is not set"
  else
    MOUNT="$(list_item "$WORK/trusted-ca.yaml" "mountPath: $CA_DIR")"
    grep -qx " *readOnly: true" <<<"$MOUNT" && grep -qE "^ *(- )?name: trusted-ca\$" <<<"$MOUNT" ||
      failed "internal CA: SSL_CERT_DIR $CA_DIR is not where the trusted-ca volume is mounted read-only: ${MOUNT:-no mount}"
    grep -q "subPath:" <<<"$MOUNT" && failed "internal CA: mounted with subPath, a file where SSL_CERT_DIR wants the directory"
    grep -qF "#   mountPath: $CA_DIR" "$CHART/values.yaml" ||
      failed "internal CA: values.yaml's example does not mount at $CA_DIR"
  fi
  VOLUME="$(list_item "$WORK/trusted-ca.yaml" "name: kubeat-trusted-ca")"
  grep -qE "^ *(- )?configMap:\$" <<<"$VOLUME" && grep -qE "^ *(- )?name: trusted-ca\$" <<<"$VOLUME" ||
    failed "internal CA: the ConfigMap kubeat-trusted-ca is not the trusted-ca volume: ${VOLUME:-no volume}"
  # A ConfigMap not there must not keep the pod from starting: with the
  # Recreate strategy the old pod is already gone, and every check stops.
  grep -qx " *optional: true" <<<"$VOLUME" ||
    failed "internal CA: the trusted-ca volume is not optional, so a missing ConfigMap stops all monitoring: ${VOLUME:-no volume}"
  grep -qF "#     optional: true" "$CHART/values.yaml" ||
    failed "internal CA: values.yaml's example volume is not optional"
  grep -qF "SSL_CERT_FILE" "$WORK/trusted-ca.yaml" && failed "internal CA: SSL_CERT_FILE is set, which replaces the system bundle file"
fi
helm template kubeat "$CHART" --show-only templates/backend.yaml | grep -qE "SSL_CERT_DIR|trusted-ca" &&
  failed "internal CA: the defaults mount a CA bundle"
for text in "**Kurum CA'sı.**" "config.openshift.io/inject-trusted-cabundle: \"true\"" \
  "oc -n kubeat rollout restart deploy/kubeat-backend" "kubectl -n kubeat rollout restart deploy/kubeat-backend" \
  "**Sıra ve eksik ConfigMap.**" "**Kurum CA'sının kapsamı.**" "**Kurum CA'sı değişince.**" \
  "[\"OpenShift\" → \"Kurum CA'sı\"](#openshift)"; do
  grep -qF -- "$text" "$README" || failed "README lacks \"$text\" of the internal CA recipe"
done

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

# The installation's name in notifications: UPTIME_INSTANCE_NAME only when
# instanceName is given; without it, as with older values that lack the
# key, the backend takes publicURL's host (the Route's or Ingress's).
# instance NAME WANT VALUES...: the backend renders with these values and
# UPTIME_INSTANCE_NAME set to WANT, as quoted in the manifest, or without
# it when WANT is empty.
instance() {
  local name="$1" want="$2" got
  shift 2
  if ! helm template kubeat "$CHART" --show-only templates/backend.yaml "$@" >"$WORK/backend.yaml" 2>"$WORK/err"; then
    failed "$name: refused: $(cat "$WORK/err")"
    return
  fi
  got="$(awk '/^ *- name: UPTIME_INSTANCE_NAME$/ { getline; sub(/^ *value: /, ""); print }' "$WORK/backend.yaml")"
  [ "$got" = "$want" ] || failed "$name: want UPTIME_INSTANCE_NAME ${want:-unset}, got: ${got:-unset}"
}
instance "no instanceName" ""
instance "no instanceName, a Route" "" "${ROUTE[@]}"
instance "instanceName missing from older values" "" --set instanceName=null
instance "instanceName given" '"prod-eu"' --set instanceName=prod-eu
instance "instanceName with a Route" '"prod-eu"' "${ROUTE[@]}" --set instanceName=prod-eu
instance "instanceName with a space" '"Prod EU"' --set-string 'instanceName=Prod EU'

# Where the pods run and how the frontend Service is exposed, for an infra
# node pool (nodeSelector, tolerations, priorityClassName) and for GKE's own
# Ingress (the frontend Service's type and annotations). All off by default,
# so the render is the same as up to 0.47; a value upgraded with
# --reuse-values from before them renders as null, which the chart reads as
# unset too.
# placement NAME FILE has|lacks TEXT VALUES...: the manifest from
# templates/FILE rendered with these values has TEXT, or lacks it.
placement() {
  local name="$1" file="$2" want="$3" text="$4"
  shift 4
  if ! helm template kubeat "$CHART" --show-only "templates/$file" "$@" >"$WORK/placement" 2>"$WORK/err"; then
    failed "$name: refused: $(cat "$WORK/err")"
    return
  fi
  if grep -qF -- "$text" "$WORK/placement"; then
    [ "$want" = has ] || failed "$name: $file has \"$text\""
  else
    [ "$want" = lacks ] || failed "$name: $file lacks \"$text\""
  fi
}
for file in backend.yaml frontend.yaml; do
  comp="${file%.yaml}"
  placement "no placement $comp" "$file" lacks "nodeSelector:"
  placement "no placement $comp" "$file" lacks "tolerations:"
  placement "no placement $comp" "$file" lacks "priorityClassName:"
  placement "no placement from older values $comp" "$file" lacks "nodeSelector:" \
    --set "$comp.nodeSelector=null" --set "$comp.tolerations=null" --set "$comp.priorityClassName=null"
  placement "$comp nodeSelector" "$file" has "kubeat.io/role: infra" --set-json "$comp.nodeSelector={\"kubeat.io/role\":\"infra\"}"
  placement "$comp tolerations" "$file" has "key: dedicated" \
    --set-json "$comp.tolerations=[{\"key\":\"dedicated\",\"operator\":\"Equal\",\"value\":\"infra\",\"effect\":\"NoSchedule\"}]"
  placement "$comp priorityClassName" "$file" has "priorityClassName: system-cluster-critical" --set "$comp.priorityClassName=system-cluster-critical"
done
# The frontend Service: no type (ClusterIP) and no annotations by default, a
# NodePort or a ClusterIP with a NEG annotation for GKE's own Ingress. The
# Service's type is a line of its own at the top of spec; a deeper type: (the
# container's seccompProfile) is not it.
# serviceType WANT VALUES...: whether the frontend Service sets its own type.
serviceType() {
  local want="$1"
  shift
  if ! helm template kubeat "$CHART" --show-only templates/frontend.yaml "$@" >"$WORK/placement" 2>"$WORK/err"; then
    failed "Service type: refused: $(cat "$WORK/err")"
    return
  fi
  if grep -qE '^  type:' "$WORK/placement"; then
    [ "$want" = set ] || failed "Service type: set, want $want"
  else
    [ "$want" = unset ] || failed "Service type: unset, want $want"
  fi
}
serviceType unset
serviceType unset --set frontend.service=null
serviceType set --set frontend.service.type=NodePort
placement "Service type NodePort" frontend.yaml has "type: NodePort" --set frontend.service.type=NodePort
placement "no Service annotations" frontend.yaml lacks "cloud.google.com/neg:"
placement "Service annotation" frontend.yaml has "cloud.google.com/neg:" \
  --set-json 'frontend.service.annotations={"cloud.google.com/neg":"{\"ingress\": true}"}'

# The frontend token: both pods read it from the same Secret, the chart's
# or the one named in frontendToken.existingSecret, so that the backend
# reads X-Forwarded-For from the frontend alone.
for file in backend.yaml frontend.yaml; do
  var=FRONTEND_TOKEN
  [ "$file" = backend.yaml ] && var=UPTIME_FRONTEND_TOKEN
  placement "frontend token in $file" "$file" has "- name: $var"
  helm template kubeat "$CHART" --show-only "templates/$file" >"$WORK/token" 2>"$WORK/err" || failed "frontend token in $file: refused"
  grep -A4 -- "- name: $var\$" "$WORK/token" | grep -qF "name: kubeat-frontend-token" ||
    failed "frontend token in $file: $var is not read from kubeat-frontend-token"
  helm template kubeat "$CHART" --show-only "templates/$file" --set frontendToken.existingSecret=my-frontend-token >"$WORK/token" 2>"$WORK/err" ||
    failed "frontend token of one's own in $file: refused"
  grep -A4 -- "- name: $var\$" "$WORK/token" | grep -qF "name: my-frontend-token" ||
    failed "frontend token of one's own in $file: $var is not read from my-frontend-token"
  grep -A4 -- "- name: $var\$" "$WORK/token" | grep -qF "key: token" ||
    failed "frontend token in $file: $var is not read from the key token"
  # A Secret that is not there (yet) must not keep the pods from starting:
  # the monitoring never stops for a setting.
  grep -A5 -- "- name: $var\$" "$WORK/token" | grep -qF "optional: true" ||
    failed "frontend token in $file: $var is not optional"
done

# The cluster's networks declared in targetScope.clusterNetworks (S96): a
# company range of UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS that overlaps one fails
# the render, so helm upgrade and an Argo CD sync stop before the backend
# would ignore the range at its start; the message names the range and the
# network. IPv4 only: an IPv6 network goes to the backend
# (UPTIME_TARGET_SCOPE_CLUSTER_CIDRS), whose start compares it. A single
# address, the router's VIP in the node network, is not compared. Without a
# declaration, as with values from before it, backend.yaml renders as
# without the value.
# clusterEnv NAME WANT VALUES...: the backend renders with these values and
# UPTIME_TARGET_SCOPE_CLUSTER_CIDRS set to WANT, as quoted in the manifest,
# or without it when WANT is empty.
clusterEnv() {
  local name="$1" want="$2" got
  shift 2
  if ! helm template kubeat "$CHART" --show-only templates/backend.yaml "$@" >"$WORK/backend.yaml" 2>"$WORK/err"; then
    failed "$name: refused: $(cat "$WORK/err")"
    return
  fi
  got="$(awk '/^ *- name: UPTIME_TARGET_SCOPE_CLUSTER_CIDRS$/ { getline; sub(/^ *value: /, ""); print }' "$WORK/backend.yaml")"
  [ "$got" = "$want" ] || failed "$name: want UPTIME_TARGET_SCOPE_CLUSTER_CIDRS ${want:-unset}, got: ${got:-unset}"
}
NETWORKS=(--set-json 'targetScope.clusterNetworks={"pods":["10.128.0.0/14","fd02::/48"],"services":["172.30.0.0/16"],"nodes":["192.168.40.0/24"]}')
company() { printf 'backend.extraEnv={"UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS":"%s"}' "$1"; }
OVERLAP="backend.extraEnv.UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS: "
clusterEnv "no declaration" ""
clusterEnv "no declaration, a company range over the pod network" "" --set-json "$(company 10.0.0.0/8)"
clusterEnv "targetScope missing from older values" "" --set targetScope=null
clusterEnv "empty declaration" "" --set-json 'targetScope.clusterNetworks={"pods":[],"services":[],"nodes":[]}'
clusterEnv "declaration missing a kind" '"10.128.0.0/14"' --set-json 'targetScope.clusterNetworks={"pods":["10.128.0.0/14"]}'
clusterEnv "declaration" '"10.128.0.0/14,fd02::/48,172.30.0.0/16,192.168.40.0/24"' "${NETWORKS[@]}"
clusterEnv "declaration, company ranges clear of it" '"10.128.0.0/14,fd02::/48,172.30.0.0/16,192.168.40.0/24"' "${NETWORKS[@]}" \
  --set-json "$(company '10.50.0.0/16, 172.31.0.0/16,192.168.41.0/24')"
# Without a declaration the render is the same as with an empty one, and as
# with values from before targetScope, whatever the company ranges.
# undeclared NAME VALUES...: the backend renders alike with these values
# alone, without targetScope and with an empty declaration.
undeclared() {
  local name="$1"
  shift
  helm template kubeat "$CHART" --show-only templates/backend.yaml "$@" >"$WORK/plain" 2>&1
  helm template kubeat "$CHART" --show-only templates/backend.yaml "$@" --set targetScope=null >"$WORK/older" 2>&1
  helm template kubeat "$CHART" --show-only templates/backend.yaml "$@" \
    --set-json 'targetScope.clusterNetworks={"pods":[],"services":[],"nodes":[]}' >"$WORK/empty" 2>&1
  cmp -s "$WORK/plain" "$WORK/older" || failed "$name: values without targetScope render otherwise"
  cmp -s "$WORK/plain" "$WORK/empty" || failed "$name: an empty declaration renders otherwise"
}
undeclared "no declaration"
undeclared "no declaration, a company range over the pod network" --set-json "$(company 10.0.0.0/8)"
# A company range over a declared network, any part of it, its host bits
# set or not: the message names each range and network, and its value.
refuses "company range over the pod network" "${OVERLAP}10.0.0.0/8 overlaps 10.128.0.0/14, the cluster's pod network (targetScope.clusterNetworks.pods)" \
  "${NETWORKS[@]}" --set-json "$(company 10.0.0.0/8)"
refuses "company range in a part of the pod network" "10.130.0.0/16 overlaps 10.128.0.0/14, the cluster's pod network" \
  "${NETWORKS[@]}" --set-json "$(company '10.50.0.0/16,10.130.0.0/16')"
refuses "company range over the Service network" "172.16.0.0/12 overlaps 172.30.0.0/16, the cluster's Service network (targetScope.clusterNetworks.services)" \
  "${NETWORKS[@]}" --set-json "$(company 172.16.0.0/12)"
refuses "company range over the node network" "192.168.40.64/26 overlaps 192.168.40.0/24, the cluster's node network (targetScope.clusterNetworks.nodes)" \
  "${NETWORKS[@]}" --set-json "$(company 192.168.40.64/26)"
refuses "company range with host bits" "10.131.7.1/16 overlaps 10.128.0.0/14" "${NETWORKS[@]}" --set-json "$(company 10.131.7.1/16)"
refuses "every network over 0.0.0.0/0" "0.0.0.0/0 overlaps 10.128.0.0/14, the cluster's pod network (targetScope.clusterNetworks.pods); 0.0.0.0/0 overlaps 172.30.0.0/16, the cluster's Service network (targetScope.clusterNetworks.services); 0.0.0.0/0 overlaps 192.168.40.0/24, the cluster's node network" \
  "${NETWORKS[@]}" --set-json "$(company 0.0.0.0/0)"
refuses "the message points at the README" '(README, "Hedef politikası", "Kurum ağı")' "${NETWORKS[@]}" --set-json "$(company 10.0.0.0/8)"
refuses "a declaration in --set" "10.0.0.0/8 overlaps 10.128.0.0/14" --set 'targetScope.clusterNetworks.pods={10.128.0.0/14}' \
  --set backend.extraEnv.UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS=10.0.0.0/8
# A network declared as a single address, a node's own, is compared to its
# edges: a company range that starts or ends at it overlaps it, one next to
# it does not.
NODES=(--set-json 'targetScope.clusterNetworks.nodes=["192.168.40.4/32","192.168.40.7/32"]')
refuses "company range starting at a declared node address" "192.168.40.4/30 overlaps 192.168.40.4/32, the cluster's node network" \
  "${NODES[@]}" --set-json "$(company 192.168.40.4/30)"
refuses "company range ending at a declared node address" "192.168.40.4/30 overlaps 192.168.40.7/32, the cluster's node network" \
  "${NODES[@]}" --set-json "$(company 192.168.40.4/30)"
renders "company ranges next to declared node addresses" "${NODES[@]}" --set-json "$(company '192.168.40.0/30,192.168.40.8/29')"
# A key no template reads, and the variable in extraEnv, fail the render
# rather than turn the comparison off unsaid.
refuses "declared networks under pod" "targetScope.clusterNetworks.pod is not read: give pods, services and nodes" \
  --set-json 'targetScope.clusterNetworks.pod=["10.128.0.0/14"]' --set-json "$(company 10.0.0.0/8)"
refuses "declared networks under node and pod" "targetScope.clusterNetworks.node, targetScope.clusterNetworks.pod are not read" \
  --set-json 'targetScope.clusterNetworks={"pod":["10.128.0.0/14"],"node":["192.168.40.0/24"]}'
refuses "declared networks under clusterNetwork" "targetScope.clusterNetwork is not read: give targetScope.clusterNetworks" \
  --set-json 'targetScope.clusterNetwork={"pods":["10.128.0.0/14"]}' --set-json "$(company 10.0.0.0/8)"
refuses "targetScope not a map" "targetScope is corp: give targetScope.clusterNetworks" --set targetScope=corp
for value in 10.128.0.0/14 ""; do
  refuses "UPTIME_TARGET_SCOPE_CLUSTER_CIDRS=$value in extraEnv" "backend.extraEnv.UPTIME_TARGET_SCOPE_CLUSTER_CIDRS: the chart sets this variable itself" \
    --set-string "backend.extraEnv.UPTIME_TARGET_SCOPE_CLUSTER_CIDRS=$value"
done
# Not compared, left to the backend: the router's VIP as a /32 in the node
# network, an IPv6 range over an IPv6 network, an entry the backend refuses.
renders "router's VIP as a /32 in the node network" "${NETWORKS[@]}" --set-json "$(company '10.50.0.0/16,192.168.40.5/32')"
renders "IPv6 company range over an IPv6 network" "${NETWORKS[@]}" --set-json "$(company 'fd02:0:0:5::/64,10.50.0.0/16')"
renders "company entry that is not a CIDR" "${NETWORKS[@]}" --set-json "$(company 'corp,10.50.0.1')"
# A declared network the backend would refuse fails the render, naming it.
refuses "declared network not a CIDR" 'targetScope.clusterNetworks.services: "corp" is not a CIDR such as 10.128.0.0/14' \
  --set-json 'targetScope.clusterNetworks.services=["corp"]'
for cidr in 10.128.0.0 10.128.0.0/33 010.128.0.0/14 fd02::/129 fd02::1%eth0/64; do
  refuses "declared network $cidr" "targetScope.clusterNetworks.pods: \"$cidr\" is not a CIDR" --set-json "targetScope.clusterNetworks.pods=[\"$cidr\"]"
done
refuses "declared IPv4 network written as IPv6" '"::ffff:10.128.0.0/110" is an IPv4 network written as IPv6' \
  --set-json 'targetScope.clusterNetworks.pods=["::ffff:10.128.0.0/110"]'
refuses "declared networks not a list" "targetScope.clusterNetworks.pods is 10.128.0.0/14: give a list of CIDRs" \
  --set targetScope.clusterNetworks.pods=10.128.0.0/14
# The pilot's declaration (docs/pilot.md, "Önce cluster'ın ağlarını okuyun")
# renders with the pilot's company ranges, the router's VIP among them, and
# refuses the 10.0.0.0/8 the pilot warns against.
awk '/Önce cluster.ın ağlarını okuyun/ { section = 1; next }
  section && /^  ```yaml$/ { inblock = 1; next }
  inblock && /^  ```$/ { exit }
  inblock { sub(/^  /, ""); print }' "$PILOT" >"$WORK/pilot-networks.yaml"
if ! grep -q "^targetScope:" "$WORK/pilot-networks.yaml"; then
  failed "pilot: no targetScope block under \"Önce cluster'ın ağlarını okuyun\" in docs/pilot.md"
else
  clusterEnv "pilot declaration" '"10.128.0.0/14,172.30.0.0/16,192.168.40.0/24"' -f "$WORK/pilot-networks.yaml" \
    --set-json "$(company '10.50.0.0/16,192.168.40.5/32')"
  refuses "pilot declaration, 10.0.0.0/8" "10.0.0.0/8 overlaps 10.128.0.0/14, the cluster's pod network" -f "$WORK/pilot-networks.yaml" \
    --set-json "$(company 10.0.0.0/8)"
fi
for text in "targetScope.clusterNetworks" "UPTIME_TARGET_SCOPE_CLUSTER_CIDRS" "**Cluster ağlarının beyanı"; do
  grep -qF -- "$text" "$README" || failed "README lacks \"$text\", which the render error points at"
done

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
# The warning carries the two lines that turn the heartbeat on, as the
# README's quick start gives them, and offers the rules only where the
# PrometheusRule CRD is: helm template sees it with --api-versions, as the
# cluster's API shows it to helm install. A heartbeat without the rules
# hears no channel that takes no notifications; the notes say so, and say
# nothing of it once the rules are on.
CHART_VERSION="$(awk '/^version:/ { print $2; exit }' "$CHART/Chart.yaml")"
HEARTBEAT_SECRET="kubectl -n kubeat create secret generic kubeat-heartbeat --from-literal=url="
HEARTBEAT_SET="helm upgrade kubeat oci://ghcr.io/yusufceng/charts/kubeat --version $CHART_VERSION -n kubeat --reset-then-reuse-values --set heartbeat.urlSecret.name=kubeat-heartbeat"
RULES_SET="--set metrics.prometheusRule.enabled=true"
NO_CRD="need the PrometheusRule CRD, which this cluster does not have"
UNHEARD_CHANNEL="The heartbeat does not hear a channel that takes no notifications"
CRD=(--api-versions monitoring.coreos.com/v1)
HEARTBEAT=(--set heartbeat.urlSecret.name=kubeat-heartbeat)
RULES_ON=(--set metrics.prometheusRule.enabled=true)
notes "no heartbeat, no rules" says "$HEARTBEAT_SECRET" --namespace kubeat
notes "no heartbeat, no rules" says "$HEARTBEAT_SET" --namespace kubeat
notes "no heartbeat, no rules" says "$UNHEARD_CHANNEL" --namespace kubeat
notes "no heartbeat, no rules, no CRD" omits "$RULES_SET" --namespace kubeat
notes "no heartbeat, no rules, no CRD" says "$NO_CRD" --namespace kubeat
notes "no heartbeat, no rules, no CRD" says '"Operator'"'"'süz cluster"' --namespace kubeat
notes "no heartbeat, no rules, CRD" says "$RULES_SET" --namespace kubeat "${CRD[@]}"
notes "no heartbeat, no rules, CRD" omits "$NO_CRD" --namespace kubeat "${CRD[@]}"
notes "heartbeat, no rules" says "$UNHEARD_CHANNEL" "${HEARTBEAT[@]}"
notes "heartbeat, no rules" omits "$HEARTBEAT_SECRET" --namespace kubeat "${HEARTBEAT[@]}"
notes "heartbeat, no rules, no CRD" omits "$RULES_SET" "${HEARTBEAT[@]}"
notes "heartbeat, no rules, no CRD" says "$NO_CRD" "${HEARTBEAT[@]}"
notes "heartbeat, no rules, CRD" says "$RULES_SET" "${HEARTBEAT[@]}" "${CRD[@]}"
notes "heartbeat, no rules, CRD" omits "$NO_CRD" "${HEARTBEAT[@]}" "${CRD[@]}"
notes "rules, no heartbeat" omits "$UNHEARD_CHANNEL" "${RULES_ON[@]}"
notes "heartbeat and rules" omits "$UNHEARD_CHANNEL" "${HEARTBEAT[@]}" "${RULES_ON[@]}"
notes "heartbeat and rules" omits "$RULES_SET" "${HEARTBEAT[@]}" "${RULES_ON[@]}" "${CRD[@]}"
# The API: a port-forward to the backend and the token's Secret, the
# chart's or the one named in apiToken.existingSecret.
API_FORWARD="kubectl -n kubeat port-forward svc/kubeat-backend 8090:8080"
notes "API port-forward" says "$API_FORWARD" --namespace kubeat
notes "API token from the chart's Secret" says "kubectl -n kubeat get secret kubeat-api-token -o jsonpath='{.data.token}' | base64 -d" --namespace kubeat
notes "API token from apiToken.existingSecret" says "kubectl -n kubeat get secret my-api-token -o jsonpath='{.data.token}' | base64 -d" --namespace kubeat --set apiToken.existingSecret=my-api-token
notes "API token from apiToken.existingSecret" omits "kubeat-api-token" --namespace kubeat --set apiToken.existingSecret=my-api-token

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
for alert in KubeatSignInSourceDegraded KubeatDown KubeatSchedulerStalled KubeatUnhealthy KubeatMetricsIncomplete KubeatResultsDropped \
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
# KubeatScopeExternalRangesIgnored, in kubeat.self beside KubeatUnhealthy
# but no part of it: one warning per installation from the gauge of the
# company ranges the start ignored, with no "for" (the gauge is written once
# at the start). promtool below shows it fire and stay silent.
rules "KubeatScopeExternalRangesIgnored with backups" has "alert: KubeatScopeExternalRangesIgnored" --set backup.enabled=true
rules "KubeatScopeExternalRangesIgnored without backups" has "alert: KubeatScopeExternalRangesIgnored"
rules "KubeatScopeExternalRangesIgnored reads the ignored ranges" has 'expr: max without (instance, pod, endpoint, container, service) (kubeat_target_scope_external_ranges_ignored) > 0'
if helm template kubeat "$CHART" --show-only templates/prometheusrule.yaml "${RULES[@]}" >"$WORK/rules" 2>"$WORK/err"; then
  awk '/^        - alert: KubeatScopeExternalRangesIgnored$/ { found = 1; next } found && /^        - alert: / { exit } found' "$WORK/rules" >"$WORK/rule"
  grep -qF 'severity: warning' "$WORK/rule" || failed "KubeatScopeExternalRangesIgnored is not a warning"
  ! grep -qE '^ +for: ' "$WORK/rule" || failed "KubeatScopeExternalRangesIgnored waits: $(grep -E '^ +for: ' "$WORK/rule")"
  awk '/^        - alert: KubeatUnhealthy$/ { found = 1; next } found && /^        - alert: / { exit } found' "$WORK/rules" |
    grep -qF 'kubeat_target_scope_external_ranges_ignored' && failed "KubeatUnhealthy reads the ignored ranges"
else
  failed "KubeatScopeExternalRangesIgnored: refused: $(cat "$WORK/err")"
fi
# KubeatSignInSourceDegraded, in a group of its own between the monitors'
# and Kubeat's own: one alert per installation and reason, across the
# surfaces and a Recreate, on the four reasons that leave every browser on
# one lock. A request that did not come from the UI is only counted. Its
# severity is metrics.prometheusRule.signInSeverity, warning when values
# from before 0.52 lack it, and it is no part of KubeatUnhealthy.
rules "kubeat.signin between kubeat.monitors and kubeat.self" has '- name: kubeat.signin'
if helm template kubeat "$CHART" --show-only templates/prometheusrule.yaml "${RULES[@]}" >"$WORK/rules" 2>"$WORK/err"; then
  groups="$(grep -E '^    - name: ' "$WORK/rules" | sed 's/^    - name: //' | tr '\n' ' ')"
  [ "$groups" = "kubeat.monitors kubeat.signin kubeat.self kubeat.discovery kubeat.scope " ] || failed "rule groups in order: $groups"
else
  failed "rule groups: refused: $(cat "$WORK/err")"
fi
rules "KubeatSignInSourceDegraded per installation and reason" has 'sum without (instance, pod, endpoint, container, service, surface) ('
rules "KubeatSignInSourceDegraded on the four reasons" has 'increase(kubeat_signin_source_degraded_total{reason=~"hops_zero|no_frontend_token|hops_too_high|hops_too_low"}[15m])'
rules "KubeatSignInSourceDegraded severity from values" has 'severity: "critical"' --set metrics.prometheusRule.signInSeverity=critical
rules "KubeatSignInSourceDegraded severity missing from older values" has 'severity: "warning"' \
  --set metrics.prometheusRule.signInSeverity=null --set metrics.prometheusRule.severity=info
# KubeatDiscoveryNamespaceSkipped, in a group of its own, reads the
# namespaces the annotation sync skipped, one alert per installation as
# KubeatUnhealthy; no label of it names a namespace.
rules "KubeatDiscoveryNamespaceSkipped in its group" has '- name: kubeat.discovery'
rules "KubeatDiscoveryNamespaceSkipped reads the skipped namespaces" has 'expr: max without (instance, pod, endpoint, container, service) (kubeat_discovery_namespaces_skipped) > 0'
# KubeatScopeResolveErrors, in a group of its own after kubeat.discovery,
# reads the target scope's refusals of annotation monitors' connections to
# names that did not resolve, one alert per installation and no part of
# KubeatUnhealthy; a user's refusals are left to the README's query.
rules "KubeatScopeResolveErrors in its group" has '- name: kubeat.scope'
rules "KubeatScopeResolveErrors" has 'alert: KubeatScopeResolveErrors'
rules "KubeatScopeResolveErrors per installation" has 'sum without (instance, pod, endpoint, container, service) ('
rules "KubeatScopeResolveErrors on annotation monitors' resolve errors" has 'increase(kubeat_target_scope_denied_total{reason="resolve_error", source="annotation"}[5m])'

# metrics.prometheusRule.clusterLabel, for rules evaluated over the series of
# several clusters in one store. Empty, the rules it changes render as 0.44
# rendered them, and KubeatUnhealthy and KubeatNotificationsFailing, whose
# expressions came with 0.45, KubeatSignInSourceDegraded and
# KubeatDiscoveryNamespaceSkipped, which came with 0.52, and
# KubeatScopeExternalRangesIgnored and KubeatScopeResolveErrors, which came
# with 0.53, without the scrape
# labels as those do; given,
# they group by it beside namespace and job and keep no other label a store
# adds, and every other rule renders the same either way. promtool below
# shows how they behave over two clusters' series.
CLUSTER=(--set metrics.prometheusRule.clusterLabel=cluster)
BY_CLUSTER='^(KubeatMonitorDown|KubeatCertificateExpiring|KubeatDown|KubeatUnhealthy|KubeatBackupFailing|KubeatNotificationsFailing|KubeatSignInSourceDegraded|KubeatDiscoveryNamespaceSkipped|KubeatScopeExternalRangesIgnored|KubeatScopeResolveErrors): '
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
  exprs "$WORK/rules-plain" | grep -E "$BY_CLUSTER" | grep -vE '^(KubeatUnhealthy|KubeatNotificationsFailing|KubeatSignInSourceDegraded|KubeatDiscoveryNamespaceSkipped|KubeatScopeExternalRangesIgnored|KubeatScopeResolveErrors): ' |
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
rules "KubeatSignInSourceDegraded per cluster and reason" has 'sum by (namespace, job, cluster, reason) (' "${CLUSTER[@]}"
rules "KubeatDiscoveryNamespaceSkipped per cluster" has 'expr: max by (namespace, job, cluster) (kubeat_discovery_namespaces_skipped) > 0' "${CLUSTER[@]}"
rules "KubeatScopeExternalRangesIgnored per cluster" has 'expr: max by (namespace, job, cluster) (kubeat_target_scope_external_ranges_ignored) > 0' "${CLUSTER[@]}"
rules "KubeatScopeResolveErrors per cluster" has 'sum by (namespace, job, cluster) (' "${CLUSTER[@]}"
rules "no store label kept but the cluster's" lacks 'without (' "${CLUSTER[@]}"
for label in k8s-cluster 1cluster __cluster cluster.name namespace job pod monitor_namespace channel channel_type reason surface severity; do
  refuses "clusterLabel $label" "metrics.prometheusRule.clusterLabel is \"$label\"" "${RULES[@]}" --set-string metrics.prometheusRule.clusterLabel="$label"
done
for label in cluster k8s_cluster _cluster Cluster1; do
  renders "clusterLabel $label" "${RULES[@]}" --set-string metrics.prometheusRule.clusterLabel="$label"
done

# The rules as Prometheus reads them: promtool checks the rule groups and
# runs the scenarios of prometheusrule-test.yaml (no backend target, a failed
# scrape, a Recreate, an outage that closes, a failing backup, a backend that
# stores no result, a channel that does not take notifications, a namespace
# the annotation sync cannot read, annotation monitors refused names that do
# not resolve) against the
# rules rendered for release "kubeat" in namespace "kubeat", and those of
# prometheusrule-cluster-test.yaml (two clusters, one of them failing, gone,
# in a scrape gap or recreated, and an HA pair the store does not
# deduplicate) against the rules rendered with clusterLabel=cluster. The
# PromQL examples of README "Kapsam ihlali denetimi" are taken from the
# README as written there and run against prometheusrule-readme-test.yaml.
PROMTOOL="${PROMTOOL:-promtool}"
# The README's examples: the first code block after the bullet "Kapsam
# ihlali denetimi", each expression under its comment, as the alerts
# ReadmeScopeTarget and ReadmeScopeResolveError, in that order.
README="$CHART/../../../README.md"
awk '/^- \*\*Kapsam ihlali denetimi/ { found = 1 } found && /^ *```$/ { if (inblock) exit; inblock = 1; next } inblock' "$README" |
  sed 's/^ *//' | grep -v -e '^#' -e '^$' >"$WORK/readme-exprs"
if [ "$(grep -c . "$WORK/readme-exprs")" -eq 2 ]; then
  awk 'BEGIN { split("ReadmeScopeTarget ReadmeScopeResolveError", names, " "); print "groups:\n  - name: readme.scope\n    rules:" }
    { print "      - alert: " names[NR] "\n        expr: |-\n          " $0 }' "$WORK/readme-exprs" >"$WORK/readme-rules.yaml"
else
  failed "README \"Kapsam ihlali denetimi\": want its two PromQL examples in the first code block, found: $(cat "$WORK/readme-exprs")"
fi
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
  cp "$CHART/../prometheusrule-test.yaml" "$CHART/../prometheusrule-cluster-test.yaml" \
    "$CHART/../prometheusrule-readme-test.yaml" "$WORK/promtool/"
  if rulefile "$WORK/promtool/rules.yaml" --set backup.enabled=true &&
    rulefile "$WORK/promtool/rules-without-backups.yaml" &&
    rulefile "$WORK/promtool/rules-cluster.yaml" --set backup.enabled=true "${CLUSTER[@]}" &&
    cp "$WORK/readme-rules.yaml" "$WORK/promtool/readme-rules.yaml"; then
    "$PROMTOOL" check rules --lint-fatal "$WORK/promtool/rules.yaml" "$WORK/promtool/rules-without-backups.yaml" \
      "$WORK/promtool/rules-cluster.yaml" "$WORK/promtool/readme-rules.yaml" >"$WORK/out" 2>&1 ||
      failed "promtool check rules: $(cat "$WORK/out")"
    "$PROMTOOL" test rules "$WORK/promtool/prometheusrule-test.yaml" "$WORK/promtool/prometheusrule-cluster-test.yaml" \
      "$WORK/promtool/prometheusrule-readme-test.yaml" >"$WORK/out" 2>&1 ||
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

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
# (docs/agents/sprint-takimi.md, step 2).
README="$CHART/../../../README.md"
for text in "frontend.networkPolicy.enabled=true" "frontend.networkPolicy.from" "**Arayüzün ağ politikası" \
  "policy-group.network.openshift.io/ingress" "kubernetes.io/metadata.name: ingress-nginx" "35.191.0.0/16" "130.211.0.0/22" \
  "$SET_HOPS" "$SET_HOPS_TWO" "$NGINX_LOCAL" "externalTrafficPolicy: Local" "**Vekil-hop rehberi" \
  "$ROUTER_POLICY_CMD" "$ROUTE_OWN_POLICY" "haproxy.router.openshift.io/set-forwarded-headers: append" \
  "policy-group.network.openshift.io/host-network: \"\"" "$ROUTER_NETWORK_CMD" \
  "**Politikanın uygulandığını doğrulamak" "GKE Gateway (\`gke-l7-global-external-managed\`" \
  "X-Forwarded-For: 192.0.2.99" "**Uydurma başlıkla negatif adım"; do
  grep -qF -- "$text" "$README" || failed "README lacks \"$text\", which the notes and values point at"
done

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
# KubeatSignInSourceDegraded, in a group of its own between the monitors'
# and Kubeat's own: one alert per installation and reason, across the
# surfaces and a Recreate, on the four reasons that leave every browser on
# one lock. A request that did not come from the UI is only counted. Its
# severity is metrics.prometheusRule.signInSeverity, warning when values
# from before 0.52 lack it, and it is no part of KubeatUnhealthy.
rules "kubeat.signin between kubeat.monitors and kubeat.self" has '- name: kubeat.signin'
if helm template kubeat "$CHART" --show-only templates/prometheusrule.yaml "${RULES[@]}" >"$WORK/rules" 2>"$WORK/err"; then
  groups="$(grep -E '^    - name: ' "$WORK/rules" | sed 's/^    - name: //' | tr '\n' ' ')"
  [ "$groups" = "kubeat.monitors kubeat.signin kubeat.self " ] || failed "rule groups in order: $groups"
else
  failed "rule groups: refused: $(cat "$WORK/err")"
fi
rules "KubeatSignInSourceDegraded per installation and reason" has 'sum without (instance, pod, endpoint, container, service, surface) ('
rules "KubeatSignInSourceDegraded on the four reasons" has 'increase(kubeat_signin_source_degraded_total{reason=~"hops_zero|no_frontend_token|hops_too_high|hops_too_low"}[15m])'
rules "KubeatSignInSourceDegraded severity from values" has 'severity: "critical"' --set metrics.prometheusRule.signInSeverity=critical
rules "KubeatSignInSourceDegraded severity missing from older values" has 'severity: "warning"' \
  --set metrics.prometheusRule.signInSeverity=null --set metrics.prometheusRule.severity=info

# metrics.prometheusRule.clusterLabel, for rules evaluated over the series of
# several clusters in one store. Empty, the rules it changes render as 0.44
# rendered them, and KubeatUnhealthy and KubeatNotificationsFailing, whose
# expressions came with 0.45, without the scrape labels as those do; given,
# they group by it beside namespace and job and keep no other label a store
# adds, and every other rule renders the same either way. promtool below
# shows how they behave over two clusters' series.
CLUSTER=(--set metrics.prometheusRule.clusterLabel=cluster)
BY_CLUSTER='^(KubeatMonitorDown|KubeatCertificateExpiring|KubeatDown|KubeatUnhealthy|KubeatBackupFailing|KubeatNotificationsFailing|KubeatSignInSourceDegraded): '
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
  exprs "$WORK/rules-plain" | grep -E "$BY_CLUSTER" | grep -vE '^(KubeatUnhealthy|KubeatNotificationsFailing|KubeatSignInSourceDegraded): ' |
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

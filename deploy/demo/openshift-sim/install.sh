#!/usr/bin/env bash
# Makes a minikube cluster look like OpenShift to Kubeat, as far as that can
# be done without OpenShift: the Route and ServiceMonitor APIs, a
# thanos-querier that wants the pod's service account token over TLS signed
# by a "service CA", the cluster-monitoring-view role, the restricted pod
# security standard, and two Routes in the demo namespace.
#
# Needs the demo namespaces first (make minikube-demo).
#
#   usage: install.sh [kube-context]   (default: $KUBE_CONTEXT or minikube)
set -euo pipefail
cd "$(dirname "$0")"
CTX="${1:-${KUBE_CONTEXT:-minikube}}"
k() { kubectl --context "$CTX" "$@"; }
# The namespace the chart is installed into by make minikube-openshift-sim.
NS=kubeat-ocp

echo "== CRDs: Route, ServiceMonitor, PrometheusRule"
k apply --server-side -f crds/ >/dev/null
k wait --for=condition=Established --timeout=60s \
  crd/routes.route.openshift.io crd/servicemonitors.monitoring.coreos.com crd/prometheusrules.monitoring.coreos.com >/dev/null

echo "== namespaces"
for ns in openshift-monitoring "$NS"; do
  k create namespace "$ns" --dry-run=client -o yaml | k apply -f - >/dev/null
done
# OpenShift admits pods under the restricted-v2 SCC; the closest thing a
# plain cluster has is the restricted Pod Security Standard.
k label namespace "$NS" --overwrite \
  pod-security.kubernetes.io/enforce=restricted pod-security.kubernetes.io/enforce-version=latest >/dev/null

echo "== service CA and the certificate of thanos-querier"
if ! k -n openshift-monitoring get secret thanos-querier-tls >/dev/null 2>&1; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "/CN=openshift-service-serving-signer@sim" \
    -keyout "$tmp/ca.key" -out "$tmp/ca.crt" 2>/dev/null
  openssl req -newkey rsa:2048 -nodes -subj "/CN=thanos-querier.openshift-monitoring.svc" \
    -keyout "$tmp/tls.key" -out "$tmp/tls.csr" 2>/dev/null
  printf 'subjectAltName=DNS:thanos-querier.openshift-monitoring.svc,DNS:thanos-querier.openshift-monitoring.svc.cluster.local\n' > "$tmp/san.cnf"
  openssl x509 -req -days 825 -in "$tmp/tls.csr" -CA "$tmp/ca.crt" -CAkey "$tmp/ca.key" -CAcreateserial \
    -extfile "$tmp/san.cnf" -out "$tmp/tls.crt" 2>/dev/null
  k -n openshift-monitoring create secret tls thanos-querier-tls --cert="$tmp/tls.crt" --key="$tmp/tls.key" >/dev/null
  k -n openshift-monitoring create configmap openshift-service-ca.crt --from-file=service-ca.crt="$tmp/ca.crt" >/dev/null
fi
# OpenShift puts this ConfigMap into every namespace.
ca=$(k -n openshift-monitoring get configmap openshift-service-ca.crt -o jsonpath='{.data.service-ca\.crt}')
k -n "$NS" create configmap openshift-service-ca.crt --from-literal=service-ca.crt="$ca" --dry-run=client -o yaml | k apply -f - >/dev/null

echo "== thanos-querier (kube-rbac-proxy in front of the demo Prometheus), cluster-monitoring-view"
k apply -f rbac.yaml -f thanos-querier.yaml >/dev/null
k -n openshift-monitoring rollout status deploy/thanos-querier --timeout=180s

echo "== routes in shop-demo"
k apply -f routes.yaml >/dev/null
echo "done: install the chart with values.yaml from this directory (make minikube-openshift-sim does)"

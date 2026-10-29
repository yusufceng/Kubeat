#!/usr/bin/env bash
# Writes the GitHub Release notes of a version to stdout: the images and the
# chart release.yml pushed and signed, with their digests, the command that
# verifies the signatures and the SPDX SBOM files the Release carries.
# release.yml's verify job runs it once it has verified the signatures; a dry
# run keeps the notes as the run's summary and artifact.
#
# The environment says what was published:
#   VERSION          the chart's version, e.g. 0.45.0
#   REGISTRY_PATH    where the images and the chart were pushed, e.g.
#                    ghcr.io/yusufceng (a dry run: ghcr.io/yusufceng/kubeat-dry-run)
#   BACKEND_DIGEST, FRONTEND_DIGEST, CHART_DIGEST
#                    sha256:<64 hex digits>, as the build and helm push gave them
#   IDENTITY         the signing certificate's identity: release.yml at the
#                    ref of the run
#   DRY_RUN          true for a dry run (optional)
# and the arguments are the SBOM files, at least one each for the backend and
# the frontend (kubeat-backend-*, kubeat-frontend-*).
# Usage: scripts/release-notes.sh SBOM...
set -u

ISSUER=https://token.actions.githubusercontent.com

fail() {
  echo "$0: $*" >&2
  exit 1
}

if [ $# -eq 0 ]; then
  echo "usage: $0 SBOM... (VERSION, REGISTRY_PATH, BACKEND_DIGEST, FRONTEND_DIGEST, CHART_DIGEST and IDENTITY in the environment)" >&2
  exit 2
fi
for name in VERSION REGISTRY_PATH BACKEND_DIGEST FRONTEND_DIGEST CHART_DIGEST IDENTITY; do
  [ -n "${!name:-}" ] || fail "$name is not set"
done
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]] || fail "VERSION $VERSION is not a version"
for name in BACKEND_DIGEST FRONTEND_DIGEST CHART_DIGEST; do
  [[ "${!name}" =~ ^sha256:[0-9a-f]{64}$ ]] || fail "$name ${!name} is not a sha256 digest"
done

backend=0
frontend=0
for sbom in "$@"; do
  [ -s "$sbom" ] || fail "$sbom: missing or empty"
  case "$(basename "$sbom")" in
    kubeat-backend-*) backend=$((backend + 1)) ;;
    kubeat-frontend-*) frontend=$((frontend + 1)) ;;
    *) fail "$sbom: neither the backend's nor the frontend's SBOM" ;;
  esac
done
[ "$backend" -gt 0 ] || fail "no SBOM for the backend"
[ "$frontend" -gt 0 ] || fail "no SBOM for the frontend"

backend_ref="$REGISTRY_PATH/kubeat-backend"
frontend_ref="$REGISTRY_PATH/kubeat-frontend"
chart_ref="$REGISTRY_PATH/charts/kubeat"

# Static text in quoted heredocs and single-quoted formats: their backticks
# are Markdown, not command substitution.
if [ "${DRY_RUN:-false}" = true ]; then
  printf '> **Kuru deneme** (`workflow_dispatch`): imajlar ve chart yayım\n'
  printf '> paketlerine değil `%s` altına itildi.\n' "$REGISTRY_PATH"
  cat <<'EOF'
> Sürüm etiketine, `latest`'e ve GitHub Release'e dokunulmadı.

EOF
fi

printf '%s\n\n' "Kubeat $VERSION. Yükseltmeden önce README'deki bu sürümün yükseltme notunu okuyun."
printf '| Yayımlanan | Referans | Digest |\n'
printf '| --- | --- | --- |\n'
printf '| Backend imajı | `%s:%s` | `%s` |\n' "$backend_ref" "$VERSION" "$BACKEND_DIGEST"
printf '| Frontend imajı | `%s:%s` | `%s` |\n' "$frontend_ref" "$VERSION" "$FRONTEND_DIGEST"
printf '| Helm chart | `oci://%s`, `--version %s` | `%s` |\n' "$chart_ref" "$VERSION" "$CHART_DIGEST"

cat <<'EOF'

İmajların digest'i çok mimarili index'indir; index BuildKit'in SPDX SBOM'unu
ve SLSA provenance'ını attestation olarak taşır. Yayım iş akışı üç digest'i
keyless imzaladı (Sigstore, GitHub OIDC). Doğrulamak için (cosign v3):

```sh
cosign verify \
EOF
printf "  --certificate-identity '%s' \\\\\n" "$IDENTITY"
printf '  --certificate-oidc-issuer %s \\\n' "$ISSUER"
printf '  %s@%s \\\n' "$backend_ref" "$BACKEND_DIGEST"
printf '  %s@%s \\\n' "$frontend_ref" "$FRONTEND_DIGEST"
printf '  %s@%s\n' "$chart_ref" "$CHART_DIGEST"
cat <<'EOF'
```

Ekli SPDX SBOM'lar (platform başına):

EOF
for sbom in "$@"; do
  printf -- '- `%s`\n' "$(basename "$sbom")"
done

cat <<'EOF'

İmzalarla aynalama, hava boşluklu ortamda doğrulama ve digest'le sabitleme:
README "Tedarik zinciri". Güvenlik açığı bildirimi ve desteklenen sürümler:
SECURITY.md.
EOF

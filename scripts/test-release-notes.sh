#!/usr/bin/env bash
# Runs scripts/release-notes.sh with the values release.yml gives it and
# checks that the notes name each artifact with its digest, carry a verify
# command the shell can parse and list the SBOM files; and that a missing or
# malformed value fails before any notes are written.
# Usage: scripts/test-release-notes.sh
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
FAILED=0

failed() {
  echo "FAIL: $*"
  FAILED=$((FAILED + 1))
}

digest() {
  printf 'sha256:%064d' 0 | tr 0 "$1"
}
B="$(digest b)"
F="$(digest f)"
C="$(digest c)"
IDENTITY=https://github.com/yusufceng/Kubeat/.github/workflows/release.yml@refs/tags/v0.45.0

mkdir -p "$WORK/sbom"
for name in kubeat-backend-0.45.0-linux-amd64 kubeat-backend-0.45.0-linux-arm64 \
  kubeat-frontend-0.45.0-linux-amd64 kubeat-frontend-0.45.0-linux-arm64; do
  echo '{"spdxVersion": "SPDX-2.3"}' >"$WORK/sbom/$name.spdx.json"
done
SBOMS=("$WORK"/sbom/*.spdx.json)

# notes [NAME=VALUE...] [-- SBOM...]: the script with a tag's values, changed
# by the assignments, and the four SBOMs unless others follow --.
notes() {
  local assignments=() sboms=("${SBOMS[@]}")
  while [ $# -gt 0 ] && [ "$1" != -- ]; do
    assignments+=("$1")
    shift
  done
  if [ "${1:-}" = -- ]; then
    shift
    sboms=("$@")
  fi
  env -u DRY_RUN VERSION=0.45.0 REGISTRY_PATH=ghcr.io/yusufceng \
    BACKEND_DIGEST="$B" FRONTEND_DIGEST="$F" CHART_DIGEST="$C" IDENTITY="$IDENTITY" \
    ${assignments[@]+"${assignments[@]}"} \
    "$ROOT/scripts/release-notes.sh" ${sboms[@]+"${sboms[@]}"} >"$WORK/out" 2>"$WORK/err"
}

# has NAME TEXT...: the last notes contain each TEXT.
has() {
  local name="$1" text
  shift
  for text in "$@"; do
    grep -qF -- "$text" "$WORK/out" || failed "$name: no \"$text\" in: $(cat "$WORK/out")"
  done
}

# lacks NAME TEXT: the last notes do not contain TEXT.
lacks() {
  ! grep -qF -- "$2" "$WORK/out" || failed "$1: \"$2\" in the notes"
}

# refuses NAME TEXT ARGS...: the script fails with ARGS, says TEXT and writes
# no notes.
refuses() {
  local name="$1" text="$2"
  shift 2
  if notes "$@"; then
    failed "$name: passed"
  elif ! grep -qF -- "$text" "$WORK/err"; then
    failed "$name: failed without \"$text\": $(cat "$WORK/err")"
  elif [ -s "$WORK/out" ]; then
    failed "$name: wrote notes before failing"
  fi
}

if notes; then
  has "a tag" \
    "Kubeat 0.45.0." \
    "| Backend imajı | \`ghcr.io/yusufceng/kubeat-backend:0.45.0\` | \`$B\` |" \
    "| Frontend imajı | \`ghcr.io/yusufceng/kubeat-frontend:0.45.0\` | \`$F\` |" \
    "| Helm chart | \`oci://ghcr.io/yusufceng/charts/kubeat\`, \`--version 0.45.0\` | \`$C\` |" \
    "--certificate-identity '$IDENTITY'" \
    "--certificate-oidc-issuer https://token.actions.githubusercontent.com" \
    "ghcr.io/yusufceng/kubeat-backend@$B" \
    "ghcr.io/yusufceng/kubeat-frontend@$F" \
    "ghcr.io/yusufceng/charts/kubeat@$C" \
    "- \`kubeat-backend-0.45.0-linux-amd64.spdx.json\`" \
    "- \`kubeat-frontend-0.45.0-linux-arm64.spdx.json\`" \
    'README "Tedarik zinciri"' \
    "SECURITY.md"
  lacks "a tag" "Kuru deneme"
  # The verify command, as a reader copies it out of its code block.
  awk '/^```sh$/ { inside = 1; next } /^```$/ { inside = 0 } inside' "$WORK/out" >"$WORK/verify.sh"
  if [ "$(head -n 1 "$WORK/verify.sh")" != 'cosign verify \' ]; then
    failed "a tag: the code block does not start with cosign verify: $(cat "$WORK/verify.sh")"
  elif ! bash -n "$WORK/verify.sh" 2>"$WORK/syntax"; then
    failed "a tag: the verify command does not parse: $(cat "$WORK/syntax")"
  # One command: each line but the last goes on with a backslash.
  elif [ "$(grep -cv ' \\$' "$WORK/verify.sh")" -ne 1 ] || tail -n 1 "$WORK/verify.sh" | grep -q '\\$'; then
    failed "a tag: the verify command is not one command: $(cat "$WORK/verify.sh")"
  elif [ "$(tr -d '\\\n' <"$WORK/verify.sh" | wc -w | tr -d ' ')" -ne 9 ]; then
    failed "a tag: the verify command does not have nine words: $(cat "$WORK/verify.sh")"
  fi
else
  failed "a tag: failed: $(cat "$WORK/err")"
fi

if notes DRY_RUN=true REGISTRY_PATH=ghcr.io/yusufceng/kubeat-dry-run \
  IDENTITY=https://github.com/yusufceng/Kubeat/.github/workflows/release.yml@refs/heads/chore/x; then
  has "a dry run" \
    "**Kuru deneme**" \
    "\`ghcr.io/yusufceng/kubeat-dry-run\`" \
    "ghcr.io/yusufceng/kubeat-dry-run/kubeat-backend@$B" \
    "ghcr.io/yusufceng/kubeat-dry-run/charts/kubeat@$C" \
    "release.yml@refs/heads/chore/x'"
else
  failed "a dry run: failed: $(cat "$WORK/err")"
fi

refuses "no SBOM" "usage:" --
refuses "no chart digest" "CHART_DIGEST is not set" CHART_DIGEST=
refuses "no identity" "IDENTITY is not set" IDENTITY=
refuses "a short digest" "BACKEND_DIGEST sha256:abc is not a sha256 digest" BACKEND_DIGEST=sha256:abc
refuses "an upper-case digest" "is not a sha256 digest" FRONTEND_DIGEST="$(digest B)"
refuses "a tag for a version" "VERSION v0.45.0 is not a version" VERSION=v0.45.0
refuses "no frontend SBOM" "no SBOM for the frontend" -- "$WORK"/sbom/kubeat-backend-*.spdx.json
echo '{}' >"$WORK/notes.json"
refuses "another file" "neither the backend's nor the frontend's SBOM" -- "${SBOMS[@]}" "$WORK/notes.json"
: >"$WORK/kubeat-backend-empty.spdx.json"
refuses "an empty SBOM" "missing or empty" -- "${SBOMS[@]}" "$WORK/kubeat-backend-empty.spdx.json"

if [ "$FAILED" -gt 0 ]; then
  echo "$FAILED release notes test(s) failed"
  exit 1
fi
echo "release-notes: all tests passed"

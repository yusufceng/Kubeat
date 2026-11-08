#!/usr/bin/env bash
# Checks that the places a release's version is written by hand name
# Chart.yaml's version: its appVersion (the images' default tag), the
# Makefile's VERSION (the tag make release pushes) and the install and
# mirroring commands in README.md, docs/pilot.md and the two verification
# runbooks, docs/openshift-dogrulama.md and docs/gke-dogrulama.md, the quick
# start's among them. In those files only commands count: helm's --version
# X.Y.Z, the images kubeat-backend:X.Y.Z and kubeat-frontend:X.Y.Z, the chart
# archive kubeat-X.Y.Z.tgz, the chart's OCI reference charts/kubeat:X.Y.Z
# (oras cp) and the signing identity release.yml@refs/tags/vX.Y.Z (cosign
# verify); versions in prose, placeholders such as <sürüm> and a Service's
# host:port are not commands of a release and pass. The upgrade notes,
# docs/yukseltme.md, are not read: they name the versions they were written
# for. The README's quick start must hold a command that names a version,
# as each file must: the check would otherwise pass there while checking
# nothing.
# Given a tag, as release.yml gives the one it publishes, Chart.yaml's
# version must be the tag's, which is the tag without its leading v.
# The docs must also run the image of the README's negative step by the
# digest pinned below, next to its tag (CURL_REF).
# Prints file:line for each place that differs and exits 1.
# Usage: scripts/check-versions.sh [TAG], e.g. scripts/check-versions.sh v0.45.0
set -u

if [ $# -gt 1 ]; then
  echo "usage: $0 [TAG]" >&2
  exit 2
fi
cd "$(dirname "$0")/.." || exit 1

CHART=deploy/helm/kubeat/Chart.yaml
DOCS=(README.md docs/pilot.md docs/openshift-dogrulama.md docs/gke-dogrulama.md)
# The section whose install command is copied the most.
QUICK_FILE=README.md
QUICK_HEADING="Hızlı başlangıç"
FAILED=0

failed() {
  echo "$*"
  FAILED=$((FAILED + 1))
}

for file in "$CHART" Makefile "${DOCS[@]}"; do
  [ -f "$file" ] || failed "$file: missing"
done
[ "$FAILED" -eq 0 ] || exit 1

# Each of these prints the line number and the value, or nothing.
read -r version_line version <<<"$(awk '/^version:/ { v = $2; gsub(/["\047]/, "", v); print FNR, v; exit }' "$CHART")"
read -r app_line app_version <<<"$(awk '/^appVersion:/ { v = $2; gsub(/["\047]/, "", v); print FNR, v; exit }' "$CHART")"
read -r make_line make_version <<<"$(awk '/^VERSION[ \t]*\?=/ { v = $0; sub(/^VERSION[ \t]*\?=[ \t]*/, "", v); sub(/[ \t]+$/, "", v); print FNR, v; exit }' Makefile)"

if [ -z "${version:-}" ]; then
  echo "$CHART: no version"
  exit 1
fi
if [ $# -eq 1 ] && [ "${1#v}" != "$version" ]; then
  failed "$CHART:$version_line: version $version, not ${1#v} (the tag $1)"
fi
if [ -z "${app_version:-}" ]; then
  failed "$CHART: no appVersion"
elif [ "$app_version" != "$version" ]; then
  failed "$CHART:$app_line: appVersion $app_version, not $version"
fi
if [ -z "${make_version:-}" ]; then
  failed "Makefile: no \"VERSION ?=\" line"
elif [ "$make_version" != "$version" ]; then
  failed "Makefile:$make_line: VERSION $make_version, not $version"
fi

# The commands in the docs. A file without any is a failure too: the check
# would pass while checking nothing, for example once a command moved to
# another file. So is a quick start without any, down to the next heading of
# its level or above.
docs="$(awk -v want="$version" -v qfile="$QUICK_FILE" -v qheading="$QUICK_HEADING" '
  BEGIN {
    v = "[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?"
    n = 0
    pat[++n] = "--version[ =]+" v; pre[n] = "^--version[ =]+"; post[n] = ""
    pat[++n] = "kubeat-(backend|frontend):" v; pre[n] = "^kubeat-(backend|frontend):"; post[n] = ""
    pat[++n] = "kubeat-" v "\\.tgz"; pre[n] = "^kubeat-"; post[n] = "\\.tgz$"
    pat[++n] = "charts/kubeat:" v; pre[n] = "^charts/kubeat:"; post[n] = ""
    pat[++n] = "release\\.yml@refs/tags/v" v; pre[n] = "^release\\.yml@refs/tags/v"; post[n] = ""
  }
  FNR == 1 { code = 0; quick = 0 }
  /^[ \t]*(```|~~~)/ { code = !code; next }
  !code && /^#+[ \t]/ {
    match($0, /^#+/)
    depth = RLENGTH
    if (quick && depth <= level) quick = 0
    title = $0
    sub(/^#+[ \t]+/, "", title)
    sub(/[ \t]+$/, "", title)
    if (FILENAME == qfile && title == qheading) { quick = 1; level = depth }
    next
  }
  {
    for (i = 1; i <= n; i++) {
      rest = $0
      while (match(rest, pat[i])) {
        cmd = substr(rest, RSTART, RLENGTH)
        rest = substr(rest, RSTART + RLENGTH)
        got = cmd
        sub(pre[i], "", got)
        if (post[i] != "") sub(post[i], "", got)
        seen[FILENAME]++
        if (quick) quickseen++
        if (got != want) printf "%s:%d: %s, not %s\n", FILENAME, FNR, cmd, want
      }
    }
  }
  END {
    for (i = 1; i < ARGC; i++)
      if (!seen[ARGV[i]]) printf "%s: no command names a version\n", ARGV[i]
    if (!quickseen) printf "%s: no command in \"%s\" names a version\n", qfile, qheading
  }
' "${DOCS[@]}")"
if [ -n "$docs" ]; then
  echo "$docs"
  FAILED=$((FAILED + $(printf '%s\n' "$docs" | wc -l)))
fi

# The image the README's negative step runs in another namespace ("Arayüzün
# ağ politikası" → "Politikanın uygulandığını doğrulamak"), pinned to its
# digest next to its tag: a mirror copies, and the cluster runs, what the
# step was written for, whatever the tag later points at. The digest is the
# tag's multi-arch list digest, read from the registry's API when the tag was
# checked (docs/sprints/2026-11-07-sprint-10.md, "Sprint başında"); a new tag
# takes its own digest, read the same way, here and in the docs at once.
# Every reference to the image with a tag or a digest, in any of the docs,
# a mirror's copy among them, must be CURL_REF, and README.md must hold one:
# the check would otherwise pass there while checking nothing.
CURL_REF="curlimages/curl:8.10.1@sha256:d9b4541e214bcd85196d6e92e2753ac6d0ea699f0af5741f8c6cccbfcf00ef4b"
CURL_FILE=README.md
UNPINNED=0
images="$(awk -v image="${CURL_REF%%:*}" -v want="$CURL_REF" -v must="$CURL_FILE" '
  BEGIN { pat = image "[:@][-A-Za-z0-9_.:@]*" }
  {
    rest = $0
    while (match(rest, pat)) {
      ref = substr(rest, RSTART, RLENGTH)
      rest = substr(rest, RSTART + RLENGTH)
      # The full stop of a sentence that ends with the reference.
      sub(/\.+$/, "", ref)
      seen[FILENAME]++
      if (ref != want) printf "%s:%d: %s, not %s\n", FILENAME, FNR, ref, want
    }
  }
  END { if (!seen[must]) printf "%s: no command runs %s\n", must, image }
' "${DOCS[@]}")"
if [ -n "$images" ]; then
  echo "$images"
  UNPINNED=$(($(printf '%s\n' "$images" | wc -l)))
fi

if [ "$FAILED" -gt 0 ]; then
  echo "$FAILED place(s) do not name Chart.yaml's version $version; CONTRIBUTING.md (\"Sürümler\") lists what a release raises"
fi
if [ "$UNPINNED" -gt 0 ]; then
  echo "$UNPINNED place(s) do not run $CURL_REF; the comment above CURL_REF in scripts/check-versions.sh says how a new tag is pinned"
fi
if [ "$FAILED" -gt 0 ] || [ "$UNPINNED" -gt 0 ]; then
  exit 1
fi
echo "versions: Chart.yaml, Makefile, ${DOCS[*]} name $version"
echo "images: the docs run $CURL_REF"

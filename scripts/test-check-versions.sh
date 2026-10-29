#!/usr/bin/env bash
# Runs scripts/check-versions.sh against a copy of the files it reads, each
# time with one place changed, and checks that it fails naming that file and
# line, or passes. The files themselves are never changed.
# Usage: scripts/test-check-versions.sh
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
COPY="$WORK/repo"
FAILED=0

failed() {
  echo "FAIL: $*"
  FAILED=$((FAILED + 1))
}

# fresh: a new copy of the check and of the files it reads.
fresh() {
  rm -rf "$COPY"
  mkdir -p "$COPY/scripts" "$COPY/deploy/helm/kubeat" "$COPY/docs"
  cp "$ROOT/scripts/check-versions.sh" "$COPY/scripts/"
  cp "$ROOT/deploy/helm/kubeat/Chart.yaml" "$COPY/deploy/helm/kubeat/"
  cp "$ROOT/Makefile" "$ROOT/README.md" "$COPY/"
  cp "$ROOT/docs/pilot.md" "$COPY/docs/"
}

# change FILE OLD NEW: replaces the first OLD in the copy of FILE with NEW
# and prints the number of that line.
change() {
  local file="$COPY/$1"
  rm -f "$WORK/line"
  awk -v old="$2" -v new="$3" -v at="$WORK/line" '
    !done && (i = index($0, old)) {
      $0 = substr($0, 1, i - 1) new substr($0, i + length(old))
      done = 1
      print FNR >at
    }
    { print }
  ' "$file" >"$file.new" && mv "$file.new" "$file"
  if [ -s "$WORK/line" ]; then
    cat "$WORK/line"
  else
    echo "change: \"$2\" is not in $1" >&2
  fi
}

# passes NAME [TAG]: the check passes on the copy.
passes() {
  local name="$1"
  shift
  "$COPY/scripts/check-versions.sh" "$@" >"$WORK/out" 2>&1 || failed "$name: failed: $(cat "$WORK/out")"
}

# fails NAME TEXT [TAG]: the check fails on the copy, saying TEXT.
fails() {
  local name="$1" text="$2"
  shift 2
  if "$COPY/scripts/check-versions.sh" "$@" >"$WORK/out" 2>&1; then
    failed "$name: passed"
  elif ! grep -qF -- "$text" "$WORK/out"; then
    failed "$name: failed without \"$text\": $(cat "$WORK/out")"
  fi
}

V="$(awk '/^version:/ { print $2; exit }' "$ROOT/deploy/helm/kubeat/Chart.yaml")"
OTHER="$V-other"

fresh
passes "as committed"
passes "the tag" "v$V"
passes "the tag without its v" "$V"
line="$(awk '/^version:/ { print FNR; exit }' "$COPY/deploy/helm/kubeat/Chart.yaml")"
fails "another tag" "deploy/helm/kubeat/Chart.yaml:$line: version $V, not $OTHER (the tag v$OTHER)" "v$OTHER"

line="$(change deploy/helm/kubeat/Chart.yaml "appVersion: \"$V\"" "appVersion: \"$OTHER\"")"
fails "appVersion" "deploy/helm/kubeat/Chart.yaml:$line: appVersion $OTHER, not $V"

fresh
line="$(change Makefile "VERSION ?= $V" "VERSION ?= $OTHER")"
fails "the Makefile's VERSION" "Makefile:$line: VERSION $OTHER, not $V"

# Each kind of command, where it first appears in each file.
for spec in "README.md|--version $V|--version $OTHER" \
  "docs/pilot.md|--version $V|--version $OTHER" \
  "docs/pilot.md|kubeat-backend:$V|kubeat-backend:$OTHER" \
  "docs/pilot.md|kubeat-frontend:$V|kubeat-frontend:$OTHER" \
  "docs/pilot.md|kubeat-$V.tgz|kubeat-$OTHER.tgz"; do
  IFS='|' read -r file old new <<<"$spec"
  fresh
  line="$(change "$file" "$old" "$new")"
  fails "$file, $new" "$file:$line: $new, not $V"
done
# A line can name a version twice: the mirror's copy of an image is checked
# as well as the image it copies.
fresh
line="$(change docs/pilot.md "\$R/kubeat-backend:$V" "\$R/kubeat-backend:$OTHER")"
fails "the mirror's copy" "docs/pilot.md:$line: kubeat-backend:$OTHER, not $V"

# The upgrade notes name the versions they were written for: under a heading
# ending in "yükseltme", down to the next heading of its level or above,
# commands are not checked. Neither are versions in prose, placeholders or a
# Service's host and port. A command after the notes is checked again.
fresh
cat >>"$COPY/README.md" <<'EOF'

### 0.1'e yükseltme

Önce 0.0.9'a yükseltin:

```sh
# 0.0.9 ile
helm upgrade kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.0.9 --reset-then-reuse-values
```

#### Ayrıntı

`kubeat-backend:0.0.9` ve `kubeat-0.0.9.tgz` yalnızca 0.0.9 içindir.

### Sonraki bölüm

0.43.0'a `helm rollback` ile dönülür. İmaj `kubeat-backend:<sürüm>`, chart
`--version <sürüm>`, adres `http://kubeat-backend:8080/healthz`.
EOF
passes "upgrade notes, prose, placeholders and a Service"
printf '\n```\nhelm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.0.9\n```\n' >>"$COPY/README.md"
line="$(awk 'END { print NR - 1 }' "$COPY/README.md")"
fails "a command after the upgrade notes" "README.md:$line: --version 0.0.9, not $V"

# A file in which no command names a version fails: the check would check
# nothing there.
fresh
printf '# Pilot\n' >"$COPY/docs/pilot.md"
fails "no command" "docs/pilot.md: no command names a version"

if [ "$FAILED" -gt 0 ]; then
  echo "$FAILED version check test(s) failed"
  exit 1
fi
echo "check-versions: all tests passed"

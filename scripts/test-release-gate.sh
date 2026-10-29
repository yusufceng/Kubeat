#!/usr/bin/env bash
# Runs scripts/release-gate.sh against a fake GitHub API, a gh that answers
# from a scenario's files as GitHub would, and a fake clock (date and sleep),
# and checks what the gate decides. Nothing waits and nothing goes to the
# network. A tag on main or on a release/v* branch whose push run of CI
# passed goes on. A failed check, a commit on neither branch, a pull
# request's run in place of a push run, CI still running after 30 minutes
# and an API error stop it; the wait for CI is checked on the clock. A dry
# run takes the branch's pull request run and does not ask about branches.
# Usage: scripts/test-release-gate.sh
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
FAILED=0

failed() {
  echo "FAIL: $*"
  FAILED=$((FAILED + 1))
}

if ! command -v jq >/dev/null; then
  echo "test-release-gate: jq is required"
  exit 1
fi

# The fakes, first on the gate's PATH. A scenario is a directory, FAKE: its
# clock (seconds since the gate started), what the gate asked (calls), how
# long it slept each time (sleeps), and what GitHub knows: the branches'
# heads (branches.json), the commits each head comes after (history.json),
# CI's runs (runs.json) and each run's jobs (jobs-ID.json). A run with
# "after" does not exist until the clock reaches it; one with "done_at" is
# still running, its jobs too, until the clock reaches that. A file named
# broken holds an endpoint path whose calls fail as a 502 from GitHub.
mkdir -p "$WORK/bin"
cat >"$WORK/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -u
if [ "${1:-}" != api ] || [ $# -ne 2 ]; then
  echo "fake gh: not a gh api call: $*" >&2
  exit 2
fi
endpoint="$2"
echo "$endpoint" >>"$FAKE/calls"
path="${endpoint%%\?*}"
query=""
case "$endpoint" in *\?*) query="${endpoint#*\?}" ;; esac
param() {
  printf '%s\n' "$query" | tr '&' '\n' | sed -n "s/^$1=//p"
}
not_found() {
  echo '{"message": "Not Found", "status": "404"}'
  echo "gh: Not Found (HTTP 404)" >&2
  exit 1
}
now="$(cat "$FAKE/clock")"
if [ -f "$FAKE/broken" ] && [ "$path" = "$(cat "$FAKE/broken")" ]; then
  echo "gh: Server Error (HTTP 502)" >&2
  exit 1
fi
case "$path" in
  repos/o/r/git/ref/heads/main)
    jq -e .main "$FAKE/branches.json" >/dev/null || not_found
    jq '{ref: "refs/heads/main", object: {sha: .main, type: "commit"}}' "$FAKE/branches.json"
    ;;
  repos/o/r/git/matching-refs/heads/release/v)
    jq '[to_entries[] | select(.key | startswith("release/v"))
      | {ref: ("refs/heads/" + .key), object: {sha: .value, type: "commit"}}]' "$FAKE/branches.json"
    ;;
  repos/o/r/compare/*...*)
    basehead="${path#repos/o/r/compare/}"
    jq --arg base "${basehead%%...*}" --arg head "${basehead#*...}" '
      if $base == $head then {status: "identical", ahead_by: 0, behind_by: 0}
      elif ((.[$base] // []) | index($head)) != null then {status: "behind", ahead_by: 0, behind_by: 1}
      else {status: "diverged", ahead_by: 1, behind_by: 1} end' "$FAKE/history.json"
    ;;
  repos/o/r/actions/workflows/ci.yml/runs)
    jq --arg sha "$(param head_sha)" --arg event "$(param event)" --argjson now "$now" '
      [.[] | select(.head_sha == $sha and ($event == "" or .event == $event) and .after <= $now)
        | if .done_at > $now then .status = "in_progress" | .conclusion = null else . end
        | del(.after, .done_at)]
      | {total_count: length, workflow_runs: .}' "$FAKE/runs.json"
    ;;
  repos/o/r/actions/runs/*/jobs)
    id="${path#repos/o/r/actions/runs/}"
    id="${id%/jobs}"
    [ -f "$FAKE/jobs-$id.json" ] || not_found
    jq --slurpfile runs "$FAKE/runs.json" --argjson id "$id" --argjson now "$now" '
      ([$runs[0][] | select(.id == $id)][0].done_at > $now) as $running
      | .jobs |= map(if $running then .status = "in_progress" | .conclusion = null else . end)
      | .total_count = (.jobs | length)' "$FAKE/jobs-$id.json"
    ;;
  *) not_found ;;
esac
EOF
cat >"$WORK/bin/date" <<'EOF'
#!/usr/bin/env bash
if [ "$*" = +%s ]; then
  cat "$FAKE/clock"
else
  exec /bin/date "$@"
fi
EOF
cat >"$WORK/bin/sleep" <<'EOF'
#!/usr/bin/env bash
if ! [[ "${1:-}" =~ ^[0-9]+$ ]]; then
  echo "fake sleep: not whole seconds: $*" >&2
  exit 2
fi
echo "$1" >>"$FAKE/sleeps"
echo $(($(cat "$FAKE/clock") + $1)) >"$FAKE/clock"
EOF
chmod +x "$WORK/bin/gh" "$WORK/bin/date" "$WORK/bin/sleep"

MAIN=1111111111111111111111111111111111111111
RELEASE=2222222222222222222222222222222222222222
SHA=3333333333333333333333333333333333333333

# scenario NAME: a new scenario. The clock at 0; main's head comes after SHA;
# no release branch; CI has not run on SHA.
scenario() {
  FAKE="$WORK/$1"
  mkdir -p "$FAKE"
  echo 0 >"$FAKE/clock"
  : >"$FAKE/calls"
  : >"$FAKE/sleeps"
  printf '{"main": "%s"}\n' "$MAIN" >"$FAKE/branches.json"
  printf '{"%s": ["%s"]}\n' "$MAIN" "$SHA" >"$FAKE/history.json"
  echo '[]' >"$FAKE/runs.json"
}

# json FILE FILTER [JQ OPTIONS...]: rewrites the scenario's FILE with FILTER.
json() {
  local file="$FAKE/$1" filter="$2"
  shift 2
  jq "$@" "$filter" "$file" >"$file.new" && mv "$file.new" "$file"
}

# run ID EVENT [DONE_AT [AFTER]]: a run of CI on SHA for EVENT (push or
# pull_request) whose five checks passed. With DONE_AT it is still running
# until then; with AFTER it does not exist before then.
run() {
  json runs.json '. + [{id: $id, head_sha: $sha, event: $event, status: "completed",
      conclusion: "success", html_url: "https://github.com/o/r/actions/runs/\($id)",
      done_at: $done_at, after: $after}]' \
    --argjson id "$1" --arg sha "$SHA" --arg event "$2" --argjson done_at "${3:-0}" --argjson after "${4:-0}"
  jq -n '{jobs: ["backend", "govulncheck", "frontend", "chart", "chart (Helm 3)"]
    | map({name: ., status: "completed", conclusion: "success"})}' >"$FAKE/jobs-$1.json"
}

# job ID NAME STATE: the run's job NAME ended in STATE (failure, cancelled...),
# is still in STATE (queued, in_progress) or, with -, is not there. A NAME
# that is not one of the five adds a job.
job() {
  json "jobs-$1.json" '.jobs |= (map(select(.name != $name))
      + if $state == "-" then []
        elif $state == "queued" or $state == "in_progress" then [{name: $name, status: $state, conclusion: null}]
        else [{name: $name, status: "completed", conclusion: $state}] end)' \
    --arg name "$2" --arg state "$3"
}

# running ID: the run itself is not done; its jobs are as job left them.
running() {
  json runs.json 'map(if .id == $id then .status = "in_progress" | .conclusion = null else . end)' \
    --argjson id "$1"
}

# gate ARGS...: runs the gate on the scenario as release.yml does; its output
# in out, its exit status in status.
gate() {
  local status=0
  PATH="$WORK/bin:$PATH" FAKE="$FAKE" GITHUB_REPOSITORY=o/r GH_TOKEN=fake \
    "$ROOT/scripts/release-gate.sh" "$@" >"$FAKE/out" 2>&1 || status=$?
  echo "$status" >"$FAKE/status"
}

# exited NAME STATUS TEXT...: the gate exited with STATUS, saying each TEXT.
exited() {
  local name="$1" want="$2" text
  shift 2
  [ "$(cat "$FAKE/status")" = "$want" ] ||
    failed "$name: exit status $(cat "$FAKE/status"), not $want: $(cat "$FAKE/out")"
  for text in "$@"; do
    grep -qF -- "$text" "$FAKE/out" || failed "$name: no \"$text\" in: $(cat "$FAKE/out")"
  done
}

# passes NAME TEXT...: the release goes on. stops NAME TEXT...: it does not.
passes() {
  local name="$1"
  shift
  exited "$name" 0 "$@"
}
stops() {
  local name="$1"
  shift
  exited "$name" 1 "$@"
}

# waited NAME SECONDS: the gate slept that long in all, at most 30 seconds at
# a time.
waited() {
  local total longest
  total="$(awk '{ s += $1 } END { print s + 0 }' "$FAKE/sleeps")"
  longest="$(awk '$1 > m { m = $1 } END { print m + 0 }' "$FAKE/sleeps")"
  [ "$total" = "$2" ] || failed "$1: waited ${total}s, not $2s"
  [ "$longest" -le 30 ] || failed "$1: slept ${longest}s at once"
}

# asked NAME TEXT: one of the gate's calls to the API contains TEXT.
# not_asked NAME TEXT: none does.
asked() {
  grep -qF -- "$2" "$FAKE/calls" || failed "$1: never asked for $2: $(cat "$FAKE/calls")"
}
not_asked() {
  ! grep -qF -- "$2" "$FAKE/calls" || failed "$1: asked for $2: $(cat "$FAKE/calls")"
}

CHECKS="backend, frontend, chart, chart (Helm 3), govulncheck passed"

# A tag: on main and CI's push run green.
scenario tag-on-main
run 1 push
gate tag "$SHA"
passes "a tag on main" "$SHA is on main" "$CHECKS (https://github.com/o/r/actions/runs/1)"
waited "a tag on main" 0
asked "a tag on main" "compare/$MAIN...$SHA"
asked "a tag on main" "event=push"

scenario tag-at-head
json branches.json '.main = $sha' --arg sha "$SHA"
run 1 push
gate tag "$SHA"
passes "a tag on main's head" "$SHA is on main" "$CHECKS"

scenario tag-on-release
json branches.json '. + {"release/v0.45": $release}' --arg release "$RELEASE"
json history.json '{($main): [], ($release): [$sha]}' --arg main "$MAIN" --arg release "$RELEASE" --arg sha "$SHA"
run 1 push
gate tag "$SHA"
passes "a tag on a release branch" "$SHA is on release/v0.45" "$CHECKS"

# A tag on neither branch stops before CI is asked.
scenario tag-on-neither
json branches.json '. + {"release/v0.45": $release}' --arg release "$RELEASE"
echo '{}' >"$FAKE/history.json"
run 1 push
gate tag "$SHA"
stops "a tag on neither branch" "$SHA is on neither main nor a release/v* branch" "Nothing was pushed"
asked "a tag on neither branch" "compare/$RELEASE...$SHA"
not_asked "a tag on neither branch" "actions/"

# A failed check stops the gate at once, even while others still run.
scenario tag-failed
run 1 push
job 1 govulncheck failure
gate tag "$SHA"
stops "a failed check" "CI failed on $SHA: govulncheck: failure (https://github.com/o/r/actions/runs/1). Nothing was pushed."
waited "a failed check" 0

scenario tag-failed-while-running
run 1 push
running 1
job 1 backend failure
job 1 chart in_progress
gate tag "$SHA"
stops "a failed check while CI runs" "backend: failure"
waited "a failed check while CI runs" 0

scenario tag-missing-check
run 1 push
job 1 "chart (Helm 3)" -
gate tag "$SHA"
stops "a finished run without a check" "chart (Helm 3): no such job"

# Only the five checks count, and every push run on the commit.
scenario tag-other-job
run 1 push
job 1 lint failure
gate tag "$SHA"
passes "another job failed" "$CHECKS"

scenario tag-two-runs
run 1 push
run 2 push
job 2 frontend cancelled
gate tag "$SHA"
stops "one of two push runs failed" "frontend: cancelled (https://github.com/o/r/actions/runs/2)"

# A pull request's run is not a push run: the gate waits 30 minutes for one,
# then stops.
scenario tag-pull-request-run
run 1 pull_request
gate tag "$SHA"
stops "a pull request's run only" "CI is not done on $SHA after 30 minutes: CI has no push run on it yet" \
  "Nothing was pushed; once CI is done, re-run this run's failed jobs"
waited "a pull request's run only" 1800

# While CI runs, or before it starts, the gate waits for it, asking every 30
# seconds; for 30 minutes at most.
scenario tag-running
run 1 push 600
gate tag "$SHA"
passes "CI done after 10 minutes" "chart: in_progress" "asking again in 30s" "$CHECKS"
waited "CI done after 10 minutes" 600

scenario tag-not-started
run 1 push 300 60
gate tag "$SHA"
passes "CI starting after a minute" "CI has no push run on it yet" "backend: in_progress" "$CHECKS"
waited "CI starting after a minute" 300

scenario tag-running-too-long
run 1 push 3600
gate tag "$SHA"
stops "CI still running after 30 minutes" "CI is not done on $SHA after 30 minutes: " \
  "Nothing was pushed; once CI is done, re-run this run's failed jobs"
waited "CI still running after 30 minutes" 1800

# The API failing stops the gate at once.
scenario api-error
run 1 push
echo "repos/o/r/actions/workflows/ci.yml/runs" >"$FAKE/broken"
gate tag "$SHA"
stops "an API error" "HTTP 502" "nothing was pushed"
waited "an API error" 0

# A dry run: the branch's head need not be on main or a release branch, and
# its pull request's run counts.
scenario dry-run
echo '{}' >"$FAKE/history.json"
run 1 pull_request
gate dry-run "$SHA"
passes "a dry run" "a dry run: whether $SHA is on main or a release/v* branch is not checked" "$CHECKS"
not_asked "a dry run" "git/"
not_asked "a dry run" "compare/"
not_asked "a dry run" "event="

scenario dry-run-failed
run 1 pull_request
job 1 chart failure
gate dry-run "$SHA"
stops "a dry run with a failed check" "CI failed on $SHA: chart: failure"

scenario dry-run-running
run 1 pull_request 90
gate dry-run "$SHA"
passes "a dry run while CI runs" "$CHECKS"
waited "a dry run while CI runs" 90

# Wrong usage: exit status 2, nothing asked.
scenario usage
for args in "" "tag" "release $SHA" "tag $SHA extra"; do
  gate $args
  exited "usage: '$args'" 2 "usage:"
done
for sha in 3333 AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA v0.45.0; do
  gate tag "$sha"
  exited "the SHA '$sha'" 2 "'$sha' is not a commit's full SHA"
done
PATH="$WORK/bin:$PATH" FAKE="$FAKE" GITHUB_REPOSITORY= GH_TOKEN=fake \
  "$ROOT/scripts/release-gate.sh" tag "$SHA" >"$FAKE/out" 2>&1
echo $? >"$FAKE/status"
exited "no repository" 2 "GITHUB_REPOSITORY is not owner/name"
[ ! -s "$FAKE/calls" ] || failed "wrong usage: asked for $(cat "$FAKE/calls")"

if [ "$FAILED" -gt 0 ]; then
  echo "$FAILED release gate test(s) failed"
  exit 1
fi
echo "release-gate: all tests passed"

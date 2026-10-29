#!/usr/bin/env bash
# The release gate. release.yml's versions job runs it before anything is
# built or pushed; when it fails, no other job of the run starts.
#
# For a tag (MODE tag) both must hold:
#   (a) The tagged commit is reachable from main or from a release/v* branch.
#   (b) CI (.github/workflows/ci.yml) ran on that commit for a push and
#       passed the five checks main's ruleset requires: backend, frontend,
#       chart, chart (Helm 3) and govulncheck. A pull request's run does not
#       count: with strict status checks off, a squash commit on main is not
#       the tree its pull request's run tested.
# For a dry run (MODE dry-run: release.yml run by hand on a branch) (a) is
# left out and (b) takes CI's runs on the commit whatever their event; on a
# branch with an open pull request, that is the pull request's run.
#
# Every such run counts, each by its latest attempt, and a failed check
# fails the gate at once. While CI is still running, or has not started yet,
# the gate asks again every 30 seconds and fails after 30 minutes. Nothing
# has been pushed then; once CI is done, re-run the release run's failed jobs
# (CONTRIBUTING.md, "Yayım iş akışı").
#
# The gate asks GitHub's REST API through gh api, with the token in GH_TOKEN
# (contents: read and actions: read are enough), about the repository
# GITHUB_REPOSITORY (owner/name), and reads the answers with jq.
# Usage: scripts/release-gate.sh tag|dry-run SHA
# Exits 0 if the release may go on, 1 if not, 2 on wrong usage.
set -u

# The checks main's ruleset requires (CONTRIBUTING.md, "Merge"), one a line.
# The ruleset, this list and CONTRIBUTING.md change together.
REQUIRED='backend
frontend
chart
chart (Helm 3)
govulncheck'
# How long the gate waits for CI and how often it asks, in seconds.
TIMEOUT=1800
INTERVAL=30
TAB="$(printf '\t')"

usage() {
  echo "usage: $0 tag|dry-run SHA (GITHUB_REPOSITORY and GH_TOKEN in the environment)" >&2
  exit 2
}

[ $# -eq 2 ] || usage
MODE="$1"
SHA="$2"
case "$MODE" in
  tag | dry-run) ;;
  *) usage ;;
esac
if ! [[ "$SHA" =~ ^[0-9a-f]{40}$ ]]; then
  echo "$0: '$SHA' is not a commit's full SHA" >&2
  exit 2
fi
REPO="${GITHUB_REPOSITORY:-}"
if ! [[ "$REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
  echo "$0: GITHUB_REPOSITORY is not owner/name: '$REPO'" >&2
  exit 2
fi
for tool in gh jq; do
  if ! command -v "$tool" >/dev/null; then
    echo "$0: $tool is not installed" >&2
    exit 2
  fi
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

say() {
  echo "release gate: $*"
}

# stopped: the gate could not ask GitHub; nothing has been pushed.
stopped() {
  say "nothing was pushed; re-run this run's failed jobs (CONTRIBUTING.md, \"Yayım iş akışı\")"
  exit 1
}

# api ENDPOINT: GitHub's answer on stdout. On an error it says so on stderr
# and returns 1; its answer, an error document, is not passed on.
api() {
  if gh api "$1" >"$WORK/answer" 2>"$WORK/error"; then
    cat "$WORK/answer"
  else
    say "GitHub API $1: $(tr '\n' ' ' <"$WORK/error")" >&2
    return 1
  fi
}

# branch: prints the branch, main or a release/v* one, from which SHA can be
# reached, or nothing. Returns 1 on an API error.
branch() {
  local main release heads ref head compare status
  main="$(api "repos/$REPO/git/ref/heads/main")" || return 1
  release="$(api "repos/$REPO/git/matching-refs/heads/release/v")" || return 1
  heads="$(jq -r '"\(.ref)\t\(.object.sha)"' <<<"$main" &&
    jq -r '.[] | "\(.ref)\t\(.object.sha)"' <<<"$release")" || return 1
  # The commit is reachable from a branch when the branch's head is that
  # commit or comes after it: GitHub compares the two as identical or the
  # commit as behind.
  while IFS="$TAB" read -r ref head <&3; do
    [ -n "$ref" ] || continue
    compare="$(api "repos/$REPO/compare/$head...$SHA?per_page=1")" || return 1
    status="$(jq -r .status <<<"$compare")" || return 1
    case "$status" in
      identical | behind)
        echo "${ref#refs/heads/}"
        return 0
        ;;
    esac
  done 3<<EOF
$heads
EOF
}

# ci: sets STATE to pass, fail or wait and SAW to what the gate saw in CI's
# runs on SHA. Returns 1 on an API error.
ci() {
  local query runs lines id status url jobs checks name job conclusion failed running waiting passed
  query="head_sha=$SHA&per_page=100"
  if [ "$MODE" = tag ]; then
    query="$query&event=push"
  fi
  runs="$(api "repos/$REPO/actions/workflows/ci.yml/runs?$query")" || return 1
  lines="$(jq -r '.workflow_runs[] | "\(.id)\t\(.status)\t\(.html_url)"' <<<"$runs")" || return 1
  if [ -z "$lines" ]; then
    STATE=wait
    if [ "$MODE" = tag ]; then
      SAW="CI has no push run on it yet"
    else
      SAW="CI has no run on it yet"
    fi
    return 0
  fi
  waiting=""
  passed=""
  while IFS="$TAB" read -r id status url <&3; do
    jobs="$(api "repos/$REPO/actions/runs/$id/jobs?per_page=100")" || return 1
    # One line for each of the run's jobs that is a required check, and one
    # for each required check the run has no job for: NAME, STATUS and
    # CONCLUSION, tab-separated.
    checks="$(jq -r --arg required "$REQUIRED" '
      ($required | split("\n")) as $required
      | [.jobs[] | select(.name | IN($required[]))] as $jobs
      | ($jobs[] | "\(.name)\t\(.status)\t\(.conclusion // "")"),
        ($required[] | select(IN($jobs[].name) | not) | "\(.)\tmissing\t")
    ' <<<"$jobs")" || return 1
    failed=""
    running=""
    while IFS="$TAB" read -r name job conclusion; do
      [ -n "$name" ] || continue
      if [ "$job" = completed ]; then
        [ "$conclusion" = success ] || failed="$failed, $name: $conclusion"
      elif [ "$job" != missing ]; then
        running="$running, $name: $job"
      elif [ "$status" = completed ]; then
        failed="$failed, $name: no such job"
      else
        running="$running, $name: not started"
      fi
    done <<<"$checks"
    if [ -n "$failed" ]; then
      STATE=fail
      SAW="${failed#, } ($url)"
      return 0
    fi
    if [ -n "$running" ]; then
      waiting="$waiting; ${running#, } ($url)"
    else
      passed="$passed, $url"
    fi
  done 3<<EOF
$lines
EOF
  if [ -n "$waiting" ]; then
    STATE=wait
    SAW="${waiting#; }"
  else
    STATE=pass
    SAW="$(printf '%s' "$REQUIRED" | tr '\n' ',' | sed 's/,/, /g') passed (${passed#, })"
  fi
}

if [ "$MODE" = tag ]; then
  on="$(branch)" || stopped
  if [ -z "$on" ]; then
    say "$SHA is on neither main nor a release/v* branch; a release is tagged on one of them (CONTRIBUTING.md, \"Sürümler\"). Nothing was pushed."
    exit 1
  fi
  say "$SHA is on $on"
else
  say "a dry run: whether $SHA is on main or a release/v* branch is not checked"
fi

deadline=$(($(date +%s) + TIMEOUT))
while :; do
  ci || stopped
  case "$STATE" in
    pass)
      say "CI on $SHA: $SAW"
      exit 0
      ;;
    fail)
      say "CI failed on $SHA: $SAW. Nothing was pushed."
      exit 1
      ;;
  esac
  now="$(date +%s)"
  if [ "$now" -ge "$deadline" ]; then
    say "CI is not done on $SHA after $((TIMEOUT / 60)) minutes: $SAW. Nothing was pushed; once CI is done, re-run this run's failed jobs (CONTRIBUTING.md, \"Yayım iş akışı\")."
    exit 1
  fi
  pause=$((deadline - now))
  if [ "$pause" -gt "$INTERVAL" ]; then
    pause=$INTERVAL
  fi
  say "CI is not done on $SHA: $SAW; asking again in ${pause}s"
  sleep "$pause"
done

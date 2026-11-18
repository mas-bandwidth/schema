#!/usr/bin/env bash
# check-ci: the static gate on the workflows (make ci-check). Reads files in
# the tree only: no network, no clock, no service.
#
# usage: tools/checkci/check-ci.sh [root]
#
# Checks every .github/workflows/*.yml and .github/actions/*/action.yml under
# <root> (default: the repository) against the pins make/ruby-toolchain.mk and
# make/ruby-gems.lock name, and refuses, each by its rule's name:
#
#   path-interpreter  a line that runs an interpreter or gem tool found on
#                     PATH: a bare ruby or rubyN.N word, an env shebang, a bare
#                     gem or bundle subcommand, a bare rubocop, or
#                     ruby/setup-ruby
#   foreign-pin       a CRuby version, a sha256, or a RuboCop version other
#                     than the pins' and the lock's, or a ruby-version: key
#   unpinned-test     a job that runs make test (or test-floor, test-current,
#                     lint, negative-control, conformance) before the step
#                     that restores the pins, uses: ./.github/actions/ruby
#   no-test           ci.yml does not run every part of make test (test-floor,
#                     test-current, lint and negative-control, or make test)
#   mutable-action    a remote action named by a tag or branch, not a 40-hex
#                     commit SHA
#   fork-guard        a job (outside cla.yml) without the fork guard, so a
#                     fork's pull request would run on the fleet's runners
#   uncapped          a self-hosted job with no timeout-minutes, or, in a
#                     workflow that runs on push or pull_request, one whose
#                     timeout-minutes is over 2
#   no-cancel         a workflow that runs on pull_request (outside cla.yml)
#                     whose concurrency does not cancel a superseded run
#
# Comment lines and trailing comments are not read. Every refusal is printed as
#   ci-check FAIL [<rule>] <file>:<line>: <what>
set -euo pipefail

root=${1:-.}
mk="$root/make/ruby-toolchain.mk"
lock="$root/make/ruby-gems.lock"

if [ ! -f "$mk" ] || [ ! -f "$lock" ]; then
  echo "ci-check FAIL [foreign-pin] make/ruby-toolchain.mk or make/ruby-gems.lock: missing; there is no pin to hold the workflows to"
  exit 1
fi

pin() { sed -n "s/^$1[[:space:]]*:=[[:space:]]*//p" "$mk" | head -n1; }
floor=$(pin RUBY_FLOOR_VERSION)
current=$(pin RUBY_CURRENT_VERSION)
rubocop=$(pin RUBY_RUBOCOP_VERSION)
shas="$(sed -n 's/^RUBY_SHA256_[0-9.]*[[:space:]]*:=[[:space:]]*//p' "$mk" | tr '\n' ' ') $(sed -n 's/^[^#].* sha256:\([0-9a-f]*\)$/\1/p' "$lock" | tr '\n' ' ')"
if [ -z "$floor" ] || [ -z "$current" ] || [ -z "$rubocop" ] || [ -z "${shas// /}" ]; then
  echo "ci-check FAIL [foreign-pin] make/ruby-toolchain.mk: does not name RUBY_FLOOR_VERSION, RUBY_CURRENT_VERSION, RUBY_RUBOCOP_VERSION and a RUBY_SHA256_<version>"
  exit 1
fi

GUARD="    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository"

if [ ! -f "$root/.github/workflows/ci.yml" ]; then
  echo "ci-check FAIL [no-test] .github/workflows/ci.yml: missing"
  exit 1
fi
files=()
for f in "$root"/.github/workflows/*.yml "$root"/.github/actions/*/action.yml; do
  [ -f "$f" ] && files+=("$f")
done

fail=0
for f in "${files[@]}"; do
  rel=${f#"$root"/}
  kind=workflow
  case "$rel" in .github/actions/*) kind=action ;; esac
  exempt=0
  [ "$(basename "$f")" = cla.yml ] && exempt=1
  ci=0
  [ "$rel" = .github/workflows/ci.yml ] && ci=1
  if ! awk -v file="$rel" -v kind="$kind" -v exempt="$exempt" -v ci="$ci" \
      -v floor="$floor" -v current="$current" -v rubocop="$rubocop" -v shas="$shas" \
      -v guard="$GUARD" '
    function refuse(rule, line, what) {
      printf "ci-check FAIL [%s] %s:%d: %s\n", rule, file, line, what
      bad = 1
    }
    function endjob() {
      if (job != "" && kind == "workflow" && !exempt) {
        if (!guarded)
          refuse("fork-guard", jobline, "job " job " runs without the fork guard")
        if (selfhosted && timeout == "")
          refuse("uncapped", jobline, "self-hosted job " job " carries no timeout-minutes")
      }
      job = ""
    }
    BEGIN {
      hex64 = ""
      for (i = 0; i < 64; i++) hex64 = hex64 "[0-9a-f]"
      hex40 = ""
      for (i = 0; i < 40; i++) hex40 = hex40 "[0-9a-f]"
      n = split(shas, s, " ")
      for (i = 1; i <= n; i++) pinned[s[i]] = 1
      injobs = 0; inon = 0; inconc = 0; job = ""; bad = 0
      percommit = 0; onpr = 0; cancels = 0
      split("test-floor test-current lint negative-control", parts, " ")
    }
    /^[[:space:]]*#/ { next }
    {
      raw = $0
      line = $0
      sub(/[[:space:]]+#.*$/, "", line)
    }
    /^[^[:space:]]/ {
      endjob()
      injobs = (raw ~ /^jobs:/)
      inon = (raw ~ /^on:/)
      inconc = (raw ~ /^concurrency:/)
    }
    inon && line ~ /^  (push|pull_request)(:|[[:space:]]*$)/ { percommit = 1 }
    inon && line ~ /^  pull_request(:|[[:space:]]*$)/ { onpr = 1 }
    inconc && line ~ /^  cancel-in-progress:/ && line !~ /cancel-in-progress:[[:space:]]*false[[:space:]]*$/ { cancels = 1 }
    injobs && raw ~ /^  [A-Za-z0-9_-]+:[[:space:]]*$/ {
      endjob()
      job = raw; sub(/^  /, "", job); sub(/:.*$/, "", job)
      jobline = NR; guarded = 0; restored = 0; selfhosted = 0; timeout = ""
    }
    job != "" && raw == guard { guarded = 1 }
    job != "" && line ~ /^    runs-on:.*self-hosted/ { selfhosted = 1 }
    job != "" && line ~ /^    timeout-minutes:/ {
      timeout = line; sub(/^    timeout-minutes:[[:space:]]*/, "", timeout)
      if (percommit && !exempt && (timeout !~ /^[0-9]+$/ || timeout + 0 > 2))
        refuse("uncapped", NR, "job " job " runs on push or pull_request with timeout-minutes " timeout ", over the two-minute wall")
    }

    # path-interpreter
    line ~ /(^|[^A-Za-z0-9_.\/$-])ruby[0-9.]*([^A-Za-z0-9_.\/-]|$)/ ||
    line ~ /\/usr\/bin\/env[[:space:]]+ruby/ || line ~ /setup-ruby/ ||
    line ~ /(^|[[:space:];&|(])(gem|bundle|bundler)[[:space:]]+(install|exec|update|build|push|list|env|binstubs)([[:space:]]|$)/ ||
    line ~ /(^|[[:space:];&|(])rubocop([[:space:]]|$)/ {
      refuse("path-interpreter", NR, "runs an interpreter from PATH, not a pin: " line)
    }

    # foreign-pin
    line ~ /ruby-version[[:space:]]*:/ {
      refuse("foreign-pin", NR, "names a ruby-version, not the pins of make/ruby-toolchain.mk")
    }
    {
      rest = line
      while (match(rest, /(^|[^0-9A-Za-z.])[34]\.[0-9]+(\.[0-9]+)?/)) {
        v = substr(rest, RSTART, RLENGTH); sub(/^[^0-9]/, "", v)
        if (v != floor && v != current)
          refuse("foreign-pin", NR, "names CRuby " v ", the pins are " floor " and " current)
        rest = substr(rest, RSTART + RLENGTH)
      }
      rest = line
      while (match(rest, hex64)) {
        v = substr(rest, RSTART, RLENGTH)
        if (!(v in pinned))
          refuse("foreign-pin", NR, "names sha256 " v ", not a pin of make/ruby-toolchain.mk or make/ruby-gems.lock")
        rest = substr(rest, RSTART + RLENGTH)
      }
      rest = line
      while (match(rest, /[Rr]ubo[Cc]op(==|@|-|[[:space:]])[0-9]+\.[0-9]+(\.[0-9]+)?/)) {
        v = substr(rest, RSTART, RLENGTH); sub(/^[Rr]ubo[Cc]op(==|@|-|[[:space:]])/, "", v)
        if (v != rubocop)
          refuse("foreign-pin", NR, "names RuboCop " v ", the lock is RuboCop " rubocop)
        rest = substr(rest, RSTART + RLENGTH)
      }
    }

    # mutable-action
    line ~ /uses:[[:space:]]*[^.[:space:]][^[:space:]]*@/ {
      ref = line; sub(/^.*@/, "", ref); sub(/[[:space:]].*$/, "", ref)
      if (ref !~ ("^" hex40 "$"))
        refuse("mutable-action", NR, "uses an action at the mutable ref " ref ", not a commit SHA")
    }

    # unpinned-test, no-test
    line ~ /uses:[[:space:]]*\.\/\.github\/actions\/ruby([[:space:]]|$)/ { restored = 1 }
    line ~ /(^|[^A-Za-z0-9_-])make([[:space:]]+[^[:space:]]+)*[[:space:]]+(test|test-floor|test-current|lint|negative-control|conformance)([[:space:]]|$)/ {
      t = line; sub(/^.*(^|[^A-Za-z0-9_-])make[[:space:]]+/, "", t)
      m = split(t, words, /[[:space:]]+/)
      for (i = 1; i <= m; i++) {
        if (words[i] == "test") for (j in parts) ran[parts[j]] = 1
        else ran[words[i]] = 1
      }
      if (kind == "workflow" && !restored)
        refuse("unpinned-test", NR, "runs the tests before uses: ./.github/actions/ruby restores the pins" (job != "" ? " (job " job ")" : ""))
    }
    END {
      endjob()
      if (ci)
        for (j = 1; j <= 4; j++)
          if (!(parts[j] in ran))
            refuse("no-test", NR, "ci.yml never runs make " parts[j] " (nor make test)")
      if (kind == "workflow" && onpr && !exempt && !cancels)
        refuse("no-cancel", NR, "runs on pull_request, but its concurrency does not cancel a superseded run")
      exit bad
    }
  ' "$f"; then
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "ci-check: FAIL"
  exit 1
fi
echo "ci-check: ${#files[@]} workflow files hold the pins of make/ruby-toolchain.mk (CRuby $floor and $current, RuboCop $rubocop)"

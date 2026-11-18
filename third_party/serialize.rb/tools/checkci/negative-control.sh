#!/usr/bin/env bash
# The negative control of check-ci (make ci-check-negative-control). The clean
# tree must pass first; then each rule's fault is planted in a scratch copy of
# the workflows (one fault per copy, under build/), and check-ci must go red
# on that copy by that rule's name. Reads and writes files under build/ only.
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo"
check=tools/checkci/check-ci.sh
nc=build/ci-check-nc
GUARD="    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository"

if ! bash "$check" . > /dev/null; then
  bash "$check" . || true
  echo "NEGATIVE CONTROL FAILED: ci-check is red on the clean tree, so no red below would be the planted fault"
  exit 1
fi

rm -rf "$nc"
mkdir -p "$nc"
planted=0

# copy <name>: a fresh root holding the tree's workflows, actions and pins
copy() {
  local dst="$nc/$1"
  mkdir -p "$dst/make"
  cp -R .github "$dst/.github"
  cp make/ruby-toolchain.mk make/ruby-gems.lock "$dst/make/"
  echo "$dst"
}

# expect <name> <rule> <what>: check-ci on the copy is red, by the rule's name
expect() {
  local dst="$nc/$1" rule=$2 what=$3
  if bash "$check" "$dst" > "$dst.log" 2>&1; then
    cat "$dst.log"
    echo "NEGATIVE CONTROL FAILED: $what passed ci-check"
    exit 1
  fi
  if ! grep -q "^ci-check FAIL \[$rule\]" "$dst.log"; then
    cat "$dst.log"
    echo "NEGATIVE CONTROL FAILED: $what went red, but not by [$rule]"
    exit 1
  fi
  planted=$((planted + 1))
  echo "ci-check-negative-control: $what -> red by [$rule]"
}

# without <file> <pattern>: the file with every line matching the fixed
# pattern removed, in place; refuses a pattern that matches nothing
without() {
  if ! grep -qF -- "$2" "$1"; then
    echo "NEGATIVE CONTROL FAILED: $1 has no line \"$2\" to remove"
    exit 1
  fi
  grep -vF -- "$2" "$1" > "$1.tmp"
  mv "$1.tmp" "$1"
}

wf=.github/workflows/ci.yml
action=.github/actions/ruby/action.yml

d=$(copy path-interpreter)
printf '      - run: ruby -w test/run.rb\n' >> "$d/$wf"
expect path-interpreter path-interpreter "a step running ruby from PATH in ci.yml"

d=$(copy path-interpreter-action)
printf '    - shell: bash\n      run: ruby3.4 -e 1\n' >> "$d/$action"
expect path-interpreter-action path-interpreter "a step running ruby3.4 from PATH in the pins action"

d=$(copy env-shebang)
printf '      - run: /usr/bin/env ruby test/run.rb\n' >> "$d/$wf"
expect env-shebang path-interpreter "an env lookup of ruby in ci.yml"

d=$(copy setup-ruby)
printf '      - uses: ruby/setup-ruby@0000000000000000000000000000000000000000\n' >> "$d/$wf"
expect setup-ruby path-interpreter "ruby/setup-ruby in ci.yml"

d=$(copy path-gem)
printf '      - run: gem install rubocop\n' >> "$d/$wf"
expect path-gem path-interpreter "gem install from PATH in ci.yml"

d=$(copy path-rubocop)
printf '      - run: rubocop lib test\n' >> "$d/$wf"
expect path-rubocop path-interpreter "rubocop from PATH in ci.yml"

d=$(copy foreign-version)
printf '      - run: make dist-ruby RUBY_FLOOR_VERSION=3.4.11\n' >> "$d/$wf"
expect foreign-version foreign-pin "a CRuby version other than the pins in ci.yml"

d=$(copy foreign-version-action)
printf '    - shell: bash\n      run: echo dist/ruby-4.0.8\n' >> "$d/$action"
expect foreign-version-action foreign-pin "a CRuby version other than the pins in the pins action"

d=$(copy foreign-sha)
printf '      - run: echo 0000000000000000000000000000000000000000000000000000000000000000\n' >> "$d/$wf"
expect foreign-sha foreign-pin "a sha256 other than the pins and the lock in ci.yml"

d=$(copy foreign-rubocop)
printf '      - run: echo rubocop==1.93.0\n' >> "$d/$wf"
expect foreign-rubocop foreign-pin "a RuboCop version other than the lock in ci.yml"

d=$(copy ruby-version-key)
printf '        with:\n          ruby-version: 3.4.12\n' >> "$d/$wf"
expect ruby-version-key foreign-pin "a ruby-version: key in ci.yml"

d=$(copy unpinned-test)
without "$d/$wf" 'uses: ./.github/actions/ruby'
expect unpinned-test unpinned-test "make test with the pins-restoring step removed from ci.yml"

d=$(copy unpinned-test-release)
without "$d/.github/workflows/release.yml" 'uses: ./.github/actions/ruby'
expect unpinned-test-release unpinned-test "make test with the pins-restoring step removed from release.yml"

d=$(copy no-test)
without "$d/$wf" 'run: make test'
expect no-test no-test "ci.yml with every make test removed"

d=$(copy mutable-action)
printf '      - uses: actions/checkout@v4\n' >> "$d/$wf"
expect mutable-action mutable-action "an action named by a tag in ci.yml"

d=$(copy fork-guard)
without "$d/$wf" "$GUARD"
expect fork-guard fork-guard "ci.yml with the fork guard removed from its jobs"

d=$(copy uncapped)
without "$d/$wf" 'timeout-minutes:'
expect uncapped uncapped "ci.yml with timeout-minutes removed from its self-hosted jobs"

d=$(copy uncapped-toolchain)
without "$d/.github/workflows/toolchain.yml" 'timeout-minutes:'
expect uncapped-toolchain uncapped "toolchain.yml with timeout-minutes removed from its self-hosted job"

d=$(copy over-wall)
sed 's/timeout-minutes: 2$/timeout-minutes: 10/' "$wf" > "$d/$wf"
expect over-wall uncapped "a ci.yml job capped at 10 minutes, over the two-minute wall"

d=$(copy no-cancel)
sed 's/^  cancel-in-progress:.*$/  cancel-in-progress: false/' "$wf" > "$d/$wf"
expect no-cancel no-cancel "ci.yml whose concurrency never cancels a superseded pull request run"

echo "ci-check-negative-control: $planted planted faults, each turns ci-check red by its rule's name"

#!/bin/sh
# make/bench-check.sh -- this machine has every variable and tool schema's CI and legs expect, or it fails by name.
# `make bench-check` runs it; run it on a bench or CI runner after a toolchain change (nova-fleet sdk-toolchain).
#
# It takes the machine's SDK environment the way a login or job does (the SDK's env.sh, path differing per host:
# /opt/nova/sdk or ~/sdk, never hard-coded here), then checks, naming each gap and the line that fixes it:
#   - PYTHON, PYTHON_FLOOR, MYPY, RUBY, RUBY_FLOOR and NOVA_SDK are exported, and each answers its pinned version
#     (pins read from make/python-toolchain.mk and make/ruby-toolchain.mk);
#   - rubocop, steep and rbs beside RUBY and RUBY_FLOOR answer the pinned analyzer versions;
#   - on Linux, the linters the lint lanes of ci-fast.yml run from the SDK: modernize, golangci-lint, luacheck,
#     eslint, clang-format (a Mac runs no Linux lane and is not asked for them).
# Exit 0 and one BENCH-CHECK OK line, or exit 1 with a BENCH-CHECK MISSING line per gap.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
for f in "${NOVA_SDK_ENV:-}" "$HOME/sdk/env.sh" /opt/nova/sdk/env.sh; do
	if [ -n "$f" ] && [ -f "$f" ]; then . "$f"; break; fi
done
export PATH="$HOME/sdk/bin:$PATH"
fail=0
miss() { echo "BENCH-CHECK MISSING $1: $2"; fail=1; }
pin() { sed -n "s/^$1 *[:?]\{0,1\}= *//p" "$root/make/$2" | sed -n 1p; }

# the pins ci-fast.yml's lint lanes name (bumped by hand with the workflow and the nova-tools sdk-toolchain defaults)
modernize_pin=0.23.0
golangci_pin=2.12.2
eslint_pin=9.39.5
luacheck_pin=1.2.0

py_cur=$(pin PYTHON_CURRENT_VERSION python-toolchain.mk); py_floor=$(pin PYTHON_FLOOR_VERSION python-toolchain.mk)
mypy_pin=$(pin PYTHON_MYPY_VERSION python-toolchain.mk)
rb_cur=$(pin RUBY_CURRENT_VERSION ruby-toolchain.mk); rb_floor=$(pin RUBY_FLOOR_VERSION ruby-toolchain.mk)
rubocop_pin=$(pin RUBY_RUBOCOP_VERSION ruby-toolchain.mk); steep_pin=$(pin RUBY_STEEP_VERSION ruby-toolchain.mk)
rbs_pin=$(pin RUBY_RBS_VERSION ruby-toolchain.mk)
fixsdk="nova-fleet sdk-toolchain -- -l <this machine's fleet name> (nova-tools release with the pin)"

[ -n "${NOVA_SDK:-}" ] && [ -d "$NOVA_SDK" ] || miss NOVA_SDK "not exported by the SDK's env.sh, or not a directory (${NOVA_SDK:-unset}); fix: $fixsdk"

pyver() { "$1" -I -c 'import sys; print("%d.%d.%d" % sys.version_info[:3])' 2>/dev/null; }
rbver() { "$1" --disable-gems -e 'print RUBY_VERSION' 2>/dev/null; }
checkvar() { # name want-version probe
	eval "v=\${$1:-}"
	if [ -z "$v" ]; then miss "$1" "not exported by the SDK's env.sh; fix: $fixsdk"; return; fi
	got=$($3 "$v")
	[ "$got" = "$2" ] || miss "$1" "$v answers '${got:-nothing}', the pin is $2; fix: $fixsdk"
}
checkvar PYTHON "$py_cur" pyver
checkvar PYTHON_FLOOR "$py_floor" pyver
checkvar RUBY "$rb_cur" rbver
checkvar RUBY_FLOOR "$rb_floor" rbver
if [ -z "${MYPY:-}" ]; then miss MYPY "not exported by the SDK's env.sh; fix: $fixsdk"
else
	got=$("$MYPY" --version 2>/dev/null)
	case "$got" in "mypy $mypy_pin "* | "mypy $mypy_pin") ;; *) miss MYPY "$MYPY answers '${got:-nothing}', the pin is mypy $mypy_pin; fix: $fixsdk" ;; esac
fi

for r in "${RUBY:-}" "${RUBY_FLOOR:-}"; do
	[ -n "$r" ] || continue
	d=$(dirname "$r")
	for a in rubocop:$rubocop_pin steep:$steep_pin rbs:$rbs_pin; do
		n=${a%%:*}; want=${a#*:}
		got=$("$d/$n" --version 2>/dev/null | sed -n '1s/^[^0-9]*\([0-9][0-9.]*\).*/\1/p')
		[ "$got" = "$want" ] || miss "$n" "$d/$n answers '${got:-nothing}', the pin is $want; fix: $fixsdk"
	done
done

if [ "$(uname -s)" = Linux ]; then
	need() { # command pin-text version-probe-output-test-pattern
		p=$(command -v "$1" 2>/dev/null)
		if [ -z "$p" ]; then miss "$1" "not on PATH (the SDK's bin is first); need $2; fix: $fixsdk"; return 1; fi
		return 0
	}
	if need modernize "modernize $modernize_pin"; then
		readlink -f "$(command -v modernize)" | grep "modernize-$modernize_pin/" >/dev/null || miss modernize "$(readlink -f "$(command -v modernize)") is not modernize-$modernize_pin; fix: $fixsdk"
	fi
	if need golangci-lint "golangci-lint $golangci_pin"; then
		golangci-lint version 2>/dev/null | grep "$golangci_pin" >/dev/null || miss golangci-lint "answers '$(golangci-lint version 2>&1 | head -1)', the pin is $golangci_pin; fix: $fixsdk"
	fi
	if need luacheck "luacheck $luacheck_pin"; then
		luacheck --version 2>/dev/null | grep "$luacheck_pin" >/dev/null || miss luacheck "answers '$(luacheck --version 2>&1 | head -1)', the pin is $luacheck_pin; fix: $fixsdk"
	fi
	if need eslint "eslint $eslint_pin"; then
		[ "$(eslint --version 2>/dev/null)" = "v$eslint_pin" ] || miss eslint "answers '$(eslint --version 2>&1 | head -1)', the pin is v$eslint_pin; fix: $fixsdk"
	fi
	if need clang-format "clang-format $(pin CLANG_FORMAT_PIN c.mk)"; then
		cfm=$(pin CLANG_FORMAT_PIN c.mk | cut -d. -f1)
		[ "$(clang-format --version | sed -n 's/.*version \([0-9][0-9]*\)\..*/\1/p')" = "$cfm" ] || miss clang-format "major is not $cfm; fix: $fixsdk"
	fi
fi

if [ "$fail" -ne 0 ]; then echo "BENCH-CHECK FAILED host=$(hostname -s)"; exit 1; fi
echo "BENCH-CHECK OK host=$(hostname -s) sdk=$NOVA_SDK python=$py_cur/$py_floor mypy=$mypy_pin ruby=$rb_cur/$rb_floor rubocop=$rubocop_pin"

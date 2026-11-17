#!/bin/sh
# make/ruby-toolchain-check.sh — the Ruby leg's pins are what they say
# (make/ruby-toolchain.mk, ruby-toolchain-check; docs/PORT-RUBY.md, THE PIN).
#
# usage: sh make/ruby-toolchain-check.sh [root]
#
# Reads RUBY, RUBY_FLOOR, RUBOCOP, STEEP, RBS and the expected versions from
# the environment make exports, and refuses, by pin name:
#
#   - an interpreter whose own RUBY_VERSION is not its pinned release;
#   - an analyzer whose own --version is not the locked gem, including the
#     analyzer beside each pinned interpreter;
#   - any file of the leg under <root> (default: the working directory) that
#     runs an interpreter it would find on PATH: a bare ruby command, an env
#     shebang, or a Go exec.Command or exec.LookPath of a ruby.
#
# Every interpreter runs isolated (--disable-gems): no gem environment, no
# RUBYOPT, no user gem directory.
set -u
root=${1:-.}
lock=${RUBY_GEMS_LOCK:-$root/make/ruby-gems.lock}
fail=0

refuse() {
	echo "ruby-toolchain-check REFUSES: $*"
	fail=1
}

interpreter_version() {
	"$1" --disable-gems -e 'print RUBY_VERSION' 2>/dev/null
}

analyzer_version() {
	case "$1" in
	rubocop)
		"$2" --version 2>/dev/null | sed -n '1s/^[^0-9]*\([0-9][0-9.]*\).*/\1/p'
		;;
	steep)
		"$2" --version 2>/dev/null | sed -n '1s/^[^0-9]*\([0-9][0-9.]*\).*/\1/p'
		;;
	rbs)
		"$2" --version 2>/dev/null | sed -n 's/^rbs[[:space:]]*//p'
		;;
	esac
}

lock_version() {
	sed -n "s/^$1==\([^ ]*\) .*/\1/p" "$lock" 2>/dev/null | sed -n '1p'
}

if [ ! -f "$lock" ]; then
	refuse "the Ruby leg's analyzer lock is missing: $lock"
else
	for a in rubocop steep rbs; do
		if [ -z "$(lock_version "$a")" ]; then
			refuse "make/ruby-gems.lock names no $a"
		fi
	done
fi

check_interpreter() {
	pin=$1 path=$2 want=$3
	got=$(interpreter_version "$path")
	if [ "$got" != "$want" ]; then
		refuse "the ruby leg's pinned $pin is Ruby ${got:-<no answer>}, the pin is $want"
		echo "  $pin = $path"
		echo "  make dist-ruby builds the pinned interpreters into dist/"
	fi
}

check_analyzer() {
	pin=$1 path=$2 name=$3 want=$4
	got=$(analyzer_version "$name" "$path")
	if [ "$got" != "$want" ]; then
		refuse "the ruby leg's pinned $pin reports \"${got:-<no answer>}\", the lock is $name $want"
		echo "  $pin = $path"
		echo "  make dist-ruby installs the analyzers from make/ruby-gems.lock"
	fi
}

check_interpreter RUBY "${RUBY:?RUBY is not set; run this through make ruby-toolchain-check}" "${RUBY_CURRENT_VERSION:?}"
check_interpreter RUBY_FLOOR "${RUBY_FLOOR:?}" "${RUBY_FLOOR_VERSION:?}"

check_analyzer RUBOCOP "${RUBOCOP:?}" rubocop "$(lock_version rubocop)"
check_analyzer STEEP "${STEEP:?}" steep "$(lock_version steep)"
check_analyzer RBS "${RBS:?}" rbs "$(lock_version rbs)"

# EVERY INTERPRETER'S OWN ANALYZERS. A pin is a tree, not one script: the
# analyzer beside each pinned interpreter must be the lock's too, or a gate
# that runs one interpreter would pass over the other's toolchain.
for interp in RUBY RUBY_FLOOR; do
	eval "ipath=\$$interp"
	bindir=$(dirname "$ipath")
	for a in rubocop steep rbs; do
		want=$(lock_version "$a")
		got=$(analyzer_version "$a" "$bindir/$a")
		if [ "$got" != "$want" ]; then
			refuse "the ruby leg's $a beside $interp reports \"${got:-<no answer>}\", the lock is $a $want"
			echo "  $interp = $ipath"
		fi
	done
done

# THE PATH GATE. The leg's files: its make fragments and scripts, its Go
# packages, its conformance driver and the action and workflow that run it. A
# line that names the pinned path (dist/ruby-4.0.9/bin/ruby) is no PATH
# lookup; the patterns below match a command word, not a path component.
files=$(cd "$root" && {
	find make -maxdepth 1 -type f \( -name 'ruby*.mk' -o -name 'ruby*.sh' \) 2>/dev/null
	for d in internal/codegen/ruby internal/codegen/rubytable test/conformance/ruby; do
		[ -d "$d" ] && find "$d" -type f 2>/dev/null
	done
	find compiler -maxdepth 1 -type f -name '*ruby*' 2>/dev/null
	[ -f .github/workflows/ruby-leg.yml ] && echo .github/workflows/ruby-leg.yml
	[ -d .github/actions/ruby ] && find .github/actions/ruby -type f 2>/dev/null
} | sort)
if [ -z "$files" ]; then
	refuse "found none of the leg's files under $root; the gate would have checked nothing"
fi
for f in $files; do
	[ "$f" = make/ruby-toolchain-check.sh ] && continue
	hits=$(cd "$root" && grep -nE \
		-e '(^|[;&|(])ruby([[:space:]]|$)' \
		-e '^[[:space:]]+ruby([[:space:]]|$)' \
		-e '/usr/bin/env[[:space:]]+ruby' \
		-e '(Command|LookPath)\("ruby' \
		"$f" | grep -vE '^[0-9]+:[[:space:]]*(#|//)')
	if [ -n "$hits" ]; then
		refuse "$f runs an interpreter from PATH, not a pin:"
		echo "$hits" | sed 's/^/  /'
	fi
done

if [ $fail -ne 0 ]; then
	exit 1
fi
echo "ruby-toolchain-check: RUBY is Ruby $RUBY_CURRENT_VERSION, RUBY_FLOOR is Ruby $RUBY_FLOOR_VERSION, RUBOCOP is RuboCop $(lock_version rubocop), STEEP is Steep $(lock_version steep), RBS is RBS $(lock_version rbs); $(echo "$files" | wc -l | tr -d ' ') leg files run no interpreter from PATH"

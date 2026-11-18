#!/bin/sh
# make/ruby-toolchain-check.sh: the pins are what they say
# (make/ruby-toolchain.mk, ruby-toolchain-check).
#
# Reads RUBY, RUBY_FLOOR and the expected versions from the environment make
# sets, and refuses, by pin name, an interpreter whose own RUBY_VERSION is not
# its pinned release and a RuboCop beside the floor whose own --version is not
# the locked gem. Every interpreter runs isolated (--disable-gems: no gem
# environment), and RuboCop runs with no RUBYOPT, RUBYLIB, GEM_HOME or
# GEM_PATH of the caller's.
set -u
fail=0

refuse() {
	echo "ruby-toolchain-check REFUSES: $*"
	fail=1
}

check_interpreter() {
	pin=$1 path=$2 want=$3
	got=$("$path" --disable-gems -e 'print RUBY_VERSION' 2>/dev/null)
	if [ "$got" != "$want" ]; then
		refuse "the pinned $pin is Ruby ${got:-<no answer>}, the pin is $want"
		echo "  $pin = $path"
		echo "  make dist-ruby builds the pinned interpreters into dist/"
	fi
}

check_interpreter RUBY "${RUBY:?RUBY is not set; run this through make ruby-toolchain-check}" "${RUBY_CURRENT_VERSION:?}"
check_interpreter RUBY_FLOOR "${RUBY_FLOOR:?}" "${RUBY_FLOOR_VERSION:?}"

want=${RUBY_RUBOCOP_VERSION:?}
got=$(env -u RUBYOPT -u RUBYLIB -u GEM_HOME -u GEM_PATH "$RUBY_FLOOR" -e 'load Gem.bin_path("rubocop", "rubocop")' -- --version 2>/dev/null)
if [ "$got" != "$want" ]; then
	refuse "the pinned RUBOCOP reports \"${got:-<no answer>}\", the lock is RuboCop $want"
	echo "  RUBOCOP = RuboCop under $RUBY_FLOOR"
	echo "  make dist-ruby installs it from make/ruby-gems.lock"
fi

if [ $fail -ne 0 ]; then
	exit 1
fi
echo "ruby-toolchain-check: RUBY is Ruby $RUBY_CURRENT_VERSION, RUBY_FLOOR is Ruby $RUBY_FLOOR_VERSION, RUBOCOP is RuboCop $want"

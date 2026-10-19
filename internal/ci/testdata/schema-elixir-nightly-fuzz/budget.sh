#!/bin/sh
# Derive the harness loop seconds from the chosen timeout budget.
#
# The timed target's outer timeout is the budget the caller picked (for example
# `make schema-elixir-nightly-fuzz SCHEMA_ELIXIR_FUZZ_TIME=1m`); the harness
# deadline is derived from that budget with a bounded shutdown margin, so a
# healthy run is not killed by its own timeout. Startup and coverage
# instrumentation are counted inside the harness deadline. The clock is an
# argument: this script reads no time source.
#
#   budget.sh <budget> <slack-seconds>
#
# <budget> is seconds ("600"), or a make-style minute/second suffix ("10m",
# "45s"). The result is always at least one second.
set -eu

budget=$1
slack=${2:-5}

case "$budget" in
*[0-9]m) seconds=$((${budget%m} * 60)) ;;
*[0-9]s) seconds=${budget%s} ;;
*[0-9]*) seconds=$budget ;;
*)
	echo "budget.sh: cannot read time budget '$budget'" >&2
	exit 2
	;;
esac

loop=$((seconds - slack))
[ "$loop" -ge 1 ] || loop=1
echo "$loop"

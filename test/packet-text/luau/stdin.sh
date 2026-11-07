#!/bin/sh
# stdin.sh DIR MAIN [ARG...] — the host shim of the Luau packet drivers
# (test/packet-text/luau, test/packet-wide/luau). The pinned interpreter has
# no io library (docs/PORT-LUAU.md, THE HARNESS), so a Luau program cannot
# read the harness's stdin. This shim carries stdin in AS SOURCE: it writes
# DIR/stdin.luau, a module returning the input lines as an array of strings,
# then runs DIR/MAIN under $LUAU with the remaining arguments after -a. The
# lines are hexadecimal wires, optionally led by a decimal bound; any other
# byte is dropped before it reaches the string literal.
set -eu
dir=$1
main=$2
shift 2
{
	printf 'return {\n'
	sed -e 's/[^0-9a-fA-F -]//g' -e 's/.*/"&",/'
	printf '}\n'
} >"$dir/stdin.luau"
cd "$dir"
exec "${LUAU:?LUAU names the pinned interpreter: run make dist-luau}" "$main" -a "$@"

#!/bin/sh
# Strict Elixir compile. Elixir 1.20.4 on OTP 29 ACCEPTS `elixirc
# --warnings-as-errors` (the help text says it returns non-zero) but exits 0
# even when warnings are printed: the diagnostics go out through the logger, not
# the compiler's warning counter, so the flag is a no-op for the gate. Every
# gate that must fail on a warning runs this wrapper instead: it runs elixirc,
# echoes the full output, and exits non-zero when the output carries a warning
# or the compiler itself failed. Verified on Elixir 1.20.4 / OTP 29: the
# bare elixirc --warnings-as-errors exit is 0; this wrapper's exit is non-zero.
out=$("$@" 2>&1)
status=$?
printf '%s\n' "$out"
if [ "$status" -ne 0 ] || printf '%s\n' "$out" | grep -Eq '(^|[[:space:]])warning:'; then
	exit 1
fi
exit 0

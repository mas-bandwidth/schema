# PR #4588 review: under pipefail an early-exit reader at the end of a pipeline used as
# a condition lets the stage before it die of SIGPIPE, and the condition reads
# the opposite once the log is bigger than a pipe holds. Each line is one shape.
check: build/log
	@if grep -E '^[a-z-]+ +FAIL' build/log | grep -vqE '^retain '; then echo "another surface went red"; exit 1; fi
	@grep -F 'FAILED: ' build/log | grep -q 'reads back' && { echo "a boundary case went red"; exit 1; }
	@grep -F 'PASS' build/log | grep --quiet TestA || { echo "TestA did not run"; exit 1; }
	@if ! grep -F 'FAILED: ' build/log | grep -m1 'cook-write'; then exit 1; fi
	@grep -c SABOTAGED build/log | head -1 || exit 1
	@while cut -f4 build/log | awk '/RED/ { exit 1 }'; do break; done
	@if sort build/log | sed 1q | grep -E . >/dev/null; then :; fi
	@if cat build/log | sed -n '/x/q'; then :; fi

# The F-067 form: the last stage reads ALL of its input, so the stages before
# it never write into a closed pipe. grep -q on a file (no pipe), and a reader
# that quits early in a pipeline nobody reads as a condition, are not findings.
check: build/log
	@if grep -E '^[a-z-]+ +FAIL' build/log | grep -vE '^retain ' >/dev/null; then echo "another surface went red"; exit 1; fi
	@grep -F 'FAILED: ' build/log | grep 'reads back' >/dev/null && { echo "a boundary case went red"; exit 1; }
	@grep -F 'PASS' build/log | grep -e -q >/dev/null || { echo "TestA did not run"; exit 1; }
	@grep -F 'FAILED: ' build/log | awk '/cook-write/ && !n++ { print } END { exit !n }' || exit 1
	@grep -q 'reads back' build/log || exit 1
	@if test -n "$$(sort build/log | head -1)"; then :; fi
	@if sort build/log | sed -n 1p | grep . >/dev/null; then :; fi

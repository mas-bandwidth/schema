# The F-046 fix: regenerate unless the stamp is newer than every prerequisite,
# touch it only when the generator succeeds, and exit with its status.
build/corpus/.stamp: build/gen-bin bin/schema
	@lockdir="build/corpus.lock"; \
	while ! mkdir "$$lockdir" 2>/dev/null; do sleep 1; done; \
	trap 'rmdir "$$lockdir" 2>/dev/null || true' EXIT INT TERM HUP; \
	status=0; current=1; \
	for p in $^; do test "$@" -nt "$$p" || current=0; done; \
	if test "$$current" -eq 0; then \
		rm -f "$@"; \
		if ./build/gen-bin --corpus build/corpus; then touch "$@"; else status=$$?; fi; \
	fi; \
	rmdir "$$lockdir" 2>/dev/null || true; \
	trap - EXIT INT TERM HUP; \
	exit $$status

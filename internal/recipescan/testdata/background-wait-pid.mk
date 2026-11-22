# The F-068 fix: each job's pid is kept and waited on, and the line fails when
# any of them failed.
build/gen/.stamp: bin/gen
	@set -e; pids=; \
	./bin/gen --out build/gen/a a.schema & pids="$$pids $$!"; \
	./bin/gen --out build/gen/b b.schema & pids="$$pids $$!"; \
	failed=0; for p in $$pids; do wait "$$p" || failed=1; done; \
	if test "$$failed" -ne 0; then echo "FAILED: a generator failed" >&2; exit 1; fi
	@touch $@

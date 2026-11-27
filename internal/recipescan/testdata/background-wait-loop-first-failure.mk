# #4956: each job is waited on by pid, but only -e reads the loop's waits, so the
# first failed generator ends the line and the later ones run on, unwaited,
# after make has returned. Line 7 is red; line 12 reads every status and is green.
build/gen/.stamp: bin/gen
	@set -e; pids=; \
	./bin/gen --out build/gen/a a.schema & pids="$$pids $$!"; \
	./bin/gen --out build/gen/b b.schema & pids="$$pids $$!"; for pid in $$pids; do wait $$pid; done
	@touch $@

build/gen2/.stamp: bin/gen
	@set -e; pids=; ./bin/gen --out build/gen2/a a.schema & pids="$$pids $$!"; \
	rc=0; for pid in $$pids; do wait $$pid || rc=1; done; exit $$rc
	@touch $@

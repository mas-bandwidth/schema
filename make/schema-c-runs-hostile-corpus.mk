# make/schema-c-runs-hostile-corpus.mk — the C port runs the shared
# hostile-input corpus (schema-c-runs-hostile-corpus).
#
# The corpus is the committed portable directory docs/HOSTILE-CORPUS.md names:
# internal/ci/hostilecorpus/corpus/manifest.tsv plus one raw input per line,
# with the Go engine's verdicts. This target generates the units the manifest
# names, builds the C case binary (test/c-hostile) against them and the pinned
# serialize.c runtime under AddressSanitizer and UndefinedBehaviorSanitizer,
# and hands every case to tools/hostilecorpusexec, which runs each one in its
# own process under the per-case budget, so a crash, a sanitizer report or a
# hang fails that case and the run goes on. Reads files only: no network.
#
# A disagreement is never excused by name: every one is a FAIL line naming its
# case. A case the port
# has no API for is a failure like any other: nothing is skipped, so a binding
# dropped from the case binary reads red.
#
# The planted-verdict control copies the corpus to a scratch directory, flips
# one accepted case's verdict to a refusal, and requires the runner to go red
# naming that case, then runs the unplanted target green.

C_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
C_HOSTILE_BUILD    := build/c-hostile
C_HOSTILE_SANITIZE ?= -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g
C_HOSTILE_CFLAGS   := -std=c99 -Wall -Wextra -Werror -O1 -ffp-contract=off $(C_HOSTILE_SANITIZE)
# One sanitizer translation unit per core. Four-wide left the fleet runner's
# other cores idle and the per-commit job died inside this compile.
C_HOSTILE_JOBS     ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 8)
# The runner starts one sanitizer process per case. At the default
# half-the-machine fan-out, a process that has already answered can still leave
# its pipes to the reaper past the exec wait bound under load; two at a time
# keeps each case's own verdict the only thing the run measures.
C_HOSTILE_RUN_JOBS ?= 2
# the harness build's spelling of every unit's table-entry inline hook (below)
C_HOSTILE_INLINE_OVERRIDE ?= inline

C_HOSTILE_UNITS := tabledemo:tables/examples \
	messagedemo:tables/messages blobdemo:tables/blobs \
	graphdemo:tables/pointers scalardemo:tables/scalars \
	streamdemo:tables/stream mapdemo:tables/maps listdemo:tables/lists shapes:test/tables/shapes inner:test/tables/inner probe:$(C_HOSTILE_BUILD)/probe.schema \
	featurewitnesses:testdata/conformance/feature-witnesses/full/Features.schema \
	hostilepacket:internal/ci/hostilecorpus/corpus/packet/hostile.schema \
	tblvaluedefaults:test/tables/ValueDefaults.schema \
	tblmoodmap:test/tables/moodmap

$(C_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-c-runs-hostile-corpus.mk internal/ci/hostilecorpus/corpus.go \
		$(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema \
			tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema \
			tables/maps/*.schema tables/lists/*.schema) test/tables/shapes/Shapes.schema test/tables/inner/Inner.schema testdata/conformance/feature-witnesses/full/Features.schema \
		internal/ci/hostilecorpus/corpus/packet/hostile.schema test/tables/ValueDefaults.schema test/tables/moodmap/MoodMap.schema
	@rm -rf $(C_HOSTILE_BUILD)/generated && mkdir -p $(C_HOSTILE_BUILD)/generated
	@go run ./tools/hostilecorpusexec probe-schema $(C_HOSTILE_BUILD)/probe.schema
	@# one generator per unit, in parallel: each writes its own directory, and
	@# the serial loop was 15 s of a cold build (1 to 2 s a unit)
	@for unit in $(C_HOSTILE_UNITS); do echo "$${unit%%:*} $${unit#*:}"; done | \
	xargs -P $(C_HOSTILE_JOBS) -L 1 sh -c './bin/schema generate --lang c --out $(C_HOSTILE_BUILD)/generated/$$0 $$1 >/dev/null 2>&1 || \
		{ ./bin/schema generate --lang c --out $(C_HOSTILE_BUILD)/generated/$$0 $$1; exit 1; }'
	@touch $@

# Each unit's translation units see only that unit's generated directory, so
# two units' identically named headers never meet. The objects build in
# parallel; the link is the one serial step.
#
# THE INLINE OVERRIDE. A unit's header spells its table entries
# SCHEMA_<UNIT>_TABLE_INLINE, always_inline unless the build defines it (the
# header's own #ifndef). Forced inlining is a speed choice for shipped code;
# under the sanitizers every forced copy of a reader is instrumented again at
# every call site, and that was most of a cold build: the 15 unit_*.c harness
# files were 124 of 224 compile seconds on one core (unit_tblvaluedefaults.c
# alone 21 s, 7 s with plain inline). The harness defines the hook as plain
# inline, so the compiler inlines where it judges and each reader is
# instrumented once; the code, the checks and the sanitizers are the same. A
# unit whose headers no longer carry the hook fails the build by name, so a
# renamed hook cannot quietly turn the define into one that names nothing.
# The unit_*.c files are still the longest compiles (4 to 14 s each against
# about 1.3 s for a generated file), so they are queued first: on N cores the
# long ones start together and the short ones fill in behind them.
$(C_HOSTILE_BUILD)/case: $(C_HOSTILE_BUILD)/generated/.stamp $(wildcard test/c-hostile/*.c test/c-hostile/*.h)
	@rm -rf $(C_HOSTILE_BUILD)/obj && mkdir -p $(C_HOSTILE_BUILD)/obj
	@{ for unit in $(C_HOSTILE_UNITS); do echo "$${unit%%:*} test/c-hostile/unit_$${unit%%:*}.c"; done; \
	for unit in $(C_HOSTILE_UNITS); do \
		name=$${unit%%:*}; \
		for src in $(C_HOSTILE_BUILD)/generated/$$name/*.c; do echo "$$name $$src"; done; \
	done; echo "- test/c-hostile/main.c"; echo "- $(SERIALIZE_C)/serialize.c"; } | \
	xargs -P $(C_HOSTILE_JOBS) -L 1 sh -c 'out=$(C_HOSTILE_BUILD)/obj/$$0-$$(basename $$1 .c).o; inl=; \
		if [ "$$0" != - ]; then \
			hook=SCHEMA_$$(echo $$0 | tr a-z A-Z)_TABLE_INLINE; \
			grep -qs "^#ifndef $$hook\$$" $(C_HOSTILE_BUILD)/generated/$$0/*.h || \
				{ echo "c hostile build: no header of unit $$0 carries #ifndef $$hook, so the inline override would name nothing"; exit 255; }; \
			inl=-D$$hook=$(C_HOSTILE_INLINE_OVERRIDE); \
		fi; \
		$(CC) $(C_HOSTILE_CFLAGS) $$inl -I$(C_HOSTILE_BUILD)/generated/$$0 -Itest/c-hostile -I$(SERIALIZE_C) -c $$1 -o $$out'
	$(CC) $(C_HOSTILE_SANITIZE) $(C_HOSTILE_BUILD)/obj/*.o -o $@ -lpthread -lm

$(C_HOSTILE_BUILD)/hostilecorpusexec: $(wildcard tools/hostilecorpusexec/*.go internal/ci/hostilecorpus/*.go)
	@mkdir -p $(C_HOSTILE_BUILD)
	go build -o $@ ./tools/hostilecorpusexec

# THE BUILD, DECLARED: the prerequisites the corpus target builds first.
# tools/schema-test builds this target before a row's deadline starts (the
# hostile harness's Build step), so what it builds is never a case's timeout;
# what the run recipe itself still compiles is paid inside the run.
.PHONY: schema-c-runs-hostile-corpus-build
schema-c-runs-hostile-corpus-build: $(C_HOSTILE_BUILD)/case $(C_HOSTILE_BUILD)/hostilecorpusexec

.PHONY: schema-c-runs-hostile-corpus
schema-c-runs-hostile-corpus: schema-c-runs-hostile-corpus-build
	$(C_HOSTILE_BUILD)/hostilecorpusexec run -port c -exec $(C_HOSTILE_BUILD)/case \
		-jobs $(C_HOSTILE_RUN_JOBS) \
		-corpus "$(C_HOSTILE_CORPUS)"

test-c: schema-c-runs-hostile-corpus

# The plant: one case the engine accepts is recorded as a refusal it never
# names, in a scratch copy of the corpus, and the runner must name it FAIL.
.PHONY: schema-c-runs-hostile-corpus-negative-control
schema-c-runs-hostile-corpus-negative-control: $(C_HOSTILE_BUILD)/case $(C_HOSTILE_BUILD)/hostilecorpusexec
	@rm -rf $(C_HOSTILE_BUILD)/plant && mkdir -p $(C_HOSTILE_BUILD)/plant
	@cp -R $(C_HOSTILE_CORPUS) $(C_HOSTILE_BUILD)/plant/corpus
	@awk -F'\t' 'BEGIN { OFS = "\t" } $$1 == "wire-valid" { $$7 = "refuse"; $$8 = "planted"; $$9 = "-" } { print }' \
		$(C_HOSTILE_CORPUS)/manifest.tsv > $(C_HOSTILE_BUILD)/plant/corpus/manifest.tsv
	@if cmp -s $(C_HOSTILE_CORPUS)/manifest.tsv $(C_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
		echo 'NEGATIVE CONTROL FAILED: the planted verdict did not apply'; exit 1; fi
	@if $(C_HOSTILE_BUILD)/hostilecorpusexec run -port c -exec $(C_HOSTILE_BUILD)/case \
			-jobs $(C_HOSTILE_RUN_JOBS) \
			-corpus $(C_HOSTILE_BUILD)/plant/corpus > $(C_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a changed verdict left the corpus green'; \
		cat $(C_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(C_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the planted case'; \
		  cat $(C_HOSTILE_BUILD)/plant/log; exit 1; }
	@$(MAKE) --no-print-directory schema-c-runs-hostile-corpus
	@echo 'c hostile-corpus negative control: a changed verdict reds the corpus; unplanted it is green'

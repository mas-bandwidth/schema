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
# A disagreement the port already owes is carried, by name, in
# test/c-hostile/known-findings.tsv, and printed as a FIX-CARD. A disagreement
# no FIX-CARD names is a failure, and a FIX-CARD that has gone green is a
# failure too, so the list cannot rot into silence.
#
# The planted-verdict control copies the corpus to a scratch directory, flips
# one accepted case's verdict to a refusal, and requires the runner to go red
# naming that case, then runs the unplanted target green.

C_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
C_HOSTILE_BUILD    := build/c-hostile
C_HOSTILE_FINDINGS ?= test/c-hostile/known-findings.tsv
C_HOSTILE_SANITIZE ?= -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g
C_HOSTILE_CFLAGS   := -std=c99 -Wall -Wextra -Werror -O1 -ffp-contract=off $(C_HOSTILE_SANITIZE)
C_HOSTILE_JOBS     ?= 4

C_HOSTILE_UNITS := tabledemo:tables/examples \
	messagedemo:tables/messages blobdemo:tables/blobs \
	graphdemo:tables/pointers scalardemo:tables/scalars \
	streamdemo:tables/stream mapdemo:tables/maps listdemo:tables/lists probe:$(C_HOSTILE_BUILD)/probe.schema

$(C_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-c-runs-hostile-corpus.mk internal/ci/hostilecorpus/corpus.go \
		$(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema \
			tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema \
			tables/maps/*.schema tables/lists/*.schema)
	@rm -rf $(C_HOSTILE_BUILD)/generated && mkdir -p $(C_HOSTILE_BUILD)/generated
	@go run ./tools/hostilecorpusexec probe-schema $(C_HOSTILE_BUILD)/probe.schema
	@for unit in $(C_HOSTILE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		./bin/schema generate --lang c --out $(C_HOSTILE_BUILD)/generated/$$name $$path >/dev/null 2>&1 || \
			{ ./bin/schema generate --lang c --out $(C_HOSTILE_BUILD)/generated/$$name $$path; exit 1; }; \
	done
	@touch $@

# Each unit's translation units see only that unit's generated directory, so
# two units' identically named headers never meet. The objects build in
# parallel; the link is the one serial step.
$(C_HOSTILE_BUILD)/case: $(C_HOSTILE_BUILD)/generated/.stamp $(wildcard test/c-hostile/*.c test/c-hostile/*.h)
	@rm -rf $(C_HOSTILE_BUILD)/obj && mkdir -p $(C_HOSTILE_BUILD)/obj
	@{ for unit in $(C_HOSTILE_UNITS); do \
		name=$${unit%%:*}; \
		echo "$$name test/c-hostile/unit_$$name.c"; \
		for src in $(C_HOSTILE_BUILD)/generated/$$name/*.c; do echo "$$name $$src"; done; \
	done; echo "- test/c-hostile/main.c"; echo "- $(SERIALIZE_C)/serialize.c"; } | \
	xargs -P $(C_HOSTILE_JOBS) -L 1 sh -c 'out=$(C_HOSTILE_BUILD)/obj/$$0-$$(basename $$1 .c).o; \
		$(CC) $(C_HOSTILE_CFLAGS) -I$(C_HOSTILE_BUILD)/generated/$$0 -Itest/c-hostile -I$(SERIALIZE_C) -c $$1 -o $$out'
	$(CC) $(C_HOSTILE_SANITIZE) $(C_HOSTILE_BUILD)/obj/*.o -o $@ -lpthread -lm

$(C_HOSTILE_BUILD)/hostilecorpusexec: $(wildcard tools/hostilecorpusexec/*.go internal/ci/hostilecorpus/*.go)
	@mkdir -p $(C_HOSTILE_BUILD)
	go build -o $@ ./tools/hostilecorpusexec

.PHONY: schema-c-runs-hostile-corpus
schema-c-runs-hostile-corpus: $(C_HOSTILE_BUILD)/case $(C_HOSTILE_BUILD)/hostilecorpusexec
	$(C_HOSTILE_BUILD)/hostilecorpusexec run -port c -exec $(C_HOSTILE_BUILD)/case \
		-corpus "$(C_HOSTILE_CORPUS)" -findings "$(C_HOSTILE_FINDINGS)"

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
			-corpus $(C_HOSTILE_BUILD)/plant/corpus -findings "$(C_HOSTILE_FINDINGS)" > $(C_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a changed verdict left the corpus green'; \
		cat $(C_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(C_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the planted case'; \
		  cat $(C_HOSTILE_BUILD)/plant/log; exit 1; }
	@$(MAKE) --no-print-directory schema-c-runs-hostile-corpus
	@echo 'c hostile-corpus negative control: a changed verdict reds the corpus; unplanted it is green'

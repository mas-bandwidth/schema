# make/schema-cpp-runs-hostile-corpus.mk — the C++ port runs the shared
# hostile-input corpus (schema-cpp-runs-hostile-corpus), the twin of
# make/schema-c-runs-hostile-corpus.mk.
#
# The corpus is the committed portable directory docs/HOSTILE-CORPUS.md names:
# internal/ci/hostilecorpus/corpus/manifest.tsv plus one raw input per line,
# with the Go engine's verdicts. This target generates the units the manifest
# names, builds the C++ case binary (test/cpp-hostile's units, linked with the
# case driver test/c-hostile/main.c the C port shares) against them and the
# pinned serialize runtime under AddressSanitizer and UndefinedBehaviorSanitizer,
# and hands every case to tools/hostilecorpusexec, which runs each one in its
# own process under the per-case budget, so a crash, a sanitizer report or a
# hang fails that case and the run goes on. Reads files only: no network.
#
# A disagreement the port already owes is carried, by name, in
# test/cpp-hostile/known-findings.tsv, and printed as a FIX-CARD. A disagreement
# no FIX-CARD names is a failure, and a FIX-CARD that has gone green is a
# failure too, so the list cannot rot into silence.
#
# The planted-verdict control copies the corpus to a scratch directory, flips
# one accepted case's verdict to a refusal, and requires the runner to go red
# naming that case, then runs the unplanted target green.

CPP_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
CPP_HOSTILE_BUILD    := build/cpp-hostile
CPP_HOSTILE_FINDINGS ?= test/cpp-hostile/known-findings.tsv
CPP_HOSTILE_SANITIZE ?= -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g
CPP_HOSTILE_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -O1 -ffp-contract=off -pthread $(CPP_HOSTILE_SANITIZE)
CPP_HOSTILE_CFLAGS   := -std=c99 -Wall -Wextra -Werror -O1 $(CPP_HOSTILE_SANITIZE)
CPP_HOSTILE_JOBS     ?= 4

CPP_HOSTILE_UNITS := tabledemo:tables/examples \
	messagedemo:tables/messages blobdemo:tables/blobs \
	graphdemo:tables/pointers scalardemo:tables/scalars \
	streamdemo:tables/stream mapdemo:tables/maps listdemo:tables/lists probe:$(CPP_HOSTILE_BUILD)/probe.schema

$(CPP_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-cpp-runs-hostile-corpus.mk internal/ci/hostilecorpus/corpus.go \
		$(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema \
			tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema \
			tables/maps/*.schema tables/lists/*.schema)
	@rm -rf $(CPP_HOSTILE_BUILD)/generated && mkdir -p $(CPP_HOSTILE_BUILD)/generated
	@go run ./tools/hostilecorpusexec probe-schema $(CPP_HOSTILE_BUILD)/probe.schema
	@for unit in $(CPP_HOSTILE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		./bin/schema generate --lang cpp --out $(CPP_HOSTILE_BUILD)/generated/$$name $$path >/dev/null 2>&1 || \
			{ ./bin/schema generate --lang cpp --out $(CPP_HOSTILE_BUILD)/generated/$$name $$path; exit 1; }; \
	done
	@touch $@

# Each unit's translation units see only that unit's generated directory, so
# two units' identically named headers never meet; test/tables carries the
# hand-written header tables/pointers' schema names (graph_colour.h). The
# objects build in parallel; the link is the one serial step.
$(CPP_HOSTILE_BUILD)/case: $(CPP_HOSTILE_BUILD)/generated/.stamp $(wildcard test/cpp-hostile/*.cpp test/cpp-hostile/*.hpp) test/c-hostile/main.c test/c-hostile/hostile.h
	@rm -rf $(CPP_HOSTILE_BUILD)/obj && mkdir -p $(CPP_HOSTILE_BUILD)/obj
	@{ for unit in $(CPP_HOSTILE_UNITS); do \
		name=$${unit%%:*}; \
		echo "$$name test/cpp-hostile/unit_$$name.cpp"; \
		for src in $(CPP_HOSTILE_BUILD)/generated/$$name/*.cpp; do echo "$$name $$src"; done; \
	done; } | \
	xargs -P $(CPP_HOSTILE_JOBS) -L 1 sh -c 'out=$(CPP_HOSTILE_BUILD)/obj/$$0-$$(basename $$1 .cpp).o; \
		$(CXX) $(CPP_HOSTILE_CXXFLAGS) -I$(CPP_HOSTILE_BUILD)/generated/$$0 -Itest/cpp-hostile -Itest/c-hostile -Itest/tables -I$(SERIALIZE) -c $$1 -o $$out'
	$(CC) $(CPP_HOSTILE_CFLAGS) -Itest/c-hostile -c test/c-hostile/main.c -o $(CPP_HOSTILE_BUILD)/obj/main.o
	$(CXX) $(CPP_HOSTILE_SANITIZE) -pthread $(CPP_HOSTILE_BUILD)/obj/*.o -o $@

$(CPP_HOSTILE_BUILD)/hostilecorpusexec: $(wildcard tools/hostilecorpusexec/*.go internal/ci/hostilecorpus/*.go)
	@mkdir -p $(CPP_HOSTILE_BUILD)
	go build -o $@ ./tools/hostilecorpusexec

.PHONY: schema-cpp-runs-hostile-corpus
schema-cpp-runs-hostile-corpus: $(CPP_HOSTILE_BUILD)/case $(CPP_HOSTILE_BUILD)/hostilecorpusexec
	$(CPP_HOSTILE_BUILD)/hostilecorpusexec run -port cpp -exec $(CPP_HOSTILE_BUILD)/case \
		-corpus "$(CPP_HOSTILE_CORPUS)" -findings "$(CPP_HOSTILE_FINDINGS)"

# The plant: one case the engine accepts is recorded as a refusal it never
# names, in a scratch copy of the corpus, and the runner must name it FAIL.
.PHONY: schema-cpp-runs-hostile-corpus-negative-control
schema-cpp-runs-hostile-corpus-negative-control: $(CPP_HOSTILE_BUILD)/case $(CPP_HOSTILE_BUILD)/hostilecorpusexec
	@rm -rf $(CPP_HOSTILE_BUILD)/plant && mkdir -p $(CPP_HOSTILE_BUILD)/plant
	@cp -R $(CPP_HOSTILE_CORPUS) $(CPP_HOSTILE_BUILD)/plant/corpus
	@awk -F'\t' 'BEGIN { OFS = "\t" } $$1 == "wire-valid" { $$7 = "refuse"; $$8 = "planted"; $$9 = "-" } { print }' \
		$(CPP_HOSTILE_CORPUS)/manifest.tsv > $(CPP_HOSTILE_BUILD)/plant/corpus/manifest.tsv
	@if cmp -s $(CPP_HOSTILE_CORPUS)/manifest.tsv $(CPP_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
		echo 'NEGATIVE CONTROL FAILED: the planted verdict did not apply'; exit 1; fi
	@if $(CPP_HOSTILE_BUILD)/hostilecorpusexec run -port cpp -exec $(CPP_HOSTILE_BUILD)/case \
			-corpus $(CPP_HOSTILE_BUILD)/plant/corpus -findings "$(CPP_HOSTILE_FINDINGS)" > $(CPP_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a changed verdict left the corpus green'; \
		cat $(CPP_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(CPP_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the planted case'; \
		  cat $(CPP_HOSTILE_BUILD)/plant/log; exit 1; }
	@$(MAKE) --no-print-directory schema-cpp-runs-hostile-corpus
	@echo 'cpp hostile-corpus negative control: a changed verdict reds the corpus; unplanted it is green'

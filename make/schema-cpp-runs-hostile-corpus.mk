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
# A disagreement is never excused by name: every one is a FAIL line naming its
# case. A case the port
# has no API for is a failure like any other: nothing is skipped, so a binding
# dropped from the case binary reads red.
#
# The planted-verdict control copies the corpus to a scratch directory, flips
# one accepted case's verdict to a refusal, and requires the runner to go red
# naming that case, then runs the unplanted target green.

CPP_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
CPP_HOSTILE_BUILD    := build/cpp-hostile
# -g1, not -g: line tables are all a sanitizer report needs to name file and
# line, and full -g made GCC's variable tracking 81% of compiling
# unit_messagedemo.cpp (218 of 270 s, -ftime-report, g++ 16; the whole
# ToolMessage read inlines into one function). That one object outran the
# 2-minute per-commit rule on its own; at -g1 it compiles in about a quarter.
CPP_HOSTILE_SANITIZE ?= -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g1
CPP_HOSTILE_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -O1 -ffp-contract=off -pthread $(CPP_HOSTILE_SANITIZE)
CPP_HOSTILE_CFLAGS   := -std=c99 -Wall -Wextra -Werror -O1 $(CPP_HOSTILE_SANITIZE)
# -O1 is load-bearing, not a speed choice: the case thread has a fixed 512 KiB
# stack (test/c-hostile/main.c), and at -O0 the sanitizers' unshared frames
# overflow it on five accepting cases (measured 2026-11-07), so -O0 would move
# the stack-depth verdicts. The compile time is bought back with -g1, cores and ccache.
# THE INLINE OVERRIDE. A unit's headers spell its table code <UNIT>_TABLE_INLINE,
# always_inline unless the build defines it (the header's own #ifndef). Forced
# inlining is a speed choice for shipped code; under the sanitizers every forced
# copy is instrumented again at every call site, and that was most of a cold
# build: test/cpp-hostile/unit_messagedemo.cpp, the critical path, compiled in
# 53 to 75 s forced and 10.6 s plain (one core, hetzner1). The harness defines
# it as plain inline; the code, the checks and the sanitizers are the same, and
# the corpus's stack-depth verdicts stay the manifest's (422 of 422). Every
# <Table>Table.h of a unit must carry the hook, or the build fails naming the
# header, so a renamed hook cannot quietly turn the define into one that names
# nothing; a unit with no table code (the packet-only hostilepacket) has no
# Table.h and no hook, and gets no define.
CPP_HOSTILE_INLINE_OVERRIDE ?= inline
# One compile per core: the objects are independent, only the link is serial.
CPP_HOSTILE_JOBS     ?= $(shell getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)
# ccache when the host has it: an unchanged generated unit is not recompiled.
# It keys on the preprocessed source and the flags, so a changed unit, header
# or flag always compiles fresh.
CPP_HOSTILE_CCACHE   ?= $(shell command -v ccache 2>/dev/null)
# The cache must not key on the checkout's directory. With -g1 ccache hashes
# the working directory into every key (hash_dir), and each fleet runner checks
# out under its own path (actions-runner/<repo>-<n>/_work/...), so a unit
# compiled warm by one runner missed on every other runner of the same host,
# and the gate went red at 2 minutes on commits that changed no C++ (main at
# de14e15, 2026-11-14). The prefix map writes the directory out of the
# objects' debug info, so an object really is the same from any checkout and
# NOHASHDIR is sound; BASEDIR makes any absolute path under it relative.
CPP_HOSTILE_DIRFREE  := -ffile-prefix-map=$(CURDIR)=.
CPP_HOSTILE_CCACHE_ENV := CCACHE_NOHASHDIR=1 CCACHE_BASEDIR=$(CURDIR)

CPP_HOSTILE_UNITS := tabledemo:tables/examples \
	messagedemo:tables/messages blobdemo:tables/blobs \
	graphdemo:tables/pointers scalardemo:tables/scalars \
	streamdemo:tables/stream mapdemo:tables/maps listdemo:tables/lists shapes:test/tables/shapes inner:test/tables/inner probe:$(CPP_HOSTILE_BUILD)/probe.schema \
	featurewitnesses:testdata/conformance/feature-witnesses/full/Features.schema \
	hostilepacket:internal/ci/hostilecorpus/corpus/packet/hostile.schema \
	tblvaluedefaults:test/tables/ValueDefaults.schema \
	tblmoodmap:test/tables/moodmap

$(CPP_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-cpp-runs-hostile-corpus.mk internal/ci/hostilecorpus/corpus.go \
		$(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema \
			tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema \
			tables/maps/*.schema tables/lists/*.schema) test/tables/shapes/Shapes.schema test/tables/inner/Inner.schema testdata/conformance/feature-witnesses/full/Features.schema \
		internal/ci/hostilecorpus/corpus/packet/hostile.schema test/tables/ValueDefaults.schema test/tables/moodmap/MoodMap.schema
	@rm -rf $(CPP_HOSTILE_BUILD)/generated && mkdir -p $(CPP_HOSTILE_BUILD)/generated
	@go run ./tools/hostilecorpusexec probe-schema $(CPP_HOSTILE_BUILD)/probe.schema
	@# one generator per unit, in parallel: each writes its own directory
	@for unit in $(CPP_HOSTILE_UNITS); do echo "$${unit%%:*} $${unit#*:}"; done | \
	xargs -P $(CPP_HOSTILE_JOBS) -L 1 sh -c './bin/schema generate --lang cpp --out $(CPP_HOSTILE_BUILD)/generated/$$0 $$1 >/dev/null 2>&1 || \
		{ ./bin/schema generate --lang cpp --out $(CPP_HOSTILE_BUILD)/generated/$$0 $$1; exit 1; }'
	@touch $@

# Each unit's translation units see only that unit's generated directory, so
# two units' identically named headers never meet. The runner's own headers
# come first: the hostile corpus's packet unit generates a hostile.h of its
# own, which hostile.hpp must not find in place of test/c-hostile's; test/tables carries the
# hand-written header tables/pointers' schema names (graph_colour.h). The
# objects build in parallel; the link is the one serial step.
$(CPP_HOSTILE_BUILD)/case: $(CPP_HOSTILE_BUILD)/generated/.stamp $(wildcard test/cpp-hostile/*.cpp test/cpp-hostile/*.hpp) test/c-hostile/main.c test/c-hostile/hostile.h
	@rm -rf $(CPP_HOSTILE_BUILD)/obj && mkdir -p $(CPP_HOSTILE_BUILD)/obj
	@echo "cpp-hostile: compiling with $(CPP_HOSTILE_JOBS) jobs$(if $(CPP_HOSTILE_CCACHE), through ccache)"
	@# the unit_*.cpp files are the longest compiles (up to 75 s against about
	@# 2 s for a generated file), so they are queued first and start together
	@{ for unit in $(CPP_HOSTILE_UNITS); do echo "$${unit%%:*} test/cpp-hostile/unit_$${unit%%:*}.cpp"; done; \
	for unit in $(CPP_HOSTILE_UNITS); do \
		name=$${unit%%:*}; \
		for src in $(CPP_HOSTILE_BUILD)/generated/$$name/*.cpp; do if [ -f "$$src" ]; then echo "$$name $$src"; fi; done; \
	done; } | \
	xargs -P $(CPP_HOSTILE_JOBS) -L 1 sh -c 'out=$(CPP_HOSTILE_BUILD)/obj/$$0-$$(basename $$1 .cpp).o; \
		hook=$$(echo $$0 | tr a-z A-Z)_TABLE_INLINE; inl=; \
		for h in $(CPP_HOSTILE_BUILD)/generated/$$0/*Table.h; do \
			[ -e "$$h" ] || continue; \
			grep -qs "^#ifndef $$hook\$$" "$$h" || \
				{ echo "cpp hostile build: $$h does not carry #ifndef $$hook, so the inline override would name nothing"; exit 255; }; \
			inl=-D$$hook=$(CPP_HOSTILE_INLINE_OVERRIDE); \
		done; \
		$(if $(CPP_HOSTILE_CCACHE),$(CPP_HOSTILE_CCACHE_ENV)) $(CPP_HOSTILE_CCACHE) $(CXX) $(CPP_HOSTILE_CXXFLAGS) $$inl $(CPP_HOSTILE_DIRFREE) -Itest/cpp-hostile -Itest/c-hostile -I$(CPP_HOSTILE_BUILD)/generated/$$0 -Itest/tables -I$(SERIALIZE) -c $$1 -o $$out'
	$(if $(CPP_HOSTILE_CCACHE),$(CPP_HOSTILE_CCACHE_ENV)) $(CPP_HOSTILE_CCACHE) $(CC) $(CPP_HOSTILE_CFLAGS) $(CPP_HOSTILE_DIRFREE) -Itest/c-hostile -c test/c-hostile/main.c -o $(CPP_HOSTILE_BUILD)/obj/main.o
	$(CXX) $(CPP_HOSTILE_SANITIZE) -pthread $(CPP_HOSTILE_BUILD)/obj/*.o -o $@

$(CPP_HOSTILE_BUILD)/hostilecorpusexec: $(wildcard tools/hostilecorpusexec/*.go internal/ci/hostilecorpus/*.go)
	@mkdir -p $(CPP_HOSTILE_BUILD)
	go build -o $@ ./tools/hostilecorpusexec

.PHONY: schema-cpp-runs-hostile-corpus
# THE BUILD, DECLARED: the prerequisites the corpus target builds first.
# tools/schema-test builds this target before a row's deadline starts (the
# hostile harness's Build step), so what it builds is never a case's timeout;
# what the run recipe itself still compiles is paid inside the run.
.PHONY: schema-cpp-runs-hostile-corpus-build
schema-cpp-runs-hostile-corpus-build: $(CPP_HOSTILE_BUILD)/case $(CPP_HOSTILE_BUILD)/hostilecorpusexec

schema-cpp-runs-hostile-corpus: schema-cpp-runs-hostile-corpus-build
	$(CPP_HOSTILE_BUILD)/hostilecorpusexec run -port cpp -exec $(CPP_HOSTILE_BUILD)/case \
		-corpus "$(CPP_HOSTILE_CORPUS)"

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
			-corpus $(CPP_HOSTILE_BUILD)/plant/corpus > $(CPP_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a changed verdict left the corpus green'; \
		cat $(CPP_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(CPP_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the planted case'; \
		  cat $(CPP_HOSTILE_BUILD)/plant/log; exit 1; }
	@$(MAKE) --no-print-directory schema-cpp-runs-hostile-corpus
	@echo 'cpp hostile-corpus negative control: a changed verdict reds the corpus; unplanted it is green'

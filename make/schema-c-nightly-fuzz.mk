# make/schema-c-nightly-fuzz.mk — nightly fuzzing for the C table readers.
#
# One fuzz entry point per reader kind, per schema in the corpus list: load,
# strict read (the measured extent path) and text/JSON parse. The harness is
# generated from the emitted header's own declarations, so a new table shape
# needs no edit here. libFuzzer with ASan and UBSan is the nightly driver;
# AFL++ drives the same generated harnesses as an additional nightly run.
#
# The toolchain is probed by name and pinned: clang 18 (LLVM's official
# release) for libFuzzer, AFL++ 4.x (its official source) for the extra run.
# A missing tool is reported as `tool missing: <name>`, never faked. The replay
# path needs no fuzzer runtime: it builds the same drivers as plain mains with
# ASan and UBSan, which is what every push runs over the saved inputs.
#
#   make schema-c-nightly-fuzz                    # 10 minutes, all binaries
#   make schema-c-nightly-fuzz SCHEMA_C_FUZZ_SECONDS=5
#   make schema-c-nightly-fuzz-replay             # fast: saved inputs only
#   make schema-c-nightly-fuzz-negative-control   # planted reader bug must red
#
# This file is included by the Makefile's wildcard; it registers itself through
# the targets below and needs no Makefile edit.

GO ?= go

SCHEMA_C_FUZZ_SECONDS   ?= 600
SCHEMA_C_FUZZ_CORPUS    ?= make/schema-c-nightly-fuzz.corpus
SCHEMA_C_FUZZ_OUT       ?= build/schema-c-nightly-fuzz
SCHEMA_C_FUZZ_SAVED     ?= testdata/fuzz/c-nightly
SCHEMA_C_FUZZ_ARTIFACTS ?= $(SCHEMA_C_FUZZ_OUT)/artifacts

# The pinned series by name: a bench whose plain `clang` is another release
# (amd-3 carries clang 21 as `clang`) still builds with clang 18.
CLANG          ?= clang-$(CLANG_SERIES)
CLANG_SERIES   ?= 18
AFL_FUZZ       ?= afl-fuzz
AFL_CC         ?= afl-clang-fast
# AFL++ 4.x is the pinned series; afl-fuzz -V prints its release line.
AFL_VERSION    ?= 4.

SCHEMA_C_FUZZ_KINDS := load strict json

# Some units' headers include serialize.h for its types, so every compile of a
# generated unit names the serialize.c sibling checkout make/c.mk pins.
SERIALIZE_C ?= ../serialize.c

SCHEMA_C_FUZZ_SANITIZE := -fsanitize=fuzzer,address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer
SCHEMA_C_FUZZ_REPLAY_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer
SCHEMA_C_FUZZ_CFLAGS   := -std=c99 -O1 -g -ffp-contract=off
# Replay is compiled at -O0: it runs a handful of saved inputs and buys nothing
# from the optimiser, and the lower level is markedly faster to build.
SCHEMA_C_FUZZ_REPLAY_CFLAGS := -std=c99 -O0 -g -ffp-contract=off

# THE LIBFUZZER RUNTIME IS ITS OWN PROBE. `clang` can be installed and still
# carry no libFuzzer runtime (Apple's Command Line Tools is exactly that), so
# the series check alone would pass and the nightly link would fail late. This
# compiles and links a one-line fuzzer; a host without the runtime reports the
# missing tool by name before anything is built.
.PHONY: schema-c-nightly-fuzz-toolchain
schema-c-nightly-fuzz-toolchain:
	@command -v $(CLANG) >/dev/null 2>&1 || { echo 'tool missing: $(CLANG) (pinned clang $(CLANG_SERIES))'; exit 1; }
	@$(CLANG) --version | grep -q 'clang version $(CLANG_SERIES)' || { echo 'tool missing: clang $(CLANG_SERIES) (found: '"$$($(CLANG) --version | head -1)"')'; exit 1; }
	@mkdir -p $(dir $(SCHEMA_C_FUZZ_OUT))
	@printf 'extern int LLVMFuzzerTestOneInput(const unsigned char*,unsigned long);\nint LLVMFuzzerTestOneInput(const unsigned char*d,unsigned long n){(void)d;(void)n;return 0;}\n' > $(SCHEMA_C_FUZZ_OUT)-probe.c
	@$(CLANG) -fsanitize=fuzzer,address,undefined $(SCHEMA_C_FUZZ_OUT)-probe.c -o $(SCHEMA_C_FUZZ_OUT)-probe >/dev/null 2>&1 || { rm -f $(SCHEMA_C_FUZZ_OUT)-probe.c $(SCHEMA_C_FUZZ_OUT)-probe; echo 'tool missing: libFuzzer (clang $(CLANG_SERIES) runtime, -fsanitize=fuzzer)'; exit 1; }
	@rm -f $(SCHEMA_C_FUZZ_OUT)-probe.c $(SCHEMA_C_FUZZ_OUT)-probe
	@command -v $(AFL_FUZZ) >/dev/null 2>&1 || echo 'tool missing: $(AFL_FUZZ) (pinned AFL++ $(AFL_VERSION))'
	@command -v $(AFL_CC) >/dev/null 2>&1 || echo 'tool missing: $(AFL_CC) (pinned AFL++ $(AFL_VERSION))'

$(SCHEMA_C_FUZZ_OUT)/.stamp: bin/schema make/schema-c-nightly-fuzz-gen/main.go $(SCHEMA_C_FUZZ_CORPUS)
	@rm -rf $(SCHEMA_C_FUZZ_OUT)
	@mkdir -p $(SCHEMA_C_FUZZ_OUT)
	$(GO) run ./make/schema-c-nightly-fuzz-gen -schema-bin ./bin/schema -out $(SCHEMA_C_FUZZ_OUT) -corpus $(SCHEMA_C_FUZZ_CORPUS)
	@touch $@

# The libFuzzer binaries the nightly run drives. The corpus emits about 195
# drivers over 36 units, and linking each one against its unit's sources
# compiled afresh took about 30 s a binary on hulk (2026-10-18), past an hour
# serial. So each unit's table sources compile once to objects, the drivers
# compile to objects beside them, and a link per driver joins them; both passes
# run as a parallel sub-make, SCHEMA_C_FUZZ_JOBS wide. The same two passes
# build the AFL++ binaries with afl-clang-fast and their own suffix.
SCHEMA_C_FUZZ_JOBS ?= 8
SCHEMA_C_FUZZ_FLAVOR ?= fuzz

$(SCHEMA_C_FUZZ_OUT)/.bins: $(SCHEMA_C_FUZZ_OUT)/.stamp schema-c-nightly-fuzz-toolchain
	@$(MAKE) --no-print-directory schema-c-nightly-fuzz-link SCHEMA_C_FUZZ_FLAVOR=fuzz \
		SCHEMA_C_FUZZ_CC='$(CLANG)' SCHEMA_C_FUZZ_OBJFLAGS='$(SCHEMA_C_FUZZ_OBJ_SANITIZE)' SCHEMA_C_FUZZ_LINKFLAGS='$(SCHEMA_C_FUZZ_SANITIZE)'
	@touch $@

SCHEMA_C_FUZZ_OBJ_SANITIZE := -fsanitize=fuzzer-no-link,address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer

# The two passes. FLAVOR names the suffix (fuzz or afl); CC, OBJFLAGS and
# LINKFLAGS are the compiler and its flags for that flavor.
.PHONY: schema-c-nightly-fuzz-link
schema-c-nightly-fuzz-link:
	@objs=$$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 \( -name '*Table.c' -o -name 'fuzz_*.c' \) | sort | sed 's/\.c$$/.$(SCHEMA_C_FUZZ_FLAVOR).o/'); \
	bins=$$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name 'fuzz_*.c' | sort | sed 's/\.c$$/.$(SCHEMA_C_FUZZ_FLAVOR)/'); \
	[ -n "$$bins" ] || { echo 'no fuzz drivers generated'; exit 1; }; \
	$(MAKE) --no-print-directory -j$(SCHEMA_C_FUZZ_JOBS) $$objs && \
	$(MAKE) --no-print-directory -j$(SCHEMA_C_FUZZ_JOBS) $$bins

$(SCHEMA_C_FUZZ_OUT)/%.$(SCHEMA_C_FUZZ_FLAVOR).o: $(SCHEMA_C_FUZZ_OUT)/%.c
	@$(SCHEMA_C_FUZZ_CC) $(SCHEMA_C_FUZZ_CFLAGS) $(SCHEMA_C_FUZZ_OBJFLAGS) -I$(dir $<) -I$(SERIALIZE_C) -c $< -o $@

$(SCHEMA_C_FUZZ_OUT)/%.$(SCHEMA_C_FUZZ_FLAVOR): $(SCHEMA_C_FUZZ_OUT)/%.$(SCHEMA_C_FUZZ_FLAVOR).o
	@$(SCHEMA_C_FUZZ_CC) $(SCHEMA_C_FUZZ_LINKFLAGS) $< $(dir $@)*Table.$(SCHEMA_C_FUZZ_FLAVOR).o -o $@ -lm

# The same drivers as plain mains, sanitized but with no fuzzer runtime. This
# is the fast replay path and it works on a host without libFuzzer.
$(SCHEMA_C_FUZZ_OUT)/.replay-bins: $(SCHEMA_C_FUZZ_OUT)/.stamp
	@set -e; for src in $$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name 'fuzz_*.c' | sort); do \
		dir=$$(dirname $$src); stem=$$(basename $$src .c); stem=$${stem#fuzz_}; \
		kind=$${stem##*_}; base=$${stem%_*}; \
		$(CLANG) $(SCHEMA_C_FUZZ_REPLAY_CFLAGS) $(SCHEMA_C_FUZZ_REPLAY_SANITIZE) -DSCHEMA_C_FUZZ_STANDALONE -I$$dir -I$(SERIALIZE_C) \
			$$src $$dir/*Table.c -o $$dir/$${base}_$${kind}.replay -lm; \
	done
	@touch $@

.PHONY: schema-c-nightly-fuzz-seeds
schema-c-nightly-fuzz-seeds:
	@rm -rf $(SCHEMA_C_FUZZ_OUT)/corpus/load $(SCHEMA_C_FUZZ_OUT)/corpus/strict $(SCHEMA_C_FUZZ_OUT)/corpus/json
	@mkdir -p $(SCHEMA_C_FUZZ_OUT)/corpus/load $(SCHEMA_C_FUZZ_OUT)/corpus/strict $(SCHEMA_C_FUZZ_OUT)/corpus/json
	@$(GO) run ./make/schema-c-nightly-fuzz-gen -seed-out $(SCHEMA_C_FUZZ_OUT)/corpus
	@cp testdata/wire/tables/*.bin $(SCHEMA_C_FUZZ_OUT)/corpus/load/ 2>/dev/null || true
	@cp testdata/wire/tables/*.bin $(SCHEMA_C_FUZZ_OUT)/corpus/strict/ 2>/dev/null || true
	@cp testdata/conformance/tables/json/*.json $(SCHEMA_C_FUZZ_OUT)/corpus/json/ 2>/dev/null || true
	@mkdir -p $(SCHEMA_C_FUZZ_ARTIFACTS) $(SCHEMA_C_FUZZ_SAVED)

.PHONY: schema-c-nightly-fuzz
schema-c-nightly-fuzz: $(SCHEMA_C_FUZZ_OUT)/.bins schema-c-nightly-fuzz-seeds
	@bins=$$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name 'fuzz_*.fuzz' | sort); \
	count=$$(echo $$bins | wc -w); \
	[ $$count -gt 0 ] || { echo 'no fuzz binaries built'; exit 1; }; \
	per=$$(( $(SCHEMA_C_FUZZ_SECONDS) / count )); [ $$per -lt 1 ] && per=1; \
	for bin in $$bins; do \
		kind=$$(basename $$bin .fuzz); kind=$${kind##*_}; \
		echo "schema-c-nightly-fuzz: $$bin $${per}s"; \
		./$$bin -max_total_time=$$per -artifact_prefix=$(SCHEMA_C_FUZZ_ARTIFACTS)/ \
			$(SCHEMA_C_FUZZ_OUT)/corpus/$$kind $(SCHEMA_C_FUZZ_SAVED) || exit 1; \
	done

# FAST REPLAY. Every saved input runs on every reader kind of the unit it was
# saved from, with no clock and no fuzzer: the standalone main takes the files
# as arguments. Saved inputs live under $(SAVED)/<unit>/, so the fast path
# compiles only the units that have a saved input rather than the whole corpus.
# A nightly crash is minimised by libFuzzer's own -minimize_crash and saved
# there, so this is the regression half of the nightly run on every push.
.PHONY: schema-c-nightly-fuzz-replay
# The planted control's red line under $(SAVED)/planted/ is not a unit: it is
# replayed against the planted reader only, so the shipped replay skips it.
# The corpus is generated only when a saved unit exists, so a push with no
# saved inputs costs no compiler build.
schema-c-nightly-fuzz-replay:
	@units=$$(find $(SCHEMA_C_FUZZ_SAVED) -mindepth 1 -maxdepth 1 -type d ! -name planted 2>/dev/null | sort); \
	if [ -z "$$units" ]; then echo 'schema-c-nightly-fuzz-replay: no saved inputs'; exit 0; fi; \
	$(MAKE) --no-print-directory $(SCHEMA_C_FUZZ_OUT)/.stamp || exit 1; \
	failed=0; count=0; \
	for unitdir in $$units; do \
		unit=$$(basename $$unitdir); \
		files=$$(find $$unitdir -type f | sort); \
		[ -n "$$files" ] || continue; \
		srcdir=$(SCHEMA_C_FUZZ_OUT)/$$unit; \
		[ -d "$$srcdir" ] || { echo "REPLAY FAILED: saved unit $$unit has no generated reader"; failed=1; continue; }; \
		for src in $$(find $$srcdir -maxdepth 1 -name 'fuzz_*.c' | sort); do \
			stem=$$(basename $$src .c); stem=$${stem#fuzz_}; kind=$${stem##*_}; base=$${stem%_*}; \
			bin=$$srcdir/$${base}_$${kind}.replay; \
			$(CLANG) $(SCHEMA_C_FUZZ_REPLAY_CFLAGS) $(SCHEMA_C_FUZZ_REPLAY_SANITIZE) -DSCHEMA_C_FUZZ_STANDALONE \
				-I$$srcdir -I$(SERIALIZE_C) $$src $$srcdir/*Table.c -o $$bin -lm || { failed=1; continue; }; \
			"$$bin" $$files >/dev/null || { echo "REPLAY FAILED: $$bin"; failed=1; }; \
			count=$$((count+1)); \
		done; \
	done; \
	[ $$failed -eq 0 ] || exit 1; \
	echo "schema-c-nightly-fuzz-replay: saved inputs replayed on $$count reader-kind binary/binaries"

# THE NEGATIVE CONTROL. A scratch copy of one generated reader loses its bounds
# checks (table_reader_has and table_reader_room return true unconditionally;
# removing has alone stayed green for 300 s and 4.6M runs on hulk, 2026-10-18,
# because the reader's length paths are guarded by room); libFuzzer must find a
# sanitizer finding inside the nightly budget and the saved artifact must fail
# the same replay. The red line is kept beside the normal corpus, never
# replayed against the unplanted reader. A host without libFuzzer reports the
# tool by name and does not pretend the control ran.
SCHEMA_C_FUZZ_PLANTED := $(SCHEMA_C_FUZZ_OUT)-negative
SCHEMA_C_FUZZ_PLANT_UNIT := tables_examples
SCHEMA_C_FUZZ_PLANT_GUARD := return n >= 0 && r->offset >= 0 && r->offset <= r->size && n <= r->size - r->offset;
SCHEMA_C_FUZZ_PLANT_ROOM := return r->offset >= 0 && r->offset <= r->size && n <= (uint64_t) (r->size - r->offset);

.PHONY: schema-c-nightly-fuzz-negative-control
schema-c-nightly-fuzz-negative-control: schema-c-nightly-fuzz-toolchain $(SCHEMA_C_FUZZ_OUT)/.stamp
	@rm -rf $(SCHEMA_C_FUZZ_PLANTED) && mkdir -p $(SCHEMA_C_FUZZ_PLANTED)
	@cp -r $(SCHEMA_C_FUZZ_OUT)/$(SCHEMA_C_FUZZ_PLANT_UNIT) $(SCHEMA_C_FUZZ_PLANTED)/unit
	@sed -e 's|$(SCHEMA_C_FUZZ_PLANT_GUARD)|(void) r; (void) n; return 1;|' \
		-e 's|$(SCHEMA_C_FUZZ_PLANT_ROOM)|(void) r; (void) n; return 1;|' \
		$(SCHEMA_C_FUZZ_PLANTED)/unit/GuardedTable.h > $(SCHEMA_C_FUZZ_PLANTED)/planted.h
	@mv $(SCHEMA_C_FUZZ_PLANTED)/planted.h $(SCHEMA_C_FUZZ_PLANTED)/unit/GuardedTable.h
	@[ "$$(grep -Fc '(void) r; (void) n; return 1;' $(SCHEMA_C_FUZZ_PLANTED)/unit/GuardedTable.h)" = 2 ] || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant did not land in both reader guards'; exit 1; }
	@$(CLANG) $(SCHEMA_C_FUZZ_CFLAGS) $(SCHEMA_C_FUZZ_SANITIZE) -I$(SCHEMA_C_FUZZ_PLANTED)/unit -I$(SERIALIZE_C) \
		$(SCHEMA_C_FUZZ_PLANTED)/unit/fuzz_Guarded_load.c $(SCHEMA_C_FUZZ_PLANTED)/unit/*Table.c \
		-o $(SCHEMA_C_FUZZ_PLANTED)/load.bin -lm
	@mkdir -p $(SCHEMA_C_FUZZ_PLANTED)/artifacts $(SCHEMA_C_FUZZ_PLANTED)/corpus $(SCHEMA_C_FUZZ_SAVED)/planted
	@cp testdata/wire/tables/*.bin $(SCHEMA_C_FUZZ_PLANTED)/corpus/ 2>/dev/null || true
	@set +e; $(SCHEMA_C_FUZZ_PLANTED)/load.bin -max_total_time=$(SCHEMA_C_FUZZ_SECONDS) \
		-artifact_prefix=$(SCHEMA_C_FUZZ_PLANTED)/artifacts/ $(SCHEMA_C_FUZZ_PLANTED)/corpus \
		>$(SCHEMA_C_FUZZ_PLANTED)/log 2>&1; status=$$?; set -e; \
	[ $$status -ne 0 ] || { echo 'NEGATIVE CONTROL FAILED: the bounds check is gone and the fuzzer stayed green'; cat $(SCHEMA_C_FUZZ_PLANTED)/log; exit 1; }
	@grep -qE 'heap-buffer-overflow|AddressSanitizer|runtime error' $(SCHEMA_C_FUZZ_PLANTED)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the fuzzer went red for some other reason'; cat $(SCHEMA_C_FUZZ_PLANTED)/log; exit 1; }
	@artifact=$$(find $(SCHEMA_C_FUZZ_PLANTED)/artifacts -type f | head -1); \
		[ -n "$$artifact" ] || { echo 'NEGATIVE CONTROL FAILED: no crash artifact saved'; exit 1; }; \
		cp "$$artifact" $(SCHEMA_C_FUZZ_SAVED)/planted/planted-overread; \
		$(CLANG) $(SCHEMA_C_FUZZ_CFLAGS) $(SCHEMA_C_FUZZ_REPLAY_SANITIZE) -DSCHEMA_C_FUZZ_STANDALONE \
			-I$(SCHEMA_C_FUZZ_PLANTED)/unit -I$(SERIALIZE_C) $(SCHEMA_C_FUZZ_PLANTED)/unit/fuzz_Guarded_load.c \
			$(SCHEMA_C_FUZZ_PLANTED)/unit/*Table.c -o $(SCHEMA_C_FUZZ_PLANTED)/replay -lm; \
		set +e; $(SCHEMA_C_FUZZ_PLANTED)/replay $(SCHEMA_C_FUZZ_SAVED)/planted/planted-overread >/dev/null 2>&1; replay=$$?; set -e; \
		[ $$replay -ne 0 ] || { echo 'NEGATIVE CONTROL FAILED: the saved input does not fail the planted replay'; exit 1; }
	@echo 'schema C nightly fuzz negative control: the planted reader bug crashes the fuzzer and its saved input fails the planted replay'

# AFL++ is the additional nightly run: the same generated harnesses, compiled
# through afl-clang-fast and driven by afl-fuzz. It is not part of the fast
# pull-request path, and a host without AFL++ reports the missing tool by name.
.PHONY: schema-c-nightly-fuzz-afl
schema-c-nightly-fuzz-afl: $(SCHEMA_C_FUZZ_OUT)/.stamp schema-c-nightly-fuzz-seeds
	@command -v $(AFL_CC) >/dev/null 2>&1 || { echo 'tool missing: $(AFL_CC) (pinned AFL++ $(AFL_VERSION))'; exit 1; }
	@command -v $(AFL_FUZZ) >/dev/null 2>&1 || { echo 'tool missing: $(AFL_FUZZ) (pinned AFL++ $(AFL_VERSION))'; exit 1; }
	@$(AFL_FUZZ) -h 2>&1 | grep -q 'afl-fuzz++$(AFL_VERSION)' || { echo 'tool missing: AFL++ $(AFL_VERSION) (found: '"$$($(AFL_FUZZ) -h 2>&1 | head -1)"')'; exit 1; }
	@AFL_USE_ASAN=1 AFL_USE_UBSAN=1 $(MAKE) --no-print-directory schema-c-nightly-fuzz-link SCHEMA_C_FUZZ_FLAVOR=afl \
		SCHEMA_C_FUZZ_CC='$(AFL_CC)' SCHEMA_C_FUZZ_OBJFLAGS= SCHEMA_C_FUZZ_LINKFLAGS=-fsanitize=fuzzer
	@bins=$$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name 'fuzz_*.afl' | sort); \
	count=$$(echo $$bins | wc -w); per=$$(( $(SCHEMA_C_FUZZ_SECONDS) / count )); [ $$per -lt 1 ] && per=1; \
	failed=0; \
	for bin in $$bins; do \
		name=$$(basename $$bin .afl); kind=$${name##*_}; out=$(SCHEMA_C_FUZZ_OUT)/afl/$$(basename $$(dirname $$bin))_$$name; \
		mkdir -p $$out; \
		AFL_NO_UI=1 AFL_SKIP_CPUFREQ=1 AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES=1 \
			$(AFL_FUZZ) -i $(SCHEMA_C_FUZZ_OUT)/corpus/$$kind -o $$out -V $$per -- $$bin @@ >$$out.log 2>&1 \
			|| { echo "AFL++ FAILED TO RUN: $$bin (see $$out.log)"; failed=1; continue; }; \
		if [ -n "$$(find $$out -path '*/crashes/id:*' -o -path '*/hangs/id:*' 2>/dev/null | head -1)" ]; then \
			echo "AFL++ FINDING: $$bin (see $$out)"; failed=1; fi; \
	done; \
	[ $$failed -eq 0 ] || exit 1
	@echo 'schema C nightly fuzz: AFL++ run complete, no crash and no hang'

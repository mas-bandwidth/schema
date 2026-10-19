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
# A missing tool is reported as `tool missing: <name>`, never faked.
#
#   make schema-c-nightly-fuzz                    # 10 minutes per kind, long form
#   make schema-c-nightly-fuzz SCHEMA_C_FUZZ_SECONDS=5
#   make schema-c-nightly-fuzz-replay             # fast: saved inputs only
#   make schema-c-nightly-fuzz-negative-control   # planted reader bug must red
#
# This file is included by the Makefile's wildcard; it registers itself through
# the targets below and needs no Makefile edit.

SCHEMA_C_FUZZ_SECONDS   ?= 600
SCHEMA_C_FUZZ_CORPUS    ?= make/schema-c-nightly-fuzz.corpus
SCHEMA_C_FUZZ_OUT       ?= build/schema-c-nightly-fuzz
SCHEMA_C_FUZZ_SAVED     ?= testdata/fuzz/c-nightly
SCHEMA_C_FUZZ_ARTIFACTS ?= $(SCHEMA_C_FUZZ_OUT)/artifacts

CLANG          ?= clang
CLANG_VERSION  ?= 18
AFL_FUZZ       ?= afl-fuzz
AFL_CC         ?= afl-clang-fast
# AFL++ 4.x is the pinned series; afl-fuzz -V prints its release line.
AFL_VERSION    ?= 4.

SCHEMA_C_FUZZ_KINDS := load strict json

SCHEMA_C_FUZZ_SANITIZE := -fsanitize=fuzzer,address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer
SCHEMA_C_FUZZ_CFLAGS   := -std=c99 -O1 -g -ffp-contract=off

.PHONY: schema-c-nightly-fuzz-toolchain
schema-c-nightly-fuzz-toolchain:
	@command -v $(CLANG) >/dev/null 2>&1 || { echo 'tool missing: $(CLANG) (pinned clang $(CLANG_VERSION))'; exit 1; }
	@$(CLANG) --version | grep -q 'clang version $(CLANG_VERSION)' || { echo 'tool missing: clang $(CLANG_VERSION) (found: '"$$($(CLANG) --version | head -1)"')'; exit 1; }
	@command -v $(AFL_FUZZ) >/dev/null 2>&1 || echo 'tool missing: $(AFL_FUZZ) (pinned AFL++ $(AFL_VERSION))'
	@command -v $(AFL_CC) >/dev/null 2>&1 || echo 'tool missing: $(AFL_CC) (pinned AFL++ $(AFL_VERSION))'

$(SCHEMA_C_FUZZ_OUT)/.stamp: bin/schema make/schema-c-nightly-fuzz-gen/main.go $(SCHEMA_C_FUZZ_CORPUS)
	@rm -rf $(SCHEMA_C_FUZZ_OUT)
	@mkdir -p $(SCHEMA_C_FUZZ_OUT)
	go run ./make/schema-c-nightly-fuzz-gen -schema-bin ./bin/schema -out $(SCHEMA_C_FUZZ_OUT) -corpus $(SCHEMA_C_FUZZ_CORPUS)
	@touch $@

$(SCHEMA_C_FUZZ_OUT)/.bins: $(SCHEMA_C_FUZZ_OUT)/.stamp
	@set -e; for src in $$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name 'fuzz_*.c' | sort); do \
		dir=$$(dirname $$src); stem=$$(basename $$src .c); stem=$${stem#fuzz_}; \
		kind=$${stem##*_}; base=$${stem%_*}; \
		$(CLANG) $(SCHEMA_C_FUZZ_CFLAGS) $(SCHEMA_C_FUZZ_SANITIZE) -I$$dir \
			$$src $$dir/*Table.c -o $$dir/$${base}_$${kind}.bin -lm; \
	done
	@touch $@

.PHONY: schema-c-nightly-fuzz-seeds
schema-c-nightly-fuzz-seeds:
	@mkdir -p $(SCHEMA_C_FUZZ_OUT)/corpus/load $(SCHEMA_C_FUZZ_OUT)/corpus/strict $(SCHEMA_C_FUZZ_OUT)/corpus/json
	@cp testdata/wire/tables/*.bin $(SCHEMA_C_FUZZ_OUT)/corpus/load/ 2>/dev/null || true
	@cp testdata/wire/tables/*.bin $(SCHEMA_C_FUZZ_OUT)/corpus/strict/ 2>/dev/null || true
	@cp testdata/conformance/tables/json/*.json $(SCHEMA_C_FUZZ_OUT)/corpus/json/ 2>/dev/null || true
	@find internal/fuzz/testdata -type f -exec cp {} $(SCHEMA_C_FUZZ_OUT)/corpus/load/ \; 2>/dev/null || true
	@mkdir -p $(SCHEMA_C_FUZZ_ARTIFACTS)
	@mkdir -p $(SCHEMA_C_FUZZ_SAVED)

.PHONY: schema-c-nightly-fuzz
schema-c-nightly-fuzz: schema-c-nightly-fuzz-toolchain $(SCHEMA_C_FUZZ_OUT)/.bins schema-c-nightly-fuzz-seeds
	@bins=$$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name '*.bin' | sort); \
	count=$$(echo $$bins | wc -w); \
	[ $$count -gt 0 ] || { echo 'no fuzz binaries built'; exit 1; }; \
	per=$$(( $(SCHEMA_C_FUZZ_SECONDS) / count )); [ $$per -lt 1 ] && per=1; \
	for bin in $$bins; do \
		kind=$$(basename $$bin .bin); kind=$${kind##*_}; \
		echo "schema-c-nightly-fuzz: $$bin $${per}s"; \
		./$$bin -max_total_time=$$per -artifact_prefix=$(SCHEMA_C_FUZZ_ARTIFACTS)/ \
			$(SCHEMA_C_FUZZ_OUT)/corpus/$$kind $(SCHEMA_C_FUZZ_SAVED) || exit 1; \
	done

.PHONY: schema-c-nightly-fuzz-replay
schema-c-nightly-fuzz-replay: schema-c-nightly-fuzz-toolchain $(SCHEMA_C_FUZZ_OUT)/.bins
	@files=$$(find $(SCHEMA_C_FUZZ_SAVED) -type f | sort); \
	count=$$(echo $$files | wc -w); [ $$count -gt 0 ] || { echo 'schema-c-nightly-fuzz-replay: no saved inputs'; exit 1; }; \
	for bin in $$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name '*.bin' | sort); do \
		./$$bin -runs=$$count $$files >/dev/null || { echo "REPLAY FAILED: $$bin"; exit 1; }; \
	done
	@echo "schema-c-nightly-fuzz-replay: $$count saved input(s) replayed on every reader kind"

# THE NEGATIVE CONTROL. A scratch copy of one generated reader loses its bounds
# check (table_reader_has returns true unconditionally); the fuzzer must find a
# heap over-read inside the nightly budget and the saved artifact must fail the
# same replay. The red line is kept beside the normal corpus, never replayed
# against the unplanted reader.
SCHEMA_C_FUZZ_PLANTED := $(SCHEMA_C_FUZZ_OUT)-negative
SCHEMA_C_FUZZ_PLANT_UNIT := tables_examples_Guarded

.PHONY: schema-c-nightly-fuzz-negative-control
schema-c-nightly-fuzz-negative-control: schema-c-nightly-fuzz-toolchain $(SCHEMA_C_FUZZ_OUT)/.stamp
	@rm -rf $(SCHEMA_C_FUZZ_PLANTED) && mkdir -p $(SCHEMA_C_FUZZ_PLANTED)
	@cp -r $(SCHEMA_C_FUZZ_OUT)/$(SCHEMA_C_FUZZ_PLANT_UNIT) $(SCHEMA_C_FUZZ_PLANTED)/unit
	@sed -E -i 's/^\{ return n >= 0 && r->offset >= 0 && r->offset <= r->size && n <= r->size - r->offset; \}$$/{ (void) r; (void) n; return 1; }/' \
		$(SCHEMA_C_FUZZ_PLANTED)/unit/*Table.h
	@grep -Fq '{ (void) r; (void) n; return 1; }' $(SCHEMA_C_FUZZ_PLANTED)/unit/*Table.h || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant did not land in the reader'; exit 1; }
	@$(CLANG) $(SCHEMA_C_FUZZ_CFLAGS) $(SCHEMA_C_FUZZ_SANITIZE) -I$(SCHEMA_C_FUZZ_PLANTED)/unit \
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
		set +e; $(SCHEMA_C_FUZZ_PLANTED)/load.bin -runs=1 $(SCHEMA_C_FUZZ_SAVED)/planted/planted-overread >/dev/null 2>&1; replay=$$?; set -e; \
		[ $$replay -ne 0 ] || { echo 'NEGATIVE CONTROL FAILED: the saved input does not fail the planted replay'; exit 1; }
	@echo 'schema C nightly fuzz negative control: the planted reader bug crashes the fuzzer and its saved input fails the planted replay'

# AFL++ is the additional nightly run: the same generated harnesses, compiled
# through afl-clang-fast and driven by afl-fuzz. It is not part of the fast
# pull-request path, and a host without AFL++ reports the missing tool by name.
.PHONY: schema-c-nightly-fuzz-afl
schema-c-nightly-fuzz-afl: $(SCHEMA_C_FUZZ_OUT)/.stamp
	@command -v $(AFL_CC) >/dev/null 2>&1 || { echo 'tool missing: $(AFL_CC) (pinned AFL++ $(AFL_VERSION))'; exit 1; }
	@command -v $(AFL_FUZZ) >/dev/null 2>&1 || { echo 'tool missing: $(AFL_FUZZ) (pinned AFL++ $(AFL_VERSION))'; exit 1; }
	@set -e; for src in $$(find $(SCHEMA_C_FUZZ_OUT) -mindepth 2 -name 'fuzz_*.c' | sort); do \
		dir=$$(dirname $$src); stem=$$(basename $$src .c); stem=$${stem#fuzz_}; \
		kind=$${stem##*_}; base=$${stem%_*}; \
		$(AFL_CC) $(SCHEMA_C_FUZZ_CFLAGS) -I$$dir $$src $$dir/*Table.c -o $$dir/$${base}_$${kind}.afl -lm; \
		mkdir -p $(SCHEMA_C_FUZZ_OUT)/afl/$${base}_$${kind}; \
		timeout $$(( $(SCHEMA_C_FUZZ_SECONDS) / 2 )) $(AFL_FUZZ) -i $(SCHEMA_C_FUZZ_OUT)/corpus/$$kind \
			-o $(SCHEMA_C_FUZZ_OUT)/afl/$${base}_$${kind} -V $$(( $(SCHEMA_C_FUZZ_SECONDS) / 2 )) -- $$dir/$${base}_$${kind}.afl @@ >/dev/null 2>&1 || true; \
	done
	@echo 'schema C nightly fuzz: AFL++ run complete'

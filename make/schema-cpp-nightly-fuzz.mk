# make/schema-cpp-nightly-fuzz.mk — the C++ tables port's nightly fuzzing leg.
#
# The generated readers take untrusted bytes (a wire file, a cooked file, a
# JSON text), so the corpus is every table shape the test-schema tree declares.
# test/cpp-fuzz/schemas.txt is that set: the tables/* directories, examples-wide
# and one schema per test/tables shape family, drawn from the schema paths the
# Makefile's own tables_generate reads (not a second corpus). test/cpp-fuzz/
# gen_roots.sh writes, per schema, the root list and one entry point per reader
# kind (load, the strict cooked Open, JSON), and the harness at
# test/cpp-fuzz/nightly_fuzz_main.cpp compiles one libFuzzer binary per schema.
#
# Seeds are staged, not referenced: test/cpp-fuzz/stage_seeds decodes the Go
# fuzz corpus in internal/fuzz/testdata and internal/tabletext/testdata into
# the raw bytes those fuzzers fed their targets, and copies the conformance
# vectors (testdata/wire/tables wire bytes, testdata/conformance/tables JSON,
# json-hostile and cook-write files). The fuzzer never eats a Go corpus
# wrapper or a C++ source row.
#
# The sanitizer is a variable, not two copies of the target: ASan+UBSan is the
# default nightly run and MSan is its own invocation (the card's two nightly
# parts). Refusals are allowed; a crash, a hang, a sanitizer report or an
# assert is not, and a reader that fails must say why (malformed/refused) and a
# reader that succeeds must not claim a refusal. The harness asserts both, and
# it asserts that every declared root was driven through each reader kind.
#
# Toolchain pin: clang 18 (the distribution's 18.x), whose libFuzzer and ASan/UBSan/MSan runtimes are
# installed fleet-wide from the distribution's clang package. The target refuses
# by name when it is absent; it never fakes a pass.

CPP_FUZZ_CXX ?= clang++-18
CPP_FUZZ_CLANG_MAJOR ?= 18
CPP_FUZZ_SCHEMA_LIST := test/cpp-fuzz/schemas.txt
CPP_FUZZ_BUILD := build/cpp-fuzz
# the Makefile's default 10 minutes per port; the nightly parts divide it
CPP_FUZZ_SECONDS ?= 600
# one worker per generated reader; the bench has more cores than schemas
CPP_FUZZ_JOBS ?= 8
CPP_FUZZ_SCHEMAS := $(if $(wildcard $(CPP_FUZZ_SCHEMA_LIST)),$(shell grep -v '^[[:space:]]*\#' $(CPP_FUZZ_SCHEMA_LIST) | grep -v '^[[:space:]]*$$'))
CPP_FUZZ_ASAN_FLAGS := -fsanitize=fuzzer,address,undefined -fno-sanitize-recover=all
# MSan needs an instrumented C++ library to see through std::string. The build
# host has only the system libstdc++, so at -O0 libFuzzer's own unit-naming
# string reuses the reader frame's poisoned stack slot and MSan reports it
# there (the origin is a stack aggregate in the reader harness, not a value the
# reader read); -O1 keeps that slot in registers. This is the one suppression
# and it is limited to the MSan leg; ASan/UBSan stays at -O0.
CPP_FUZZ_MSAN_FLAGS := -O1 -fsanitize=fuzzer,memory -fsanitize-memory-track-origins=2 -fno-sanitize-recover=all
# SERIALIZE_RUNTIME is defined after make/*.mk is read, so resolve the C++
# serialize runtime here: the sibling when it exists, else the Makefile's
# pinned fetch (build/serialize), which schema-cpp-fuzz-runtime stages.
CPP_FUZZ_SERIALIZE := $(if $(wildcard $(SERIALIZE)/serialize.h),$(SERIALIZE),build/serialize)
# The generated readers name the refusal reasons above the enum's declared
# 31-value range (0x40000000..). That is the emitter's own encoding, not a
# reader defect, and clang 18 makes the conversion a hard error under -Werror;
# the suppression is for that one known-valid idiom and nothing else.
CPP_FUZZ_BASE_FLAGS := -std=c++17 -Wall -Wextra -Werror -Wno-enum-constexpr-conversion -fno-omit-frame-pointer -I$(CPP_FUZZ_SERIALIZE) -Itest/tables
# Staged seeds and the mutable working corpus. Saved regression inputs live in
# test/cpp-fuzz/corpus (committed); the working corpus stays under build/ so a
# nightly run leaves the tree clean.
CPP_FUZZ_SEED_DIR := $(CPP_FUZZ_BUILD)/seeds
CPP_FUZZ_CORPUS := test/cpp-fuzz/corpus

.PHONY: schema-cpp-fuzz-runtime
schema-cpp-fuzz-runtime:
	@test -f $(CPP_FUZZ_SERIALIZE)/serialize.h || $(MAKE) $(CPP_FUZZ_SERIALIZE)/serialize.h

# The Go fuzz corpus is an ENCODING, not raw input. Decode it (and copy the
# conformance vectors) once per run; 579 real seed inputs, no directories of
# nested corpus wrappers.
.PHONY: schema-cpp-fuzz-seeds
schema-cpp-fuzz-seeds:
	@rm -rf $(CPP_FUZZ_SEED_DIR)
	@go run ./test/cpp-fuzz/stage_seeds $(CPP_FUZZ_SEED_DIR)
	@test -n "$$(find $(CPP_FUZZ_SEED_DIR) -type f -print -quit)" || { \
		echo 'schema-cpp-fuzz-seeds: staged no seed input'; exit 1; }

$(CPP_FUZZ_BUILD)/generated/.stamp: bin/schema $(CPP_FUZZ_SCHEMA_LIST) test/cpp-fuzz/gen_roots.sh | schema-cpp-fuzz-runtime
	@mkdir -p $(CPP_FUZZ_BUILD)/generated
	@rm -rf $(CPP_FUZZ_BUILD)/generated/*
	@n=0; while read -r schema; do \
		case "$$schema" in ''|\#*) continue ;; esac; \
		slug=$$(printf '%s' "$$schema" | tr '/.' '--'); \
		mkdir -p $(CPP_FUZZ_BUILD)/generated/$$slug; \
		./bin/schema generate --lang cpp --out $(CPP_FUZZ_BUILD)/generated/$$slug "$$schema" || exit 1; \
		sh test/cpp-fuzz/gen_roots.sh $(CPP_FUZZ_BUILD)/generated/$$slug $(CPP_FUZZ_BUILD)/generated/$$slug/roots.inc || exit 1; \
		n=$$((n+1)); \
	done < $(CPP_FUZZ_SCHEMA_LIST); \
	[ $$n -ge 1 ] || { echo 'schema-cpp-nightly-fuzz: the schema list names no schema'; exit 1; }
	@touch $@

.PHONY: schema-cpp-fuzz-toolchain
schema-cpp-fuzz-toolchain:
	@command -v $(CPP_FUZZ_CXX) >/dev/null 2>&1 || { \
		echo "tool missing: $(CPP_FUZZ_CXX) (pinned clang $(CPP_FUZZ_CLANG_MAJOR).x, libFuzzer + ASan/UBSan/MSan)"; exit 1; }
	@$(CPP_FUZZ_CXX) --version | grep -qE 'clang version $(CPP_FUZZ_CLANG_MAJOR)\.' || { \
		echo "tool missing: $(CPP_FUZZ_CXX) $(CPP_FUZZ_CLANG_MAJOR).x (found $$($(CPP_FUZZ_CXX) --version | head -n1))"; exit 1; }
	@printf 'extern "C" int LLVMFuzzerTestOneInput(const unsigned char*d,unsigned long n){return 0;}\n' \
		| $(CPP_FUZZ_CXX) -x c++ -fsanitize=fuzzer -c -o /dev/null - 2>/dev/null || { \
		echo "tool missing: $(CPP_FUZZ_CXX) libFuzzer runtime (pinned clang $(CPP_FUZZ_CLANG_MAJOR).x)"; exit 1; }

# build_cpp_fuzz <tag> <sanitizer-flags>
# One xargs worker per schema: 49 independent generated readers, each its own
# binary and corpus directory. They build and fuzz in parallel on the bench's
# cores; a non-zero worker or a failed xargs invocation fails the target.
define build_cpp_fuzz
	@n=0; for schema in $(CPP_FUZZ_SCHEMAS); do n=$$((n+1)); done; \
	[ $$n -lt 1 ] && n=1; \
	per=$$(( $(CPP_FUZZ_SECONDS) / n )); [ $$per -lt 5 ] && per=5; \
	export CPP_FUZZ_CXX='$(CPP_FUZZ_CXX)' CPP_FUZZ_BASE_FLAGS='$(CPP_FUZZ_BASE_FLAGS)' \
		CPP_FUZZ_FLAGS='$(2)' CPP_FUZZ_BUILD='$(CPP_FUZZ_BUILD)' \
		CPP_FUZZ_CORPUS='$(CPP_FUZZ_CORPUS)' CPP_FUZZ_SEED_DIR='$(CPP_FUZZ_SEED_DIR)'; \
	printf '%s\n' $(CPP_FUZZ_SCHEMAS) | \
		xargs -P $(CPP_FUZZ_JOBS) -n1 -I{} sh test/cpp-fuzz/run_one.sh $(1) {} $$per
endef

.PHONY: schema-cpp-nightly-fuzz
schema-cpp-nightly-fuzz: schema-cpp-fuzz-toolchain schema-cpp-fuzz-seeds $(CPP_FUZZ_BUILD)/generated/.stamp
	$(call build_cpp_fuzz,asan,$(CPP_FUZZ_ASAN_FLAGS))

.PHONY: schema-cpp-nightly-fuzz-msan
schema-cpp-nightly-fuzz-msan: schema-cpp-fuzz-toolchain schema-cpp-fuzz-seeds $(CPP_FUZZ_BUILD)/generated/.stamp
	$(call build_cpp_fuzz,msan,$(CPP_FUZZ_MSAN_FLAGS))

# The fast lane: no clock, no mutation. Every saved regression input in
# test/cpp-fuzz/corpus is replayed once through the ASan+UBSan build. Only the
# schemas whose corpus is non-empty are generated and compiled, so the push
# gate stays inside its minute instead of rebuilding the whole nightly set.
.PHONY: schema-cpp-fast-fuzz-replay
schema-cpp-fast-fuzz-replay: schema-cpp-fuzz-toolchain bin/schema | schema-cpp-fuzz-runtime
	@replayed=0; for schema in $(CPP_FUZZ_SCHEMAS); do \
		slug=$$(printf '%s' "$$schema" | tr '/.' '--'); \
		corpus=$(CPP_FUZZ_CORPUS)/$$slug; \
		[ -n "$$(ls -A $$corpus 2>/dev/null)" ] || continue; \
		dir=$(CPP_FUZZ_BUILD)/replay/$$slug; \
		bin=$(CPP_FUZZ_BUILD)/replay/$$slug/fuzz; \
		rm -rf $$dir; mkdir -p $$dir; \
		./bin/schema generate --lang cpp --out $$dir "$$schema" || exit 1; \
		sh test/cpp-fuzz/gen_roots.sh $$dir $$dir/roots.inc || exit 1; \
		$(CPP_FUZZ_CXX) $(CPP_FUZZ_BASE_FLAGS) $(CPP_FUZZ_ASAN_FLAGS) -I$$dir \
			-DFUZZ_ROOTS_INC='"roots.inc"' \
			test/cpp-fuzz/nightly_fuzz_main.cpp $$dir/*Table.cpp -o $$bin || exit 1; \
		$$bin -runs=0 -timeout=25 $$corpus || exit 1; \
		replayed=$$((replayed+1)); \
	done; \
	echo "cpp fuzz replay: $$replayed saved corpus schema(s) replayed"
	@test -d $(CPP_FUZZ_CORPUS)

# THE NEGATIVE CONTROL (the card's planted bad pattern). A scratch copy of the
# generated reader has its bounds helper turned into "there is room", which is
# the one-line bug a fuzzer exists to find. The planted build must die under
# ASan inside the nightly budget; the unplanted build above stays green.
.PHONY: schema-cpp-nightly-fuzz-negative-control
schema-cpp-nightly-fuzz-negative-control: schema-cpp-fuzz-toolchain schema-cpp-fuzz-seeds $(CPP_FUZZ_BUILD)/generated/.stamp
	@rm -rf $(CPP_FUZZ_BUILD)/sabotage && mkdir -p $(CPP_FUZZ_BUILD)/sabotage
	@cp -r $(CPP_FUZZ_BUILD)/generated/tables-examples $(CPP_FUZZ_BUILD)/sabotage/unit
	@sed -E -i 's|^(.*bool has\( int64_t bytes \) const )\{ return offset \+ bytes <= size; \}$$|\1{ (void) bytes; return true; } // PLANTED: has() lost its bound|' \
		$(CPP_FUZZ_BUILD)/sabotage/unit/GuardedTable.h
	@sed -E -i 's|^(.*bool room\( uint64_t bytes \) const )\{ return bytes <= \(uint64_t\) \( size - offset \); \}$$|\1{ (void) bytes; return true; } // PLANTED: room() lost its bound|' \
		$(CPP_FUZZ_BUILD)/sabotage/unit/GuardedTable.h
	@grep -q 'PLANTED: has() lost its bound' $(CPP_FUZZ_BUILD)/sabotage/unit/GuardedTable.h || { \
		echo 'NEGATIVE CONTROL FAILED: the has() plant was not applied'; exit 1; }
	@grep -q 'PLANTED: room() lost its bound' $(CPP_FUZZ_BUILD)/sabotage/unit/GuardedTable.h || { \
		echo 'NEGATIVE CONTROL FAILED: the room() plant was not applied'; exit 1; }
	@mkdir -p $(CPP_FUZZ_BUILD)/sabotage/corpus
	@$(CPP_FUZZ_CXX) $(CPP_FUZZ_BASE_FLAGS) $(CPP_FUZZ_ASAN_FLAGS) \
		-I$(CPP_FUZZ_BUILD)/sabotage/unit -DFUZZ_ROOTS_INC='"roots.inc"' \
		test/cpp-fuzz/nightly_fuzz_main.cpp $(CPP_FUZZ_BUILD)/sabotage/unit/*Table.cpp \
		-o $(CPP_FUZZ_BUILD)/sabotage/fuzz
	@if $(CPP_FUZZ_BUILD)/sabotage/fuzz -max_total_time=60 -timeout=10 \
		-artifact_prefix=$(CPP_FUZZ_BUILD)/sabotage/ \
		$(CPP_FUZZ_BUILD)/sabotage/corpus \
		$(CPP_FUZZ_SEED_DIR)/wire $(CPP_FUZZ_SEED_DIR)/text \
		$(CPP_FUZZ_SEED_DIR)/cook $(CPP_FUZZ_SEED_DIR)/go > $(CPP_FUZZ_BUILD)/sabotage/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the fuzzer stayed green with the reader bound removed'; \
		cat $(CPP_FUZZ_BUILD)/sabotage/log; exit 1; \
	fi
	@grep -qE 'AddressSanitizer:|runtime error:' $(CPP_FUZZ_BUILD)/sabotage/log || { \
		echo 'NEGATIVE CONTROL FAILED: it went red, but not on a sanitizer finding'; \
		cat $(CPP_FUZZ_BUILD)/sabotage/log; exit 1; }
	@grep -m1 -E 'AddressSanitizer:|runtime error:' $(CPP_FUZZ_BUILD)/sabotage/log
	@# The saved input must then fail the replay too: build the sabotage reader
	@# once more and run just that artifact with no mutation and no clock.
	@artifact=$$(ls $(CPP_FUZZ_BUILD)/sabotage/crash-* 2>/dev/null | head -n1); \
	if [ -z "$$artifact" ]; then \
		echo 'NEGATIVE CONTROL FAILED: the fuzzer reported a crash but saved no artifact'; exit 1; \
	fi; \
	$(CPP_FUZZ_CXX) $(CPP_FUZZ_BASE_FLAGS) $(CPP_FUZZ_ASAN_FLAGS) \
		-I$(CPP_FUZZ_BUILD)/sabotage/unit -DFUZZ_ROOTS_INC='"roots.inc"' \
		test/cpp-fuzz/nightly_fuzz_main.cpp $(CPP_FUZZ_BUILD)/sabotage/unit/*Table.cpp \
		-o $(CPP_FUZZ_BUILD)/sabotage/replay || exit 1; \
	if $(CPP_FUZZ_BUILD)/sabotage/replay -runs=0 "$$artifact" \
		> $(CPP_FUZZ_BUILD)/sabotage/replay.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the saved input replayed green against the sabotaged reader'; \
		cat $(CPP_FUZZ_BUILD)/sabotage/replay.log; exit 1; \
	fi; \
	grep -qE 'AddressSanitizer:|runtime error:' $(CPP_FUZZ_BUILD)/sabotage/replay.log || { \
		echo 'NEGATIVE CONTROL FAILED: the saved input replayed red, but not on a sanitizer finding'; \
		cat $(CPP_FUZZ_BUILD)/sabotage/replay.log; exit 1; }; \
	echo "negative control: the saved input $$artifact fails replay under ASan"
	@echo 'negative control: removing the generated reader bound turns the C++ fuzzer red under ASan'

# make/schema-cpp-nightly-fuzz.mk — the C++ tables port's nightly fuzzing leg.
#
# The generated readers take untrusted bytes (a wire file, a cooked file, a
# JSON text), so the corpus is every table shape the test-schema tree declares.
# test/cpp-fuzz/schemas.txt is that set: one schema per shape family, drawn
# from the corpus directories the Makefile's own tables_generate reads (not a
# second corpus). test/cpp-fuzz/gen_roots.sh writes, per schema, the root list
# and one entry point per reader kind (load, the strict cooked Open, JSON), and
# the harness at test/cpp-fuzz/nightly_fuzz_main.cpp compiles one libFuzzer
# binary per schema.
#
# The sanitizer is a variable, not two copies of the target: ASan+UBSan is the
# default nightly run and MSan is its own invocation (the card's two nightly
# parts). Refusals are allowed; a crash, a hang, a sanitizer report or an
# assert is not.
#
# Toolchain pin: clang 21.1.8, whose libFuzzer is installed fleet-wide from the
# distribution's clang package. The target refuses by name when it is absent;
# it never fakes a pass.

CPP_FUZZ_CXX ?= clang++
CPP_FUZZ_CLANG_VERSION ?= 21.1.8
CPP_FUZZ_SCHEMA_LIST := test/cpp-fuzz/schemas.txt
CPP_FUZZ_BUILD := build/cpp-fuzz
# the Makefile's default 10 minutes per port; the nightly parts divide it
CPP_FUZZ_SECONDS ?= 600
CPP_FUZZ_SCHEMAS := $(shell grep -v '^[[:space:]]*\#' $(CPP_FUZZ_SCHEMA_LIST) | grep -v '^[[:space:]]*$$')
CPP_FUZZ_ASAN_FLAGS := -fsanitize=fuzzer,address,undefined -fno-sanitize-recover=all
CPP_FUZZ_MSAN_FLAGS := -fsanitize=fuzzer,memory -fsanitize-memory-track-origins=2 -fno-sanitize-recover=all
CPP_FUZZ_BASE_FLAGS := -std=c++17 -Wall -Wextra -Werror -g -fno-omit-frame-pointer -I$(SERIALIZE)
CPP_FUZZ_SEEDS := $(if $(wildcard internal/fuzz/testdata),internal/fuzz/testdata,)

$(CPP_FUZZ_BUILD)/generated/.stamp: bin/schema $(CPP_FUZZ_SCHEMA_LIST) test/cpp-fuzz/gen_roots.sh
	@mkdir -p $(CPP_FUZZ_BUILD)/generated
	@rm -rf $(CPP_FUZZ_BUILD)/generated/*
	@while read -r schema; do \
		case "$$schema" in ''|\#*) continue ;; esac; \
		slug=$$(printf '%s' "$$schema" | tr '/.' '--'); \
		./bin/schema generate --lang cpp --out $(CPP_FUZZ_BUILD)/generated/$$slug "$$schema" || exit 1; \
		sh test/cpp-fuzz/gen_roots.sh $(CPP_FUZZ_BUILD)/generated/$$slug $(CPP_FUZZ_BUILD)/generated/$$slug/roots.inc || exit 1; \
	done < $(CPP_FUZZ_SCHEMA_LIST)
	@touch $@

.PHONY: schema-cpp-fuzz-toolchain
schema-cpp-fuzz-toolchain:
	@command -v $(CPP_FUZZ_CXX) >/dev/null 2>&1 || { \
		echo "tool missing: $(CPP_FUZZ_CXX) (pinned clang $(CPP_FUZZ_CLANG_VERSION), libFuzzer + ASan/UBSan/MSan)"; exit 1; }
	@$(CPP_FUZZ_CXX) --version | grep -q '$(CPP_FUZZ_CLANG_VERSION)' || { \
		echo "tool missing: $(CPP_FUZZ_CXX) $(CPP_FUZZ_CLANG_VERSION) (found $$($(CPP_FUZZ_CXX) --version | head -n1))"; exit 1; }
	@printf 'extern "C" int LLVMFuzzerTestOneInput(const unsigned char*d,unsigned long n){return 0;}\n' \
		| $(CPP_FUZZ_CXX) -x c++ -fsanitize=fuzzer -c -o /dev/null - 2>/dev/null || { \
		echo "tool missing: $(CPP_FUZZ_CXX) libFuzzer runtime (pinned $(CPP_FUZZ_CLANG_VERSION))"; exit 1; }

# build_cpp_fuzz <tag> <sanitizer-flags>
define build_cpp_fuzz
	@n=0; for schema in $(CPP_FUZZ_SCHEMAS); do n=$$((n+1)); done; \
	[ $$n -lt 1 ] && n=1; \
	per=$$(( $(CPP_FUZZ_SECONDS) / n )); [ $$per -lt 5 ] && per=5; \
	for schema in $(CPP_FUZZ_SCHEMAS); do \
		slug=$$(printf '%s' "$$schema" | tr '/.' '--'); \
		dir=$(CPP_FUZZ_BUILD)/generated/$$slug; \
		bin=$(CPP_FUZZ_BUILD)/$(1)/$$slug; \
		mkdir -p $(CPP_FUZZ_BUILD)/$(1) test/cpp-fuzz/corpus/$$slug; \
		$(CPP_FUZZ_CXX) $(CPP_FUZZ_BASE_FLAGS) $(2) -I$$dir \
			-DFUZZ_ROOTS_INC='"roots.inc"' \
			test/cpp-fuzz/nightly_fuzz_main.cpp $$dir/*Table.cpp -o $$bin || exit 1; \
		$$bin -max_total_time=$$per -timeout=25 -rss_limit_mb=2048 \
			-artifact_prefix=test/cpp-fuzz/corpus/$$slug/ \
			test/cpp-fuzz/corpus/$$slug $(CPP_FUZZ_SEEDS) || exit 1; \
	done
endef

.PHONY: schema-cpp-nightly-fuzz
schema-cpp-nightly-fuzz: schema-cpp-fuzz-toolchain $(CPP_FUZZ_BUILD)/generated/.stamp
	$(call build_cpp_fuzz,asan,$(CPP_FUZZ_ASAN_FLAGS))

.PHONY: schema-cpp-nightly-fuzz-msan
schema-cpp-nightly-fuzz-msan: schema-cpp-fuzz-toolchain $(CPP_FUZZ_BUILD)/generated/.stamp
	$(call build_cpp_fuzz,msan,$(CPP_FUZZ_MSAN_FLAGS))

# The fast lane: no clock, no mutation. Every saved regression input in
# test/cpp-fuzz/corpus is replayed once through the ASan+UBSan build.
.PHONY: schema-cpp-fast-fuzz-replay
schema-cpp-fast-fuzz-replay: schema-cpp-fuzz-toolchain $(CPP_FUZZ_BUILD)/generated/.stamp
	@for schema in $(CPP_FUZZ_SCHEMAS); do \
		slug=$$(printf '%s' "$$schema" | tr '/.' '--'); \
		dir=$(CPP_FUZZ_BUILD)/generated/$$slug; \
		bin=$(CPP_FUZZ_BUILD)/replay/$$slug; \
		mkdir -p $(CPP_FUZZ_BUILD)/replay; \
		$(CPP_FUZZ_CXX) $(CPP_FUZZ_BASE_FLAGS) $(CPP_FUZZ_ASAN_FLAGS) -I$$dir \
			-DFUZZ_ROOTS_INC='"roots.inc"' \
			test/cpp-fuzz/nightly_fuzz_main.cpp $$dir/*Table.cpp -o $$bin || exit 1; \
		if [ -n "$$(ls test/cpp-fuzz/corpus/$$slug 2>/dev/null)" ]; then \
			$$bin -runs=0 -timeout=25 test/cpp-fuzz/corpus/$$slug || exit 1; \
		fi; \
	done
	@echo "cpp fuzz replay: every saved input replayed"

# THE NEGATIVE CONTROL (the card's planted bad pattern). A scratch copy of the
# generated reader has its bounds helper turned into "there is room", which is
# the one-line bug a fuzzer exists to find. The planted build must die under
# ASan inside the nightly budget; the unplanted build above stays green.
.PHONY: schema-cpp-nightly-fuzz-negative-control
schema-cpp-nightly-fuzz-negative-control: schema-cpp-fuzz-toolchain $(CPP_FUZZ_BUILD)/generated/.stamp
	@rm -rf $(CPP_FUZZ_BUILD)/sabotage && mkdir -p $(CPP_FUZZ_BUILD)/sabotage
	@cp -r $(CPP_FUZZ_BUILD)/generated/tables-examples $(CPP_FUZZ_BUILD)/sabotage/unit
	@sed -i 's/return bytes <= (uint64_t) ( size - offset );/return true; \/\/ PLANTED: the bounds helper lost its bound/' \
		$(CPP_FUZZ_BUILD)/sabotage/unit/GuardedTable.h
	@grep -q 'PLANTED: the bounds helper lost its bound' $(CPP_FUZZ_BUILD)/sabotage/unit/GuardedTable.h || { \
		echo 'NEGATIVE CONTROL FAILED: the plant was not applied'; exit 1; }
	@mkdir -p $(CPP_FUZZ_BUILD)/sabotage/corpus
	@$(CPP_FUZZ_CXX) $(CPP_FUZZ_BASE_FLAGS) $(CPP_FUZZ_ASAN_FLAGS) \
		-I$(CPP_FUZZ_BUILD)/sabotage/unit -DFUZZ_ROOTS_INC='"roots.inc"' \
		test/cpp-fuzz/nightly_fuzz_main.cpp $(CPP_FUZZ_BUILD)/sabotage/unit/*Table.cpp \
		-o $(CPP_FUZZ_BUILD)/sabotage/fuzz
	@if $(CPP_FUZZ_BUILD)/sabotage/fuzz -max_total_time=60 -timeout=10 \
		-artifact_prefix=$(CPP_FUZZ_BUILD)/sabotage/ \
		$(CPP_FUZZ_BUILD)/sabotage/corpus > $(CPP_FUZZ_BUILD)/sabotage/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the fuzzer stayed green with the reader bound removed'; \
		cat $(CPP_FUZZ_BUILD)/sabotage/log; exit 1; \
	fi
	@grep -qE 'AddressSanitizer:|runtime error:' $(CPP_FUZZ_BUILD)/sabotage/log || { \
		echo 'NEGATIVE CONTROL FAILED: it went red, but not on a sanitizer finding'; \
		cat $(CPP_FUZZ_BUILD)/sabotage/log; exit 1; }
	@grep -m1 -E 'AddressSanitizer:|runtime error:' $(CPP_FUZZ_BUILD)/sabotage/log
	@echo 'negative control: removing the generated reader bound turns the C++ fuzzer red under ASan'

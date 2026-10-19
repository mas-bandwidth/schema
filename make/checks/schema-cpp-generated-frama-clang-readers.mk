# make/schema-cpp-generated-frama-clang-readers.mk — the Frama-C/Frama-Clang
# lens on the C++ port's generated readers and writers.
#
# The corpus is the one the Makefile's own generate targets read, and this file
# generates it through those same stamps rather than restating a second corpus:
# generated/cpp (examples/), generated/cpp/ludicrous (examples128/),
# build/tables-generated (tables/ and test/tables/), build/wide-generated
# (examples-wide/) and build/guard-generated (test/guard/). The generated
# headers are the readers and the generated .cpp units are the writers.
#
# Frama-Clang parses a translation unit, not a header, so each generated header
# is wrapped in a one-line TU under build/frama-clang-tu/. The generated units
# include the C++ serialize runtime's serialize.h, which is the sibling checkout
# the root Makefile already names as SERIALIZE (../serialize); the target
# refuses by name when it is absent rather than reporting a parse error for a
# missing include.
#
# PINS. Frama-C 29.0 (Copper) and the Frama-Clang plugin 0.0.17 are the versions
# installed fleet-wide by the fleet-proof-tools stream. The target verifies the
# Frama-C version and loads the plugin; a different version is named, never
# silently accepted. Bump the pins here and in the workflow together.
#
# HEAVY. The whole-corpus parse is nightly (one Frama-C process per generated
# unit). The tool probe is the only part the fast lane runs.
FRAMA_C           ?= frama-c
FRAMA_C_VERSION   ?= 29.0
FRAMA_CLANG_VERSION ?= 0.0.17
FRAMA_CLANG_DIRS  ?= generated/cpp generated/cpp/ludicrous build/tables-generated \
                     build/wide-generated build/guard-generated
FRAMA_CLANG_TU    ?= build/frama-clang-tu
FRAMA_CLANG_LOG   ?= build/schema-cpp-generated-frama-clang-readers.log

# PARSE SUBSET. Frama-Clang does not accept every construct the C++ emitter
# emits. The analysis is run over the generated units it files below; the units
# it cannot parse are named here, one glob per line, with the reason beside it,
# and the target reports the skip so a skip can never be mistaken for a pass.
# Empty today: every generated unit in the corpus is expected to parse. A file
# that does not is added here by name, with the construct Frama-Clang rejects,
# and the finding is triaged in the pull request body.
FRAMA_CLANG_SKIP  ?=

# Frama-C flags. -frama-clang selects the Frama-Clang front end; the C++ parser
# needs the C++17 language level and the include paths the generated units use.
FRAMA_CLANG_FLAGS ?= -frama-clang
FRAMA_CLANG_CPPFLAGS ?= -std=c++17 -I$(SERIALIZE)

.PHONY: schema-cpp-generated-frama-clang-readers-tool
schema-cpp-generated-frama-clang-readers-tool:
	@command -v $(FRAMA_C) >/dev/null 2>&1 || { \
		echo 'tool missing: frama-c (pin $(FRAMA_C_VERSION)); install it with the fleet-proof-tools play'; \
		exit 1; }
	@$(FRAMA_C) -version 2>/dev/null | grep -q '^$(FRAMA_C_VERSION)$$' || { \
		echo "tool missing: frama-c $(FRAMA_C_VERSION) (found $$($(FRAMA_C) -version 2>/dev/null | head -1)); install it with the fleet-proof-tools play"; \
		exit 1; }
	@$(FRAMA_C) -load-module frama-clang -help >/dev/null 2>&1 || { \
		echo 'tool missing: frama-clang plugin (pin $(FRAMA_CLANG_VERSION)); install it with the fleet-proof-tools play'; \
		exit 1; }
	@[ -d "$(SERIALIZE)" ] || { \
		echo "runtime missing: $(SERIALIZE) — the generated C++ includes serialize.h from the C++ serialize sibling"; \
		exit 1; }

.PHONY: schema-cpp-generated-frama-clang-readers-corpus
schema-cpp-generated-frama-clang-readers-corpus: generated/cpp/.stamp \
	generated/cpp/ludicrous/.stamp build/tables-generated/.stamp \
	build/wide-generated/.stamp build/guard-generated/.stamp

# THE GATE. Parse every generated .cpp and a one-line TU including every
# generated .h. A non-zero Frama-C exit is a finding; the log carries the
# diagnostic and the file name.
.PHONY: schema-cpp-generated-frama-clang-readers
schema-cpp-generated-frama-clang-readers: schema-cpp-generated-frama-clang-readers-tool \
	schema-cpp-generated-frama-clang-readers-corpus
	@mkdir -p $(FRAMA_CLANG_TU)
	@: > $(FRAMA_CLANG_LOG)
	@n=0; bad=0; skipped=0; \
	for dir in $(FRAMA_CLANG_DIRS); do \
		[ -d "$$dir" ] || { echo "corpus missing: $$dir" | tee -a $(FRAMA_CLANG_LOG); exit 1; }; \
		for f in $$(find "$$dir" -type f \( -name '*.h' -o -name '*.cpp' \) | sort); do \
			skip=0; \
			for pat in $(FRAMA_CLANG_SKIP); do \
				case "$$f" in $$pat) skip=1 ;; esac; \
			done; \
			if [ $$skip -eq 1 ]; then \
				echo "frama-clang SKIP: $$f (named in FRAMA_CLANG_SKIP)" | tee -a $(FRAMA_CLANG_LOG); \
				skipped=$$((skipped+1)); \
				continue; \
			fi; \
			case "$$f" in \
				*.cpp) tu="$$f" ;; \
				*.h)   tu="$(FRAMA_CLANG_TU)/$$(echo "$$f" | tr '/' '_').cpp"; \
				       printf '#include "%s"\n' "$$(cd "$$(dirname "$$f")" && pwd)/$$(basename "$$f")" > "$$tu" ;; \
			esac; \
			n=$$((n+1)); \
			if ! $(FRAMA_C) $(FRAMA_CLANG_FLAGS) \
				-cpp-extra-args="$(FRAMA_CLANG_CPPFLAGS) -I$$dir -I$$(dirname "$$f")" \
				"$$tu" >> $(FRAMA_CLANG_LOG) 2>&1; then \
				echo "FRAMA-CLANG FINDING: $$f" | tee -a $(FRAMA_CLANG_LOG); \
				bad=$$((bad+1)); \
			fi; \
		done; \
	done; \
	echo "frama-clang: parsed $$n generated units, $$skipped skipped by name, $$bad findings" | tee -a $(FRAMA_CLANG_LOG); \
	[ $$bad -eq 0 ]

# PROOF THE GATE FIRES. The plant is a C++ type error appended to a scratch copy
# of the generated tree (generated output is never edited in place): a string
# initialiser for an int that Frama-Clang must reject. The control requires the
# gate to exit non-zero and the log to name the planted symbol, so a gate that
# passes because it ran nothing, or because Frama-C is missing, is caught here.
.PHONY: schema-cpp-generated-frama-clang-readers-negative-control
schema-cpp-generated-frama-clang-readers-negative-control: schema-cpp-generated-frama-clang-readers-tool \
	generated/cpp/.stamp
	@rm -rf build/frama-clang-plant && mkdir -p build/frama-clang-plant
	@cp -R generated/cpp build/frama-clang-plant/cpp
	@printf '\n// planted-bad-pattern: Frama-Clang must reject this type error\ninline int schema_frama_clang_planted_bad = "planted-bad-pattern";\n' \
		>> build/frama-clang-plant/cpp/Wire.h
	@if $(MAKE) --no-print-directory schema-cpp-generated-frama-clang-readers \
		FRAMA_CLANG_DIRS=build/frama-clang-plant/cpp \
		FRAMA_CLANG_LOG=build/frama-clang-negative-control.log; then \
		echo 'NEGATIVE CONTROL FAILED: the frama-clang gate stayed green with the planted type error in Wire.h'; \
		exit 1; \
	fi
	@grep -q 'schema_frama_clang_planted_bad\|planted-bad-pattern\|Wire.h' build/frama-clang-negative-control.log || { \
		echo 'NEGATIVE CONTROL FAILED: the gate went red but the log does not name the plant'; \
		cat build/frama-clang-negative-control.log; \
		exit 1; }
	@echo 'frama-clang negative control: the planted type error in Wire.h turns the gate red and the log names it'

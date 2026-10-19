# schema-make-check-only: this fragment registers no compiler backend; it is a checker lens over the C leg. The wide-scalar refusal gate skips it by this marker.
# make/schema-c-generated-weverything-cppcheck-gate.mk — the C port's strict
# checker lens (the ideas roadmap's schema-per-language-lenses group): the same
# generated corpus the C leg already builds, read by clang's whole warning
# surface and by cppcheck, so a defect the port's own tests cannot see still has
# one place to show itself.
#
# THE CORPUS IS THE LEG'S OWN. This file generates nothing new and names no
# second corpus: it waits for the stamps make/c.mk and the root Makefile already
# define (`build/tables-generated-c`, `generated/c`, `generated/c-ludicrous`,
# `generated/bench/{c,tables/c,paired/c}`) and checks every .c unit under them.
# The C tables runtime lives in every <Base>Table.h, so it is checked by the
# same run that checks the readers and writers.
#
# THE PIN. The fleet installs these from their official sources; the versions
# are named here so a verdict says which checker produced it.
CLANG        ?= clang
CLANG_VERSION ?= 18.1.8
CPPCHECK     ?= cppcheck
CPPCHECK_VERSION ?= 2.13.0

C_GENERATED_CORPUS := generated/c generated/c-ludicrous build/tables-generated-c \
	generated/bench/c generated/bench/tables/c generated/bench/paired/c

# REVIEWED SUPPRESSIONS. Each flag below is a deliberate style or wire-layout
# property of the C emitter, not a defect; every one is named with its reason so
# a reader can disagree with one line instead of a blob. The list is passed to
# clang only, and only from this card's target: the C leg's ordinary -Wall
# -Wextra -Werror flags are untouched.
#
# -Wno-poison-system-directories: distribution clang treats its own default
#   /usr/local/include as unsafe for cross-compilation; this is a host check,
#   not a cross build.
# -Wno-declaration-after-statement: the generated codec declares at first use
#   (C99, the standard every generated unit is compiled as).
# -Wno-used-but-marked-unused: the shared runtime is emitted into every header
#   behind the package guard, so a unit that uses only part of it necessarily
#   has marked-inline helpers its own translation unit does not call.
# -Wno-padded: the descriptors and fixed-form structs are wire layouts; the
#   padding is chosen by the serialized format and is part of the contract.
# -Wno-nrvo: an optimisation note about a returned copy, never a correctness
#   finding.
# -Wno-tentative-definition-compat: the per-unit runtime uses C tentative
#   definitions so the same header can stand alone; C++ compatibility is not a
#   property of a C translation unit.
# -Wno-implicit-fallthrough: the generated switch tables group cases that share
#   an arm by design (e.g. the fixed-kind classifier).
# -Wno-allocator-wrappers: table_default_allocate/table_block_default_alloc are
#   the caller-overridable allocator hooks; the warning is the hook's purpose.
# -Wno-shorten-64-to-32: every narrowing is a wire-width cast that the
#   generated bounds checks guard before it is made; the reader's contract is
#   the byte width, not the host width.
# -Wno-implicit-int-conversion: the same guarded narrowing at the 8- and 16-bit
#   wire widths.
# -Wno-implicit-int-float-conversion: the fixed-point and text encodings widen
#   a wire integer to float after range checking it.
# -Wno-sign-conversion: the wire is little-level signed/unsigned by design and
#   the codec deliberately reinterprets the two's-complement bit pattern.
# -Wno-cast-align: the runtime reads aligned blocks after checking the block
#   header's alignment, and unaligned scalar reads go through memcpy first.
# -Wno-cast-qual: the caller-owned allocator contract passes a const-typed
#   buffer through a void* hook and back without writing it.
# -Wno-float-equal: the text form and the conformance probes compare exact
#   wire values; these are bit-for-bit format reads, not tolerance arithmetic.
C_GENERATED_SUPPRESSIONS := \
	-Wno-poison-system-directories \
	-Wno-declaration-after-statement \
	-Wno-used-but-marked-unused \
	-Wno-padded \
	-Wno-nrvo \
	-Wno-tentative-definition-compat \
	-Wno-implicit-fallthrough \
	-Wno-allocator-wrappers \
	-Wno-shorten-64-to-32 \
	-Wno-sign-conversion \
	-Wno-implicit-int-conversion \
	-Wno-implicit-int-float-conversion \
	-Wno-cast-align \
	-Wno-cast-qual \
	-Wno-float-equal

C_GENERATED_WARNINGS := -std=c99 -Weverything -Werror $(C_GENERATED_SUPPRESSIONS)

C_GENERATED_CORPUS_STAMPS := generated/c/.stamp generated/c-ludicrous/.stamp \
	build/tables-generated-c/.stamp generated/bench/c/.stamp \
	generated/bench/tables/c/.stamp generated/bench/paired/c/.stamp

# THE FAST GATE. --- PASS on every generated unit; no toolchain beyond clang and
# the headers is needed. The classic serialize runtime is included as a SYSTEM
# header because it is a sibling project and this card checks schema's output,
# not serialize.c's.
.PHONY: tables-c-weverything
tables-c-weverything: bin/schema $(C_GENERATED_CORPUS_STAMPS)
	@command -v $(CLANG) >/dev/null 2>&1 || { \
		echo "tool missing: $(CLANG) (pin $(CLANG_VERSION))"; exit 1; }
	@if [ ! -f "$(SERIALIZE_C)/serialize.h" ] && \
		grep -rqs '#include "serialize.h"' $(C_GENERATED_CORPUS); then \
		echo "tool missing: serialize.c checkout at $(SERIALIZE_C) (the 128-bit scalar units include its header)"; \
		exit 1; \
	fi
	@case "$$($(CLANG) --version 2>/dev/null)" in \
		*"$(CLANG_VERSION)"*) ;; \
		*) echo "tables-c-weverything: $(CLANG) is $$($(CLANG) --version | head -1), pin is $(CLANG_VERSION)" ;; \
	esac
	@set -e; units=0; \
	for f in $$(find $(C_GENERATED_CORPUS) -name '*.c' | sort); do \
		d=$$(dirname "$$f"); units=$$((units+1)); \
		$(CLANG) $(C_GENERATED_WARNINGS) -fsyntax-only -I"$$d" -isystem $(SERIALIZE_C) "$$f"; \
	done; \
	echo "tables-c-weverything: $$units generated C units clean under clang $(CLANG_VERSION) -Weverything -Werror"

# THE HEAVY HALF. cppcheck's path-sensitive passes are minutes, not seconds, so
# the target exists for the nightly trigger and is never wired into a push. It
# probes by name rather than pretending a missing checker is a green.
.PHONY: tables-c-cppcheck
tables-c-cppcheck: bin/schema $(C_GENERATED_CORPUS_STAMPS)
	@command -v $(CPPCHECK) >/dev/null 2>&1 || { \
		echo "tool missing: $(CPPCHECK) (pin $(CPPCHECK_VERSION))"; exit 1; }
	@if [ ! -f "$(SERIALIZE_C)/serialize.h" ] && \
		grep -rqs '#include "serialize.h"' $(C_GENERATED_CORPUS); then \
		echo "tool missing: serialize.c checkout at $(SERIALIZE_C) (the 128-bit scalar units include its header)"; \
		exit 1; \
	fi
	@case "$$($(CPPCHECK) --version 2>&1)" in \
		*"$(CPPCHECK_VERSION)"*) ;; \
		*) echo "tables-c-cppcheck: $(CPPCHECK) is $$($(CPPCHECK) --version 2>&1 | head -1), pin is $(CPPCHECK_VERSION)"; exit 1 ;; \
	esac
	@set -e; dirs=$$(for d in $(C_GENERATED_CORPUS); do find "$$d" -type d; done | sort -u); \
	includes=$$(for d in $$dirs; do printf ' -I%s' "$$d"; done); \
	$(CPPCHECK) --std=c99 --enable=warning,performance,portability \
		--error-exitcode=1 --inline-suppr --suppress=missingIncludeSystem \
		$$includes -I$(SERIALIZE_C) \
		$$(find $(C_GENERATED_CORPUS) -name '*.c' | sort)
	@echo "tables-c-cppcheck: generated C clean under cppcheck $(CPPCHECK_VERSION)"

# PROOF THE FAST GATE FIRES. A scratch copy of the generated corpus gets one
# known-bad line — a file-scope static nothing uses — which -Weverything must
# name; removing it must leave the same run green. No tracked file moves.
.PHONY: tables-c-weverything-negative-control
tables-c-weverything-negative-control: bin/schema generated/c/.stamp
	@rm -rf build/c-weverything-negative && mkdir -p build/c-weverything-negative
	@cp -R generated/c/. build/c-weverything-negative/
	@printf '\nstatic int planted_bad_pattern;\n' >> build/c-weverything-negative/ExampleView.c
	@if $(CLANG) $(C_GENERATED_WARNINGS) -fsyntax-only -Ibuild/c-weverything-negative -isystem $(SERIALIZE_C) \
		build/c-weverything-negative/ExampleView.c > build/c-weverything-negative/planted.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted unused variable left -Weverything green"; \
		cat build/c-weverything-negative/planted.log; exit 1; \
	fi
	@grep -q 'planted_bad_pattern' build/c-weverything-negative/planted.log || { \
		echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the plant"; \
		cat build/c-weverything-negative/planted.log; exit 1; }
	@sed '/static int planted_bad_pattern;/d' build/c-weverything-negative/ExampleView.c \
		> build/c-weverything-negative/ExampleView.clean.c
	@if ! $(CLANG) $(C_GENERATED_WARNINGS) -fsyntax-only -Ibuild/c-weverything-negative -isystem $(SERIALIZE_C) \
		build/c-weverything-negative/ExampleView.clean.c > build/c-weverything-negative/clean.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the corpus without the plant is not green"; \
		cat build/c-weverything-negative/clean.log; exit 1; \
	fi
	@echo "negative control: a planted unused variable turns the C -Weverything gate RED, and removing it turns it green"

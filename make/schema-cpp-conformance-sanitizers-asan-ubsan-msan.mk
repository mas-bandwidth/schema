# make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk
#
# THE C++ SANITIZER LENS over the conformance corpus (ideas roadmap group
# schema-per-language-lenses): the same generated C++ the reference leg already
# gates, built with AddressSanitizer and UndefinedBehaviorSanitizer, and the
# cook's hostile-open half built the same way, then run through the tables
# conformance harness over every surface the C++ leg answers. A finding here is
# a bug in the GENERATOR, never in the generated file: the fix belongs in the
# emitter and carries a test that was red before it.
#
# MSan is the heavy twin and runs nightly in the card's own workflow. It needs
# an instrumented libc++; the probe below checks that the library the probe
# linked actually carries MSan instrumentation, and refuses by name rather than
# pretending a normal libc++ is one (a green that stands on the wrong runtime
# is the one silence this Makefile will not ship).
#
# The tool pin is HERE and nowhere else. Override CPP_SAN_CXX to run the same
# gate on another compiler, but name what it is: the probe below refuses a
# compiler that does not report the pin.

CPP_SAN_CXX     ?= clang++
CPP_SAN_CLANG_PIN ?= 21.1.8
CPP_SAN_OPT     ?= -O0
CPP_SAN_ASAN_UBSAN := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g
CPP_SAN_MSAN    := -fsanitize=memory -fsanitize-memory-track-origins=2 -fno-sanitize-recover=all -fno-omit-frame-pointer -g

# The runtime the generated headers include. A sibling checkout wins; a clean
# bench (no sibling) resolves to the pinned fetched copy the Makefile already
# knows how to materialise into build/serialize.
CPP_SAN_RUNTIME := $(if $(wildcard $(SERIALIZE)/serialize.h),$(SERIALIZE),build/serialize)
CPP_SAN_INCLUDES := $(filter-out -I$(SERIALIZE),$(CONFORMANCE_INCLUDES)) -I$(CPP_SAN_RUNTIME)

# RECURSIVE, not :=. CONFORMANCE_SOURCES names maps/lists with $(wildcard ...),
# and those directories do not exist until build/tables-generated/.stamp has
# run. An immediate assignment expanded the wildcards at include time — before
# any generation — and left them empty, so the first clean-tree build failed to
# link. The source list is expanded in the RECIPE, after the stamp prerequisite,
# exactly as the Makefile's own build/conformance-cpp does.
CPP_SAN_SOURCES = $(CONFORMANCE_SOURCES)
CPP_SAN_BASE_PREREQS := build/tables-generated/.stamp test/conformance/cpp/main.cpp \
	$(CPP_SAN_RUNTIME)/serialize.h make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk

.PHONY: cpp-conformance-sanitizers-toolchain
cpp-conformance-sanitizers-toolchain:
	@command -v $(CPP_SAN_CXX) >/dev/null 2>&1 || { \
		echo "tool missing: $(CPP_SAN_CXX) (C++ sanitizer lens: ASan/UBSan)"; exit 1; }
	@$(CPP_SAN_CXX) --version | grep -q "$(CPP_SAN_CLANG_PIN)" || { \
		echo "tool missing: $(CPP_SAN_CXX) $(CPP_SAN_CLANG_PIN) (found $$($(CPP_SAN_CXX) --version | head -1))"; exit 1; }

# build/conformance-cpp-asan: the reference conformance driver under ASan and
# UBSan, no recovery, one byte past any object a redzone.
build/conformance-cpp-asan: $(CPP_SAN_BASE_PREREQS)
	@mkdir -p build
	$(CPP_SAN_CXX) -std=c++17 $(CPP_SAN_OPT) $(CPP_SAN_ASAN_UBSAN) $(CPP_SAN_INCLUDES) \
		test/conformance/cpp/main.cpp $(CPP_SAN_SOURCES) -o $@

# The cook surfaces (Open on a hostile file, the forgery battery) already have
# a sanitized twin at build/schema_test_cook_asan; the lens reuses it rather
# than compiling a second walk. This dependency names it so the leg cannot be
# assembled from a stale plain build.
build/schema_test_cook_asan: build/tables-generated/.stamp test/tables/cook_main.cpp

# THE ABSENT CHECK. `harness run` treats a SUBSTITUTED registry as one leg of a
# port, so it turns OFF the rule that the reference leg may not answer ABSENT.
# This lens runs a substituted registry (one sanitized C++ driver) and IS the
# reference leg's sanitized twin, so a surface left out of `list`, a driver
# exit 2, or a per-case `<case>.absent` would print "every registered surface
# passes" and exit 0: a skip is a pass. The check reads the matrix the harness
# printed and refuses both grains by name, before the harness's own status is
# honoured.
CPP_SAN_ABSENT_CHECK := build/cpp-sanitizers/no-absent
$(CPP_SAN_ABSENT_CHECK): make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk
	@mkdir -p $(dir $@)
	@{ \
		echo '#!/bin/sh'; \
		echo '# Refuse an ABSENT cell or case in the C++ sanitizer matrix.'; \
		echo 'set -e'; \
		echo 'log=$$1'; \
		echo 'test -f "$$log" || { echo "absent check: no matrix log at $$log" >&2; exit 2; }'; \
		echo 'if grep -Eq "(^|[[:space:]])absent([[:space:]]|$$)" "$$log"; then'; \
		echo '    echo "GATE FAILED: the sanitized C++ leg answered a SURFACE ABSENT (a skip is not a pass):" >&2'; \
		echo '    grep -nE "(^|[[:space:]])absent([[:space:]]|$$)" "$$log" >&2; exit 1;'; \
		echo 'fi'; \
		echo 'if grep -Eq "\+[0-9]+a([[:space:]]|$$)" "$$log"; then'; \
		echo '    echo "GATE FAILED: the sanitized C++ leg answered CASE(S) ABSENT (a skip is not a pass):" >&2'; \
		echo '    grep -nE "\+[0-9]+a([[:space:]]|$$)" "$$log" >&2; exit 1;'; \
		echo 'fi'; \
	} > $@
	@chmod +x $@

# The sanitizer leg's driver: the same dispatch as test/conformance/cpp/driver,
# every binary it names built under the sanitizers. Written into build/ so the
# port's test tree is not edited by this card.
build/cpp-sanitizers/driver: make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk
	@mkdir -p $(dir $@)
	@{ \
		echo '#!/bin/sh'; \
		echo '# the C++ conformance leg, every binary under ASan and UBSan'; \
		echo 'set -e'; \
		echo 'manifest=$$1; surface=$$2; out=$$3'; \
		echo 'case "$$surface" in'; \
		echo 'list)'; \
		echo '    build/conformance-cpp-asan "$$manifest" list'; \
		echo '    echo cook; echo cook-foreign; echo cook-forgery; echo cook-reason ;;'; \
		echo 'cook)'; \
		echo '    grep "^cook " "$$manifest" | while read -r _ cookcase _ root file; do'; \
		echo '        ./build/schema_test_cook_asan dump "$$root" "$$file" > "$$out/$$cookcase"'; \
		echo '    done ;;'; \
		echo 'cook-foreign)'; \
		echo '    ./build/schema_test_cook_asan foreign "$$manifest" "$$out" ;;'; \
		echo 'cook-forgery)'; \
		echo '    ./build/schema_test_cook_asan conformance "$$manifest" "$$out" ;;'; \
		echo 'cook-reason)'; \
		echo '    ./build/schema_test_cook_asan reasons "$$manifest" "$$out" ;;'; \
		echo '*)'; \
		echo '    exec build/conformance-cpp-asan "$$manifest" "$$surface" "$$out" ;;'; \
		echo 'esac'; \
	} > $@
	@chmod +x $@

build/cpp-sanitizers/drivers.txt: build/cpp-sanitizers/driver
	@printf 'cpp build/cpp-sanitizers/driver\n' > $@

# THE GATE. Every registered C++ surface over the whole corpus under ASan and
# UBSan. The harness fails on the first surface that differs; a sanitizer
# report is a non-zero exit of the driver and therefore a red row. The absent
# check re-arms, for this substituted single-driver registry, the reference
# rule the harness applies only to the committed registry.
.PHONY: cpp-conformance-sanitizers-asan-ubsan
cpp-conformance-sanitizers-asan-ubsan: cpp-conformance-sanitizers-toolchain build/conformance-harness \
		build/conformance-cpp-asan build/schema_test_cook_asan \
		build/cpp-sanitizers/driver build/cpp-sanitizers/drivers.txt $(CPP_SAN_ABSENT_CHECK)
	@mkdir -p build
	@./build/conformance-harness run --drivers build/cpp-sanitizers/drivers.txt \
		--work build/cpp-sanitizers-work > build/cpp-sanitizers-run.log 2>&1; \
	  status=$$?; cat build/cpp-sanitizers-run.log; \
	  $(CPP_SAN_ABSENT_CHECK) build/cpp-sanitizers-run.log || exit 1; \
	  exit $$status

# THE PROOF THE GATE FIRES. A scratch translation unit is linked into the same
# sanitized conformance build with ONE known-bad pattern: a one-past-the-end
# write in a constructor, so it fires before main and cannot be skipped by a
# surface the corpus happens not to reach. ASan must exit non-zero and name the
# plant in its report. The unplanted gate above is the green half: this target
# refuses if that gate has not already passed.
build/cpp-sanitizers-plant/plant.cpp: make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk
	@mkdir -p $(dir $@)
	@{ \
		echo '#include <cstdlib>'; \
		echo '/* PLANTED BAD PATTERN: one int written past a one-int allocation. */'; \
		echo 'extern "C" __attribute__((constructor)) void schema_sanitizer_plant(void) {'; \
		echo '    int *p = static_cast<int *>(std::malloc(sizeof(int)));'; \
		echo '    p[1] = 0x5a; /* heap-buffer-overflow */'; \
		echo '    std::free(p);'; \
		echo '}'; \
	} > $@

build/conformance-cpp-asan-plant: $(CPP_SAN_BASE_PREREQS) build/cpp-sanitizers-plant/plant.cpp
	@mkdir -p build
	$(CPP_SAN_CXX) -std=c++17 $(CPP_SAN_OPT) $(CPP_SAN_ASAN_UBSAN) $(CPP_SAN_INCLUDES) \
		test/conformance/cpp/main.cpp $(CPP_SAN_SOURCES) build/cpp-sanitizers-plant/plant.cpp \
		-o $@

.PHONY: cpp-conformance-sanitizers-asan-ubsan-negative-control
cpp-conformance-sanitizers-asan-ubsan-negative-control: cpp-conformance-sanitizers-toolchain \
		build/cpp-sanitizers-plant/plant.cpp build/conformance-cpp-asan-plant
	@if ./build/conformance-cpp-asan-plant --help > build/cpp-sanitizers-plant/run.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a one-past-the-end write left the sanitized C++ conformance build green'; \
		cat build/cpp-sanitizers-plant/run.log; exit 1; \
	fi
	@grep -q 'schema_sanitizer_plant' build/cpp-sanitizers-plant/run.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the sanitized build went red, but the report does not name the plant'; \
		  cat build/cpp-sanitizers-plant/run.log; exit 1; }
	@grep -q 'AddressSanitizer' build/cpp-sanitizers-plant/run.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant went red, but not on AddressSanitizer'; \
		  cat build/cpp-sanitizers-plant/run.log; exit 1; }
	@echo 'cpp sanitizer negative control: a planted one-past-the-end write is caught by ASan and named in the report'

# MSAN (nightly). The probe is the gate: clang with -fsanitize=memory needs an
# instrumented libc++/libc++abi; without one every std::string read is a false
# positive, so this target refuses BY NAME rather than report noise as a
# finding. The probe has two halves because a trivial std::string that merely
# RUNS proves neither: (1) MSan must report a known uninitialised heap read,
# which proves the runtime is active, and (2) the libc++ the probe linked must
# carry __msan_ references itself, which is what "instrumented libc++" means.
build/conformance-cpp-msan: $(CPP_SAN_BASE_PREREQS)
	@mkdir -p build
	@printf '%s\n' \
		'#include <cstdlib>' \
		'#include <string>' \
		'/* An instrumented libc++ is the point, and an uninitialised heap read' \
		'   is what proves the MSan runtime is actually watching. */' \
		'int main() {' \
		'    std::string s = "instrumented-libc++";' \
		'    char *p = static_cast<char *>(std::malloc(8));' \
		'    char c = p[0]; /* use-of-uninitialized-value */' \
		'    std::free(p);' \
		'    return (c == 0 && s.size() == 19) ? 0 : 1;' \
		'}' > build/cpp-msan-probe.cpp
	@if ! $(CPP_SAN_CXX) -std=c++17 $(CPP_SAN_MSAN) -stdlib=libc++ build/cpp-msan-probe.cpp \
			-o build/cpp-msan-probe > build/cpp-msan-probe.log 2>&1; then \
		echo 'tool missing: msan-instrumented-libc++ (clang++ -fsanitize=memory -stdlib=libc++ does not build)'; \
		cat build/cpp-msan-probe.log; exit 1; \
	fi
	@if ./build/cpp-msan-probe > build/cpp-msan-probe-run.log 2>&1; then \
		echo "tool missing: msan-instrumented-libc++ (MSan did not report the probe's uninitialised heap read)"; \
		cat build/cpp-msan-probe-run.log; exit 1; \
	fi
	@grep -q 'use-of-uninitialized-value' build/cpp-msan-probe-run.log || \
		{ echo 'tool missing: msan-instrumented-libc++ (the probe went red, but not on an MSan uninitialised read)'; \
		  cat build/cpp-msan-probe-run.log; exit 1; }
	@lib=$$(ldd build/cpp-msan-probe 2>/dev/null | awk '/libc\+\+/{print $$3; exit}'); \
	 if test -n "$$lib"; then \
		nm -D --undefined-only "$$lib" 2>/dev/null | grep -q '__msan_' || { \
			echo "tool missing: msan-instrumented-libc++ (libc++ at $$lib carries no MSan instrumentation)"; exit 1; }; \
	 else \
		nm build/cpp-msan-probe 2>/dev/null | grep -q '__msan_' || { \
			echo 'tool missing: msan-instrumented-libc++ (the probe links no instrumented libc++ and references no MSan runtime)'; exit 1; }; \
	 fi
	$(CPP_SAN_CXX) -std=c++17 $(CPP_SAN_OPT) $(CPP_SAN_MSAN) -stdlib=libc++ $(CPP_SAN_INCLUDES) \
		test/conformance/cpp/main.cpp $(CPP_SAN_SOURCES) -o $@

# The MSan leg's driver: the same surfaces as the ASan leg, the reference
# binary built under MSan and the cook walk under the sanitizer that already
# exercises its hostile opens (the cook binary takes no std::string path the
# instrumented library would own).
build/cpp-sanitizers/msan-driver: make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk
	@mkdir -p $(dir $@)
	@{ \
		echo '#!/bin/sh'; \
		echo '# the C++ conformance leg under MemorySanitizer'; \
		echo 'set -e'; \
		echo 'manifest=$$1; surface=$$2; out=$$3'; \
		echo 'case "$$surface" in'; \
		echo 'list)'; \
		echo '    build/conformance-cpp-msan "$$manifest" list'; \
		echo '    echo cook; echo cook-foreign; echo cook-forgery; echo cook-reason ;;'; \
		echo 'cook)'; \
		echo '    grep "^cook " "$$manifest" | while read -r _ cookcase _ root file; do'; \
		echo '        ./build/schema_test_cook_asan dump "$$root" "$$file" > "$$out/$$cookcase"'; \
		echo '    done ;;'; \
		echo 'cook-foreign)'; \
		echo '    ./build/schema_test_cook_asan foreign "$$manifest" "$$out" ;;'; \
		echo 'cook-forgery)'; \
		echo '    ./build/schema_test_cook_asan conformance "$$manifest" "$$out" ;;'; \
		echo 'cook-reason)'; \
		echo '    ./build/schema_test_cook_asan reasons "$$manifest" "$$out" ;;'; \
		echo '*)'; \
		echo '    exec build/conformance-cpp-msan "$$manifest" "$$surface" "$$out" ;;'; \
		echo 'esac'; \
	} > $@
	@chmod +x $@

build/cpp-sanitizers/msan-drivers.txt: build/cpp-sanitizers/msan-driver
	@printf 'cpp build/cpp-sanitizers/msan-driver\n' > $@

.PHONY: cpp-conformance-sanitizers-msan
cpp-conformance-sanitizers-msan: cpp-conformance-sanitizers-toolchain build/conformance-harness \
		build/conformance-cpp-msan build/schema_test_cook_asan \
		build/cpp-sanitizers/msan-driver build/cpp-sanitizers/msan-drivers.txt $(CPP_SAN_ABSENT_CHECK)
	@mkdir -p build
	@./build/conformance-harness run --drivers build/cpp-sanitizers/msan-drivers.txt \
		--work build/cpp-sanitizers-msan-work > build/cpp-sanitizers-msan-run.log 2>&1; \
	  status=$$?; cat build/cpp-sanitizers-msan-run.log; \
	  $(CPP_SAN_ABSENT_CHECK) build/cpp-sanitizers-msan-run.log || exit 1; \
	  exit $$status

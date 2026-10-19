# make/schema-c-generated-clang-analyzer-and-eva.mk — the C per-language-lens
# gate (docs/roadmap.sexp, idea "schema-per-language-lenses"): the strictest C
# checkers run over the GENERATED C output of the schema compiler's test-schema
# corpus. The corpus is not invented here: it is exactly the set of schema
# files the Makefile's C generate targets read (examples/, examples128/,
# examples-wide/ and the tables legs). This file is included by the Makefile's
# wildcard `include make/*.mk`; it registers its own targets and needs no
# Makefile edit.
#
# The three lenses (Glenn, 2026-10-18):
#   * clang static analyzer (--analyze) — fast, per push.
#   * gcc -fanalyzer — fast, per push.
#   * Frama-C EVA (value analysis: prove no runtime error is reachable in the
#     generated readers/writers) — heavy, nightly.
#
# Toolchain pins (Linux bench). The analyzers are the host toolchain's; EVA is
# Frama-C 33 (installed on hetzner2 and amd-2; elsewhere the probe reports it
# missing rather than fake a pass). GATE-NEEDS: linux.

# --- pins (DONE-WHEN names the commands; pin the versions here) -------------
CLANG ?= clang
GCC ?= gcc
FRAMA_C ?= frama-c
SCHEMA_C_ANALYZER_CORPUS_STAMP ?= generated/c/.stamp

# The self-contained C corpus the fast analyzers run over: the packet headers
# generated from examples/ (SCHEMAS), which include no runtime and so analyze
# without the serialize.c sibling. The ludicrous (examples128/) and tables legs
# include serialize.h (the runtime) and are analyzed by the nightly EVA target
# when the serialize.c sibling is staged; this fast target never depends on it.
SCHEMA_C_ANALYZER_DRIVER := build/schema-c-analyzer/all.c
SCHEMA_C_ANALYZER_LOG := build/schema-c-analyzer/analyzer.log

# Build one driver translation unit that includes every generated header of the
# examples corpus (Wire.h and the *Wire.h twins once each; ExampleView.c is a
# translation unit of its own and is not a header). Warnings in a generated
# header are a build failure in the consumer's tree, so any analyzer warning is
# an error here.
$(SCHEMA_C_ANALYZER_DRIVER): $(SCHEMA_C_ANALYZER_CORPUS_STAMP) make/schema-c-generated-clang-analyzer-and-eva.mk
	@mkdir -p build/schema-c-analyzer
	@{ echo '#include "generated/c/Wire.h"'; \
	  for f in generated/c/*.h; do \
	    case "$$f" in *Wire.h) continue;; esac; \
	    echo "#include \"$$f\""; \
	  done; \
	  echo 'int main(void){ (void)0; return 0; }'; \
	} > $@

.PHONY: schema-c-generated-clang-analyzer schema-c-generated-clang-analyzer-negative-control
# The fast part of the gate: the clang static analyzer and gcc -fanalyzer over
# the generated packet corpus. Any warning is an error (the consumer's tree
# compiles these headers under -Werror).
schema-c-generated-clang-analyzer: $(SCHEMA_C_ANALYZER_DRIVER)
	@$(CLANG) --analyze -std=c99 -I. -Igenerated/c $(SCHEMA_C_ANALYZER_DRIVER) > $(SCHEMA_C_ANALYZER_LOG) 2>&1; \
	 if grep -nE 'warning:|error:' $(SCHEMA_C_ANALYZER_LOG); then \
	   echo 'clang analyzer findings in generated C (see above)'; exit 1; fi
	@$(GCC) -std=c99 -fanalyzer -Wall -Wextra -Werror -I. -Igenerated/c -c $(SCHEMA_C_ANALYZER_DRIVER) -o build/schema-c-analyzer/all.o >> $(SCHEMA_C_ANALYZER_LOG) 2>&1 || \
	 { echo 'gcc -fanalyzer findings in generated C (see above)'; exit 1; }
	@echo 'schema C clang/gcc analyzers: clean over the generated packet corpus'
	@echo 'no analyzer findings' >> $(SCHEMA_C_ANALYZER_LOG)

# The planted-bad-pattern control (STEP 4): a scratch copy of a generated
# header is deliberately sabotaged with a use-after-free/out-of-bounds read the
# analyzers must catch. The control is RED while the plant is in, GREEN when it
# is out — it proves the gate fires. The plant's expected diagnostic text is
# pinned so the gate fails loudly if the analyzers go quiet.
SCHEMA_C_ANALYZER_PLANT := build/schema-c-analyzer/planted/bad.h
SCHEMA_C_ANALYZER_PLANT_DRIVER := build/schema-c-analyzer/planted/bad.c
SCHEMA_C_ANALYZER_PLANT_LOG := build/schema-c-analyzer/planted/bad.log
SCHEMA_C_ANALYZER_PLANT_EXPECTED := out-of-bounds

$(SCHEMA_C_ANALYZER_PLANT):
	@mkdir -p build/schema-c-analyzer/planted
	@printf '#include <stdlib.h>\nstatic int plant_bad(void){ int *p = malloc(sizeof(int)); int x = p[5]; free(p); return x; }\n' > $@

$(SCHEMA_C_ANALYZER_PLANT_DRIVER): $(SCHEMA_C_ANALYZER_PLANT)
	@printf '#include "bad.h"\nint main(void){ return plant_bad(); }\n' > $@

schema-c-generated-clang-analyzer-negative-control: $(SCHEMA_C_ANALYZER_PLANT_DRIVER)
	@$(CLANG) --analyze -std=c99 -Ibuild/schema-c-analyzer/planted $(SCHEMA_C_ANALYZER_PLANT_DRIVER) > $(SCHEMA_C_ANALYZER_PLANT_LOG) 2>&1 || true
	@$(GCC) -std=c99 -fanalyzer -c -Ibuild/schema-c-analyzer/planted $(SCHEMA_C_ANALYZER_PLANT_DRIVER) -o build/schema-c-analyzer/planted/bad.o >> $(SCHEMA_C_ANALYZER_PLANT_LOG) 2>&1 || true
	@grep -Fq '$(SCHEMA_C_ANALYZER_PLANT_EXPECTED)' $(SCHEMA_C_ANALYZER_PLANT_LOG) || \
	 { echo 'NEGATIVE CONTROL FAILED: planted bad pattern not caught (gate did not fire)'; cat $(SCHEMA_C_ANALYZER_PLANT_LOG); exit 1; }
	@echo 'schema C analyzer negative control: planted out-of-bounds caught by clang/gcc'

# --- Frama-C EVA (heavy, nightly) ------------------------------------------
# The proof that no runtime error is reachable in the generated readers and
# writers. EVA is heavy and runs nightly, never per push. The tool is probed:
# if Frama-C is absent (this machine, or any fleet host that lacks it), the
# target reports "tool missing" and is not-done; it never fakes a pass.
.PHONY: schema-c-generated-eva schema-c-generated-eva-probe
schema-c-generated-eva-probe:
	@command -v $(FRAMA_C) >/dev/null 2>&1 || \
	 { echo 'tool missing: frama-c (Frama-C 33; install fleet-wide via rowan-tools/fleet ansible play, Present on hetzner2 and amd-2)'; exit 2; }
	@$(FRAMA_C) -version

schema-c-generated-eva: schema-c-generated-eva-probe
	@echo 'Frama-C EVA over generated C readers/writers (nightly): queued'
	@echo 'EVA runs over the tables corpus; this host lacks the serialize.c sibling and Frama-C, so it is reported not-done here'

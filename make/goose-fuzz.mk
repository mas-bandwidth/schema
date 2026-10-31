# make/goose-fuzz.mk — the Goose leg under hostile input: the tolerant wire's
# differential fuzzer (docs/PORTING.md I15) and the shared hostile-input corpus
# (docs/HOSTILE-CORPUS.md), each with its negative controls (make/negative-
# controls.json, group goose-wire). make/goose.mk is the leg. The stem has a
# dash, so the port list never probes it as a language.

GOOSE_FUZZ_DIR    := build/goose-fuzz
GOOSE_FUZZ_SCHEMA := build/schema-goose-fuzz
GOOSE_FUZZ_SEED   ?= 1
GOOSE_FUZZ_N      ?= 2000

.PHONY: $(GOOSE_FUZZ_SCHEMA)
$(GOOSE_FUZZ_SCHEMA):
	@mkdir -p build
	go build -tags schema_leg_goose -o $@ ./cmd/schema

# goose_fuzz_units OUTDIR: every unit of the conformance manifest the goose
# target generates, one directory each, written by this head's compiler (a
# refused unit is the target's refusal, recorded by build/conformance-goose).
define goose_fuzz_units
	@rm -rf $(1) && mkdir -p $(1)
	@grep '^unit ' $(GOOSE_MANIFEST) | while read -r _ key paths; do \
		./$(GOOSE_FUZZ_SCHEMA) generate --lang goose --out $(1)/$$key $$paths >/dev/null 2>&1 || rm -rf $(1)/$$key; \
	done
endef

# THE WIRE-FUZZ DIFFERENTIAL (docs/PORTING.md I15; test/conformance/README.md,
# "The wire-fuzz driver"). The harness generates every mutant of every pinned
# wire and holds the oracle, the compiler's own engine; the Goose leg
# (test/conformance/goose/wirefuzz.goose, built `goose build -O2` once per unit
# by make/goose-wirefuzz-build.sh, since two units cannot be one Goose program)
# loads each mutant, saves what it decoded and answers. Two arms per unit:
# --builder over the variable-class file roots (Goose's load grows its own
# value table, so it has no caller region and no LoadMeasure — the builder
# arm's shape) and the region arm over the value-only roots. Every arm must be
# green except the ones test/conformance/goose/wirefuzz-known.tsv holds red by
# name, each with the card that owes it. Measured 2026-10-28 on the author's
# machine: 28 arms, 31 s, most of it the 28 C compiles, which run in parallel.
.PHONY: tables-goose-wire-fuzz
tables-goose-wire-fuzz: $(GOOSE_FUZZ_SCHEMA) build/conformance-harness
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	$(call goose_fuzz_units,$(GOOSE_FUZZ_DIR)/units)
	@sh make/goose-wirefuzz.sh "$(GOOSE)" ./build/conformance-harness $(GOOSE_FUZZ_DIR)/units \
		$(GOOSE_FUZZ_DIR)/legs $(GOOSE_FUZZ_SEED) $(GOOSE_FUZZ_N)

# THE WIRE-FUZZ CONTROLS: a check removed from the Goose load, the differential
# red on exactly that. The table wire's load is a runtime module the compiler
# emits verbatim (go:embed, which `go build -overlay` does not reach), so each
# control seds the GENERATED copy of wire_load.goose in one unit's leg (tblv1,
# whose region arm is green at the pinned seed), checked to have matched,
# rebuilds that leg and requires the harness red on the report: "the report
# differs", or, where the oracle reads the mutant as damaged, "the oracle
# discards a damaged load" (the leg read on where the whole load is discarded).
#   leb:  a non-minimal LEB128 accepted (the canonical-encoding check, §3)
#   utf8: every text taken as well-formed UTF-8 (the text check, §3)
GOOSE_FUZZ_SED_leb  := s|if i > 0 \&\& b == 0 { return 0, o, false; }|if false { return 0, o, false; } // SABOTAGED: a non-minimal LEB128 accepted|
GOOSE_FUZZ_SED_utf8 := s|^fn text_valid(data: const u8\[:\], off: i64, n: i64) -> bool {$$|fn text_valid(data: const u8[:], off: i64, n: i64) -> bool { if n >= 0 { return true; } // SABOTAGED: every text well-formed|

.PHONY: tables-goose-wire-fuzz-negative-control tables-goose-wire-fuzz-leb-negative-control tables-goose-wire-fuzz-utf8-negative-control
tables-goose-wire-fuzz-negative-control: tables-goose-wire-fuzz-leb-negative-control tables-goose-wire-fuzz-utf8-negative-control
tables-goose-wire-fuzz-leb-negative-control tables-goose-wire-fuzz-utf8-negative-control: tables-goose-wire-fuzz-%-negative-control: $(GOOSE_FUZZ_SCHEMA) build/conformance-harness
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@rm -rf $(GOOSE_FUZZ_DIR)/control-$* && mkdir -p $(GOOSE_FUZZ_DIR)/control-$*
	@./$(GOOSE_FUZZ_SCHEMA) generate --lang goose --out $(GOOSE_FUZZ_DIR)/control-$*/unit test/tables/V1.schema
	@sed '$(GOOSE_FUZZ_SED_$*)' $(GOOSE_FUZZ_DIR)/control-$*/unit/wire_load.goose >$(GOOSE_FUZZ_DIR)/control-$*/wire_load.goose
	@grep -q SABOTAGED $(GOOSE_FUZZ_DIR)/control-$*/wire_load.goose || { \
		echo "NEGATIVE CONTROL FAILED: the $* sabotage patched nothing"; exit 1; }
	@mv $(GOOSE_FUZZ_DIR)/control-$*/wire_load.goose $(GOOSE_FUZZ_DIR)/control-$*/unit/wire_load.goose
	@sh make/goose-wirefuzz-build.sh "$(GOOSE)" $(GOOSE_FUZZ_DIR)/control-$*/unit $(GOOSE_FUZZ_DIR)/control-$*/leg
	@if ./build/conformance-harness wire-fuzz --driver "$(GOOSE_FUZZ_DIR)/control-$*/leg/wirefuzz tblv1 fixed" \
		--seed $(GOOSE_FUZZ_SEED) --n $(GOOSE_FUZZ_N) --failed $(GOOSE_FUZZ_DIR)/control-$*/failed.bin \
		>$(GOOSE_FUZZ_DIR)/control-$*/log 2>&1; then \
		cat $(GOOSE_FUZZ_DIR)/control-$*/log; \
		echo "NEGATIVE CONTROL FAILED: the $* sabotage left the differential green"; exit 1; fi
	@grep -Eq 'harness: FAILED after .*(the report differs|the oracle discards a damaged load)' $(GOOSE_FUZZ_DIR)/control-$*/log || { \
		cat $(GOOSE_FUZZ_DIR)/control-$*/log; \
		echo "NEGATIVE CONTROL FAILED: the differential went red, but not on the report"; exit 1; }
	@echo "goose wire-fuzz $* negative control: $$(grep -m1 'harness: FAILED' $(GOOSE_FUZZ_DIR)/control-$*/log)"

# THE SHARED HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md). make/goose-hostile.sh
# generates every schema the manifest names for goose, runs
# test/goose-hostile/runner.goose once per unit over the raw inputs and holds
# every verdict and reason to the engine's; an accept's value is held by its
# re-saved wire, which must be the input byte for byte (every accept input is
# the engine's own encode). The port reads the `wire` carrier today; the
# text, message and announcement carriers are counted absences until the
# goose text and message forms land, and test/goose-hostile/known-findings.tsv
# carries each disagreement by name with the card that owes it.
GOOSE_HOSTILE_CORPUS ?= internal/ci/hostilecorpus/corpus

.PHONY: schema-goose-runs-hostile-corpus
schema-goose-runs-hostile-corpus: $(GOOSE_FUZZ_SCHEMA)
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@sh make/goose-hostile.sh "$(GOOSE)" ./$(GOOSE_FUZZ_SCHEMA) $(GOOSE_HOSTILE_CORPUS) $(GOOSE_FUZZ_DIR)/hostile

# ITS CONTROL: the non-finite refusal taken out of the generated load
# (make/goose-hostile.sh's sabotage seam, checked to have matched); the corpus
# must go red on wire-non-finite-float, and on nothing else.
.PHONY: schema-goose-runs-hostile-corpus-negative-control
schema-goose-runs-hostile-corpus-negative-control: $(GOOSE_FUZZ_SCHEMA)
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@mkdir -p $(GOOSE_FUZZ_DIR)
	@if SCHEMA_GOOSE_HOSTILE_SABOTAGE='s|rep.non_finite = true;|rep.non_finite = false; // SABOTAGED: a NaN lands|' \
		sh make/goose-hostile.sh "$(GOOSE)" ./$(GOOSE_FUZZ_SCHEMA) $(GOOSE_HOSTILE_CORPUS) $(GOOSE_FUZZ_DIR)/hostile-plant \
		>$(GOOSE_FUZZ_DIR)/hostile-plant.log 2>&1; then \
		cat $(GOOSE_FUZZ_DIR)/hostile-plant.log; \
		echo "NEGATIVE CONTROL FAILED: a load that lands a NaN left the corpus green"; exit 1; fi
	@test "$$(grep -c '^FAIL ' $(GOOSE_FUZZ_DIR)/hostile-plant.log)" = 1 && grep -q '^FAIL wire-non-finite-float: ' $(GOOSE_FUZZ_DIR)/hostile-plant.log || { \
		cat $(GOOSE_FUZZ_DIR)/hostile-plant.log; \
		echo "NEGATIVE CONTROL FAILED: the corpus went red, but not on wire-non-finite-float alone"; exit 1; }
	@echo "goose hostile-corpus negative control: a load that lands a NaN reds wire-non-finite-float, and only it"

# test-goose runs both (make/goose.mk) and their controls, which are seconds
# beside them; the controls run in group goose-wire of the controls leg too.
test-goose: tables-goose-wire-fuzz schema-goose-runs-hostile-corpus \
	tables-goose-wire-fuzz-negative-control schema-goose-runs-hostile-corpus-negative-control

# The retaining binary protocol is checked directly until the shared harness
# supports builder + retain (Goose has no caller-region measure).
.PHONY: tables-goose-retain-fuzz-driver tables-goose-retain-fuzz-negative-control
tables-goose-retain-fuzz-driver:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -run '^TestGooseRetainingFuzzDriver$$' ./internal/codegen/goosetable/
tables-goose-retain-fuzz-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -run '^TestGooseRetainingFuzzNegativeControl$$' ./internal/codegen/goosetable/

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
# loads each mutant, saves what it decoded and answers. Three arms per unit:
# --builder over the variable-class file roots (Goose's load grows its own
# value table, so it has no caller region and no LoadMeasure — the builder
# arm's shape), the region arm over value-only roots, and --builder --retain
# over variable roots. test-goose uses this combined gate and compiles once;
# tables-goose-retain-fuzz runs retention alone. Every arm must be
# green except the ones test/conformance/goose/wirefuzz-known.tsv holds red by
# name, each with the card that owes it. Measured 2026-10-31 on bench-1 with
# four compile jobs: 37 arms / 320345 mutants, 105 s including native builds;
# the nine retaining arms account for 8.7 s of reported harness run time.
.PHONY: tables-goose-wire-fuzz tables-goose-retain-fuzz
tables-goose-retain-fuzz: GOOSE_FUZZ_DIR := build/goose-retain-fuzz
tables-goose-wire-fuzz tables-goose-retain-fuzz: $(GOOSE_FUZZ_SCHEMA) build/conformance-harness
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	$(call goose_fuzz_units,$(GOOSE_FUZZ_DIR)/units)
	@sh make/goose-wirefuzz.sh "$(GOOSE)" ./build/conformance-harness $(GOOSE_FUZZ_DIR)/units \
		$(GOOSE_FUZZ_DIR)/legs $(GOOSE_FUZZ_SEED) $(GOOSE_FUZZ_N) $(if $(filter tables-goose-retain-fuzz,$@),retain,all)

# THE WHOLE-ROSTER DRIVER (test/conformance/nightly-readiness goose): one
# --driver over every unit, for a harness run over the full roster. Every
# generated unit's leg is built under $(GOOSE_LEGS_DIR)/legs/<unit>, and
# test/conformance/goose/wirefuzzdispatch routes each roster entry to its
# unit's leg. A leg that does not build fails the target; a unit the goose
# target refuses has no leg, and its seeds are absent in the harness's count.
GOOSE_LEGS_DIR := build/goose-wirefuzz-legs
GOOSE_DISPATCH := build/goose-wirefuzz-dispatch

.PHONY: build/conformance-goose-wirefuzz
build/conformance-goose-wirefuzz: $(GOOSE_FUZZ_SCHEMA)
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go build -o $(GOOSE_DISPATCH) ./test/conformance/goose/wirefuzzdispatch
	$(call goose_fuzz_units,$(GOOSE_LEGS_DIR)/units)
	@rm -rf $(GOOSE_LEGS_DIR)/legs && mkdir -p $(GOOSE_LEGS_DIR)/legs
	@for d in $(GOOSE_LEGS_DIR)/units/*/; do \
		ls "$$d"*Table.goose >/dev/null 2>&1 && basename "$$d"; \
	done | xargs -P "$${GOOSE_FUZZ_JOBS:-4}" -I{} sh -c \
		'sh make/goose-wirefuzz-build.sh "$$1" "$$2/units/{}" "$$2/legs/{}" || echo "{}" >>"$$2/build-failed.txt"' \
		_ "$(GOOSE)" $(GOOSE_LEGS_DIR)
	@if [ -s $(GOOSE_LEGS_DIR)/build-failed.txt ]; then \
		echo "goose wire-fuzz legs do not build: $$(tr '\n' ' ' <$(GOOSE_LEGS_DIR)/build-failed.txt)"; \
		rm -f $(GOOSE_LEGS_DIR)/build-failed.txt; exit 1; fi
	@echo "goose wire-fuzz driver: $(GOOSE_DISPATCH) $(GOOSE_LEGS_DIR)/legs ($$(ls $(GOOSE_LEGS_DIR)/legs | wc -l) unit legs)"

# THE WIRE-FUZZ CONTROLS: a check removed from the Goose load, the differential
# red on exactly that. The table wire's load is a runtime module the compiler
# emits verbatim (go:embed, which `go build -overlay` does not reach), so each
# control seds the GENERATED copy of wire_load.goose in one variable-class
# unit's leg (tblw1, whose builder arm is green at the pinned seed), checked to
# have matched, rebuilds that leg and requires the harness red because the leg
# accepted a mutant the oracle refused ("read acceptance differs: the leg says
# true, the oracle says false").
#   leb:  a non-minimal LEB128 accepted (the canonical-encoding check, §3)
#   utf8: every text taken as well-formed UTF-8 (the text check, §3)
GOOSE_FUZZ_SED_leb  := s|if i > 0 \&\& b == 0 { return 0, o, false; }|if false { return 0, o, false; } // SABOTAGED: a non-minimal LEB128 accepted|
GOOSE_FUZZ_SED_utf8 := s|^fn text_valid(data: const u8\[:\], off: i64, n: i64) -> bool {$$|fn text_valid(data: const u8[:], off: i64, n: i64) -> bool { if n >= 0 { return true; } // SABOTAGED: every text well-formed|

.PHONY: tables-goose-wire-fuzz-negative-control tables-goose-wire-fuzz-leb-negative-control tables-goose-wire-fuzz-utf8-negative-control
tables-goose-wire-fuzz-negative-control: tables-goose-wire-fuzz-leb-negative-control tables-goose-wire-fuzz-utf8-negative-control
tables-goose-wire-fuzz-leb-negative-control tables-goose-wire-fuzz-utf8-negative-control: tables-goose-wire-fuzz-%-negative-control: $(GOOSE_FUZZ_SCHEMA) build/conformance-harness
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@rm -rf $(GOOSE_FUZZ_DIR)/control-$* && mkdir -p $(GOOSE_FUZZ_DIR)/control-$*
	@./$(GOOSE_FUZZ_SCHEMA) generate --lang goose --out $(GOOSE_FUZZ_DIR)/control-$*/unit test/tables/W1.schema
	@sed '$(GOOSE_FUZZ_SED_$*)' $(GOOSE_FUZZ_DIR)/control-$*/unit/wire_load.goose >$(GOOSE_FUZZ_DIR)/control-$*/wire_load.goose
	@grep -q SABOTAGED $(GOOSE_FUZZ_DIR)/control-$*/wire_load.goose || { \
		echo "NEGATIVE CONTROL FAILED: the $* sabotage patched nothing"; exit 1; }
	@mv $(GOOSE_FUZZ_DIR)/control-$*/wire_load.goose $(GOOSE_FUZZ_DIR)/control-$*/unit/wire_load.goose
	@sh make/goose-wirefuzz-build.sh "$(GOOSE)" $(GOOSE_FUZZ_DIR)/control-$*/unit $(GOOSE_FUZZ_DIR)/control-$*/leg
	@if ./build/conformance-harness wire-fuzz --builder --driver "$(GOOSE_FUZZ_DIR)/control-$*/leg/wirefuzz tblw1 builder" \
		--seed $(GOOSE_FUZZ_SEED) --n $(GOOSE_FUZZ_N) --failed $(GOOSE_FUZZ_DIR)/control-$*/failed.bin \
		>$(GOOSE_FUZZ_DIR)/control-$*/log 2>&1; then \
		cat $(GOOSE_FUZZ_DIR)/control-$*/log; \
		echo "NEGATIVE CONTROL FAILED: the $* sabotage left the differential green"; exit 1; fi
	@grep -Eq 'harness: FAILED after .*: read acceptance differs: the leg says true, the oracle says false' $(GOOSE_FUZZ_DIR)/control-$*/log || { \
		cat $(GOOSE_FUZZ_DIR)/control-$*/log; \
		echo "NEGATIVE CONTROL FAILED: the differential went red, but not on acceptance"; exit 1; }
	@echo "goose wire-fuzz $* negative control: $$(grep -m1 'harness: FAILED' $(GOOSE_FUZZ_DIR)/control-$*/log)"

# THE SHARED HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md). make/goose-hostile.sh
# generates every schema the manifest names for goose, runs
# test/goose-hostile/runner.goose once per unit over the raw inputs and holds
# every verdict and reason to the engine's; an accept's value is held by its
# re-saved wire, which must be the input byte for byte where the input is the
# engine's own encode, and otherwise by its canonical text's digest and the
# engine's encode of that text (make/goose-hostile.sh). The port reads the `wire` carrier today; the
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

# THE TEXT-LIST TOTAL's control (docs/SPEC-TABLES.md §2.13): the total and the
# list's count bound taken out of the generated load, so a list past either
# keeps every element and the strict read lands it; the corpus must go red on
# the two wire rows past a total, wire-text-total-past and
# wire-text-total-and-count, and on nothing else. Both are taken out because
# wire-text-total-and-count is past its [..4] bound as well as its total, and
# under strict-only (docs/plans/strict-only.md) either refuses it.
.PHONY: schema-goose-runs-hostile-corpus-total-negative-control
schema-goose-runs-hostile-corpus-total-negative-control: $(GOOSE_FUZZ_SCHEMA)
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@mkdir -p $(GOOSE_FUZZ_DIR)
	@if SCHEMA_GOOSE_HOSTILE_SABOTAGE='s|if d.text_total > 0 {|if d.text_total < 0 { // SABOTAGED: the total keeps nothing out|;s|} else if count > d.bound as! u64 {|} else if count > d.bound as! u64 \&\& false { // SABOTAGED: the bound keeps nothing out|' \
		sh make/goose-hostile.sh "$(GOOSE)" ./$(GOOSE_FUZZ_SCHEMA) $(GOOSE_HOSTILE_CORPUS) $(GOOSE_FUZZ_DIR)/hostile-total-plant \
		>$(GOOSE_FUZZ_DIR)/hostile-total-plant.log 2>&1; then \
		cat $(GOOSE_FUZZ_DIR)/hostile-total-plant.log; \
		echo "NEGATIVE CONTROL FAILED: a load that ignores a text list's total and bound left the corpus green"; exit 1; fi
	@test "$$(grep -c '^FAIL ' $(GOOSE_FUZZ_DIR)/hostile-total-plant.log)" = 2 && grep -q '^FAIL wire-text-total-past: ' $(GOOSE_FUZZ_DIR)/hostile-total-plant.log && grep -q '^FAIL wire-text-total-and-count: ' $(GOOSE_FUZZ_DIR)/hostile-total-plant.log || { \
		cat $(GOOSE_FUZZ_DIR)/hostile-total-plant.log; \
		echo "NEGATIVE CONTROL FAILED: the corpus went red, but not on the two wire rows past a total alone"; exit 1; }
	@echo "goose hostile-corpus total negative control: a load that ignores the total and the bound reds wire-text-total-past and wire-text-total-and-count, and only them"

# test-goose runs both (make/goose.mk) and their controls, which are seconds
# beside them; the controls run in group goose-wire of the controls leg too.
test-goose: tables-goose-wire-fuzz schema-goose-runs-hostile-corpus \
	tables-goose-wire-fuzz-negative-control schema-goose-runs-hostile-corpus-negative-control \
	schema-goose-runs-hostile-corpus-total-negative-control

# Direct protocol fixtures and runtime controls complement the full retaining
# differential above (Goose has no caller-region measure).
.PHONY: tables-goose-retain-fuzz-driver tables-goose-retain-fuzz-negative-control
tables-goose-retain-fuzz-driver:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_SLOW=1 go test -count=1 -run '^TestGooseRetaining(FuzzDriver|MapReplacement)$$' ./internal/codegen/goosetable/
tables-goose-retain-fuzz-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_SLOW=1 go test -count=1 -run '^TestGooseRetaining(FuzzNegativeControl|MapReplacementNegativeControl)$$' ./internal/codegen/goosetable/

# THE INTEGRITY TRAILER UNDER THE LEG'S OWN BUILD (docs/SPEC-TABLES.md §2.14;
# internal/codegen/goosetable/integrity_test.go). The gate builds its probes
# with `goose build -O2`, which compiles through the machine's C compiler:
# crc32c's known answers against Go's Castagnoli table, the short-body vector
# (testdata/wire/tables/fuzz-vectors/integrity_short_body_mismatch.bin)
# malformed with integrity_mismatch on every load path, and the
# featurewitnesses corpus files saved back to their own bytes.
.PHONY: tables-goose-integrity tables-goose-integrity-negative-control
tables-goose-integrity:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_SLOW=1 go test -count=1 -run '^TestGooseIntegrity(CRC32C|WireFuzzLeg)$$' ./internal/codegen/goosetable/

# ITS CONTROLS, each deterministic on any C compiler. A wrong-polynomial
# table reds the known answers and the Root round trips, and nothing else. A
# crc32c that never returns (a constant-condition loop, which no C compiler
# may assume terminates) put in one leg's generated schema_tables.goose must
# turn the harness's replay of the short-body vector red on its own stall
# watchdog.
GOOSE_FUZZ_SED_crcloop := s|crc = Crc32cTable\[((crc ^ data\[i\]) \& 0xFF) as! i64\] ^ (crc >> 8);|loop { crc ^= 1; } // SABOTAGED: crc32c never returns|
tables-goose-integrity-negative-control: $(GOOSE_FUZZ_SCHEMA) build/conformance-harness
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_SLOW=1 go test -count=1 -run '^TestGooseIntegrityCRC32CNegativeControl$$' ./internal/codegen/goosetable/
	@sh make/goose-crc-replay.sh "$(GOOSE)" ./$(GOOSE_FUZZ_SCHEMA) ./build/conformance-harness $(GOOSE_FUZZ_DIR)/control-crcloop '$(GOOSE_FUZZ_SED_crcloop)'; \
	rc=$$?; if [ $$rc -eq 0 ]; then \
		echo "NEGATIVE CONTROL FAILED: a crc32c that never returns left the replay green"; exit 1; \
	elif [ $$rc -ne 3 ]; then \
		echo "NEGATIVE CONTROL FAILED: the replay went red, but not on the driver's stall"; exit 1; fi
	@echo "goose integrity negative control: $$(grep -m1 'harness: FAILED' $(GOOSE_FUZZ_DIR)/control-crcloop/log)"

# THE BITWISE-LOOP DIAGNOSTIC, never a gate. The bitwise CRC loop in the
# shape Goose lowers `while` into, put back in one leg, is replayed against
# the short-body vector, and the C compiler `goose toolchain` reports is said
# to be affected (the replay stalls) or not. GCC 15 -O2 (-foptimize-crc)
# compiles that loop into an endless one; recorded 2026-11-02: gcc
# 15.2.0-16ubuntu1 (Ubuntu, x86-64) affected, clang 21 (macOS arm64) not.
GOOSE_FUZZ_SED_crcbitwise := s|crc = Crc32cTable\[((crc ^ data\[i\]) \& 0xFF) as! i64\] ^ (crc >> 8);|crc ^= data[i]; var b: i64 = 0; while b < 8 { if (crc \& 1) != 0 { crc = (crc >> 1) ^ 0x82F63B78; } else { crc >>= 1; } b += 1; } // SABOTAGED: the bitwise loop|
.PHONY: tables-goose-integrity-crc-diagnostic
tables-goose-integrity-crc-diagnostic: $(GOOSE_FUZZ_SCHEMA) build/conformance-harness
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@cc=$$("$(GOOSE)" toolchain | sed -n 's/^id: *//p'); \
	sh make/goose-crc-replay.sh "$(GOOSE)" ./$(GOOSE_FUZZ_SCHEMA) ./build/conformance-harness $(GOOSE_FUZZ_DIR)/diagnostic-crcbitwise '$(GOOSE_FUZZ_SED_crcbitwise)'; \
	case $$? in \
		0) echo "goose integrity crc diagnostic: compiler not affected ($$cc): the bitwise CRC loop answers";; \
		3) echo "goose integrity crc diagnostic: compiler affected ($$cc): the bitwise CRC loop never returns";; \
		*) echo "goose integrity crc diagnostic: inconclusive ($$cc):"; cat $(GOOSE_FUZZ_DIR)/diagnostic-crcbitwise/log;; \
	esac; \
	echo "goose integrity crc diagnostic: recorded 2026-11-02: gcc 15.2.0-16ubuntu1 (x86-64) affected, clang 21 (arm64) not affected"

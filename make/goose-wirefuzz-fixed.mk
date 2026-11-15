# make/goose-wirefuzz-fixed.mk — the Goose wire fuzz's FIXED ARM control.
# make/goose-fuzz.mk is the differential itself (tables-goose-wire-fuzz); its
# fixed arm reads a root the unit emits <Root>FixedLoad for through the
# generated fixed-form pair, chosen by the root (make/goose-wirefuzz-build.sh,
# conformance_fixed_fuzz; test/conformance/goose/wirefuzz.goose).
#
# THE FIXED ARM'S CONTROL: the proof that the fixed arm reaches the generated
# fixed-form reader. The strict bounds refusal is taken out of one unit's
# GENERATED <Root>FixedLoad (tblp1's ChainFixedLoad: Chain nests Link, whose
# value is ranged [0, 1000]), checked to have matched, after the unsabotaged
# leg's fixed arm is shown green at the same seed and count (so a plant cannot
# pass on an arm that is red already); that leg is rebuilt and
# the harness's fixed arm over it must go red because the leg accepted a
# mutant the oracle refused ("read acceptance differs: the leg says true, the
# oracle says false"). A leg that read a fixed root anywhere but its
# <Root>FixedLoad could not go red on this plant. Its group is goose-wire
# (make/negative-controls.d/tables-goose-wire-fuzz-fixed-negative-control.json);
# measured on 2026-11-15 at MEASURED_WALL s make wall (time -p real), with
# schema-goose-fuzz rebuilt and the harness already built.
GOOSE_FUZZ_SED_fixed := s|let why = _ChainFixedStrictRefuse(values\[k\]);|let why = _ChainFixedStrictRefuse(values[k]) * 0; // SABOTAGED: the strict bounds refusal dropped|

.PHONY: tables-goose-wire-fuzz-fixed-negative-control
tables-goose-wire-fuzz-fixed-negative-control: build/schema-goose-fuzz build/conformance-harness
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@rm -rf $(GOOSE_FUZZ_DIR)/control-fixed && mkdir -p $(GOOSE_FUZZ_DIR)/control-fixed
	@./$(GOOSE_FUZZ_SCHEMA) generate --lang goose --out $(GOOSE_FUZZ_DIR)/control-fixed/unit test/tables/P1.schema
	@sh make/goose-wirefuzz-build.sh "$(GOOSE)" $(GOOSE_FUZZ_DIR)/control-fixed/unit $(GOOSE_FUZZ_DIR)/control-fixed/clean
	@./build/conformance-harness wire-fuzz --driver "$(GOOSE_FUZZ_DIR)/control-fixed/clean/wirefuzz tblp1 fixed" \
		--seed $(GOOSE_FUZZ_SEED) --n $(GOOSE_FUZZ_N) --failed $(GOOSE_FUZZ_DIR)/control-fixed/clean-failed.bin \
		>$(GOOSE_FUZZ_DIR)/control-fixed/clean.log 2>&1 || { \
		cat $(GOOSE_FUZZ_DIR)/control-fixed/clean.log; \
		echo "NEGATIVE CONTROL FAILED: the fixed arm is red before the sabotage, so the plant proves nothing"; exit 1; }
	@sed '$(GOOSE_FUZZ_SED_fixed)' $(GOOSE_FUZZ_DIR)/control-fixed/unit/P1Table.goose >$(GOOSE_FUZZ_DIR)/control-fixed/P1Table.goose
	@grep -q SABOTAGED $(GOOSE_FUZZ_DIR)/control-fixed/P1Table.goose || { \
		echo "NEGATIVE CONTROL FAILED: the fixed sabotage patched nothing"; exit 1; }
	@mv $(GOOSE_FUZZ_DIR)/control-fixed/P1Table.goose $(GOOSE_FUZZ_DIR)/control-fixed/unit/P1Table.goose
	@sh make/goose-wirefuzz-build.sh "$(GOOSE)" $(GOOSE_FUZZ_DIR)/control-fixed/unit $(GOOSE_FUZZ_DIR)/control-fixed/leg
	@if ./build/conformance-harness wire-fuzz --driver "$(GOOSE_FUZZ_DIR)/control-fixed/leg/wirefuzz tblp1 fixed" \
		--seed $(GOOSE_FUZZ_SEED) --n $(GOOSE_FUZZ_N) --failed $(GOOSE_FUZZ_DIR)/control-fixed/failed.bin \
		>$(GOOSE_FUZZ_DIR)/control-fixed/log 2>&1; then \
		cat $(GOOSE_FUZZ_DIR)/control-fixed/log; \
		echo "NEGATIVE CONTROL FAILED: the fixed sabotage left the differential green"; exit 1; fi
	@grep -Eq 'harness: FAILED after .*: read acceptance differs: the leg says true, the oracle says false' $(GOOSE_FUZZ_DIR)/control-fixed/log || { \
		cat $(GOOSE_FUZZ_DIR)/control-fixed/log; \
		echo "NEGATIVE CONTROL FAILED: the differential went red, but not on acceptance"; exit 1; }
	@echo "goose wire-fuzz fixed negative control: $$(grep -m1 'harness: FAILED' $(GOOSE_FUZZ_DIR)/control-fixed/log)"

# the aggregate wire-fuzz control (make/goose-fuzz.mk) runs this one too
tables-goose-wire-fuzz-negative-control: tables-goose-wire-fuzz-fixed-negative-control

test-goose: tables-goose-wire-fuzz tables-goose-wire-fuzz-fixed-negative-control

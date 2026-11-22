# make/python.mk — the PYTHON leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-PYTHON.md is the port's design). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself
# at the end of this file.
#
# Scaffolded by `schema new-leg python`. The pinned interpreters and analyzer
# are make/python-toolchain.mk's; serialize.py, the bit layer, is
# make/python-serialize.mk's. Python is a built-in compiler target.

# test-python: the leg's make test half. The toolchain and serialize.py
# (python-toolchain-check, its control, python-serialize-test) are
# prerequisites from their own fragments; here the emitter's package, which
# generates into temp directories and runs the generated package under both
# pinned interpreters and the analyzer (internal/codegen/python/pyrun), and
# the target through the compiler's driver in the tagged build.
.PHONY: test-python update-goldens-python python-packet-core-negative-control python-table-core-negative-control python-table-maps-negative-control python-vt-widening python-vt-widening-negative-control
test-python: python-ci-support
	$(MAKE) --no-print-directory python-ci-packages
	$(MAKE) --no-print-directory python-ci-compiler

.PHONY: python-ci-support python-ci-packages python-ci-compiler python-ci-core python-ci-finite python-ci-close python-ci-defaults python-ci-hosted python-ci-vt-widening
PYTHON_CI_SUPPORT_GROUPS += python-ci-core python-ci-finite python-ci-close python-ci-defaults python-ci-hosted python-ci-vt-widening python-ci-compiler-coverage python-ci-map-bench-current python-ci-map-bench-floor python-ci-map-bench-coverage
PYTHON_PACKET_CORE_SHARDS := $(addprefix python-ci-packet-core-,0 1 2 3 4 5 6 7)
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_PACKET_CORE_SHARDS) python-ci-packet-core-coverage
python-ci-support: $(PYTHON_CI_SUPPORT_GROUPS)
.PHONY: python-ci-support-list
python-ci-support-list:
	@printf '%s\n' $(PYTHON_CI_SUPPORT_GROUPS) python-ci-compiler
python-ci-core: python-toolchain-check python-toolchain-negative-control python-table-core-negative-control
python-ci-packages:
	SCHEMA_SLOW=1 go test -timeout 0 ./internal/codegen/python/... ./internal/codegen/pythontable/... -count=1
python-ci-compiler:
	sh test/slowgate/proof test-python-compiler 'TestPythonTargetIsRegistered TestPythonGeneratesAnEmptyUnitThroughTheDriver TestPythonCarriesThePacketCoreThroughTheDriver TestNewLegPythonRecipe' ./compiler -run 'Python' -skip '^TestPythonRequiredPublicShapes$$' -count=1
python-ci-compiler-coverage:
	sh test/slowgate/proof python-ci-compiler-coverage 'TestPythonCompilerCIShardCoverage' ./internal/ci -run '^TestPythonCompilerCIShardCoverage$$' -count=1
# Planner-selected compiler shards retain run-proof, including the four
# named gates of the aggregate. New matching tests are discovered by Go.
python-ci-compiler-shard-%:
	sh tools/python-ci-compiler-shard.sh '$*'
update-goldens-python: ;

# python-packet-core-negative-control: the packet core's own control. It edits
# a COPY of a generated reader and requires TestPythonPacketCoreNegativeControl
# to find the write-back marker red. It is a Go test under the slow gate,
# because the probe runs under both pinned interpreters.
python-packet-core-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run TestPythonPacketCoreNegativeControl ./internal/codegen/python/

# packet-finite-python: test/packet-finite, SPEC §4.3's finite floats, on the
# Python packet wire. test/packet-finite/python/main.py runs every bare-float
# position of its Finite.schema under both pinned interpreters: a NaN or an
# infinity is refused on write as InvalidValue naming the field and on read by
# its raw bits as Refused (non_finite_float); -0, the subnormals and the
# largest finite values ride bit exact.
#
# packet-finite-python-negative-control: TestPythonPacketFloats/negative-control
# removes the finite rule from a COPY of the generated tree, once per
# direction (the bit layer's read checks; the field validators' and the bit
# layer's write checks), and requires the driver to fail exactly the vectors
# in test/packet-finite/python/<direction>.expected; beside them it cuts the
# compressed float's range check from a copy of a generated validator and
# requires the contract probe to name exactly the vectors that check holds,
# and cuts the fixed point read's raw span check from a copy of the bit layer
# and requires the probe to name exactly the headroom reads.
.PHONY: packet-finite-python packet-finite-python-negative-control
python-ci-finite: packet-finite-python
packet-finite-python: build/schema-python python-toolchain-check
	@rm -rf build/packet-finite/python && mkdir -p build/packet-finite/python
	./build/schema-python generate --lang python --out build/packet-finite/python/generated test/packet-finite/python/Finite.schema
	$(PYTHON) -W error -I -B test/packet-finite/python/main.py build/packet-finite/python/generated
	$(PYTHON_FLOOR) -W error -I -B test/packet-finite/python/main.py build/packet-finite/python/generated

packet-finite-python-negative-control: python-toolchain-check
	sh test/slowgate/proof python-packet-finite-negative-control 'TestPythonPacketFloats/negative-control' -count=1 -timeout 600s -run 'TestPythonPacketFloats/negative-control' ./internal/codegen/python/

# build/schema-python is the compiler the conformance tree is generated with.
# The tag remains accepted by existing leg gates; the target is built in.
build/schema-python: $(shell find compiler internal cmd ir -name '*.go' 2>/dev/null) $(EMBED_SOURCES)
	go build -o $@ ./cmd/schema
# The runtime the compiler embeds and emits verbatim, and SERIALIZE_PIN beside
# it: a vendored serialize.py bump changes both, and either must rebuild the
# compiler and so build/conformance-python, or a stale runtime can pass.
# TestConformanceCompilersRebuildOnTheirRuntime holds it.
build/schema-python: $(shell find internal/codegen/python/runtime -type f 2>/dev/null)

# build/conformance-python is the tables manifest's units generated for
# python, one directory per unit key. A unit the target refuses leaves its
# refusal, by name, in <unit>.refused instead of a directory — listed, never
# dropped. Every surface reads PYTHON_CONFORMANCE_GENERATED, default this tree.
build/conformance-python: build/schema-python testdata/conformance/tables/MANIFEST.txt make/python.mk internal/codegen/python/runtime/SERIALIZE_PIN $(CONFORMANCE_UNIT_SCHEMAS)
	@rm -rf $@ && mkdir -p $@
	@awk '$$1 == "unit" { key = $$2; $$1 = ""; $$2 = ""; print key $$0 }' testdata/conformance/tables/MANIFEST.txt | \
	while read -r key paths; do \
		first=""; for p in $$paths; do if [ -d "$$p" ]; then f=$$(ls "$$p"/*.schema 2>/dev/null | sed -n 1p); else f=$$p; fi; first=$$f; break; done; \
		pkg=$$(sed -n 's/^[[:space:]]*package[[:space:]]\+\([A-Za-z_][A-Za-z0-9_]*\).*/\1/p' "$$first" | sed -n 1p); \
		if [ -n "$$pkg" ]; then mkdir -p $@/$$key/$$pkg/_schema; fi; \
		if ./build/schema-python generate --lang python --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			rm -rf $@/$$key; mv $@/$$key.log $@/$$key.refused; \
			echo "conformance-python: $$key refused: $$(head -1 $@/$$key.refused)"; \
		fi; \
	done
	@echo "conformance-python: $$(ls -d $@/*/ 2>/dev/null | wc -l | tr -d " ") units generated, $$(ls $@/*.refused 2>/dev/null | wc -l | tr -d " ") refused by name"
	# Framing exercises form 1 for Cfg, whose source also declares a fixed class.
	@test "$$(grep -c '^fixed table Cfg$$' test/tables/R1.schema)" = 1
	@test "$$(grep -c '^package tblr1$$' test/tables/R1.schema)" = 1
	@mkdir -p $@/tblr1/_framing/source $@/tblr1/_framing/tblr1
	@sed -e 's/^fixed table Cfg$$/table Cfg/' -e 's/^package tblr1$$/package tblr1_framing/' test/tables/R1.schema > $@/tblr1/_framing/source/R1.schema
	./build/schema-python generate --lang python --out $@/tblr1/_framing/tblr1 $@/tblr1/_framing/source/R1.schema

# The table core's negative control: the generated runtime's form byte is
# moved in a copy, and the interpreter probe must go red by name. It needs the
# pins, so test-python depends on it and it is registered in
# make/negative-controls.json as a gate test-python runs.
python-table-core-negative-control:
	SCHEMA_SLOW=1 go test -run 'TestPythonTableCoreNegativeControl' ./internal/codegen/pythontable/ -count=1

# The map cell's negative controls: the map runtime's writer sort is removed,
# then the reader's duplicate check, then its descending check, then the
# ascending order the save numbers a map's nodes in, each in a copy, and the
# pinned probe must go red under each.
python-table-maps-negative-control:
	SCHEMA_SLOW=1 go test -run 'TestPythonVtMapsNegativeControl' ./internal/codegen/pythontable/ -count=1

# The widening cell: TestPythonVtWidening holds every manifest report row the
# Go engine widens (the field, union arm, array element and map key sites) and
# its sibling refusals under both pinned interpreters, and refuses to skip a
# row the engine widens. The negative control plants the kind-pair ladder, then
# the field, arm and map widened counters, in a COPY of the emitted runtime and
# requires its rows red by name.
python-ci-vt-widening: python-vt-widening python-vt-widening-negative-control
python-vt-widening: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonVtWidening$$' ./internal/codegen/pythontable/
python-vt-widening-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonVtWideningNegativeControl$$' ./internal/codegen/pythontable/

generated/bench/tables/python/.stamp: ;

TEST_LEGS             += test-python
CONFORMANCE_LEGS      += $(call unless_skipped,python,build/conformance-python)
BENCH_TABLES_LEGS     += generated/bench/tables/python/.stamp
GOLDENS_LEGS          += update-goldens-python

# The packet close gate covers every packet wire pin, including framed and
# shared-stream multi-root pins, and analyzes the complete generated corpus.
.PHONY: python-packet-close python-packet-close-negative-control python-packet-analyzer python-packet-analyzer-negative-control
python-packet-close: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonPacketClose$$' ./internal/codegen/python/
python-packet-close-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonPacketCloseNegativeControl$$' ./internal/codegen/python/
python-packet-analyzer: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonPacketCloseAnalyzer$$' ./internal/codegen/python/
python-packet-analyzer-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonPacketCloseAnalyzerNegativeControl$$' ./internal/codegen/python/
python-ci-close: python-packet-close python-packet-close-negative-control python-packet-analyzer python-packet-analyzer-negative-control

.PHONY: packet-defaults-python packet-defaults-python-negative-control
packet-defaults-python: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonPacketDefaults$$' ./internal/codegen/python/
packet-defaults-python-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonPacketDefaultsNegativeControl$$' ./internal/codegen/python/
python-ci-defaults: packet-defaults-python packet-defaults-python-negative-control

generated/bench/python/.stamp: bin/schema $(SCHEMAS_BENCH)
	@mkdir -p generated/bench/python
	./bin/schema generate --lang python --out generated/bench/python bench/corpus/Bench.schema
	@touch $@

.PHONY: bench-python-gate bench-python-negative-control
bench-python-gate: generated/bench/python/.stamp python-toolchain-check
	PYTHON=$(PYTHON) bench/python/run --gate
	PYTHON=$(PYTHON_FLOOR) bench/python/run --gate
bench-python-negative-control: generated/bench/python/.stamp python-toolchain-check
	PYTHON=$(PYTHON) bench/python/negative-control
	PYTHON=$(PYTHON_FLOOR) bench/python/negative-control

# Pin the complete generated packet packages, including their vendored runtime.
generated/python/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang python --out generated/python examples
	@touch $@
generated/python-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang python --out generated/python-ludicrous examples128
	@touch $@

.PHONY: python-hosted-packet-text python-hosted-packet-text-negative-control
python-hosted-packet-text: python-toolchain-check
	sh test/slowgate/proof python-hosted-packet-text 'TestPythonHostedPacketText/current TestPythonHostedPacketText/floor' -count=1 -timeout 600s -run '^TestPythonHostedPacketText$$' ./internal/codegen/pythontable/
python-hosted-packet-text-negative-control: python-toolchain-check
	sh test/slowgate/proof python-hosted-packet-text-negative-control 'TestPythonHostedPacketTextNegativeControl/current TestPythonHostedPacketTextNegativeControl/floor' -count=1 -timeout 600s -run '^TestPythonHostedPacketTextNegativeControl$$' ./internal/codegen/pythontable/
python-ci-hosted: python-hosted-packet-text python-hosted-packet-text-negative-control

# Run every packet pin on both interpreters, partitioned for per-commit CI.
.PHONY: $(PYTHON_PACKET_CORE_SHARDS)
$(PYTHON_PACKET_CORE_SHARDS): python-ci-packet-core-%:
	SCHEMA_SLOW=1 PYTHON_PACKET_CORE_SHARD=$* go test -count=1 -timeout 600s -run '^TestPythonPacketCore$$' ./internal/codegen/python/

.PHONY: python-ci-packet-core-coverage
python-ci-packet-core-coverage:
	SCHEMA_SLOW=1 go test -count=1 -run '^TestPythonPacketCoreCIPlan$$' ./internal/ci/
.PHONY: python-ci-map-bench-current python-ci-map-bench-floor python-ci-map-bench-coverage
python-ci-map-bench-current python-ci-map-bench-floor: python-ci-map-bench-%:
	SCHEMA_SLOW=1 PYTHON_MAP_BENCH_PIN=$* go test -v -count=1 -timeout 600s -run '^TestPythonMapShapeBench$$' ./internal/codegen/pythontable/
python-ci-map-bench-coverage:
	SCHEMA_SLOW=1 go test -count=1 -run '^TestPythonMapBenchCIPlan$$' ./internal/ci/

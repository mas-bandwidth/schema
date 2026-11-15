# make/python.mk — the PYTHON leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-PYTHON.md is the port's design). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself
# at the end of this file.
#
# Scaffolded by `schema new-leg python`. The pinned interpreters and analyzer
# are make/python-toolchain.mk's; serialize.py, the bit layer, is
# make/python-serialize.mk's. The target is behind the schema_leg_python build
# tag until the packet layer (compiler/target_python.go), so its tests build
# the compiler with the tag.

# test-python: the leg's make test half. The toolchain and serialize.py
# (python-toolchain-check, its control, python-serialize-test) are
# prerequisites from their own fragments; here the emitter's package, which
# generates into temp directories and runs the generated package under both
# pinned interpreters and the analyzer (internal/codegen/python/pyrun), and
# the target through the compiler's driver in the tagged build.
.PHONY: test-python update-goldens-python python-packet-core-negative-control python-table-core-negative-control python-table-maps-negative-control
test-python: python-toolchain-check python-toolchain-negative-control python-table-core-negative-control
	SCHEMA_SLOW=1 go test -tags schema_leg_python ./internal/codegen/python/... ./internal/codegen/pythontable/... -count=1
	sh test/slowgate/proof test-python-compiler 'TestPythonTargetIsRegistered TestPythonGeneratesAnEmptyUnitThroughTheDriver TestPythonCarriesThePacketCoreThroughTheDriver TestNewLegPythonRecipe' -tags schema_leg_python ./compiler -run 'Python' -count=1
update-goldens-python: ;

# python-packet-core-negative-control: the packet core's own control. It edits
# a COPY of a generated reader and requires TestPythonPacketCoreNegativeControl
# to find the write-back marker red. It is a Go test under the slow gate,
# because the probe runs under both pinned interpreters.
python-packet-core-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketCoreNegativeControl ./internal/codegen/python/

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
test-python: packet-finite-python
packet-finite-python: build/schema-python python-toolchain-check
	@rm -rf build/packet-finite/python && mkdir -p build/packet-finite/python
	./build/schema-python generate --lang python --out build/packet-finite/python/generated test/packet-finite/python/Finite.schema
	$(PYTHON) -W error -I -B test/packet-finite/python/main.py build/packet-finite/python/generated
	$(PYTHON_FLOOR) -W error -I -B test/packet-finite/python/main.py build/packet-finite/python/generated

packet-finite-python-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonPacketFloats/negative-control' ./internal/codegen/python/

# build/schema-python is the tagged compiler the conformance tree is generated
# with; the ordinary bin/schema does not carry the target until the packet
# layer drops the tag.
build/schema-python: $(shell find compiler internal cmd ir -name '*.go' 2>/dev/null)
	go build -tags schema_leg_python -o $@ ./cmd/schema

# build/conformance-python is the tables manifest's units generated for
# python, one directory per unit key. A unit the target refuses leaves its
# refusal, by name, in <unit>.refused instead of a directory — listed, never
# dropped. Every surface reads PYTHON_CONFORMANCE_GENERATED, default this tree.
build/conformance-python: build/schema-python testdata/conformance/tables/MANIFEST.txt make/python.mk $(CONFORMANCE_UNIT_SCHEMAS)
	@rm -rf $@ && mkdir -p $@
	@awk '$$1 == "unit" { key = $$2; $$1 = ""; $$2 = ""; print key $$0 }' testdata/conformance/tables/MANIFEST.txt | \
	while read -r key paths; do \
		first=""; for p in $$paths; do if [ -d "$$p" ]; then f=$$(ls "$$p"/*.schema 2>/dev/null | head -1); else f=$$p; fi; first=$$f; break; done; \
		pkg=$$(sed -n 's/^[[:space:]]*package[[:space:]]\+\([A-Za-z_][A-Za-z0-9_]*\).*/\1/p' "$$first" | head -1); \
		if [ -n "$$pkg" ]; then mkdir -p $@/$$key/$$pkg/_schema; fi; \
		if ./build/schema-python generate --lang python --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			rm -rf $@/$$key; mv $@/$$key.log $@/$$key.refused; \
			echo "conformance-python: $$key refused: $$(head -1 $@/$$key.refused)"; \
		fi; \
	done
	@echo "conformance-python: $$(ls -d $@/*/ 2>/dev/null | wc -l | tr -d " ") units generated, $$(ls $@/*.refused 2>/dev/null | wc -l | tr -d " ") refused by name"

# The table core's negative control: the generated runtime's form byte is
# moved in a copy, and the interpreter probe must go red by name. It needs the
# pins, so test-python depends on it and it is registered in
# make/negative-controls.json as a gate test-python runs.
python-table-core-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run 'TestPythonTableCoreNegativeControl' ./internal/codegen/pythontable/ -count=1

# The map cell's negative controls: the map runtime's writer sort is removed,
# then the reader's duplicate check, then its descending check, then the
# ascending order the save numbers a map's nodes in, each in a copy, and the
# pinned probe must go red under each.
python-table-maps-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run 'TestPythonVtMapsNegativeControl' ./internal/codegen/pythontable/ -count=1

generated/bench/tables/python/.stamp: ;

TEST_LEGS             += test-python
CONFORMANCE_LEGS      += $(call unless_skipped,python,build/conformance-python)
BENCH_TABLES_LEGS     += generated/bench/tables/python/.stamp
GOLDENS_LEGS          += update-goldens-python

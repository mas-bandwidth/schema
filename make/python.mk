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
.PHONY: test-python update-goldens-python python-packet-core-negative-control python-table-core-negative-control
test-python: python-toolchain-check python-toolchain-negative-control python-table-core-negative-control packet-utf8-python
	SCHEMA_SLOW=1 go test -tags schema_leg_python ./internal/codegen/python/... ./internal/codegen/pythontable/... -count=1
	sh test/slowgate/proof test-python-compiler 'TestPythonTargetIsRegistered TestPythonGeneratesAnEmptyUnitThroughTheDriver TestPythonCarriesThePacketCoreThroughTheDriver TestNewLegPythonRecipe' -tags schema_leg_python ./compiler -run 'Python' -count=1
update-goldens-python: ;

# python-packet-core-negative-control: the packet core's own control. It edits
# a COPY of a generated reader and requires TestPythonPacketCoreNegativeControl
# to find the write-back marker red. It is a Go test under the slow gate,
# because the probe runs under both pinned interpreters.
python-packet-core-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketCoreNegativeControl ./internal/codegen/python/

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

# ---------------------------------------------------------------- packet text
# The text kinds' own control: a copy of the generated Narrow reader and a
# copy of the runtime it carries are each planted with a fault, and
# TestPythonPacketTextNegativeControl must find the corpus verdict red by
# name. A Go test under the slow gate, as the packet core's control is.
.PHONY: python-packet-text-negative-control packet-utf8-python packet-utf8-python-negative-control
python-packet-text-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketTextNegativeControl ./internal/codegen/python/

# packet-utf8-python: the shared packet-text sweep (test/packet-text/README.md)
# against the C++ oracle: the 33 corpus rows and every bit flip of every
# accepted row, through test/packet-text/python/main.py on the current pin.
build/packet-text/python/.stamp: build/schema-python test/packet-text/Narrow.schema
	@rm -rf build/packet-text/python && mkdir -p build/packet-text/python
	./build/schema-python generate --lang python --out build/packet-text/python/generated test/packet-text/Narrow.schema
	@touch $@

packet-utf8-python: python-toolchain-check build/packet-text/python/.stamp build/packet-text/cpp/driver build/packet-text/harness
	./build/packet-text/harness $(PYTHON) -W error -I -B test/packet-text/python/main.py $(CURDIR)/build/packet-text/python/generated

# The sweep's control: the strict UTF-8 decode in a copy of the generated
# runtime is replaced by Latin-1, and the mutation-only sweep must fail its
# verdict marker; the corpus's refusal rows alone cannot satisfy it.
packet-utf8-python-negative-control: packet-utf8-python
	@rm -rf build/packet-text/python-negative && mkdir -p build/packet-text/python-negative
	cp -R build/packet-text/python/generated build/packet-text/python-negative/generated
	sed -i.orig 's/payload.decode("utf-8")/payload.decode("latin-1")/' build/packet-text/python-negative/generated/packettext/_schema/serialize/_core.py
	@if cmp -s build/packet-text/python-negative/generated/packettext/_schema/serialize/_core.py build/packet-text/python-negative/generated/packettext/_schema/serialize/_core.py.orig; then \
		echo 'NEGATIVE CONTROL FAILED: the UTF-8 decode was not found to plant over'; exit 1; fi
	@if ./build/packet-text/harness -mutations-only $(PYTHON) -W error -I -B test/packet-text/python/main.py $(CURDIR)/build/packet-text/python-negative/generated \
		> build/packet-text/python-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Python UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/python-negative/log || \
		{ cat build/packet-text/python-negative/log; exit 1; }
	@echo 'packet UTF-8 Python negative control: removed read validation fails bit-flip agreement'

generated/bench/tables/python/.stamp: ;

TEST_LEGS             += test-python
CONFORMANCE_LEGS      += $(call unless_skipped,python,build/conformance-python)
BENCH_TABLES_LEGS     += generated/bench/tables/python/.stamp
GOLDENS_LEGS          += update-goldens-python

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
# prerequisites from their own fragments; here the emitter packages, which
# generate into temp directories and run the generated package under both
# pinned interpreters and the analyzer (internal/codegen/python/pyrun), and
# the target through the compiler's driver in the tagged build.
.PHONY: test-python update-goldens-python python-table-core-negative-control
test-python: python-toolchain-check python-toolchain-negative-control
	SCHEMA_SLOW=1 go test -tags schema_leg_python ./internal/codegen/python/... ./internal/codegen/pythontable/... -count=1
	go test -tags schema_leg_python ./compiler -run 'Python' -count=1
update-goldens-python: ;

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
# moved in a copy, and the interpreter probe must go red. It needs the pins, so
# it runs beside test-python and is registered in make/negative-controls.json.
python-table-core-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run 'TestPythonTableCoreNegativeControl' ./internal/codegen/pythontable/ -count=1

generated/bench/tables/python/.stamp: ;

TEST_LEGS             += test-python
CONFORMANCE_LEGS      += $(call unless_skipped,python,build/conformance-python)
BENCH_TABLES_LEGS     += generated/bench/tables/python/.stamp
GOLDENS_LEGS          += update-goldens-python

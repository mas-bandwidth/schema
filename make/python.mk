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
.PHONY: test-python update-goldens-python python-packet-core-negative-control
test-python: python-toolchain-check python-toolchain-negative-control
	SCHEMA_SLOW=1 go test ./internal/codegen/python/... -count=1
	go test -tags schema_leg_python ./compiler -run 'Python' -count=1
update-goldens-python: ;

# python-packet-core-negative-control: the packet core's own control. It edits
# a COPY of a generated reader and requires TestPythonPacketCoreNegativeControl
# to find the write-back marker red. It is a Go test under the slow gate,
# because the probe runs under both pinned interpreters.
python-packet-core-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketCoreNegativeControl ./internal/codegen/python/

# The conformance driver answers every surface ABSENT until a layer carries
# one (test/conformance/python/driver), so the leg builds nothing for it.
build/conformance-python: ;
generated/bench/tables/python/.stamp: ;

TEST_LEGS             += test-python
CONFORMANCE_LEGS      += $(call unless_skipped,python,build/conformance-python)
BENCH_TABLES_LEGS     += generated/bench/tables/python/.stamp
GOLDENS_LEGS          += update-goldens-python

# make/moo-readiness.mk — the MOO leg's targets for its acceptance run
# (test/conformance/nightly-readiness moo). Included by the Makefile's wildcard
# include; a dashed stem, so the port registry never probes it as a --lang.
# make/moo.mk is the leg and pins the toolchain; build/schema-moo is the
# compiler with the moo target (make/moo-conformance.mk).

# THE PINNED INTERPRETER AND ANALYZER, built from the pin when either is
# missing and then probed: the readiness prerequisite.
.PHONY: moo-toolchain-ready
moo-toolchain-ready:
	@test -x "$(MOO)" && test -x "$(MOO_ANALYZE)" || $(MAKE) dist-moo
	@$(call toolchain_probe,moo,MOO,$(MOO))
	@$(call toolchain_probe,moo,MOO_ANALYZE,$(MOO_ANALYZE))

# THE HARNESS FOR THE MOO LEG: `implementation --lang moo` reads
# compiler.Carried("moo"), which every build registers now that moo is a
# built-in target (builtinImplementationLanguages, test/conformance/harness/implementation_tagged.go).
build/conformance-harness-moo: $(wildcard test/conformance/harness/*.go) $(GO_SOURCES) $(wildcard internal/codegen/moo*/runtime/*)
	@mkdir -p build
	go build -o $@ ./test/conformance/harness

# THE MESSAGE FORM ON THE MOO LEG (docs/SPEC-TABLES.md §3.3): the batch load
# and save against the manifest, the Go engine and its evolution, the
# quantizer and the batch bounds, each with its negative control
# (internal/codegen/mootable/message_test.go, message_batch_test.go).
.PHONY: tables-moo-message-form
tables-moo-message-form: moo-toolchain-ready build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -run '^TestMooMessage' ./internal/codegen/mootable/

# THE TARGET'S COMPILER CLAIMS (compiler/tablesmoo_test.go,
# compiler/tablesmoonames_test.go) under the slow gate and the pinned toolchain.
.PHONY: tables-moo-compiler-claims
tables-moo-compiler-claims: moo-toolchain-ready
	SCHEMA_SLOW=1 go test -count=1 -run 'Moo' ./compiler/

# THE WHOLE-ROSTER DRIVER's parts (test/conformance/moo/wirefuzz-driver): the
# generated units and the native host its one process runs under, both built
# by make/moo-conformance.mk and make/moo-fuzz.mk.
.PHONY: moo-wire-fuzz-ready
moo-wire-fuzz-ready: build/conformance-moo $(MOOFUZZHOST)

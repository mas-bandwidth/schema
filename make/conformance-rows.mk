# make/conformance-rows.mk — EVERY ROW FILE OF EVERY LEG'S ROWS DIRECTORY.
#
# A row under test/conformance/<leg>/rows/ is a self-contained program that
# drives one law of docs/FIXED-FORM-ALGORITHM.md through the generated code.
# The rows a leg runs ARE its rows directory (tools/conformancerows lists it):
# there is no list to copy by hand, so a row file added is a row run. Before
# this file read the directory, nine hand lists ran c 3/23, cpp 1/35, dart
# 3/41, java 2/26, rust 1/33 and elixir 1/21 rows, js and cs none, and the rows
# nothing ran rotted against the generated code.
#
# Nothing excuses a failing row: there is no ledger of rows a leg owes, so a
# row the leg cannot answer today is red, named with the cause read off its
# log. To write a leg's failures with their causes to a file, to read:
#
#   make conformance-rows-<leg> CONFORMANCE_ROWS_MEASURED=build/<leg>-failing.tsv
#
# The Go rows are TestRow functions run by go test (make/go.mk derives their
# names from the same directory), so Go is not a leg of this file.
#
# conformance-rows-<leg> runs every row (ci-fast.yml's conformance-rows job,
# one per leg, and a prerequisite of test-<leg>, the nightly certify row);
# CONFORMANCE_ROWS_SHARD=i/n runs shard i of n (every n-th row by name), for a
# person splitting a run by hand.

CONFORMANCE_ROWS_LEGS := $(filter-out go,$(patsubst test/conformance/%/rows,%,$(wildcard test/conformance/*/rows)))
CONFORMANCE_ROWS_SHARD ?= 0/1
CONFORMANCE_ROWS_MEASURED ?=

build/conformancerows: $(wildcard tools/conformancerows/*.go)
	@mkdir -p build
	go build -o $@ ./tools/conformancerows

# What each leg's rows build against: the generated trees the rows name, and
# the toolchain, passed in the environment (tools/conformancerows refuses an
# unset one by name). Each list is what the leg's rows READ and nothing more:
# the Java rows compile only the generated units they name (the runner reads
# them off the rows), and only the Dart rows read build/fixedform-corpus (R27,
# its corpus law), so no other leg builds the C++ reference dump.
CONFORMANCE_ROWS_DEPS_c      := build/tables-generated-c/.stamp build/tables-generated-c-fixed/.stamp
CONFORMANCE_ROWS_ENV_c        = CC="$(CC)" SERIALIZE_C="$(SERIALIZE_C)"
CONFORMANCE_ROWS_DEPS_cpp    := build/tables-generated/.stamp
CONFORMANCE_ROWS_ENV_cpp      = CXX="$(CXX)" SERIALIZE="$(SERIALIZE)"
CONFORMANCE_ROWS_DEPS_cs     := toolchain-cs sibling-cs build/tables-generated-cs/.stamp
CONFORMANCE_ROWS_ENV_cs       = DOTNET="$(DOTNET)" SERIALIZE_CS="$(abspath $(SERIALIZE_CS))"
CONFORMANCE_ROWS_DEPS_dart   := toolchain-dart build/tables-generated-dart/.stamp build/dart-fixed/.stamp build/fixedform-corpus/.stamp
CONFORMANCE_ROWS_ENV_dart     = DART="$(DART)"
CONFORMANCE_ROWS_DEPS_elixir := toolchain-elixir build/elixir-tables-ebin/.stamp build/elixir-fixedform/.stamp
CONFORMANCE_ROWS_ENV_elixir   = ELIXIR='$(ELIXIR)'
CONFORMANCE_ROWS_DEPS_java   := toolchain-java build/tables-generated-java/.stamp
CONFORMANCE_ROWS_ENV_java     = JAVA="$(JAVA)" JAVAC="$(JAVAC)"
CONFORMANCE_ROWS_DEPS_js     := build/tables-generated-js/.stamp build/js-fixed/.stamp
CONFORMANCE_ROWS_ENV_js       = NODE="$(NODE)"
CONFORMANCE_ROWS_DEPS_rust   := bin/schema build/tables-generated-rust/.stamp
CONFORMANCE_ROWS_ENV_rust     = CARGO='PATH="$(RUSTUP_BIN):$$PATH" cargo' SCHEMA="$(CURDIR)/bin/schema"

define conformance_rows_rule
.PHONY: conformance-rows-$(1) verify-$(1)-rows
conformance-rows-$(1): build/conformancerows $$(CONFORMANCE_ROWS_DEPS_$(1))
	$$(CONFORMANCE_ROWS_ENV_$(1)) ./build/conformancerows -leg $(1) -shard $$(CONFORMANCE_ROWS_SHARD) \
		$$(if $$(CONFORMANCE_ROWS_MEASURED),-measured $$(CONFORMANCE_ROWS_MEASURED))
verify-$(1)-rows: conformance-rows-$(1)
test-$(1): conformance-rows-$(1)
endef
$(foreach leg,$(CONFORMANCE_ROWS_LEGS),$(eval $(call conformance_rows_rule,$(leg))))

# ci-fast.yml's conformance-rows-elixir job takes the generated corpus
# compiled strictly by lang-clean's agreement shards (make/elixir.mk
# build-conformance-elixir-from-shards verifies each shard's identity and
# compiles the driver) instead of compiling it again, and runs the rows on it
# with -o build/elixir-tables-ebin/.stamp.
.PHONY: conformance-rows-elixir-ebin-from-shards
conformance-rows-elixir-ebin-from-shards: build-conformance-elixir-from-shards
	rm -rf build/elixir-tables-ebin
	cp -R $(ELIXIR_HANDOFF_EBIN) build/elixir-tables-ebin
	touch build/elixir-tables-ebin/.stamp

# The standard appendix's small executable witnesses use separate fast CI
# rows so their toolchain compiles do not extend the conformance jobs.
.PHONY: appendix-java-announcement-budgets appendix-go-fixed appendix-python-fixed
appendix-java-announcement-budgets:
	sh test/slowgate/proof $@ 'TestAnnouncementBudgetsJava' \
		-count=1 ./internal/codegen/javatable -run '^TestAnnouncementBudgetsJava$$'

appendix-go-fixed:
	sh test/slowgate/proof $@ 'TestGoFixedLoadCapacityWitness TestDeclaredDeprecatedEnumCook' \
		-count=1 ./internal/codegen/gotable -run '^Test(GoFixedLoadCapacityWitness|DeclaredDeprecatedEnumCook)$$'

appendix-python-fixed:
	sh test/slowgate/proof $@ 'TestPythonFixedLoadCapacityWitness' \
		-count=1 ./internal/codegen/pythontable -run '^TestPythonFixedLoadCapacityWitness$$'

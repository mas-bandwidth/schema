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
# A row the leg cannot answer today is named, with its measured cause, in
# test/conformance/<leg>/rows-owed.tsv, `<row><TAB>owed: <cause>`, and the
# ledger is an EXACT SET: a failing row it does not name is red, and a row it
# names that now passes is red until its line comes off. An owed row still
# runs every time, so the ledger cannot outlive its failure. To measure a
# leg's failures as ledger lines (to read, never to paste unread):
#
#   make conformance-rows-<leg> CONFORMANCE_ROWS_MEASURED=build/<leg>-owed.tsv
#
# The Go rows are TestRow functions run by go test (make/go.mk derives their
# names from the same directory), so Go is not a leg of this file.
#
# conformance-rows-<leg> runs every row: ci-full.yml's nightly
# conformance-rows job runs it once per leg, and it is a prerequisite of
# test-<leg>, the nightly certify row. No per-PR job runs a whole leg: each
# leg took 80-203 s on a loaded bench, past the per-PR two minutes, and a leg
# comes back per-PR only once it fits in 60 s on a CI runner
# (internal/ci/conformance_rows_nightly_test.go holds this). The ten rows the
# per-PR verify targets ran before run there still (THE PER-PR ROWS, below).
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
CONFORMANCE_ROWS_DEPS_dart   := toolchain-dart build/tables-generated-dart/.stamp build/dart-fixed/.stamp build/hb-dart/.stamp build/fixedform-corpus/.stamp
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

# THE PER-PR ROWS. Before the rows ran nightly, five per-PR verify targets
# ran ten rows by hand list, and those ten still run on every pull request
# that touches their leg (ci-fast.yml's verify-leg job), through the same
# runner, ledger and cause match as the nightly run (-rows), each against a
# tree holding only the units its rows read:
#
#   c     F4 F10 F12   V1 and K1, generated here     _verify-c-conformance
#   cpp   R15          build/tables-generated        _verify-cpp-conformance
#   dart  E8 R8 R13    build/tables-generated-dart   _verify-dart-packets
#   java  F10 R8       V1, generated here            _verify-java-conformance
#   rust  F8           the block unit, generated     _verify-rust-conformance
#
# (the C++ and Dart rows name their tree's path in their own source, and the
# verify targets build those trees already). A row comes off a list only by
# changing internal/ci/conformance_rows_pr_test.go, which reads make's own
# database and fails when a verify target loses its rows target or a list
# loses one of these rows.
CONFORMANCE_ROWS_PR_DIR := build/conformance-rows-pr
CONFORMANCE_ROWS_PR_c    := F4 F10 F12
CONFORMANCE_ROWS_PR_cpp  := R15
CONFORMANCE_ROWS_PR_dart := E8 R8 R13
CONFORMANCE_ROWS_PR_java := F10 R8
CONFORMANCE_ROWS_PR_rust := F8
CONFORMANCE_ROWS_PR_LEGS := c cpp dart java rust

CONFORMANCE_ROWS_PR_DEPS_c    := $(CONFORMANCE_ROWS_PR_DIR)/c/gen/.stamp
CONFORMANCE_ROWS_PR_GEN_c     := $(CONFORMANCE_ROWS_PR_DIR)/c/gen
CONFORMANCE_ROWS_PR_DEPS_cpp  := build/tables-generated/.stamp
CONFORMANCE_ROWS_PR_DEPS_dart := toolchain-dart build/tables-generated-dart/.stamp
CONFORMANCE_ROWS_PR_DEPS_java := toolchain-java $(CONFORMANCE_ROWS_PR_DIR)/java/gen/.stamp
CONFORMANCE_ROWS_PR_GEN_java  := $(CONFORMANCE_ROWS_PR_DIR)/java/gen
CONFORMANCE_ROWS_PR_DEPS_rust := bin/schema $(CONFORMANCE_ROWS_PR_DIR)/rust/gen/.stamp
CONFORMANCE_ROWS_PR_GEN_rust  := $(CONFORMANCE_ROWS_PR_DIR)/rust/gen

# F4 and F10 read V1's Cell, F12 reads K1's Root. Each generate writes its
# unit's every file, so a rerun leaves nothing stale.
$(CONFORMANCE_ROWS_PR_DIR)/c/gen/.stamp: bin/schema test/tables/V1.schema test/tables/K1.schema make/conformance-rows.mk
	@mkdir -p $(@D)
	./bin/schema generate --lang c --out $(@D)/v1 test/tables/V1.schema
	./bin/schema generate --lang c --out $(@D)/k1 test/tables/K1.schema
	@touch $@

# F10 and R8 read V1's package tblv1.
$(CONFORMANCE_ROWS_PR_DIR)/java/gen/.stamp: bin/schema test/tables/V1.schema make/conformance-rows.mk
	@mkdir -p $(@D)
	./bin/schema generate --lang java --out $(@D)/v1 test/tables/V1.schema
	@touch $@

# F8 reads the block unit's PaddedRow, the crate blockdemo.
$(CONFORMANCE_ROWS_PR_DIR)/rust/gen/.stamp: bin/schema $(wildcard tables/block/*.schema) make/conformance-rows.mk
	@mkdir -p $(@D)/blockdemo
	./bin/schema generate --lang rust --out $(@D)/blockdemo/src tables/block
	@printf '[package]\nname = "blockdemo"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "$(abspath $(SERIALIZE_RS))" }\n' \
		> $(@D)/blockdemo/Cargo.toml
	@touch $@

define conformance_rows_pr_rule
.PHONY: conformance-rows-pr-$(1)
conformance-rows-pr-$(1): build/conformancerows $$(CONFORMANCE_ROWS_PR_DEPS_$(1))
	$$(CONFORMANCE_ROWS_ENV_$(1)) ./build/conformancerows -leg $(1) -rows "$$(CONFORMANCE_ROWS_PR_$(1))" \
		-work $(CONFORMANCE_ROWS_PR_DIR)/$(1)/work $$(if $$(CONFORMANCE_ROWS_PR_GEN_$(1)),-gen $$(CONFORMANCE_ROWS_PR_GEN_$(1)))
endef
$(foreach leg,$(CONFORMANCE_ROWS_PR_LEGS),$(eval $(call conformance_rows_pr_rule,$(leg))))

# The standard appendix's small executable witnesses use separate fast CI
# rows so their toolchain compiles do not extend the conformance jobs.
.PHONY: appendix-java-announcement-budgets appendix-go-fixed appendix-python-fixed
appendix-java-announcement-budgets:
	sh test/slowgate/proof $@ 'TestAnnouncementBudgetsJava TestAnnouncementEntryBudgetJava' \
		-count=1 ./internal/codegen/javatable -run '^Test(AnnouncementBudgetsJava|AnnouncementEntryBudgetJava)$$'

.PHONY: announcement-entry-budget-cs announcement-entry-budget-unity \
	announcement-entry-budget-lua announcement-entry-budget-luau announcement-entry-budget-moo announcement-entry-budget-squirrel
announcement-entry-budget-cs: toolchain-cs
	sh test/slowgate/proof $@ 'TestAnnouncementEntryBudgetCSharp' \
		-count=1 ./internal/codegen/cstable -run '^TestAnnouncementEntryBudgetCSharp$$'

announcement-entry-budget-unity: toolchain-unity
	sh test/slowgate/proof $@ 'TestAnnouncementEntryBudgetUnity' \
		-count=1 ./internal/codegen/unitytable -run '^TestAnnouncementEntryBudgetUnity$$'

announcement-entry-budget-lua: verify-toolchain-lua
	sh test/slowgate/proof $@ 'TestAnnouncementEntryBudget' \
		-count=1 ./internal/codegen/luatable -run '^TestAnnouncementEntryBudget$$'

announcement-entry-budget-luau: verify-toolchain-luau
	sh test/slowgate/proof $@ 'TestAnnouncementEntryBudget' \
		-count=1 ./internal/codegen/luautable -run '^TestAnnouncementEntryBudget$$'

announcement-entry-budget-moo: verify-toolchain-moo
	sh test/slowgate/proof $@ 'TestAnnouncementEntryBudget' \
		-count=1 ./internal/codegen/mootable -run '^TestAnnouncementEntryBudget$$'

announcement-entry-budget-squirrel: verify-toolchain-squirrel
	sh test/slowgate/proof $@ 'TestAnnouncementEntryBudget' \
		-count=1 ./internal/codegen/squirreltable -run '^TestAnnouncementEntryBudget$$'

appendix-go-fixed:
	sh test/slowgate/proof $@ 'TestGoFixedLoadCapacityWitness TestDeclaredDeprecatedEnumCook' \
		-count=1 ./internal/codegen/gotable -run '^Test(GoFixedLoadCapacityWitness|DeclaredDeprecatedEnumCook)$$'

appendix-python-fixed:
	sh test/slowgate/proof $@ 'TestPythonFixedLoadCapacityWitness' \
		-count=1 ./internal/codegen/pythontable -run '^TestPythonFixedLoadCapacityWitness$$'

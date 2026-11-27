# THE C# BLOCK PAIR COMPILES ON ITS OWN, on every pull request that touches the
# C# generator (.github/nightly-fast-rows.txt, rows cs-block-compile and
# cs-block-compile-negative-control).
#
# <Base>Block.cs and <Base>Cook.cs build together and with no table source
# (docs/SPEC-TABLES.md §19.2, §7): a consumer that reads blocks and cooks
# compiles those two and the unit's packet sources, never <Base>Table.cs.
# test/cs-block is that consumer, under TreatWarningsAsErrors, for
# tables/examples (no table source at all) and for the two block units beside
# theirs. Its only run was the nightly tables-block-fuzz, so a cook row that
# named a table-only union's tag enum (#4843) and block Begin overloads that
# took the table storage class (#4854) left main unable to compile it, and no
# pull request saw it. This is the build alone, over the three units it
# compiles rather than the whole corpus, so it fits the two-minute row: on
# intel under load (2026-11-24) about 20 s to generate and 50 s to build.
#
# The negative control puts BOTH defects back into the emitter through
# `go build -overlay` (no tracked file is edited) and requires the same build
# to fail on each of them by name.

CS_BLOCK_COMPILE := build/cs-block-compile
CS_BLOCK_COMPILE_UNITS := examples block blockhome

# $(1) the compiler, $(2) the output root, $(3) the dotnet artifacts path
define cs_block_compile
	@rm -rf $(2) $(3) && mkdir -p $(2)
	@set -e; for d in $(CS_BLOCK_COMPILE_UNITS); do \
		$(1) generate --lang cs --out $(2)/$$d tables/$$d 2>$(2)/$$d.warnings || \
			{ cat $(2)/$$d.warnings; echo "cs-block-compile: generating tables/$$d failed"; exit 1; }; \
	done
	cd test/cs-block && $(DOTNET) build -nologo -v q --artifacts-path ../../$(3) \
		-p:BlockGeneratedDir=../../$(2)/block \
		-p:BlockHomeGeneratedDir=../../$(2)/blockhome \
		-p:BlockExamplesGeneratedDir=../../$(2)/examples
endef

.PHONY: tables-cs-block-compile
tables-cs-block-compile: bin/schema
ifneq ($(filter cs,$(SKIPPED_LEGS)),)
	@echo "tables-cs-block-compile: FAILED, SCHEMA_SKIP_LEGS names the cs leg and this gate has no other form"; exit 1
endif
	$(call cs_block_compile,./bin/schema,$(CS_BLOCK_COMPILE)/gen,$(CS_BLOCK_COMPILE)/artifacts)
	@echo "cs-block-compile: the C# block and cook pair of tables/examples, block and blockhome compiles with no table source"

.PHONY: tables-cs-block-compile-negative-control
tables-cs-block-compile-negative-control:
ifneq ($(filter cs,$(SKIPPED_LEGS)),)
	@echo "tables-cs-block-compile-negative-control: FAILED, SCHEMA_SKIP_LEGS names the cs leg and this control has no other form"; exit 1
endif
	@mkdir -p $(CS_BLOCK_COMPILE)
	@sed 's|if _, tableOnly := g.unit.TableUnions\[un.Name\]; tableOnly {|if false { // PLANTED: the row names the table-only tag enum again|' \
		internal/codegen/cstable/cookunion.go > $(CS_BLOCK_COMPILE)/cookunion.gotext
	@grep -q PLANTED $(CS_BLOCK_COMPILE)/cookunion.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the cook-row plant patched nothing"; exit 1; }
	@sed 's|^\([[:space:]]*\)g\.hf("    public ref %sBlockProjection WritableProjection|\1g.emitBlockBuildValue(bl) /* PLANTED: the value overloads back in the handle */; g.hf("    public ref %sBlockProjection WritableProjection|' \
		internal/codegen/cstable/blockbuild.go > $(CS_BLOCK_COMPILE)/blockbuild.gotext
	@grep -q PLANTED $(CS_BLOCK_COMPILE)/blockbuild.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the Begin-overload plant patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cstable/cookunion.go":"%s/$(CS_BLOCK_COMPILE)/cookunion.gotext","%s/internal/codegen/cstable/blockbuild.go":"%s/$(CS_BLOCK_COMPILE)/blockbuild.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > $(CS_BLOCK_COMPILE)/overlay.json
	go build -overlay=$(CS_BLOCK_COMPILE)/overlay.json -o $(CS_BLOCK_COMPILE)/schema-planted ./cmd/schema
	@if $(MAKE) --no-print-directory cs-block-compile-planted > $(CS_BLOCK_COMPILE)/planted.log 2>&1; then \
		cat $(CS_BLOCK_COMPILE)/planted.log; \
		echo "NEGATIVE CONTROL FAILED: the C# block pair compiled with both defects planted"; exit 1; \
	fi
	@grep -q "error CS0234: The type or namespace name 'MinPickType' does not exist" $(CS_BLOCK_COMPILE)/planted.log || \
		{ cat $(CS_BLOCK_COMPILE)/planted.log; echo "NEGATIVE CONTROL FAILED: the build did not fail on the cook row naming MinPickType"; exit 1; }
	@grep -q "error CS0246: The type or namespace name 'TeamConfig' could not be found" $(CS_BLOCK_COMPILE)/planted.log || \
		{ cat $(CS_BLOCK_COMPILE)/planted.log; echo "NEGATIVE CONTROL FAILED: the build did not fail on a Begin overload naming TeamConfig"; exit 1; }
	@echo "cs-block-compile negative control: the cook row naming a table-only tag enum and a Begin overload naming a table storage class each fail the build"

# the planted half of the control, on its own tree; not a target to run alone
.PHONY: cs-block-compile-planted
cs-block-compile-planted:
	$(call cs_block_compile,$(CS_BLOCK_COMPILE)/schema-planted,$(CS_BLOCK_COMPILE)/planted-gen,$(CS_BLOCK_COMPILE)/planted-artifacts)

# make/schema-luau-generated-analyze-strict-gate.mk — the Luau lens
# (ideas roadmap group schema-per-language-lenses; the card
# schema-luau-generated-analyze-strict-gate).
#
# luau-analyze --mode=strict over the generated Luau output for the schema
# corpus the Makefile's generate targets read: examples, examples128,
# examples-wide, the SCHEMAS_TABLES* directories, the bench corpus units and
# test/tables. Every generated module and every runtime module opens with
# --!strict, so the analyzer is not left in its nonstrict default. The
# analyzer exits 0 on lint warnings, so the test rejects any diagnostic line,
# not just a non-zero exit: warnings are errors. Every conformance manifest
# unit is read from testdata/conformance/tables/MANIFEST.txt and analyzed. No
# file may carry a hot comment but --!strict on line 1, --!native and
# --!optimize: a --!nolint, --!nonstrict or --!nocheck is refused by name, so
# a warning is fixed in the emitter, never silenced.
#
# No formatting gate: StyLua, the Luau formatter, cannot parse Luau 0.741's
# integer literals (0i, 0xeb26c970fde3fd71i) at v2.5.2 or v2.6.0, so it has no
# canonical form to hold the generated output to. tables/maps is analyzed whole: its Text.schema
# module collides with the runtime schema_text.luau only when case is folded, so the
# test stages that one schema file renamed on this case-sensitive gate.
#
# The analyzer is the pinned Luau release make/luau.mk names (0.741) and
# resolves as $LUAU_ANALYZE, else dist/luau-0.741/luau-analyze. `make dist-luau`
# installs it from the official luau-lang release after checking the sha256;
# a missing tool is reported by name, never skipped.
#
# moo is deterministic Luau and emits the same .luau files, so this one run
# covers both.

.PHONY: schema-luau-generated-analyze-strict-gate
schema-luau-generated-analyze-strict-gate: toolchain-luau
	@if [ ! -x "$(LUAU_ANALYZE)" ]; then \
		echo "tool missing: luau-analyze (pinned $(LUAU_VERSION)) at $(LUAU_ANALYZE); run make dist-luau"; \
		exit 1; \
	fi
	@echo "luau-analyze --mode=strict: Luau $(LUAU_VERSION) at $(LUAU_ANALYZE)"
	sh test/slowgate/proof schema-luau-generated-analyze-strict-gate \
		'TestLuauGeneratedAnalyzeStrictGate' \
		./internal/ci -run '^TestLuauGeneratedAnalyzeStrictGate$$' -count=1 -timeout 600s

# The planted-pattern control, one row per refusal the gate makes: a scratch
# copy of the generated output gets (type_error) a strict type error the
# analyzer exits non-zero on, (lint_warning) an unused local the analyzer only
# warns about at exit 0, and (nolint_directive) the same local under a
# --!nolint hot comment the analyzer is silent on. The gate must refuse each
# and name it.
.PHONY: schema-luau-generated-analyze-strict-gate-negative-control
schema-luau-generated-analyze-strict-gate-negative-control: toolchain-luau
	@if [ ! -x "$(LUAU_ANALYZE)" ]; then \
		echo "tool missing: luau-analyze (pinned $(LUAU_VERSION)) at $(LUAU_ANALYZE); run make dist-luau"; \
		exit 1; \
	fi
	@echo "luau-analyze --mode=strict: planted-pattern control on Luau $(LUAU_VERSION)"
	sh test/slowgate/proof schema-luau-generated-analyze-strict-gate-negative-control \
		'TestLuauGeneratedAnalyzeStrictGatePlantedPattern/type_error TestLuauGeneratedAnalyzeStrictGatePlantedPattern/lint_warning TestLuauGeneratedAnalyzeStrictGatePlantedPattern/nolint_directive' \
		./internal/ci -run '^TestLuauGeneratedAnalyzeStrictGatePlantedPattern$$' -count=1 -timeout 600s

# The J4 register slug (docs/PORTING.md): the lens by the name every leg uses
# for "emitted text is analyzer-clean". Luau has no formatter (see above), so
# the whole of the check is the strict analysis and its planted control; the
# Luau leg requires both.
.PHONY: tables-luau-clean
tables-luau-clean: schema-luau-generated-analyze-strict-gate schema-luau-generated-analyze-strict-gate-negative-control
test-luau: tables-luau-clean

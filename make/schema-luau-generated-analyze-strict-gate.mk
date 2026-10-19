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
# not just a non-zero exit. tables/maps is analyzed whole: its Text.schema
# module collides with the runtime text.luau only when case is folded, so the
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

# The planted-pattern control: a scratch copy of the generated output gets a
# strict type error, and the target must exit non-zero and name the plant.
.PHONY: schema-luau-generated-analyze-strict-gate-negative-control
schema-luau-generated-analyze-strict-gate-negative-control: toolchain-luau
	@if [ ! -x "$(LUAU_ANALYZE)" ]; then \
		echo "tool missing: luau-analyze (pinned $(LUAU_VERSION)) at $(LUAU_ANALYZE); run make dist-luau"; \
		exit 1; \
	fi
	@echo "luau-analyze --mode=strict: planted-pattern control on Luau $(LUAU_VERSION)"
	sh test/slowgate/proof schema-luau-generated-analyze-strict-gate-negative-control \
		'TestLuauGeneratedAnalyzeStrictGatePlantedPattern' \
		./internal/ci -run '^TestLuauGeneratedAnalyzeStrictGatePlantedPattern$$' -count=1 -timeout 600s

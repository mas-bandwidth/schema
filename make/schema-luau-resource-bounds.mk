# make/schema-luau-resource-bounds.mk — the luau port's resource-bounds lens
# (the card schema-luau-resource-bounds-r; the bound docs/SPEC-TABLES.md
# §16.9.4 and the depth cap of 128).
#
# Hostile inputs to the generated Luau readers must cost bounded memory and
# time: huge counts, deep nesting, recursion, giant strings and many unknown
# fields. The tests run the generated textform reader under the pinned
# interpreter with an allocation counter — Luau's GC memstat,
# collectgarbage("count") — and assert each hostile shape refuses by name
# within the documented factor. The planted-pattern control proves the depth
# guard has a blade: with the guard relaxed the deep shape parses, so the
# control is red when the plant is in and green when it is out.
#
# The interpreter is the pinned Luau release make/luau.mk names (0.741) and
# resolves as $LUAU, else dist/luau-0.741/luau. `make dist-luau` installs it
# from the official luau-lang release after checking the sha256; a missing
# tool is reported by name, never skipped.

.PHONY: schema-luau-resource-bounds
schema-luau-resource-bounds: toolchain-luau
	@if [ ! -x "$(LUAU)" ]; then \
		echo "tool missing: luau (pinned $(LUAU_VERSION)) at $(LUAU); run make dist-luau"; \
		exit 1; \
	fi
	@echo "luau resource-bounds: hostile shapes under the depth cap and text_limit (Luau $(LUAU_VERSION))"
	sh test/slowgate/proof schema-luau-resource-bounds \
		'TestLuauAllocationCounterCounts TestLuauResourceBoundsHostileShapes' \
		./internal/ci -run '^(TestLuauAllocationCounterCounts|TestLuauResourceBoundsHostileShapes)$$' -count=1 -timeout 600s

# The planted-pattern control: with the depth guard relaxed (the plant) the
# deep-nesting shape parses instead of refusing, and the control must see the
# difference.
.PHONY: schema-luau-resource-bounds-negative-control
schema-luau-resource-bounds-negative-control: toolchain-luau
	@if [ ! -x "$(LUAU)" ]; then \
		echo "tool missing: luau (pinned $(LUAU_VERSION)) at $(LUAU); run make dist-luau"; \
		exit 1; \
	fi
	@echo "luau resource-bounds: planted-pattern control (Luau $(LUAU_VERSION))"
	sh test/slowgate/proof schema-luau-resource-bounds-negative-control \
		'TestLuauResourceBoundsPlantedPattern' \
		./internal/ci -run '^TestLuauResourceBoundsPlantedPattern$$' -count=1 -timeout 600s

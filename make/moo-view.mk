# THE VIEW, its moo leg (docs/SPEC-TABLES.md §8): the unit registry
# <Package>View.moo and the doc, tag and type-name columns of every descriptor,
# under the pinned moo (internal/codegen/mootable/view_test.go).
#
# tables-moo-view is the corpus gate (§8.7): a moo program holding the
# generated modules and nothing else lists UnitView() for every VIEW_CORPUS
# unit and every tables conformance manifest unit, byte-identical to
# internal/viewlisting's listing of the IR, the pin every other registry is
# held to. Beside it: a type no table reaches described with no table-wire
# identity (§8.2), and the empty doc defined once.
.PHONY: tables-moo-view tables-moo-view-negative-controls
tables-moo-view: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/mootable/ \
		-run '^(TestMooUnitViewCorpus|TestMooUnitViewPacketStorage|TestMooEmptyDocIsOneDefinition)$$'

# THE NEGATIVE CONTROLS (§8.7: a gate that cannot go red proves nothing): an
# unflattened multi-line doc, a declaration dropped from the registry, a
# general arm's offset moved, a field's doc emptied and an outside field given
# a wire id, each in a copy of the generated text, and each must turn the
# listing red on the line it owns.
tables-moo-view-negative-controls: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/mootable/ -run '^TestMooUnitViewNegativeControl$$' -v
	@echo "moo view negative controls: five sabotages, each red on its own line"

test-moo: tables-moo-view tables-moo-view-negative-controls

# THE VIEW, its Goose leg (docs/SPEC-TABLES.md §8): the unit registry
# <Package>View.goose and the doc and tag columns of every descriptor, under
# the pinned compiler (internal/codegen/goosetable/view_test.go).
#
# tables-goose-view is the corpus gate (§8.7): a Goose program holding the
# generated modules and nothing else lists UnitView() for every VIEW_CORPUS
# unit and every tables conformance manifest unit, byte-identical to
# internal/viewlisting's listing of the IR, the pin every other registry is
# held to. Beside it: a type no table reaches described with no table-wire
# identity (§8.2), the tag-list names unique, and the empty doc defined once.
.PHONY: tables-goose-view tables-goose-view-negative-controls
tables-goose-view:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -timeout 600s ./internal/codegen/goosetable/ \
		-run '^(TestGooseUnitViewCorpus|TestGooseUnitViewPacketStorage|TestGooseTagListNamesDoNotCollide|TestGooseEmptyDocIsOneDefinition)$$'

# THE NEGATIVE CONTROLS (§8.7: a gate that cannot go red proves nothing): an
# unflattened multi-line doc, a declaration dropped from the registry, a
# general arm's offset moved, a field's doc emptied and an outside field given
# a wire id, each in a copy of the generated text, and each must turn the
# listing red on the line it owns.
tables-goose-view-negative-controls:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -timeout 600s ./internal/codegen/goosetable/ -run '^TestGooseUnitViewNegativeControl$$' -v
	@echo "goose view negative controls: five sabotages, each red on its own line"

test-goose: tables-goose-view tables-goose-view-negative-controls

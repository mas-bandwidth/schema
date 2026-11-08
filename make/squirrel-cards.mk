# make/squirrel-cards.mk — THE CARDS LEG for Squirrel (tables/cards/conformance/FORMAT.md):
# the card facade tables/cards/squirrel/cards.nut over the generated Card unit's
# class instances (docs/PORT-SQUIRREL.md, THE TYPE MAPPING), held by its unit
# tests. Every target that runs the interpreter builds the pin if it is missing
# (make dist-squirrel).
#
#   cards-squirrel-generate   the Card unit generated for squirrel, the runtime
#                             modules beside it
#   cards-squirrel-test       the facade's unit tests (no clock, no socket, no
#                             process of their own)
#
# A DASHED STEM (make/<lang>-<part>.mk) is a gate or support fragment, never a
# port: the port registry (SCHEMA_PORTS, the root Makefile) and the wide-scalar
# refusal gate probe only undashed stems, so this fragment is included like any
# make file without being taken for a `--lang`. That is why lua's card targets
# had to live in make/lua.mk when the registry still read every stem, and why
# Squirrel's (like Goose's, make/goose-cards.mk) need not.
#
# The unit is a STAMP, not a phony name: the targets that read it run side by
# side under -j and in sub-makes, and a phony rule's rm -rf would empty the
# directory under a sibling's run mid-read.
CARDS_SQUIRREL_GENERATED := build/tables-generated-squirrel/cards

.PHONY: cards-squirrel-generate cards-squirrel-test
cards-squirrel-generate: $(CARDS_SQUIRREL_GENERATED)/.stamp

$(CARDS_SQUIRREL_GENERATED)/.stamp: bin/schema tables/cards/Card.schema tables/cards/schema.lock
	@rm -rf $(CARDS_SQUIRREL_GENERATED)
	@mkdir -p $(CARDS_SQUIRREL_GENERATED)
	./bin/schema generate --lang squirrel --out $(CARDS_SQUIRREL_GENERATED) tables/cards
	@touch $@

# sq EXITS 0 ON A RUNTIME ERROR (docs/PORT-SQUIRREL.md, THE HARNESS): a run
# that writes anything to stderr is a failure whatever its exit status, and
# the summary line must say none failed.
cards-squirrel-test: cards-squirrel-generate tables/cards/squirrel/cards.nut tables/cards/squirrel/cards_test.nut
	@test -x "$(SQ)" || $(MAKE) dist-squirrel
	@mkdir -p build
	@CARDS_SQUIRREL_GENERATED=$(CARDS_SQUIRREL_GENERATED) "$(SQ)" tables/cards/squirrel/cards_test.nut > build/cards-squirrel-test.log 2> build/cards-squirrel-test.err; \
	status=$$?; cat build/cards-squirrel-test.log; cat build/cards-squirrel-test.err >&2; \
	if [ $$status -ne 0 ] || [ -s build/cards-squirrel-test.err ]; then echo "cards-squirrel-test FAILED"; exit 1; fi; \
	grep -qE '^cards \(squirrel\): [1-9][0-9]* passed, 0 failed$$' build/cards-squirrel-test.log || { echo "cards-squirrel-test FAILED: no clean summary line"; exit 1; }

# The leg gate reaches them (as make/goose-cards.mk's test-goose does): a
# prerequisite added here, so make/squirrel.mk is not edited.
test-squirrel: cards-squirrel-test

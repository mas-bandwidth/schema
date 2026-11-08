# make/squirrel-cards.mk — THE CARDS LEG for Squirrel (tables/cards/conformance/FORMAT.md):
# the card facade tables/cards/squirrel/cards.nut over the generated Card unit's
# class instances (docs/PORT-SQUIRREL.md, THE TYPE MAPPING), and its driver,
# held by the facade's unit tests and by the shared card corpus on all nine
# surfaces with the three negative controls every leg carries. Every target
# that runs the interpreter builds the pin if it is missing (make dist-squirrel).
#
#   cards-squirrel-generate                       the Card unit generated for
#                                                 squirrel, the runtime modules
#                                                 beside it
#   cards-squirrel-test                           the facade's unit tests (no
#                                                 clock, no socket, no process
#                                                 of their own)
#   cards-squirrel-conformance                    the harness's `cards` check
#                                                 over tables/cards/squirrel/driver
#   cards-squirrel-conformance-negative-control   one damaged answer per control
#   cards-squirrel-conformance-upgrade-negative-control  (CARDS_SQUIRREL_SABOTAGE):
#   cards-squirrel-conformance-hash-negative-control     render, upgrade and the
#                                                 task hash, each red alone
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
CARDS_REQUIRE_SQUIRREL := load,save,render,parse,bounds,upgrade,edit,taskhash,view

.PHONY: cards-squirrel-generate cards-squirrel-test cards-squirrel-conformance cards-squirrel-conformance-negative-control cards-squirrel-conformance-upgrade-negative-control cards-squirrel-conformance-hash-negative-control
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

cards-squirrel-conformance: build/conformance-harness cards-squirrel-generate
	@test -x "$(SQ)" || $(MAKE) dist-squirrel
	CARDS_SQUIRREL_GENERATED=$(CARDS_SQUIRREL_GENERATED) ./build/conformance-harness cards --work build/cards-squirrel-conformance --corpus tables/cards/conformance --driver 'tables/cards/squirrel/driver' --require $(CARDS_REQUIRE_SQUIRREL)

# $(1) the damaged surface's switch, $(2) the surface that must go red, $(3)
# the surfaces that must stay green, $(4) the work and log name.
define cards-squirrel-control
	@test -x "$(SQ)" || $(MAKE) dist-squirrel
	@if CARDS_SQUIRREL_SABOTAGE=$(1) CARDS_SQUIRREL_GENERATED=$(CARDS_SQUIRREL_GENERATED) ./build/conformance-harness cards --work build/cards-squirrel-conformance-$(4) --corpus tables/cards/conformance --driver 'tables/cards/squirrel/driver' --require $(CARDS_REQUIRE_SQUIRREL) > build/cards-squirrel-conformance-$(4).log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the damaged $(2) answer left the card conformance check green'; exit 1; fi
	@grep -qE 'cards $(2) .*FAIL' build/cards-squirrel-conformance-$(4).log || { echo 'NEGATIVE CONTROL FAILED: $(2) did not go red'; cat build/cards-squirrel-conformance-$(4).log; exit 1; }
	@for s in $(3); do grep -qE "cards $$s pass" build/cards-squirrel-conformance-$(4).log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-squirrel-conformance-$(4).log; exit 1; }; done
	@echo "cards Squirrel negative control ($(2)): the damaged answer fails $(2) and no other surface"
endef

cards-squirrel-conformance-negative-control: build/conformance-harness cards-squirrel-generate
	$(call cards-squirrel-control,render,render,load save parse bounds upgrade edit taskhash view,neg)

cards-squirrel-conformance-upgrade-negative-control: build/conformance-harness cards-squirrel-generate
	$(call cards-squirrel-control,upgrade,upgrade,load save render parse bounds edit taskhash view,upgrade-neg)

cards-squirrel-conformance-hash-negative-control: build/conformance-harness cards-squirrel-generate
	$(call cards-squirrel-control,hash,taskhash,load save render parse bounds upgrade edit view,hash-neg)

# The leg gate reaches them (as make/goose-cards.mk's test-goose does): a
# prerequisite added here, so make/squirrel.mk is not edited.
test-squirrel: cards-squirrel-test cards-squirrel-conformance cards-squirrel-conformance-negative-control cards-squirrel-conformance-upgrade-negative-control cards-squirrel-conformance-hash-negative-control

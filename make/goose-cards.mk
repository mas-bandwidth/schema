# make/goose-cards.mk — THE CARDS LEG for Goose (tables/cards/conformance/FORMAT.md):
# the card facade tables/cards/goose/cards.goose over the generated Card unit's
# flat value table (docs/PORT-GOOSE.md, THE LOADED VALUE), and its driver,
# held by the shared card corpus on all nine surfaces with the three negative
# controls every leg carries. Every target installs the pinned compiler if it
# is missing (make dist-goose).
#
#   cards-goose-generate                       the Card unit generated for goose
#                                              (the schema_leg_goose target), the
#                                              facade and the driver beside it
#   cards-goose-conformance                    the harness's `cards` check over
#                                              tables/cards/goose/driver
#   cards-goose-conformance-negative-control   one damaged answer per control:
#   cards-goose-conformance-upgrade-negative-control  render, upgrade and the
#   cards-goose-conformance-hash-negative-control    task hash, each red alone
#
# The unit is a STAMP: the targets that read it run side by side under -j, and
# a phony rule's rm -rf would empty the directory under a sibling's driver.
CARDS_GOOSE_GENERATED := build/tables-generated-goose/cards
CARDS_REQUIRE_GOOSE := load,save,render,parse,bounds,upgrade,edit,taskhash,view
CARDS_GOOSE_SOURCES := $(shell find internal/codegen/goose internal/codegen/goosetable cmd/schema compiler -name '*.go' -o -name '*.goose' -o -name '*.goosereg' 2>/dev/null)

.PHONY: cards-goose-generate cards-goose-conformance cards-goose-conformance-negative-control cards-goose-conformance-upgrade-negative-control cards-goose-conformance-hash-negative-control
cards-goose-generate: $(CARDS_GOOSE_GENERATED)/.stamp

$(CARDS_GOOSE_GENERATED)/.stamp: tables/cards/Card.schema tables/cards/schema.lock tables/cards/goose/cards.goose tables/cards/goose/driver.goose $(CARDS_GOOSE_SOURCES)
	@rm -rf $(CARDS_GOOSE_GENERATED)
	@mkdir -p $(CARDS_GOOSE_GENERATED)
	go build -tags schema_leg_goose -o build/schema-goose-cards ./cmd/schema
	./build/schema-goose-cards generate --lang goose --out $(CARDS_GOOSE_GENERATED) tables/cards
	cp tables/cards/goose/cards.goose tables/cards/goose/driver.goose $(CARDS_GOOSE_GENERATED)/
	@touch $@

cards-goose-conformance: build/conformance-harness cards-goose-generate
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	./build/conformance-harness cards --work build/cards-goose-conformance --corpus tables/cards/conformance --driver 'tables/cards/goose/driver' --require $(CARDS_REQUIRE_GOOSE)

# $(1) the damaged surface's switch, $(2) the surface that must go red, $(3)
# the surfaces that must stay green, $(4) the work and log name.
define cards-goose-control
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@if CARDS_GOOSE_SABOTAGE=$(1) ./build/conformance-harness cards --work build/cards-goose-conformance-$(4) --corpus tables/cards/conformance --driver 'tables/cards/goose/driver' --require $(CARDS_REQUIRE_GOOSE) > build/cards-goose-conformance-$(4).log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the damaged $(2) answer left the card conformance check green'; exit 1; fi
	@grep -qE 'cards $(2) .*FAIL' build/cards-goose-conformance-$(4).log || { echo 'NEGATIVE CONTROL FAILED: $(2) did not go red'; cat build/cards-goose-conformance-$(4).log; exit 1; }
	@for s in $(3); do grep -qE "cards $$s pass" build/cards-goose-conformance-$(4).log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-goose-conformance-$(4).log; exit 1; }; done
	@echo "cards Goose negative control ($(2)): the damaged answer fails $(2) and no other surface"
endef

cards-goose-conformance-negative-control: build/conformance-harness cards-goose-generate
	$(call cards-goose-control,render,render,load save parse bounds upgrade edit taskhash view,neg)

cards-goose-conformance-upgrade-negative-control: build/conformance-harness cards-goose-generate
	$(call cards-goose-control,upgrade,upgrade,load save render parse bounds edit taskhash view,upgrade-neg)

cards-goose-conformance-hash-negative-control: build/conformance-harness cards-goose-generate
	$(call cards-goose-control,hash,taskhash,load save render parse bounds upgrade edit view,hash-neg)

# The leg gate reaches them (as make/lua.mk's test-lua reaches its card
# targets): a prerequisite added here, so make/goose.mk is not edited.
test-goose: cards-goose-conformance cards-goose-conformance-negative-control cards-goose-conformance-upgrade-negative-control cards-goose-conformance-hash-negative-control

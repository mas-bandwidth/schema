# make/wire-fuzz-release.mk — THE RELEASE-TIER WIRE-FUZZ GATE, one per leg,
# all fifteen the same shape (docs/SPEC-TABLES.md §4.2, §6.6; the read-path
# layers' bar, step e). Included by the Makefile's wildcard include.
#
#   make tables-wire-fuzz-release-<leg>     one leg: both arms, three seeds
#   make wire-fuzz-release-table            every leg's rows, as one table
#   make wire-fuzz-release-targets          the per-leg target names (certify)
#
# EVERY LEG RUNS BOTH ARMS, plain and --retain, on seeds 1, 2 and 3, at the
# release count, through the shared harness (build/conformance-harness
# wire-fuzz) and its oracle. A leg's own fuzz targets keep their own seeds and
# counts for the iteration tier; this gate is the one shape certify holds every
# leg to, so a leg that diverges on an arm it never ran is seen.
#
# THE SIX RUNS ARE INDEPENDENT and are separate make targets, so `make -j`
# runs them side by side. Each run writes one row and always finishes
# (make/checks/wire-fuzz-release-run.sh); the leg's target then prints its six
# rows and is red on any RED row. An arm the leg does not carry is named in
# test/conformance/wire-fuzz-owed.txt with its reason; it still runs where the
# leg has a driver, and an owed arm that has gone green is RED until its entry
# is removed. A leg with no driver at all has both arms owed by name.
#
# Per leg, three things: the driver command (empty: the leg has no wire-fuzz
# driver), its prerequisites, and any flags or environment the leg's own fuzz
# target already passes (--finite-owed names a card, never a silence).

WIRE_FUZZ_RELEASE_N     ?= 100000
WIRE_FUZZ_RELEASE_SEEDS ?= 1 2 3
WIRE_FUZZ_RELEASE_ARMS  := plain retain
WIRE_FUZZ_RELEASE_DIR   ?= build/wire-fuzz-release
WIRE_FUZZ_RELEASE_LEGS  := c cpp cs dart elixir go goose java js lobster lua luau moo rust squirrel

WFR_DEPS_c       = build/wire-fuzz-c
WFR_DRIVER_c     = ./build/wire-fuzz-c

WFR_DEPS_cpp     = build/wire-fuzz-cpp
WFR_DRIVER_cpp   = ./build/wire-fuzz-cpp

WFR_DEPS_cs      = build-conformance-cs
WFR_DRIVER_cs    = $(DOTNET) test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz

WFR_DEPS_dart    = build/conformance-dart
WFR_DRIVER_dart  = build/conformance-dart wire-fuzz
WFR_FLAGS_dart   = --finite-owed tables-finite/dart

WFR_DEPS_elixir  = build/elixir-tables-ebin/.stamp
WFR_DRIVER_elixir = test/conformance/elixir/driver wire-fuzz
WFR_ENV_elixir   = ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin

WFR_DEPS_go      = build/conformance-go
WFR_DRIVER_go    = build/conformance-go wire-fuzz

# goose carries no form-1 reader (test/conformance/goose/main.goose): no driver
WFR_DRIVER_goose =

WFR_DEPS_java    = build-conformance-java
WFR_DRIVER_java  = $(JAVA_WIRE_FUZZ_DRIVER)
WFR_FLAGS_java   = --finite-owed tables-finite/java

WFR_DEPS_js      = build/tables-generated-js/.stamp cards-js-generate
WFR_DRIVER_js    = test/conformance/js/driver wire-fuzz
WFR_FLAGS_js     = --finite-owed tables-finite/js

WFR_DEPS_lobster = lobster-dist-present build/conformance-lobster build/lobster-wirefuzzhost
WFR_DRIVER_lobster = $(LOBSTER_WIRE_FUZZ_HOST)

WFR_DEPS_lua     = toolchain-lua build/conformance-lua
WFR_DRIVER_lua   = $(LUA) test/conformance/lua/wirefuzz.lua build/conformance-lua

WFR_DEPS_luau    = toolchain-luau build/conformance-luau build/luau-driver/units.luau build/luaufuzzhost/luaufuzzhost
WFR_DRIVER_luau  = test/conformance/luau/wirefuzz-driver
WFR_ENV_luau     = LUAUFUZZ_NOTE=/dev/null

WFR_DEPS_moo     = toolchain-moo build/conformance-moo $(MOOFUZZHOST)
WFR_DRIVER_moo   = test/conformance/moo/wirefuzz-driver
WFR_ENV_moo      = MOOFUZZ_NOTE=/dev/null

WFR_DEPS_rust    = build/conformance-rust-all
WFR_DRIVER_rust  = build/conformance-rust-all
WFR_FLAGS_rust   = --finite-owed tables-finite/rust

WFR_DEPS_squirrel = toolchain-squirrel build/conformance-squirrel
WFR_DRIVER_squirrel = $(SQ) test/conformance/squirrel/wirefuzz.nut

wfr_runs = $(foreach a,$(WIRE_FUZZ_RELEASE_ARMS),$(foreach s,$(WIRE_FUZZ_RELEASE_SEEDS),wfr-run-$(1)-$(a)-$(s)))

# $(1) the leg. A run target is wfr-run-<leg>-<arm>-<seed>; leg names carry
# no hyphen, so the words of the name are the run's coordinates.
define wfr_leg
.PHONY: tables-wire-fuzz-release-$(1) $(call wfr_runs,$(1))
$(call wfr_runs,$(1)): build/conformance-harness $$(if $$(WFR_DRIVER_$(1)),$$(WFR_DEPS_$(1)))
	@WFR_DRIVER="$$(strip $$(WFR_DRIVER_$(1)))" WFR_FLAGS="$$(WFR_FLAGS_$(1))" WFR_ENV="$$(WFR_ENV_$(1))" \
		sh make/checks/wire-fuzz-release-run.sh $(1) $$(word 4,$$(subst -, ,$$@)) $$(word 5,$$(subst -, ,$$@)) \
		$$(WIRE_FUZZ_RELEASE_N) $$(WIRE_FUZZ_RELEASE_DIR)
tables-wire-fuzz-release-$(1): $(call wfr_runs,$(1))
	@for r in $(call wfr_runs,$(1)); do set -- $$$$(echo $$$$r | tr - ' '); cat $$(WIRE_FUZZ_RELEASE_DIR)/$$$$3.$$$$4.$$$$5.row; done > $$(WIRE_FUZZ_RELEASE_DIR)/$(1).tsv
	@awk -F'\t' '{ print $$$$1 " | " $$$$2 " | " $$$$3 " | " $$$$4 }' $$(WIRE_FUZZ_RELEASE_DIR)/$(1).tsv
	@if cut -f4 $$(WIRE_FUZZ_RELEASE_DIR)/$(1).tsv | grep -q '^RED'; then \
		echo "tables-wire-fuzz-release-$(1): RED (logs and mutants under $$(WIRE_FUZZ_RELEASE_DIR)/$(1).*)"; exit 1; fi
	@echo "tables-wire-fuzz-release-$(1): every arm passes or is owed by name, seeds $$(WIRE_FUZZ_RELEASE_SEEDS) at N=$$(WIRE_FUZZ_RELEASE_N)"
endef
$(foreach l,$(WIRE_FUZZ_RELEASE_LEGS),$(eval $(call wfr_leg,$(l))))

.PHONY: wire-fuzz-release-table wire-fuzz-release-targets
# ONE TABLE over every leg's rows from the last run of each leg's target.
wire-fuzz-release-table:
	@printf '| leg | arm | seed | result |\n|---|---|---|---|\n'
	@for l in $(WIRE_FUZZ_RELEASE_LEGS); do \
		if [ -f $(WIRE_FUZZ_RELEASE_DIR)/$$l.tsv ]; then awk -F'\t' '{ print "| " $$1 " | " $$2 " | " $$3 " | " $$4 " |" }' $(WIRE_FUZZ_RELEASE_DIR)/$$l.tsv; \
		else echo "| $$l | - | - | not run |"; fi; done

wire-fuzz-release-targets:
	@echo $(foreach l,$(WIRE_FUZZ_RELEASE_LEGS),tables-wire-fuzz-release-$(l))

# THE GATE'S NEGATIVE CONTROL: #2400's Dart fix (a map's header read against
# the PARENT, docs/SPEC-TABLES.md §2.8, §3) reverted in an emitter overlay
# (tools/sabotage table-dart-map-header-bounded), the Dart driver rebuilt on
# the sabotaged units, and the gate's own run, plain arm, at the gate's seeds
# and count, required to go RED on the report.
.PHONY: tables-wire-fuzz-release-negative-control
tables-wire-fuzz-release-negative-control: build/conformance-harness
	sh test/conformance/dart/negative-control mapheader "$(DART)"
	@rm -rf build/wire-fuzz-release-nc
	@for s in $(WIRE_FUZZ_RELEASE_SEEDS); do \
		WFR_DRIVER="$(CURDIR)/build/conformance-dart-negative-mapheader/conformance-dart wire-fuzz" WFR_FLAGS="$(WFR_FLAGS_dart)" WFR_ENV= \
		sh make/checks/wire-fuzz-release-run.sh dart plain $$s $(WIRE_FUZZ_RELEASE_N) build/wire-fuzz-release-nc & done; wait
	@cat build/wire-fuzz-release-nc/*.row | awk -F'\t' '{ print $$1 " | " $$2 " | " $$3 " | " $$4 }'
	@red=$$(cut -f4 build/wire-fuzz-release-nc/*.row | grep -cE '^RED: .*report differs'); \
	if [ "$$red" -eq 0 ]; then echo "NEGATIVE CONTROL FAILED: the map header bounded by L left the release gate green on every seed"; exit 1; fi; \
	if cut -f4 build/wire-fuzz-release-nc/*.row | grep -qE 'leg died|did not answer|exit [0-9]'; then echo "NEGATIVE CONTROL FAILED: the gate went red for another reason"; exit 1; fi; \
	echo "negative control: #2400 reverted (the Dart map header bounded by L) turns the release wire-fuzz gate RED on $$red of $(words $(WIRE_FUZZ_RELEASE_SEEDS)) seeds, on the report"

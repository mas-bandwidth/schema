# make/wire-fuzz-release.mk — THE RELEASE-TIER WIRE-FUZZ GATE, one per leg,
# all legs the same shape (docs/SPEC-TABLES.md §4.2, §6.6; the read-path
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
# (make/checks/wire-fuzz-release-run.sh); the leg's target then reads its
# roster, every arm at every seed, row by row BY NAME (tools/wirefuzzrelease
# table) and is red on any RED row and on any row that is not there. A run is
# green only when its summary proves it: at least one seed, at least one
# mutant, no divergence, and every absent seed held by name in the leg's
# ledger under test/conformance/wire-absent/ (the harness stops on any other)
# and read from the summary's `N ledgered seeds absent (<ledger>)` clause, an
# absence in other words being RED. An arm the leg does not carry is named in
# test/conformance/wire-fuzz-owed.txt with its reason; it still runs where the
# leg has a driver, and an owed arm that has gone green is RED until its entry
# is removed. A leg with no driver at all has both arms owed by name.
#
# Per leg, three things: the driver command (empty: the leg has no wire-fuzz
# driver), its prerequisites, and any flags or environment the leg's own fuzz
# target already passes.

WIRE_FUZZ_RELEASE_N     ?= 100000
WIRE_FUZZ_RELEASE_SEEDS ?= 1 2 3
WIRE_FUZZ_RELEASE_ARMS  := plain retain
WIRE_FUZZ_RELEASE_DIR   ?= build/wire-fuzz-release
WIRE_FUZZ_RELEASE_LEGS  := c cpp cs dart elixir go goose java js lobster lua luau moo python rust squirrel ruby unity

WFR_DEPS_c       = build/wire-fuzz-c
WFR_DRIVER_c     = ./build/wire-fuzz-c

WFR_DEPS_cpp     = build/wire-fuzz-cpp
WFR_DRIVER_cpp   = ./build/wire-fuzz-cpp
# C++ compares every vector, including malformed unknown-arm text.
WFR_FLAGS_cpp    =

WFR_DEPS_cs      = build-conformance-cs
WFR_DRIVER_cs    = $(DOTNET) test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz

WFR_DEPS_dart    = build/conformance-dart
WFR_DRIVER_dart  = build/conformance-dart wire-fuzz
WFR_FLAGS_dart   = --mixed-builder

WFR_DEPS_elixir  = build/elixir-tables-ebin/.stamp
WFR_DRIVER_elixir = test/conformance/elixir/driver wire-fuzz
WFR_ENV_elixir   = ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin

WFR_DEPS_go      = build/conformance-go
WFR_DRIVER_go    = build/conformance-go wire-fuzz

WFR_DEPS_goose   = build/conformance-goose-wirefuzz
WFR_DRIVER_goose = $(GOOSE_DISPATCH) $(GOOSE_LEGS_DIR)/legs
WFR_FLAGS_goose  = --mixed-builder

WFR_DEPS_java    = build-conformance-java
WFR_DRIVER_java  = $(JAVA_WIRE_FUZZ_DRIVER)

WFR_DEPS_js      = build/tables-generated-js/.stamp cards-js-generate
WFR_DRIVER_js    = test/conformance/js/driver wire-fuzz
WFR_FLAGS_js     = --mixed-builder

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

WFR_DEPS_python   = build/conformance-python python-toolchain-check python-wirefuzz-release-check
WFR_DRIVER_python = $(PYTHON) -W error -I -B test/conformance/python/wirefuzz.py build/conformance-python

WFR_DEPS_ruby     = build/conformance-ruby ruby-toolchain-check ruby-wirefuzz-release-check
WFR_DRIVER_ruby   = $(RUBY) --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby

WFR_DEPS_rust    = build/conformance-rust-all
WFR_DRIVER_rust  = build/conformance-rust-all

WFR_DEPS_squirrel = toolchain-squirrel build/conformance-squirrel
WFR_DRIVER_squirrel = test/conformance/squirrel/wirefuzz-driver

WFR_DEPS_unity    = toolchain-unity build/conformance-unity/.stamp
WFR_DRIVER_unity  = test/conformance/unity/driver wire-fuzz

wfr_runs = $(foreach a,$(WIRE_FUZZ_RELEASE_ARMS),$(foreach s,$(WIRE_FUZZ_RELEASE_SEEDS),wfr-run-$(1)-$(a)-$(s)))

# $(1) the leg. A run target is wfr-run-<leg>-<arm>-<seed>; leg names carry
# no hyphen, so the words of the name are the run's coordinates.
define wfr_leg
.PHONY: tables-wire-fuzz-release-$(1) $(call wfr_runs,$(1))
$(call wfr_runs,$(1)): build/conformance-harness build/wirefuzzrelease wire-fuzz-release-cases $$(if $$(WFR_DRIVER_$(1)),$$(WFR_DEPS_$(1)))
	@WFR_DRIVER="$$(strip $$(WFR_DRIVER_$(1)))" WFR_FLAGS="$$(WFR_FLAGS_$(1))" WFR_ENV="$$(WFR_ENV_$(1))" WFR_TOOL=./build/wirefuzzrelease \
		bash make/checks/wire-fuzz-release-run.sh $(1) $$(word 4,$$(subst -, ,$$@)) $$(word 5,$$(subst -, ,$$@)) \
		$$(WIRE_FUZZ_RELEASE_N) $$(WIRE_FUZZ_RELEASE_DIR)
tables-wire-fuzz-release-$(1): $(call wfr_runs,$(1))
	@./build/wirefuzzrelease table -leg $(1) -dir $$(WIRE_FUZZ_RELEASE_DIR) -arms "$$(WIRE_FUZZ_RELEASE_ARMS)" -seeds "$$(WIRE_FUZZ_RELEASE_SEEDS)" -n $$(WIRE_FUZZ_RELEASE_N)
endef
$(foreach l,$(WIRE_FUZZ_RELEASE_LEGS),$(eval $(call wfr_leg,$(l))))

# THE STREAM ENDS ONLY BETWEEN FRAMES, on every leg's driver above
# (test/conformance/harness/wireeof.go). The harness sends the leg's roster,
# then each truncation its one case table holds (1 to 3 index bytes at EOF, a
# cut length, a cut body) and the clean close, and requires a loud failure
# with no reply for every cut and exit 0 only for the close. Same driver, same
# environment and profile as the release runs; one target a leg, in the leg's
# test-<leg>, and tables-wire-eof for all of them. A leg with no driver has
# nothing to ask, and its target says so.
define wfr_eof
.PHONY: tables-wire-eof-$(1)
tables-wire-eof-$(1): build/conformance-harness $$(if $$(WFR_DRIVER_$(1)),$$(WFR_DEPS_$(1)))
	$$(if $$(strip $$(WFR_DRIVER_$(1))),env $$(WFR_ENV_$(1)) ./build/conformance-harness wire-eof $$(WFR_FLAGS_$(1)) --driver "$$(strip $$(WFR_DRIVER_$(1)))",@echo "wire-eof: $(1) has no wire-fuzz driver")
test-$(1): tables-wire-eof-$(1)
endef
$(foreach l,$(WIRE_FUZZ_RELEASE_LEGS),$(eval $(call wfr_eof,$(l))))

# The two Card-only drivers (make/rust.mk cards-rust-wire-fuzz, make/java.mk
# cards-java-wire-fuzz) read the same stream and answer the same cases, over
# the Card manifest their gates use.
WIRE_EOF_CARDS = --manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz
.PHONY: tables-wire-eof-rust-card tables-wire-eof-java-card
tables-wire-eof-rust-card: build/conformance-harness build/conformance-rust-card
	./build/conformance-harness wire-eof $(WIRE_EOF_CARDS) --driver 'build/conformance-rust-card wire-fuzz'
tables-wire-eof-java-card: build/conformance-harness build/cards-java-wire-fuzz/.stamp
	./build/conformance-harness wire-eof $(WIRE_EOF_CARDS) --driver "$(CURDIR)/build/cards-java-wire-fuzz/driver"
test-rust: tables-wire-eof-rust-card
test-java: tables-wire-eof-java-card

.PHONY: tables-wire-eof
tables-wire-eof: $(foreach l,$(WIRE_FUZZ_RELEASE_LEGS),tables-wire-eof-$(l)) tables-wire-eof-rust-card tables-wire-eof-java-card

# THE GATE'S JUDGEMENT (tools/wirefuzzrelease): one run into one row, and a
# leg's roster of rows, read by name, into its verdict.
build/wirefuzzrelease: $(wildcard tools/wirefuzzrelease/*.go)
	@mkdir -p build
	go build -o $@ ./tools/wirefuzzrelease

# THE GATE'S OWN CASES (tools/wirefuzzrelease/testdata/gate-cases): committed
# harness logs and rosters with the verdict each owes, the reviewer's controls
# among them. Every run of the gate replays them first; it reads files only.
.PHONY: wire-fuzz-release-cases
wire-fuzz-release-cases: build/wirefuzzrelease
	./build/wirefuzzrelease cases

.PHONY: wire-fuzz-release wire-fuzz-release-table wire-fuzz-release-targets
# Dry run of the release gate's name. The runs stay
# tables-wire-fuzz-release-<leg>. This target has no prerequisites, so
# `make -n` prints the flags and does not build a leg.
wire-fuzz-release:
	@printf '%s\n' \
		"dart: $(WFR_FLAGS_dart)" \
		"js: $(WFR_FLAGS_js)" \
		"rust: $(WFR_FLAGS_rust)"

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
tables-wire-fuzz-release-negative-control: build/conformance-harness build/wirefuzzrelease build/conformance-dart
	sh test/conformance/dart/negative-control mapheader "$(DART)"
	@rm -rf build/wire-fuzz-release-nc
	@# each seed's run waited on by its pid, never a bare wait (F-068)
	@pids=; for s in $(WIRE_FUZZ_RELEASE_SEEDS); do \
		WFR_DRIVER="$(CURDIR)/build/conformance-dart-negative-mapheader/conformance-dart wire-fuzz" WFR_FLAGS="$(WFR_FLAGS_dart)" WFR_ENV= WFR_TOOL=./build/wirefuzzrelease \
		bash make/checks/wire-fuzz-release-run.sh dart plain $$s $(WIRE_FUZZ_RELEASE_N) build/wire-fuzz-release-nc & pids="$$pids $$!"; done; \
	failed=0; for p in $$pids; do wait "$$p" || failed=1; done; \
	if test "$$failed" -ne 0; then echo "NEGATIVE CONTROL FAILED: a release wire-fuzz run exited non-zero" >&2; exit 1; fi
	@cat build/wire-fuzz-release-nc/*.row | awk -F'\t' '{ print $$1 " | " $$2 " | " $$3 " | " $$4 }'
	@red=$$(cut -f4 build/wire-fuzz-release-nc/*.row | grep -cE '^RED: .*(report|read acceptance) differs'); \
	if [ "$$red" -eq 0 ]; then echo "NEGATIVE CONTROL FAILED: the map header bounded by L left the release gate green on every seed"; exit 1; fi; \
	if cut -f4 build/wire-fuzz-release-nc/*.row | grep -E 'leg died|did not answer|exit [0-9]' >/dev/null; then echo "NEGATIVE CONTROL FAILED: the gate went red for another reason"; exit 1; fi; \
	echo "negative control: #2400 reverted (the Dart map header bounded by L) turns the release wire-fuzz gate RED on $$red of $(words $(WIRE_FUZZ_RELEASE_SEEDS)) seeds, on the report"

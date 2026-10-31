# make/moo.mk — the MOO leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-MOO.md is the port's design).
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.
#
# moo, pinned: mas-bandwidth/moo v1.3.0, the deterministic fork of Luau whose
# one number is Q48.16. moo ships no binary release, so `make dist-moo` builds
# the CLI and the analyzer from the pinned COMMIT into dist/moo/ (gitignored)
# and REFUSES a checkout whose HEAD is not the pin, exactly as serialize.moo's
# own Makefile does. Generated moo is self-contained (serialize.moo v2.1.2 is
# emitted beside the modules, internal/codegen/moo/runtime), so the interpreter
# and its analyzer are the leg's only toolchain. $MOO and $MOO_ANALYZE
# override; CI restores dist/moo from a cache keyed on the pin
# (.github/actions/moo).
MOO_REPO        := https://github.com/mas-bandwidth/moo.git
MOO_PIN         := f60f0239f41c6c0a827ecee44a7f9539c5798068
MOO_DIST        := $(CURDIR)/dist/moo
MOO            ?= $(MOO_DIST)/moo
MOO_ANALYZE    ?= $(MOO_DIST)/moo-analyze
MOO_BUILD_JOBS ?= 4
export MOO MOO_ANALYZE

.PHONY: dist-moo
dist-moo:
	@rm -rf build/moo-src build/moo-build && mkdir -p build/moo-src $(MOO_DIST)
	@cd build/moo-src && git init -q && git fetch -q --depth 1 $(MOO_REPO) $(MOO_PIN) && git checkout -q FETCH_HEAD
	@got=$$(git -C build/moo-src rev-parse HEAD); if [ "$$got" != "$(MOO_PIN)" ]; then \
		echo "dist-moo REFUSES: the checkout is $$got, not the pin $(MOO_PIN)"; exit 1; fi
	@cmake -S build/moo-src -B build/moo-build -DCMAKE_BUILD_TYPE=Release >/dev/null
	@cmake --build build/moo-build --target Luau.Repl.CLI Luau.Analyze.CLI -j $(MOO_BUILD_JOBS) >/dev/null
	@cp build/moo-build/moo build/moo-build/moo-analyze $(MOO_DIST)/
	@echo $(MOO_PIN) > $(MOO_DIST)/PIN
	@echo "dist-moo: moo $(MOO_PIN) at $(MOO_DIST)"

# THE TOOLCHAIN GATE, this leg's half (issue #599): one probe per pin.
.PHONY: toolchain-moo
toolchain-moo:
	@$(call toolchain_probe,moo,MOO,$(MOO))
	@$(call toolchain_probe,moo,MOO_ANALYZE,$(MOO_ANALYZE))

# test-moo: the packet surface under the pinned interpreter — the fourteen
# C++-pinned type-wire goldens written byte for byte, every single-instance
# golden read back and rewritten, the ludicrous (fixed, int128) goldens, the
# truncation and hostile-value refusals, the out-of-contract write refusals,
# moo-analyze silent over every generated corpus, and each gate's negative
# control (internal/codegen/moo/*_test.go). No real time, no socket, no
# network: inputs go in as source.
.PHONY: test-moo update-goldens-moo
test-moo: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/moo/...
update-goldens-moo: ;
generated/bench/tables/moo/.stamp: ;

TEST_LEGS          += test-moo
TOOLCHAIN_LEGS     += moo
TOOLCHAIN_PINS_moo := MOO MOO_ANALYZE
CONFORMANCE_LEGS   += $(call unless_skipped,moo,build/conformance-moo)
BENCH_TABLES_LEGS  += generated/bench/tables/moo/.stamp
GOLDENS_LEGS       += update-goldens-moo

# J6: native moo owning card facade. The Go host supplies only CLI file IO;
# parser/render/upgrade/edit/hash/view execute under the pinned interpreter.
CARDS_MOO_GENERATED := build/tables-generated-moo/cards
CARDS_MOO_REQUIRE := load,save,render,parse,bounds,upgrade,edit,taskhash,view
export CARDS_MOO_GENERATED
.PHONY: cards-moo-generate cards-moo-test cards-moo-conformance cards-moo-conformance-negative-control cards-moo-conformance-upgrade-negative-control cards-moo-conformance-hash-negative-control
cards-moo-generate: $(CARDS_MOO_GENERATED)/.stamp
$(CARDS_MOO_GENERATED)/.stamp: build/schema-moo tables/cards/Card.schema tables/cards/schema.lock
	@mkdir -p $(CARDS_MOO_GENERATED)
	./build/schema-moo generate --lang moo --out $(CARDS_MOO_GENERATED) tables/cards
	@touch $@

build/cards-driver-moo: tables/cards/moo/host/main.go
	@mkdir -p build
	go build -o $@ tables/cards/moo/host/main.go

cards-moo-test: toolchain-moo cards-moo-generate build/cards-driver-moo cards-corpus-check
	@mkdir -p build/cards-moo-analyze
	@cp $(CARDS_MOO_GENERATED)/*.moo tables/cards/moo/*.moo build/cards-moo-analyze/
	@printf 'return {surface="unit",rows={},files={}}\n' > build/cards-moo-analyze/inputs.moo
	$(MOO_ANALYZE) build/cards-moo-analyze/cards.moo build/cards-moo-analyze/cards_codec.moo build/cards-moo-analyze/cards_test.moo build/cards-moo-analyze/driver.moo
	./build/cards-driver-moo tables/cards/moo/UNIT_INPUTS.txt unit build/cards-moo-unit
	@cat build/cards-moo-unit/unit.txt

cards-moo-conformance: toolchain-moo cards-moo-generate build/cards-driver-moo build/conformance-harness cards-corpus-check
	SCHEMA_REQUIRE_CARDS_CORPUS=1 SCHEMA_CARDS_SURFACES=$(CARDS_MOO_REQUIRE) ./build/conformance-harness cards --work build/cards-moo-conformance --corpus tables/cards/conformance --driver tables/cards/moo/driver --require $(CARDS_MOO_REQUIRE)

cards-moo-conformance-negative-control: cards-moo-test cards-moo-conformance
	sh tables/cards/moo/negative-control.sh render
cards-moo-conformance-upgrade-negative-control: cards-moo-test cards-moo-conformance
	sh tables/cards/moo/negative-control.sh upgrade
cards-moo-conformance-hash-negative-control: cards-moo-test cards-moo-conformance
	sh tables/cards/moo/negative-control.sh taskhash

test-moo: cards-moo-test cards-moo-conformance

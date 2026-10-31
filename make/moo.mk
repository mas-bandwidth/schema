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

# make/squirrel.mk — the SQUIRREL leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-SQUIRREL.md is the port's design). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself at
# the end of this file.
#
# Scaffolded by `schema new-leg squirrel`. The emitters are the TYPE and TABLE
# surfaces with no codec yet (docs/PORT-SQUIRREL.md, THE CARD MAP): the target
# stays behind the schema_leg_squirrel build tag, so the leg builds its own
# tagged compiler at build/schema-squirrel and `bin/schema` stays the default
# nine.

# THE PINNED INTERPRETER (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS).
# Squirrel 3.2 from the upstream tag, checked against one sha256, built as sq64
# with SQUSEDOUBLE: sq64 makes _intsize_ 8 (a 64-bit integer, the codec's
# int64), and without -DSQUSEDOUBLE the float is 32-bit (16777217.0 ==
# 16777216.0), which no float64 field survives. dist/ is gitignored.
SQUIRREL_VERSION := 3.2
SQUIRREL_URL     := https://github.com/albertodemichelis/squirrel/archive/refs/tags/v$(SQUIRREL_VERSION).tar.gz
SQUIRREL_SHA256  := 02805414cfadd5bbb921891d3599b83375a40650abd6404a8ab407dc5e86a996
SQUIRREL_DIR     := dist/squirrel-$(SQUIRREL_VERSION)
SQ ?= $(CURDIR)/$(SQUIRREL_DIR)/bin/sq
# the conformance driver and the test helper (internal/codegen/squirrel/sqrun)
# are processes this make spawns, so they read the pin from the environment
export SQ

.PHONY: dist-squirrel
dist-squirrel:
	@mkdir -p dist
	@if [ ! -f dist/squirrel-$(SQUIRREL_VERSION).tar.gz ]; then \
		curl -fsSL -o dist/squirrel-$(SQUIRREL_VERSION).tar.gz.part $(SQUIRREL_URL) && \
		mv dist/squirrel-$(SQUIRREL_VERSION).tar.gz.part dist/squirrel-$(SQUIRREL_VERSION).tar.gz; fi
	@got=$$(sha256sum dist/squirrel-$(SQUIRREL_VERSION).tar.gz | cut -d' ' -f1); \
	if [ "$$got" != "$(SQUIRREL_SHA256)" ]; then \
		echo "dist-squirrel REFUSES: dist/squirrel-$(SQUIRREL_VERSION).tar.gz has sha256 $$got, the pin is $(SQUIRREL_SHA256)"; \
		echo "  delete the file and run make dist-squirrel again; a different tarball is never built"; \
		exit 1; fi
	@if [ ! -x $(SQUIRREL_DIR)/bin/sq ]; then \
		rm -rf $(SQUIRREL_DIR) && tar -xzf dist/squirrel-$(SQUIRREL_VERSION).tar.gz -C dist && \
		{ $(MAKE) -C $(SQUIRREL_DIR) sq64 DEFS=-DSQUSEDOUBLE > dist/squirrel-build.log 2>&1 || { cat dist/squirrel-build.log; exit 1; }; }; fi
	@echo "squirrel $(SQUIRREL_VERSION) $(SQUIRREL_URL)"
	@echo "sha256 $(SQUIRREL_SHA256)"
	@echo "sq $(CURDIR)/$(SQUIRREL_DIR)/bin/sq"

# THE TOOLCHAIN GATE, this leg's half (issue #599): `make test` runs this before
# the chain starts and refuses by name when the pin does not resolve.
.PHONY: toolchain-squirrel
toolchain-squirrel:
	@$(call toolchain_probe,squirrel,SQ,$(SQ))

# the leg's own compiler: the target is behind its build tag until the packet
# surface has claims (compiler/target_squirrel.go)
build/schema-squirrel: $(GO_SOURCES)
	@mkdir -p build
	go build -tags schema_leg_squirrel -o $@ ./cmd/schema

# THE CONFORMANCE BUILD: every unit of the tables manifest generated for
# squirrel (testdata/conformance/tables/MANIFEST.txt). A unit the target
# refuses is written to REFUSED.txt with its refusal rather than failing the
# build: the refusal is the leg's answer for that unit until a card lifts it
# (docs/PORT-SQUIRREL.md, THE REGISTER).
build/conformance-squirrel: build/schema-squirrel testdata/conformance/tables/MANIFEST.txt
	@rm -rf $@ && mkdir -p $@
	@: > $@/REFUSED.txt
	@grep '^unit ' testdata/conformance/tables/MANIFEST.txt | while read -r _ key paths; do \
		if ! ./build/schema-squirrel generate --lang squirrel --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			printf '%s %s\n' "$$key" "$$(tr '\n' ' ' < $@/$$key.log)" >> $@/REFUSED.txt; \
		fi; rm -f $@/$$key.log; \
	done
	@echo "conformance-squirrel: $$(ls -d $@/*/ 2>/dev/null | wc -l) units generated, $$(wc -l < $@/REFUSED.txt) refused (build/conformance-squirrel/REFUSED.txt)"

.PHONY: test-squirrel update-goldens-squirrel
test-squirrel: toolchain-squirrel
	go test -count=1 -timeout 600s ./internal/codegen/squirrel/... ./internal/codegen/squirreltable/...
update-goldens-squirrel: ;
generated/bench/tables/squirrel/.stamp: ;

TEST_LEGS         += test-squirrel
CONFORMANCE_LEGS  += $(call unless_skipped,squirrel,build/conformance-squirrel)
BENCH_TABLES_LEGS += generated/bench/tables/squirrel/.stamp
GOLDENS_LEGS      += update-goldens-squirrel
TOOLCHAIN_LEGS          += squirrel
TOOLCHAIN_PINS_squirrel := SQ

# make/lua.mk — the LUA leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-LUA.md is the design). Included by the Makefile's wildcard include;
# the Makefile names no language. The leg registers itself at the end of this
# file.
#
# Scaffolded by `schema new-leg lua`, then given its pinned interpreter and its
# two packages (schema-lua-port-design). Lua is a builtin target: the ordinary
# bin/schema is `schema generate --lang lua` and `schema testgen --lang lua`.

# THE PINNED INTERPRETER: PUC-Rio Lua 5.4.8, built from source into dist/
# (gitignored). Vanilla Lua 5.4 because it has the native 64-bit integer the
# port's value model stands on (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS)
# and string.pack/string.unpack for the byte buffers. LuaJIT (5.1) is not this
# leg. Populate with `make dist-lua`; any other sha256 is refused.
LUA_VERSION := 5.4.8
LUA_TARBALL := lua-$(LUA_VERSION).tar.gz
LUA_URL     := https://www.lua.org/ftp/$(LUA_TARBALL)
LUA_SHA256  := 4f18ddae154e793e46eeab727c59ef1c0c0c2b744e7b94219710d76f530629ae
LUA ?= $(CURDIR)/dist/lua-$(LUA_VERSION)/src/lua
# the conformance driver and the Go tests (internal/codegen/lua/luarun) read
# the pin from the environment
export LUA

.PHONY: dist-lua
dist-lua:
	@mkdir -p dist
	@if [ ! -f dist/$(LUA_TARBALL) ]; then curl -fsSL -o dist/$(LUA_TARBALL).part $(LUA_URL) && mv dist/$(LUA_TARBALL).part dist/$(LUA_TARBALL); fi
	@got=$$(sha256sum dist/$(LUA_TARBALL) 2>/dev/null | cut -d" " -f1); \
	if [ -z "$$got" ]; then got=$$(shasum -a 256 dist/$(LUA_TARBALL) | cut -d" " -f1); fi; \
	if [ "$$got" != "$(LUA_SHA256)" ]; then \
		echo "dist-lua REFUSES: dist/$(LUA_TARBALL) has sha256 $$got, the pin is $(LUA_SHA256)"; \
		echo "  delete dist/$(LUA_TARBALL) and run make dist-lua again; a tarball that does not match the pin is never built"; \
		exit 1; fi
	@rm -rf dist/lua-$(LUA_VERSION) && tar -xzf dist/$(LUA_TARBALL) -C dist
	$(MAKE) -C dist/lua-$(LUA_VERSION) $(if $(filter Darwin,$(shell uname -s)),macosx,linux)
	@echo "dist-lua: Lua $(LUA_VERSION) $(LUA_URL) sha256 $(LUA_SHA256)"
	@echo "dist-lua: $$(dist/lua-$(LUA_VERSION)/src/lua -v)"

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"). `make test` runs this before the
# chain starts and refuses by name when the pin does not resolve.
.PHONY: toolchain-lua
toolchain-lua:
	@$(call toolchain_probe,lua,LUA,$(LUA))

# THE WIRE-FUZZ DIFFERENTIAL's SLOW-GATE RUN-PROOF (test/slowgate/proof): the
# in-package half of the card is gated by internal/slowtest and SKIPS unless
# SCHEMA_SLOW=1, so a bare `go test` proves nothing. `make test-lua` runs it
# THROUGH proof, exactly as make/go.mk's tables-go-textform runs its gated tests.
.PHONY: tables-lua-wire-fuzz-differential
tables-lua-wire-fuzz-differential:
	sh test/slowgate/proof tables-lua-wire-fuzz-differential \
		'TestLuaWireFuzzAgreesWithTheGoEngine TestLuaWireFuzzNegativeControl' \
		./internal/codegen/luatable -run '^TestLuaWireFuzz' -count=1 -timeout 600s

# FINITE TABLE FLOATS (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27: "Tables go
# finite-only too, same rule as packets"): every Lua read refuses a NaN or an
# infinity on its raw bits as non_finite_float (variable, retained, message,
# retaining message, fixed, cook check, uncook and block dump), every writer
# refuses one, and -0 and the subnormals ride exact. The gate is
# TestLuaPlainFloatSpecialValuesAgreeAcrossForms; its reversed witness turns
# each read's raw-bits test off in turn and the probe must red on exactly that
# read's rows.
.PHONY: tables-lua-finite-float tables-lua-finite-float-negative-control
tables-lua-finite-float: toolchain-lua
	sh test/slowgate/proof tables-lua-finite-float \
		'TestLuaPlainFloatSpecialValuesAgreeAcrossForms' \
		./internal/codegen/luatable -run '^TestLuaPlainFloatSpecialValuesAgreeAcrossForms$$' -count=1 -timeout 600s
tables-lua-finite-float-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-finite-float-negative-control \
		'TestLuaFiniteFloatNegativeControl' \
		./internal/codegen/luatable -run '^TestLuaFiniteFloatNegativeControl$$' -count=1 -timeout 600s

# THE COMMITTED TREE LOADS: every module of generated/lua and
# generated/lua-ludicrous, the serialize runtime beside them included, is
# required under the pinned interpreter and returns its module table — the
# Lua counterpart of the compiled legs building their generated/<lang> tree.
.PHONY: lua-generated-loads
lua-generated-loads: toolchain-lua generated/lua/.stamp generated/lua-ludicrous/.stamp
	@set -e; n=0; for d in generated/lua generated/lua-ludicrous; do \
		for f in $$d/*.lua; do \
			m=$$(basename $$f .lua); \
			(cd $$d && $(LUA) -e "package.path = './?.lua;' .. package.path; assert(type(require('$$m')) == 'table', '$$m returned no module table')"); \
			n=$$((n + 1)); \
		done; \
	done; \
	echo "lua-generated-loads: $$n committed modules load under Lua $(LUA_VERSION)"

.PHONY: test-lua update-goldens-lua
# The fixed-form tests read the shared byte-oracle corpus, so it is built first,
# and SCHEMA_REQUIRE_CORPUS=1 makes its absence a FAILURE rather than a skip.
# The variable also arms internal/slowtest's gate (schema#988's slow-gate scan),
# so the gated luatable tests this leg's toolchain can run (the loadfix and
# variant probes) run here instead of skipping green. The wire-fuzz pair is
# -skip'd only because tables-lua-wire-fuzz-differential, a prerequisite above,
# has just run it through test/slowgate/proof.
test-lua: toolchain-lua lua-generated-loads tables-lua-finite-float tables-lua-finite-float-negative-control tables-lua-release-negative-control conformance-negative-control-lua-absent tables-lua-usage tables-lua-usage-negative-control tables-lua-bench-gate tables-lua-bench-gate-negative-control conformance-negative-control-lua conformance-negative-control-lua-walk conformance-negative-control-lua-framing tables-lua-wire-fuzz-differential build/fixedform-corpus/.stamp packet-finite-lua packet-finite-lua-negative-control tables-lua-alloc tables-lua-alloc-negative-control tables-lua-alloc-runtime-negative-control tables-lua-soak tables-lua-soak-negative-control
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/lua/ ./internal/codegen/luatable/ -count=1 -timeout 600s -skip '^TestLuaWireFuzz'
update-goldens-lua: ;

# bin/schema embeds both Lua runtime directories by glob (//go:embed
# runtime/*.lua in internal/codegen/lua and internal/codegen/luatable), which
# GO_SOURCES (*.go only) does not see, so they are added to bin/schema's
# prerequisites here: a changed runtime rebuilds the binary every gate then
# measures, and the committed generated/lua tree with it.
#
# build/conformance-lua is the tables manifest's units generated for lua, one
# directory per unit key. A unit the target refuses leaves its refusal, by
# name, in <unit>.refused instead of a directory — listed, never dropped.
bin/schema: $(wildcard internal/codegen/luatable/runtime/*.lua) $(wildcard internal/codegen/lua/runtime/*.lua)

# the Lua target: the type surface of the examples units, with the serialize
# runtime the emitter writes beside every unit (generated Lua is self-contained:
# no rock, no wiring file). Committed and held by `make generated-current`.
generated/lua/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang lua --out generated/lua examples
	@touch $@

generated/lua-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang lua --out generated/lua-ludicrous examples128
	@touch $@

build/conformance-lua: bin/schema testdata/conformance/tables/MANIFEST.txt make/lua.mk
	@rm -rf $@ && mkdir -p $@
	@awk '$$1 == "unit" { key = $$2; $$1 = ""; $$2 = ""; print key $$0 }' testdata/conformance/tables/MANIFEST.txt | \
	while read -r key paths; do \
		if ./bin/schema generate --lang lua --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			rm -rf $@/$$key; mv $@/$$key.log $@/$$key.refused; \
			echo "conformance-lua: $$key refused: $$(head -1 $@/$$key.refused)"; \
		fi; \
	done
	@echo "conformance-lua: $$(ls -d $@/*/ 2>/dev/null | wc -l | tr -d " ") units generated, $$(ls $@/*.refused 2>/dev/null | wc -l | tr -d " ") refused by name"

generated/bench/tables/lua/.stamp: ;

# PACKET FINITE FLOATS (SPEC §4.3, serialize format 2.0). The Lua packet target
# enforces the rule through its vendored serialize.lua runtime: a NaN or an
# infinity traps on write naming non_finite_float, refuses on read on its raw
# bits, and -0 and the subnormals ride bit exact. test/packet-finite/lua/main.lua
# runs every bare-float position of Finite.schema at both widths, plus the
# quantized float's write refusal. The generator emits the runtime beside the
# module, so each control sabotages that emitted copy (tools/sabotage, one
# direction at a time) and must fail exactly the vectors its .expected names.
build/packet-finite/lua/.stamp: bin/schema test/packet-finite/lua/Finite.schema
	@rm -rf build/packet-finite/lua && mkdir -p build/packet-finite/lua
	./bin/schema generate --lang lua --out build/packet-finite/lua/generated test/packet-finite/lua/Finite.schema
	@touch $@

.PHONY: packet-finite-lua packet-finite-lua-negative-control
packet-finite-lua: toolchain-lua build/packet-finite/lua/.stamp
	$(LUA) test/packet-finite/lua/main.lua build/packet-finite/lua/generated

# $(1) is the direction: read or write.
define packet-finite-lua-control
	@rm -rf build/packet-finite/lua-negative-$(1) && mkdir -p build/packet-finite/lua-negative-$(1)
	@cp -R build/packet-finite/lua/generated build/packet-finite/lua-negative-$(1)/generated
	go run ./tools/sabotage -name packet-finite-lua-$(1) \
		-out build/packet-finite/lua-negative-$(1)/generated/serialize_float.lua build/packet-finite/lua/generated/serialize_float.lua
	@if $(LUA) test/packet-finite/lua/main.lua build/packet-finite/lua-negative-$(1)/generated > build/packet-finite/lua-negative-$(1)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Lua runtime float $(1) check was removed and the finite gate stayed green'; exit 1; fi
	@sed -n 's|^FAILED: \([^:]*\):.*|\1|p' build/packet-finite/lua-negative-$(1)/log | LC_ALL=C sort > build/packet-finite/lua-negative-$(1)/failed
	@diff -u test/packet-finite/lua/$(1).expected build/packet-finite/lua-negative-$(1)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the Lua float $(1) sabotage did not fail exactly its vectors'; cat build/packet-finite/lua-negative-$(1)/log; exit 1; }
	@echo "packet finite Lua negative control ($(1)): the removed runtime check fails exactly $$(wc -l < build/packet-finite/lua-negative-$(1)/failed | tr -d ' ') vectors"
endef

packet-finite-lua-negative-control: packet-finite-lua
	$(call packet-finite-lua-control,read)
	$(call packet-finite-lua-control,write)

# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, Lua side.
# The cook example in docs/USAGE.md is not a sketch beside the code: the gate
# pulls the marked fragment out of the page, wraps it in a script that reads the
# fixture cook off disk and supplies the one helper the page leaves to the
# caller, and runs it under the pinned interpreter against the generated
# `tables/pointers` modules it requires, so the day the emitted surface moves
# the page goes red with it. USAGE_DOC names the page; the control points it at
# a sabotaged copy. The fixture is the small synthetic Scene cook of
# test/cookgen, checked by `schema cook-check` before the example reads it.
USAGE_DOC ?= docs/USAGE.md
USAGE_LUA_DIR ?= build/lua-usage
.PHONY: tables-lua-usage tables-lua-usage-negative-control
tables-lua-usage: toolchain-lua bin/schema $(USAGE_DOC) $(SCHEMAS_TABLES_POINTERS)
	@rm -rf $(USAGE_LUA_DIR) && mkdir -p $(USAGE_LUA_DIR)
	./bin/schema generate --lang lua --out $(USAGE_LUA_DIR) tables/pointers
	go run ./test/cookgen --bytes 4096 --root Scene --ref head --chain ListNode --next next --out $(USAGE_LUA_DIR)/Scene.cook
	./bin/schema cook-check --root Scene $(USAGE_LUA_DIR)/Scene.cook tables/pointers
	printf 'package.path = "./?.lua;" .. package.path\n' > $(USAGE_LUA_DIR)/usage.lua
	printf 'local file = assert(io.open(arg[1], "rb"))\nlocal bytes = file:read("a")\nfile:close()\n' >> $(USAGE_LUA_DIR)/usage.lua
	printf 'local used = 0\nlocal function use(_) used = used + 1 end\n' >> $(USAGE_LUA_DIR)/usage.lua
	sed -n '/<!-- lua-table-usage -->/,/<!-- \/lua-table-usage -->/p' $(USAGE_DOC) \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> $(USAGE_LUA_DIR)/usage.lua
	printf 'if used == 0 then error("the cook walk read no node") end\n' >> $(USAGE_LUA_DIR)/usage.lua
	printf 'print("usage: docs/USAGE.md Lua cook example opened the Scene cook and walked " .. used .. " node(s)")\n' >> $(USAGE_LUA_DIR)/usage.lua
	grep -q 'cook.Open' $(USAGE_LUA_DIR)/usage.lua || \
		{ echo "MISSING: $(USAGE_DOC) carries no lua-table-usage example"; exit 1; }
	cd $(USAGE_LUA_DIR) && $(LUA) usage.lua Scene.cook

# THE REVERSED WITNESSES: two edited copies of the page, each checked to have
# changed, must each turn the gate red, so a gate that ran no fragment, or one
# that stopped looking at what the fragment walks, is caught. The first renames
# the uncook read the page calls (the surface moved: the run errors); the second
# walks a field the Scene does not have (the example runs but reads no node:
# the used-count refusal).
tables-lua-usage-negative-control:
	@rm -rf build/lua-usage-negative && mkdir -p build/lua-usage-negative
	sed 's/uncook\.Read(/uncook.Reed(/' docs/USAGE.md > build/lua-usage-negative/renamed.md
	sed 's/local node = scene\.head/local node = scene.tail/' docs/USAGE.md > build/lua-usage-negative/walks-nothing.md
	@for c in renamed walks-nothing; do \
		if cmp -s docs/USAGE.md build/lua-usage-negative/$$c.md; then \
			echo "NEGATIVE CONTROL: the $$c edit did not apply to docs/USAGE.md"; exit 1; fi; \
		if $(MAKE) tables-lua-usage USAGE_DOC=build/lua-usage-negative/$$c.md USAGE_LUA_DIR=build/lua-usage-negative/$$c > build/lua-usage-negative/$$c.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$c copy of the page passed the Lua usage gate"; exit 1; fi; \
		echo "tables-lua-usage negative control: the $$c copy fails the gate"; \
	done

# J6 (docs/PORTING.md) — THE CARD CONFORMANCE CHECK, this leg's half
# (tables/cards/conformance/FORMAT.md). tables/cards/lua is the facade
# (cards.lua) and its driver (driver, driver.lua) over the generated Card unit:
#
#   cards-lua-generate                    the Card unit generated for Lua, the
#                                         runtime modules beside it
#   cards-lua-test                        the facade's unit tests (no clock, no
#                                         socket, no process)
#   cards-lua-conformance                 the harness `cards` subcommand over
#                                         the Lua driver, all nine surfaces
#   cards-lua-conformance-negative-control, -upgrade-, -hash-
#                                         one damaged answer each, through the
#                                         driver's test-only switches, and the
#                                         check must go red on that surface and
#                                         on no other
#
# They live in make/lua.mk (NOT a make/cards-lua.mk): the wide-scalar refusal
# gate discovers every make/*.mk as a live port (docs/SPEC-TABLES.md §11). The
# unit is a STAMP, not a phony name: the targets that read it run side by side
# under -j and in sub-makes, and a phony rule's rm -rf would empty the directory
# under a sibling's driver mid-read.
CARDS_LUA_GENERATED := build/tables-generated-lua/cards
CARDS_REQUIRE_LUA := load,save,render,parse,bounds,upgrade,edit,taskhash,view

.PHONY: cards-lua-generate cards-lua-test cards-lua-conformance cards-lua-conformance-negative-control cards-lua-conformance-upgrade-negative-control cards-lua-conformance-hash-negative-control
cards-lua-generate: $(CARDS_LUA_GENERATED)/.stamp

$(CARDS_LUA_GENERATED)/.stamp: bin/schema tables/cards/Card.schema tables/cards/schema.lock
	@rm -rf $(CARDS_LUA_GENERATED)
	@mkdir -p $(CARDS_LUA_GENERATED)
	./bin/schema generate --lang lua --out $(CARDS_LUA_GENERATED) tables/cards
	@touch $@

cards-lua-test: toolchain-lua cards-lua-generate tables/cards/lua/cards.lua tables/cards/lua/cards_test.lua
	$(LUA) tables/cards/lua/cards_test.lua

cards-lua-conformance: toolchain-lua build/conformance-harness cards-lua-generate
	./build/conformance-harness cards --work build/cards-lua-conformance --corpus tables/cards/conformance --driver 'tables/cards/lua/driver' --require $(CARDS_REQUIRE_LUA)

# $(1) the sabotage switch, $(2) the surface that must go red, $(3) the
# surfaces that must stay green, $(4) the work and log name.
define cards-lua-control
	@if $(1)=1 ./build/conformance-harness cards --work build/cards-lua-conformance-$(4) --corpus tables/cards/conformance --driver 'tables/cards/lua/driver' --require $(CARDS_REQUIRE_LUA) > build/cards-lua-conformance-$(4).log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the damaged $(2) answer left the card conformance check green'; exit 1; fi
	@grep -qE 'cards $(2) .*FAIL' build/cards-lua-conformance-$(4).log || { echo 'NEGATIVE CONTROL FAILED: $(2) did not go red'; cat build/cards-lua-conformance-$(4).log; exit 1; }
	@grep -qE 'cards ($(3)) pass' build/cards-lua-conformance-$(4).log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-lua-conformance-$(4).log; exit 1; }
	@echo "cards Lua negative control ($(2)): the damaged answer fails $(2) and no other surface"
endef

cards-lua-conformance-negative-control: toolchain-lua build/conformance-harness cards-lua-generate
	$(call cards-lua-control,CARDS_LUA_SABOTAGE_RENDER,render,load|save|parse|bounds|upgrade|edit|taskhash|view,neg)

cards-lua-conformance-upgrade-negative-control: toolchain-lua build/conformance-harness cards-lua-generate
	$(call cards-lua-control,CARDS_LUA_SABOTAGE_UPGRADE,upgrade,load|save|render|parse|bounds|edit|taskhash|view,upgrade-neg)

cards-lua-conformance-hash-negative-control: toolchain-lua build/conformance-harness cards-lua-generate
	$(call cards-lua-control,CARDS_LUA_SABOTAGE_HASH,taskhash,load|save|render|parse|bounds|upgrade|edit|view,hash-neg)

test-lua: cards-lua-test cards-lua-conformance cards-lua-conformance-negative-control cards-lua-conformance-upgrade-negative-control cards-lua-conformance-hash-negative-control

# I8 (docs/PORTING.md) — THE BENCH LEG's CORRECTNESS GATE. bench/tables/lua is
# the tables bench's Lua leg (bench/tables/README.md): the committed 64-record
# variant corpus is loaded and re-saved through the generated table module and
# must come back byte-identical, and variant 0 must be the pinned golden,
# BEFORE any clock starts. `--gate` runs exactly that and no timing, so it is
# cheap enough for `make test`; the measured rows are the release tier's
# (tables-lua-release), never a local run.
.PHONY: tables-lua-bench-gate tables-lua-bench-gate-negative-control
tables-lua-bench-gate: toolchain-lua bin/schema
	bench/tables/run.sh --gate --only lua

# THE REVERSED WITNESSES: the corpus with one byte of variant 0 flipped must
# fail the golden gate by name, and the corpus with one byte of a later variant
# flipped must fail the round-trip gate. A bench that measured a codec that no
# longer reproduces the corpus would report a rate for the wrong work.
tables-lua-bench-gate-negative-control: toolchain-lua bin/schema
	@rm -rf build/lua-bench-negative && mkdir -p build/lua-bench-negative/golden build/lua-bench-negative/roundtrip
	@cp bench/corpus/variants/bench_table.variants.bin build/lua-bench-negative/golden/
	@cp bench/corpus/variants/bench_table.variants.bin build/lua-bench-negative/roundtrip/
	@printf '\377' | dd of=build/lua-bench-negative/golden/bench_table.variants.bin bs=1 seek=100 conv=notrunc 2>/dev/null
	@printf '\377' | dd of=build/lua-bench-negative/roundtrip/bench_table.variants.bin bs=1 seek=$$((2147 * 3 + 100)) conv=notrunc 2>/dev/null
	@LUA_BENCH_GEN=build/lua-bench-negative/gen bench/tables/lua/leg build
	@if LUA_BENCH_GEN=build/lua-bench-negative/gen bench/tables/lua/leg run --gate --variant-dir build/lua-bench-negative/golden > build/lua-bench-negative/golden.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a corpus whose variant 0 is not the pinned golden passed the Lua bench gate"; exit 1; fi
	@grep -q 'WIRE GOLDEN MISMATCH' build/lua-bench-negative/golden.log || \
		{ echo "NEGATIVE CONTROL FAILED: the golden corpus went red, but not on the golden gate"; cat build/lua-bench-negative/golden.log; exit 1; }
	@if LUA_BENCH_GEN=build/lua-bench-negative/gen bench/tables/lua/leg run --gate --variant-dir build/lua-bench-negative/roundtrip > build/lua-bench-negative/roundtrip.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a corpus whose variant 3 does not round-trip passed the Lua bench gate"; exit 1; fi
	@grep -q 'FAILED: bench_table' build/lua-bench-negative/roundtrip.log || \
		{ echo "NEGATIVE CONTROL FAILED: the round-trip corpus went red, but not on the variant gate"; cat build/lua-bench-negative/roundtrip.log; exit 1; }
	@echo "tables-lua-bench-gate negative control: a flipped golden byte and a flipped variant byte each fail the gate by name"

# J3 (docs/PORTING.md) — THE RELEASE TIER. The expensive half of the leg, behind
# one target certify.yml finds BY NAME (its release-targets job derives the list
# from the Makefile, so landing it is this target and the interpreter step):
# every instrument is the PR tier's scaled, none is new.
#
#   the wire-fuzz differential against the Go engine at ten times the PR count
#   and a second seed, because a divergence escapes by depth of search;
#   the documented surface (tables-lua-usage) and its reversed witnesses;
#   the bench leg's gate and its witnesses, then the MEASURED pass: one
#   interleaved round of the tables bench, the rows only (--bare), so the
#   certification run's log carries the leg's write and round-trip rates beside
#   the other ports'. A bench on a hosted runner is a PAIRING CHECK, not a
#   sitting (PORTING.md I8): it shows the leg runs and roughly where it stands,
#   and certifies nothing.
LUA_RELEASE_FUZZ_N ?= 20000

.PHONY: tables-lua-release
tables-lua-release: toolchain-lua build/conformance-harness build/conformance-lua
	$(MAKE) tables-lua-wire-fuzz LUA_FUZZ_N=$(LUA_RELEASE_FUZZ_N)
	$(MAKE) tables-lua-wire-fuzz LUA_FUZZ_N=$(LUA_RELEASE_FUZZ_N) LUA_FUZZ_SEED=2
	$(MAKE) tables-lua-usage
	$(MAKE) tables-lua-usage-negative-control
	$(MAKE) tables-lua-bench-gate
	$(MAKE) tables-lua-bench-gate-negative-control
	bench/tables/run.sh --only lua --bare
	@echo "tables Lua release gate: the wire fuzz at depth on two seeds, the page's example, and the bench leg's gate and measured pass"

# THE RELEASE TIER's REVERSED WITNESS (J3): the release's wire fuzz, at the
# release count and on its second seed, over a copy of the generated units
# whose load runtime counts every unknown field twice. The release tier must
# go red on it, by a divergence, or its depth of search proves nothing.
.PHONY: tables-lua-release-negative-control
tables-lua-release-negative-control: toolchain-lua build/conformance-harness build/conformance-lua
	@rm -rf build/lua-release-negative && mkdir -p build/lua-release-negative
	@cp -R build/conformance-lua build/lua-release-negative/units
	@n=0; for f in build/lua-release-negative/units/*/load.lua; do \
		sed 's/report.unknown = report.unknown + 1$$/report.unknown = report.unknown + 1 + 1/' "$$f" > "$$f.sabotaged"; \
		if ! cmp -s "$$f" "$$f.sabotaged"; then n=$$((n + 1)); fi; mv "$$f.sabotaged" "$$f"; \
	done; \
	if [ "$$n" -eq 0 ]; then echo "NEGATIVE CONTROL: the release sabotage patched no unit's load.lua"; exit 1; fi
	@if ./build/conformance-harness wire-fuzz \
		--driver '$(LUA) test/conformance/lua/wirefuzz.lua build/lua-release-negative/units' \
		--seed 2 --n $(LUA_RELEASE_FUZZ_N) > build/lua-release-negative/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the release wire fuzz stayed green over a load that counts every unknown field twice"; \
		cat build/lua-release-negative/log; exit 1; fi
	@grep -q 'FAILED after' build/lua-release-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the release wire fuzz went red, but not on a divergence"; cat build/lua-release-negative/log; exit 1; }
	@echo "tables-lua-release negative control: the release wire fuzz (seed 2, N=$(LUA_RELEASE_FUZZ_N)) reds on a load that counts every unknown field twice"

# PER-CASE ABSENT's REVERSED WITNESS (I4): a copy of the driver whose absent()
# writes nothing, so a case the leg cannot answer is left with neither an
# answer nor <case>.absent. The harness must go red on it: an unanswered case
# is never counted as a pass, so the leg's absences are what it says they are.
.PHONY: conformance-negative-control-lua-absent
conformance-negative-control-lua-absent: toolchain-lua build/conformance-harness build/conformance-lua
	@rm -rf build/lua-absent-control && mkdir -p build/lua-absent-control
	@sed 's|^  writeBytes(outdir .. "/" .. name .. ".absent", reason .. "\\n")$$|  do end|' test/conformance/lua/main.lua > build/lua-absent-control/main.lua
	@if cmp -s test/conformance/lua/main.lua build/lua-absent-control/main.lua; then \
		echo "NEGATIVE CONTROL: the absent sabotage patched nothing in test/conformance/lua/main.lua"; exit 1; fi
	@printf '#!/bin/sh\nexec env LUA=%s %s %s/build/lua-absent-control/main.lua "$$@"\n' '$(LUA)' '$(LUA)' '$(CURDIR)' > build/lua-absent-control/driver
	@chmod +x build/lua-absent-control/driver
	@printf 'lua %s/build/lua-absent-control/driver\n' '$(CURDIR)' > build/lua-absent-control/drivers.txt
	@if build/conformance-harness run --drivers build/lua-absent-control/drivers.txt --work build/lua-absent-control/work > build/lua-absent-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a driver that drops every <case>.absent left conformance green"; cat build/lua-absent-control/log; exit 1; fi
	@grep -qE '^ *lua / [a-z-]+' build/lua-absent-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: conformance went red, but not on a lua surface"; cat build/lua-absent-control/log; exit 1; }
	@echo "conformance-negative-control-lua-absent: dropping <case>.absent turns [$$(grep -oE '^ *lua / [a-z-]+' build/lua-absent-control/log | sed 's/^ *lua \/ //' | sort -u | tr '\n' ' ')] red"

TEST_LEGS         += test-lua
CONFORMANCE_LEGS  += $(call unless_skipped,lua,build/conformance-lua)
BENCH_TABLES_LEGS += generated/bench/tables/lua/.stamp
GOLDENS_LEGS      += update-goldens-lua
TOOLCHAIN_LEGS    += lua
TOOLCHAIN_PINS_lua := LUA

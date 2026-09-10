# make/js.mk — the JavaScript leg (docs/CONTRIBUTING.md, "Adding a language").
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file. The serialize.js runtime is
# a sibling checkout too (../serialize.js) but needs no variable: generated JS
# never imports the runtime, and the test legs import it by module-relative
# path directly.

# Node, pinned per project. Generated JavaScript is self-contained (no runtime
# checkout), so the runtime is the only JavaScript dependency — but the pin is
# load-bearing here rather than tidy, because ONE of this port's gates measures
# the runtime and not only the code. The zero-allocation floor is a property of
# what V8 OPTIMIZES: a double that crosses a call boundary is a heap number,
# sixteen bytes, unless V8 inlined the callee — and whether it inlines one
# differs between majors and even between processes, so a body that reads zero
# on one node can read a steady fifteen bytes on another. So the allocation
# gate runs the version CI runs, and it refuses to certify on any other major
# (test/js-tables/main.mjs, PinnedNodeMajor). The default points at the repo-local unpacked runtime; CI
# installs the same major and overrides with NODE=node. To populate dist/
# (gitignored):
#   Node.js 26.7.0 (darwin-arm64)
#   url:    https://nodejs.org/dist/v26.7.0/node-v26.7.0-darwin-arm64.tar.gz
#   sha256: 7ee659a7768e641bbfd5360940660b8e8fd0052f77488f365562bac522fc15d4
#   untar into dist/ (the tarball already unpacks to node-v26.7.0-darwin-arm64)
NODE ?= $(CURDIR)/dist/node-v26.7.0-darwin-arm64/bin/node
# the conformance driver is a shell script the harness spawns, so it reads the
# pin from the environment and falls back to PATH
export NODE

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"). `make test` runs this before the
# chain starts and refuses by name when the pin does not resolve, because a leg
# that skips in silence is a leg whose red rides a green run.
.PHONY: toolchain-js
toolchain-js:
	@$(call toolchain_probe,js,NODE,$(NODE))
build/packet-defaults/js/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/js.mk
	./bin/schema generate --lang js --out build/packet-defaults/js/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang js --out build/packet-defaults/js/plain test/packet-defaults/Plain.schema
	@touch $@

.PHONY: packet-defaults-js packet-defaults-js-negative-control
packet-defaults-js: build/packet-defaults/js/.stamp packet-defaults-cpp
	$(NODE) test/packet-defaults/js/main.mjs testdata/wire/packet-defaults
	NODE_ENV=production $(NODE) test/packet-defaults/js/main.mjs testdata/wire/packet-defaults

packet-defaults-js-negative-control: packet-defaults-js
	@mkdir -p build/packet-defaults/js-negative
	go run ./tools/sabotage -name packet-defaults-js-constructor-bytes \
		-out build/packet-defaults/js-negative/js.gotext internal/codegen/js/js.go
	@printf '{"Replace":{"%s/internal/codegen/js/js.go":"%s/build/packet-defaults/js-negative/js.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/js-negative/overlay.json
	go build -overlay=build/packet-defaults/js-negative/overlay.json -o build/packet-defaults/js-negative/schema ./cmd/schema
	./build/packet-defaults/js-negative/schema generate --lang js --out build/packet-defaults/js-negative/generated test/packet-defaults/Defaults.schema
	$(NODE) --check build/packet-defaults/js-negative/generated/Defaults.js
	$(NODE) --check build/packet-defaults/js-negative/generated/DefaultsFlat.js
	@if $(NODE) test/packet-defaults/js/main.mjs testdata/wire/packet-defaults "$(CURDIR)/build/packet-defaults/js-negative/generated" > build/packet-defaults/js-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in JavaScript'; exit 1; fi
	@grep -Fq 'packet-default constructor bytes' build/packet-defaults/js-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: JavaScript failed for another reason'; cat build/packet-defaults/js-negative/log; exit 1; }
	@echo 'packet defaults JavaScript negative control: missing constructor bytes fail the runtime check'

test-js: packet-defaults-js packet-defaults-js-negative-control

# the JavaScript target: generated ES modules only, no wiring file at all —
# generated code never imports the runtime (every wire call is a method on
# the stream parameter), so the serialize.js sibling checkout is a test-leg
# concern, not a generation one
generated/js/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang js --out generated/js examples
	@touch $@

generated/js-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang js --out generated/js-ludicrous examples128
	@touch $@

# The same corpus through the JAVASCRIPT table backend (docs/SPEC-TABLES.md): the
# tables corpus plus the evolution pair, generated at build time into build/ —
# test-only, never part of the committed generated/ tree. The full unit is
# generated (packet .js + <Base>Table.js + the two accelerators' readers),
# because a table's closure decodes into the packet emitter's own classes.
build/tables-generated-js/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-js
	./bin/schema generate --lang js --out build/tables-generated-js/examples tables/examples
	# the POINTERED unit: its JS WIRE surface is refused by name (§11) and its
	# two ACCELERATORS are emitted all the same, because neither needs a codec
	# (§7, §19). This is where the cook's JS read side comes from.
	./bin/schema generate --lang js --out build/tables-generated-js/pointers tables/pointers
	./bin/schema generate --lang js --out build/tables-generated-js/block tables/block
	./bin/schema generate --lang js --out build/tables-generated-js/blockhome tables/blockhome
	./bin/schema generate --lang js --out build/tables-generated-js/v1 test/tables/V1.schema
	./bin/schema generate --lang js --out build/tables-generated-js/v2 test/tables/V2.schema
	./bin/schema generate --lang js --out build/tables-generated-js/p1 test/tables/P1.schema
	./bin/schema generate --lang js --out build/tables-generated-js/p3 test/tables/P3.schema
	@touch $@

# The JS twin of the C++ "no serialize include path" build and of the C#
# standalone gate: generated Block and Cook modules must stand alone on the
# language, so nothing in them may import the serialize runtime. Their only
# imports are module-relative, to other files OF THIS UNIT.
.PHONY: tables-js-standalone
tables-js-standalone: build/tables-generated-js/.stamp
	@n=$$(ls build/tables-generated-js/*/*Block.js build/tables-generated-js/*/*Cook.js 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 8 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated accelerator modules, expected at least 8 — the glob, not the property, is what broke"; exit 1; \
		fi
	@for f in build/tables-generated-js/*/*Block.js build/tables-generated-js/*/*Cook.js; do \
		if grep -n '^import .*serialize' $$f; then \
			echo "STANDALONE GATE FAILED: the serialize runtime leaked into $$f"; exit 1; \
		fi; \
		if grep -n '^import .*"\.\./' $$f; then \
			echo "STANDALONE GATE FAILED: $$f imports outside its own unit"; exit 1; \
		fi; \
	done
	@echo "tables JS standalone gate: generated accelerator modules import nothing but their own unit"

# The C# VARIABLE-CLASS REFUSAL (docs/SPEC-TABLES.md §2.2, §11), and it is a refusal
# of the WIRE SURFACE — which is the half the variable class is missing: the
# arena, the builder, the region and the node-table codec. The two ACCELERATORS
# need none of that, so a pointered unit's block (§19) and cook (§7) sources are
# emitted and its <Base>Table.cs is not.
#
# NAMED, NEVER SILENT is the property this gate holds: no Table source at all,
# and every emitted source of the unit opening with a banner that names each
# refused table and the follow-on. A consumer reaching for Save or Load gets a
# missing name from its own compiler, beside a file that says why.
# The JAVASCRIPT VARIABLE-CLASS REFUSAL (docs/SPEC-TABLES.md §2.2, §11), the twin
# of the C# one: the WIRE half is refused by name and the two ACCELERATORS are
# emitted all the same, because a block and a cook are POINTED AT, not parsed.
# THE JAVASCRIPT LEG (test/js-tables/main.mjs): what the conformance harness
# does not ask, because the harness asks every backend the same questions and
# these are this one's own — the field-id hash against a second implementation,
# measure's answer as the buffer, the enum-keyed surface's None refusal, and
# every block row read the same through the generated ACCESSORS and through the
# DESCRIPTORS.
.PHONY: tables-js-leg
tables-js-leg: build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	cd $(CURDIR) && $(NODE) test/js-tables/main.mjs

# Its NEGATIVE CONTROL: move one generated accessor four bytes and the leg must
# go red. Without this the accessor half of the gate could be reading the
# descriptors twice and nobody would know.
.PHONY: tables-js-accessor-negative-control
tables-js-accessor-negative-control: bin/schema build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	@rm -rf build/js-accessor-sabotage && mkdir -p build/js-accessor-sabotage
	@sed 's|at + %d + i \* %d", fl.Offset, elem)))|at + %d + i * %d", fl.Offset+4, elem))) // SABOTAGED|' \
		internal/codegen/jstable/record.go > build/js-accessor-sabotage/record.go.txt
	@grep -q SABOTAGED build/js-accessor-sabotage/record.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/record.go":"%s/build/js-accessor-sabotage/record.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-accessor-sabotage/overlay.json
	@go build -overlay=build/js-accessor-sabotage/overlay.json -o build/js-accessor-sabotage/schema ./cmd/schema
	@./build/js-accessor-sabotage/schema generate --lang js --out build/js-accessor-sabotage/generated/examples tables/examples
	@./build/js-accessor-sabotage/schema generate --lang js --out build/js-accessor-sabotage/generated/pointers tables/pointers
	@./build/js-accessor-sabotage/schema generate --lang js --out build/js-accessor-sabotage/generated/block tables/block
	@if SCHEMA_JS_GENERATED=$(CURDIR)/build/js-accessor-sabotage/generated $(NODE) test/js-tables/main.mjs \
			> build/js-accessor-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a generated accessor four bytes off left the leg green"; \
		cat build/js-accessor-sabotage/log; exit 1; \
	fi
	@grep -q "the accessor and the descriptor disagree" build/js-accessor-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on the accessor/descriptor disagreement"; \
		  cat build/js-accessor-sabotage/log; exit 1; }
	@echo "negative control: one generated accessor four bytes off turns the JavaScript leg RED on the accessor/descriptor agreement"

# And the POINTER half of the same gate, which the scalar sabotage cannot reach:
# move a pointer SLOT's own offset — the position a self-relative delta is
# relative to (§6.3) — and the cook accessors must part company with the cook
# descriptors.
.PHONY: tables-js-slot-negative-control
tables-js-slot-negative-control: bin/schema build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	@rm -rf build/js-slot-sabotage && mkdir -p build/js-slot-sabotage
	@sed 's|pf("  function %sSlot(at) { return at + %d; }\\n", member, fl.Offset)|pf("  function %sSlot(at) { return at + %d; }\\n", member, fl.Offset+8) // SABOTAGED|' \
		internal/codegen/jstable/record.go > build/js-slot-sabotage/record.go.txt
	@grep -q SABOTAGED build/js-slot-sabotage/record.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/record.go":"%s/build/js-slot-sabotage/record.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-slot-sabotage/overlay.json
	@go build -overlay=build/js-slot-sabotage/overlay.json -o build/js-slot-sabotage/schema ./cmd/schema
	@./build/js-slot-sabotage/schema generate --lang js --out build/js-slot-sabotage/generated/examples tables/examples
	@./build/js-slot-sabotage/schema generate --lang js --out build/js-slot-sabotage/generated/pointers tables/pointers
	@./build/js-slot-sabotage/schema generate --lang js --out build/js-slot-sabotage/generated/block tables/block
	@if SCHEMA_JS_GENERATED=$(CURDIR)/build/js-slot-sabotage/generated $(NODE) test/js-tables/main.mjs \
			> build/js-slot-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a pointer slot eight bytes off left the leg green"; \
		cat build/js-slot-sabotage/log; exit 1; \
	fi
	@grep -q "the slot accessor's offset is not the descriptor's" build/js-slot-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on the pointer slot"; \
		  cat build/js-slot-sabotage/log; exit 1; }
	@echo "negative control: a pointer slot eight bytes off turns the JavaScript leg RED on the cook's slot accessor"

# THE FUZZER'S ORACLE over the two READERS (docs/SPEC-TABLES.md §7.5, §19.5): a
# forged block or cook either REFUSES or opens and reads entirely inside the
# bytes it was given. In this language that is the whole property, because a
# DataView read past its own view throws — so "no exception escaped a reader"
# IS "no read left the buffer".
build/js-fuzz-scene.cook: $(wildcard test/cookgen/*.go) $(SCHEMAS_TABLES_POINTERS) bin/schema
	@mkdir -p build
	go run ./test/cookgen --bytes 4096 --root Scene --out $@ --ref head --chain ListNode --next next

.PHONY: tables-js-fuzz
tables-js-fuzz: build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	cd $(CURDIR) && N=$(if $(N),$(N),20000) SEED=$(if $(SEED),$(SEED),0xc00c1e5eed) \
		$(NODE) test/js-tables/main.mjs fuzz testdata/wire/tables/block_render.bin build/js-fuzz-scene.cook

# THE FUZZ ORACLE's NEGATIVE CONTROL: a block reader with its extent bounds
# removed must red the oracle, or the oracle has never been shown able to go
# red. BOTH bounds come out — the rows-past-the-body check and the padding
# check — because at the pinned seed each one alone leaves the run GREEN over
# 20000 mutants: a forged count the row bound no longer catches still lands the
# used extent past the buffer, where the padding check refuses it, and a forged
# length the padding check no longer catches is still inside the row bound.
# With both gone a walk reads past the view and the DataView throws, which is
# exactly the exception the oracle exists to say never escapes.
.PHONY: tables-js-fuzz-negative-control
tables-js-fuzz-negative-control: bin/schema build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	@rm -rf build/js-fuzz-sabotage && mkdir -p build/js-fuzz-sabotage
	@sed -e 's|g.pf("      if (rows > extent - offsetOf) { return null; }\\n")|// SABOTAGED: no row bound|' \
	     -e 's|g.pf("    if (padding > extent - used) { return null; }\\n")|// SABOTAGED: no padding bound|' \
		internal/codegen/jstable/block.go > build/js-fuzz-sabotage/block.go.txt
	@[ "$$(grep -c SABOTAGED build/js-fuzz-sabotage/block.go.txt)" = "2" ] || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove both extent bounds"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/block.go":"%s/build/js-fuzz-sabotage/block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-fuzz-sabotage/overlay.json
	@go build -overlay=build/js-fuzz-sabotage/overlay.json -o build/js-fuzz-sabotage/schema ./cmd/schema
	@./build/js-fuzz-sabotage/schema generate --lang js --out build/js-fuzz-sabotage/generated/examples tables/examples
	@./build/js-fuzz-sabotage/schema generate --lang js --out build/js-fuzz-sabotage/generated/pointers tables/pointers
	@./build/js-fuzz-sabotage/schema generate --lang js --out build/js-fuzz-sabotage/generated/block tables/block
	@if SCHEMA_JS_GENERATED=$(CURDIR)/build/js-fuzz-sabotage/generated N=$(if $(N),$(N),20000) SEED=$(if $(SEED),$(SEED),0xc00c1e5eed) \
			$(NODE) test/js-tables/main.mjs fuzz testdata/wire/tables/block_render.bin build/js-fuzz-scene.cook \
			> build/js-fuzz-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a block reader with no extent bound left the fuzz oracle green"; \
		cat build/js-fuzz-sabotage/log; exit 1; \
	fi
	@grep -q "a walk of an OPENED forgery threw" build/js-fuzz-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the oracle went red, but not on a read that escaped the buffer"; \
		  cat build/js-fuzz-sabotage/log; exit 1; }
	@echo "negative control: a block reader with both extent bounds removed turns the JavaScript fuzz oracle RED on a read that escaped the buffer"

# WHAT ALLOCATES, as a RATE (test/js-tables/main.mjs's fourth property). A flat
# heap is a LEAK instrument and nothing more — an allocation made and collected
# every iteration leaves it exactly as flat as no allocation at all — so the
# claim "every read path allocates nothing" is held here as BYTES PER
# ITERATION, per path, with the floor stated and every unavoidable allocation
# named.
.PHONY: tables-js-alloc
tables-js-alloc: build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	cd $(CURDIR) && $(NODE) --expose-gc test/js-tables/main.mjs alloc $(if $(ITERS),$(ITERS),300000)

# Its NEGATIVE CONTROL: ONE extra allocation per iteration, and every gated path
# must go red. An allocation gate that has never gone red is watching nothing.
.PHONY: tables-js-alloc-negative-control
tables-js-alloc-negative-control: build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	@if SCHEMA_JS_ALLOC_LEAK=1 $(NODE) --expose-gc test/js-tables/main.mjs alloc 100000 \
			> build/js-alloc-control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one extra allocation per iteration left the gate green"; \
		cat build/js-alloc-control.log; exit 1; \
	fi
	@grep -q "RenderFrame ships walk allocates" build/js-alloc-control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the path that must be zero"; \
		  cat build/js-alloc-control.log; exit 1; }
	@grep -m1 "FAILED: RenderFrame ships walk" build/js-alloc-control.log
	@echo "negative control: one extra allocation per iteration turns every zero-floor path RED"

# THE JAVASCRIPT PORT'S RELEASE GATE (certify.yml derives the target list from
# this file, so landing one is adding it here and nothing else). What sits
# behind it is the expensive half of this port's own instruments:
#
#   the FUZZ ORACLE at ten times the PR scale, because a forged block or cook
#   that escapes is found by depth of search and by nothing else;
#
#   the ALLOCATION GATE at seven times the iterations, which is the one that
#   matters most here: the floor it measures is a property of OPTIMIZED code,
#   so a longer run is a run that has spent more of itself at the top tier.
.PHONY: tables-js-release
tables-js-release: build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	$(MAKE) tables-js-fuzz N=200000
	$(MAKE) tables-js-alloc ITERS=2000000
	$(MAKE) tables-js-alloc-negative-control
	@echo "tables JS release gate: the fuzzer at depth, and the allocation floor at scale"

# THE REFUSAL IS SCOPED TO THE POINTERED TABLE, NOT THE UNIT. This gate was
# written when JavaScript had no table wire at all, so it asked that a pointered
# unit emit no Table module whatsoever; §3.4's fixed form gave the port its
# first one, and a unit's UNPOINTERED tables now get it. What has to hold is
# what §3.4 actually says — refusal is scoped to a CONSTRUCT, never the table
# declaration — so every POINTERED table is named as refused and has no Fixed
# surface at all, while the unit's fixed-size tables carry the form.
JS_POINTERED_TABLES := ListNode TreeNode Layer Scene Depot Album Marker
JS_FIXED_TABLES     := Meta Settings Tally Stamp

.PHONY: tables-js-refuses-pointers
tables-js-refuses-pointers: bin/schema
	@rm -rf build/tables-js-refusal && mkdir -p build
	./bin/schema generate --lang js --out build/tables-js-refusal tables/pointers
	@for t in $(JS_POINTERED_TABLES); do \
		grep -qh "table $$t has NO FIXED FORM in JavaScript" build/tables-js-refusal/*Table.js || \
			{ echo "REFUSAL GATE FAILED: pointered table $$t is not refused BY NAME in any module"; exit 1; }; \
		if grep -qh "^export function $${t}Fixed" build/tables-js-refusal/*Table.js; then \
			echo "REFUSAL GATE FAILED: the JavaScript backend emitted a fixed-form surface for pointered table $$t"; exit 1; \
		fi; \
	done
	@for t in $(JS_FIXED_TABLES); do \
		grep -qh "^export function $${t}FixedSave" build/tables-js-refusal/*Table.js || \
			{ echo "REFUSAL GATE FAILED: fixed-size table $$t lost its form because the unit holds pointers"; exit 1; }; \
	done
	@n=$$(ls build/tables-js-refusal/*Cook.js 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 4 ]; then \
			echo "REFUSAL GATE FAILED: found $$n Cook modules for the pointered unit, expected at least 4"; exit 1; \
		fi
	@echo "tables JS refusal gate: every pointered table of the unit is refused BY NAME with no fixed surface, its fixed-size tables keep the form, and its cooks and blocks are emitted"

# The NEGATIVE CONTROL: put the file-order rule back — the table runtime to the
# protocol id's home — and the home must MOVE when the earlier-sorting file
# joins. A gate that has never gone red is watching nothing.
# The JAVASCRIPT twin of the runtime-home gate: one home per unit per surface,
# named by the PACKAGE, so a file that sorts earlier relocates nothing.
.PHONY: tables-js-runtime-home
tables-js-runtime-home: bin/schema
	@rm -rf build/runtime-home-js && mkdir -p build/runtime-home-js/src
	@cp tables/examples/*.schema build/runtime-home-js/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home-js/src/Aaa.schema
	@./bin/schema generate --lang js --out build/runtime-home-js/base tables/examples
	@./bin/schema generate --lang js --out build/runtime-home-js/added build/runtime-home-js/src
	@for surface in Block Cook; do \
		base=$$(cd build/runtime-home-js/base && grep -l "the unit's shared runtime lives here" *$$surface.js); \
		added=$$(cd build/runtime-home-js/added && grep -l "the unit's shared runtime lives here" *$$surface.js); \
		if [ "$$base" != "Tabledemo$$surface.js" ] || [ "$$added" != "Tabledemo$$surface.js" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$surface runtime is in $$base before the added file and $$added after — expected Tabledemo$$surface.js both times"; exit 1; \
		fi; \
	done
	@grep -v "BuildVersion" build/runtime-home-js/base/TabledemoBlock.js > build/runtime-home-js/base.strip
	@grep -v "BuildVersion" build/runtime-home-js/added/TabledemoBlock.js > build/runtime-home-js/added.strip
	@cmp -s build/runtime-home-js/base.strip build/runtime-home-js/added.strip || \
		{ echo "RUNTIME HOME GATE FAILED: the JS block runtime's bytes moved when the unit gained a file"; exit 1; }
	@echo "runtime home gate (JS): the block and cook runtimes stay in <Package><Surface>.js when an earlier-sorting file joins the unit"

.PHONY: tables-js-runtime-home-negative-control
tables-js-runtime-home-negative-control: bin/schema tables-js-runtime-home
	@sed 's|home := capitalize(u.Package)|home := ir.ProtocolIdHome(u) // SABOTAGED: back to the file order|' \
		internal/codegen/jstable/jstable.go > build/jsruntime-fileorder.gotext
	@grep -q SABOTAGED build/jsruntime-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/jstable.go":"%s/build/jsruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/jsruntime-overlay.json
	@go build -overlay=build/jsruntime-overlay.json -o build/schema-jsruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-js/base-sabotage build/runtime-home-js/added-sabotage
	@./build/schema-jsruntime-sabotaged generate --lang js --out build/runtime-home-js/base-sabotage tables/examples
	@./build/schema-jsruntime-sabotaged generate --lang js --out build/runtime-home-js/added-sabotage build/runtime-home-js/src
	@base=$$(cd build/runtime-home-js/base-sabotage && grep -l "the unit's shared runtime lives here" *Block.js); \
	 added=$$(cd build/runtime-home-js/added-sabotage && grep -l "the unit's shared runtime lives here" *Block.js); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (JS): the file-order rule moves the runtime from $$base to $$added"

# the realworld unit sits in its own subdirectory like go/cs, so the two
# units' outputs never collide
generated/bench/js/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang js --out generated/bench/js bench/corpus/Bench.schema
	./bin/schema generate --lang js --out generated/bench/js/realworld bench/corpus/RealWorld.schema
	@touch $@

# THE PAIRED UNIT, for bench/tables/js — the FIXED FORM's leg in `bench/paired`
# (bench/paired/main.go, bench/tables/js/table_main.mjs). Bench.schema and
# FixedTable.schema are ONE unit, exactly as the C, C++, Go and C# paired
# generations are: the fixed root reaches the packet type so the compiler emits
# its table codec. Nothing compiles here — node runs these modules as written —
# and the driver regenerates the same files itself, so the two agree byte for
# byte or `generated-current` says so.
generated/bench/paired/js/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@mkdir -p generated/bench/paired/js
	./bin/schema generate --lang js --out generated/bench/paired/js bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@touch $@

# THE FIXED FORM'S PAIRED LEG, gated and not timed: the same no-clock `--gate`
# the C++ and C legs answer in `tables-fixed-matched`, over the same corpus.
# The clock lives in `go run ./bench/paired`, and a clock does not gate a build.
.PHONY: tables-js-fixed-matched
tables-js-fixed-matched: generated/bench/paired/js/.stamp
	cd $(CURDIR) && $(NODE) bench/tables/js/table_main.mjs --gate --indexed \
		--wire-dir bench/paired/corpus --variant-dir bench/paired/corpus

test-js: generated/bench/paired/js/.stamp tables-js-fixed-matched


# ---------------------------------------------------------------------------
# THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
# ---------------------------------------------------------------------------
#
# THE BYTES ARE THE C++ REFERENCE'S, and that is the whole point of this gate.
# The reference writes the paired bench's own sixty-four logical records with
# its form-3 writer and states their values beside them; the JavaScript leg
# reads that file, checks every field against those values, writes it back, and
# the bytes must be IDENTICAL. bench/paired has no JavaScript half to run the
# matched gate through (bench/paired/{go,cs} are the two it has), so this is
# that gate's shape for this port: the reference is the oracle, at gate time,
# over the same corpus.
#
# Beside it rides the VERSIONING CONFORMANCE — the FX1/FX2 pair the C++ leg
# uses (test/tables/fixedform_main.cpp), read the same way here — and the
# negative controls, of which the one §3.4 names is a reader given the WRONG
# PLAN for a record, which must come out wrong.
build/js-fixed-corpus/.stamp: generated/bench/paired/cpp/.stamp test/bench/fixedform_corpus.cpp bench/corpus/variants/bench_mixed.variants.bin
	@mkdir -p build/js-fixed-corpus
	$(CXX) $(CXXFLAGS) -Igenerated/bench/paired/cpp test/bench/fixedform_corpus.cpp -o build/js-fixed-corpus/corpus
	./build/js-fixed-corpus/corpus bench/corpus/variants/bench_mixed.variants.bin \
		build/js-fixed-corpus/bench_fixed.bin build/js-fixed-corpus/bench_fixed.oracle.json
	@touch $@

build/js-fixed/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema test/tables/FX1.schema test/tables/FX2.schema test/tables/P1.schema test/tables/P3.schema test/tables/FO1.schema test/tables/FO2.schema test/tables/UT1.schema test/tables/UT2.schema make/js.mk
	@mkdir -p build/js-fixed
	./bin/schema generate --lang js --out build/js-fixed/bench bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	./bin/schema generate --lang js --out build/js-fixed/fx1 test/tables/FX1.schema
	./bin/schema generate --lang js --out build/js-fixed/fx2 test/tables/FX2.schema
	./bin/schema generate --lang js --out build/js-fixed/p1 test/tables/P1.schema
	./bin/schema generate --lang js --out build/js-fixed/p3 test/tables/P3.schema
	./bin/schema generate --lang js --out build/js-fixed/fo1 test/tables/FO1.schema
	./bin/schema generate --lang js --out build/js-fixed/fo2 test/tables/FO2.schema
	./bin/schema generate --lang js --out build/js-fixed/ut1 test/tables/UT1.schema
	./bin/schema generate --lang js --out build/js-fixed/ut2 test/tables/UT2.schema
	@touch $@

# THE OPTIONAL CORPUS, and its bytes are the C++ REFERENCE'S TOO. `?T` is the
# one place this form departs from §2.3 — a present byte in front of a payload
# that rides whole — so the present byte's POSITION is a wire fact, and the
# only honest oracle for it is the other language's writer. The reference is
# built here against the SAME P1/P3/FO1 schemas the JavaScript leg reads, and
# writes the three files that leg checks its own bytes against.
build/js-fixed-optional/.stamp: bin/schema test/js-tables/fixedoptional_corpus.cpp test/tables/P1.schema test/tables/P3.schema test/tables/FO1.schema
	@mkdir -p build/js-fixed-optional
	./bin/schema generate --lang cpp --out build/js-fixed-optional/p1 test/tables/P1.schema
	./bin/schema generate --lang cpp --out build/js-fixed-optional/p3 test/tables/P3.schema
	./bin/schema generate --lang cpp --out build/js-fixed-optional/fo1 test/tables/FO1.schema
	$(CXX) $(CXXFLAGS) -Ibuild/js-fixed-optional/p1 -Ibuild/js-fixed-optional/p3 \
		-Ibuild/js-fixed-optional/fo1 test/js-tables/fixedoptional_corpus.cpp \
		-o build/js-fixed-optional/corpus
	./build/js-fixed-optional/corpus build/js-fixed-optional/p1.bin \
		build/js-fixed-optional/p3.bin build/js-fixed-optional/fo1.bin
	@touch $@

.PHONY: tables-js-fixed-form
tables-js-fixed-form: build/js-fixed/.stamp build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp
	cd $(CURDIR) && $(NODE) test/js-tables/fixedform.mjs build/js-fixed build/js-fixed-corpus build/js-fixed-optional

# ITS NEGATIVE CONTROL: move one byte of the write template and the leg must go
# red against the reference's corpus. Without this the byte comparison could be
# comparing a file with itself and nobody would know.
.PHONY: tables-js-fixed-form-negative-control
tables-js-fixed-form-negative-control: bin/schema build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp
	@rm -rf build/js-fixed-sabotage && mkdir -p build/js-fixed-sabotage
	@sed 's|g.pf("%sview.setInt32(%s, %s.%sLength, true);\\n", ind, off(base, at), val, name)|g.pf("%sview.setInt32(%s, %s.%sLength + 1, true);\\n", ind, off(base, at), val, name) // SABOTAGED|' \
		internal/codegen/jstable/fixedjs.go > build/js-fixed-sabotage/fixedjs.go.txt
	@grep -q SABOTAGED build/js-fixed-sabotage/fixedjs.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/fixedjs.go":"%s/build/js-fixed-sabotage/fixedjs.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-fixed-sabotage/overlay.json
	@go build -overlay=build/js-fixed-sabotage/overlay.json -o build/js-fixed-sabotage/schema ./cmd/schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/bench bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/fx1 test/tables/FX1.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/fx2 test/tables/FX2.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/p1 test/tables/P1.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/p3 test/tables/P3.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/fo1 test/tables/FO1.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/fo2 test/tables/FO2.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/ut1 test/tables/UT1.schema
	@./build/js-fixed-sabotage/schema generate --lang js --out build/js-fixed-sabotage/ut2 test/tables/UT2.schema
	@if $(NODE) test/js-tables/fixedform.mjs build/js-fixed-sabotage build/js-fixed-corpus \
			build/js-fixed-optional > build/js-fixed-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a string length one byte off left the fixed form green"; \
		cat build/js-fixed-sabotage/log; exit 1; \
	fi
	@grep -q "first byte differing from the C++ reference" build/js-fixed-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fixed form went red for another reason"; cat build/js-fixed-sabotage/log; exit 1; }
	@echo 'tables JS fixed form negative control: one byte off the write template reds the reference byte match'

# THE UNION-ARM TEXT LANE'S OWN CONTROL: put the text flavour back where the
# guard's value lives — the one line this defect was — and the leg must go red.
# A plan entry under a union arm carries the tag's OFFSET and the VALUE the tag
# must hold; a text entry carries a flavour too, and the three are three facts.
# Sharing a lane between the last two is invisible to every other gate in this
# file, because the identity path reads a whole body as ONE copy and never
# builds a text entry at all — so this control is what proves the pair is
# watching.
.PHONY: tables-js-union-arm-text-negative-control
tables-js-union-arm-text-negative-control: bin/schema build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp
	@rm -rf build/js-armtext-sabotage && mkdir -p build/js-armtext-sabotage
	@sed 's|TableFixedPush(plan, TableFixedOpText, theirAt, at, units, auxAt, guard, arg, dst\[row + TableFixedDstArg\]);|TableFixedPush(plan, TableFixedOpText, theirAt, at, units, auxAt, guard, dst[row + TableFixedDstArg], 0); // SABOTAGED|' \
		internal/codegen/jstable/fixedruntime.go > build/js-armtext-sabotage/fixedruntime.go.txt
	@sed -i.bak 's|const unit = e\[b + TableFixedLaneMeta\] === TableFixedTextWide ? 2 : 1;|const unit = e[b + TableFixedLaneArg] === TableFixedTextWide ? 2 : 1; // SABOTAGED|' \
		build/js-armtext-sabotage/fixedruntime.go.txt
	@test $$(grep -c SABOTAGED build/js-armtext-sabotage/fixedruntime.go.txt) -eq 2 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not patch both halves of the lane"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/fixedruntime.go":"%s/build/js-armtext-sabotage/fixedruntime.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-armtext-sabotage/overlay.json
	@go build -overlay=build/js-armtext-sabotage/overlay.json -o build/js-armtext-sabotage/schema ./cmd/schema
	@for u in bench:bench/corpus/Bench.schema fx1:test/tables/FX1.schema fx2:test/tables/FX2.schema \
			p1:test/tables/P1.schema p3:test/tables/P3.schema fo1:test/tables/FO1.schema \
			fo2:test/tables/FO2.schema ut1:test/tables/UT1.schema ut2:test/tables/UT2.schema; do \
		d=$${u%%:*}; f=$${u#*:}; \
		if [ "$$d" = bench ]; then \
			./build/js-armtext-sabotage/schema generate --lang js --out build/js-armtext-sabotage/bench \
				bench/corpus/Bench.schema bench/corpus/FixedTable.schema; \
		else \
			./build/js-armtext-sabotage/schema generate --lang js --out build/js-armtext-sabotage/$$d $$f; \
		fi; \
	done
	@if $(NODE) test/js-tables/fixedform.mjs build/js-armtext-sabotage build/js-fixed-corpus \
			build/js-fixed-optional > build/js-armtext-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the text flavour sharing the guard's lane left the leg green"; \
		cat build/js-armtext-sabotage/log; exit 1; \
	fi
	@grep -q "union-arm text:" build/js-armtext-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red for another reason"; cat build/js-armtext-sabotage/log; exit 1; }
	@echo 'tables JS union-arm text negative control: the text flavour back in the guard'"'"'s lane reds a text field under a union arm'

# THE OPTIONAL HALF'S OWN CONTROL: invert one present byte and the leg must go
# red against the reference's optional corpus. The `?T` bytes are the ones a
# port can get wrong while round-tripping its own records perfectly — the flag
# is a byte the packet wire has never carried — so the byte comparison against
# the reference is the only thing watching them, and this is what proves it is
# watching.
.PHONY: tables-js-fixed-optional-negative-control
tables-js-fixed-optional-negative-control: bin/schema build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp
	@rm -rf build/js-fixed-opt-sabotage && mkdir -p build/js-fixed-opt-sabotage
	@sed 's|Present ? 1 : 0);|Present ? 0 : 1);  // SABOTAGED|' \
		internal/codegen/jstable/fixedjs.go > build/js-fixed-opt-sabotage/fixedjs.go.txt
	@grep -q SABOTAGED build/js-fixed-opt-sabotage/fixedjs.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/fixedjs.go":"%s/build/js-fixed-opt-sabotage/fixedjs.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-fixed-opt-sabotage/overlay.json
	@go build -overlay=build/js-fixed-opt-sabotage/overlay.json -o build/js-fixed-opt-sabotage/schema ./cmd/schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/bench bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/fx1 test/tables/FX1.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/fx2 test/tables/FX2.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/p1 test/tables/P1.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/p3 test/tables/P3.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/fo1 test/tables/FO1.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/fo2 test/tables/FO2.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/ut1 test/tables/UT1.schema
	@./build/js-fixed-opt-sabotage/schema generate --lang js --out build/js-fixed-opt-sabotage/ut2 test/tables/UT2.schema
	@if $(NODE) test/js-tables/fixedform.mjs build/js-fixed-opt-sabotage build/js-fixed-corpus \
			build/js-fixed-optional > build/js-fixed-opt-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an inverted present byte left the optionals green"; \
		cat build/js-fixed-opt-sabotage/log; exit 1; \
	fi
	@grep -q "optionals: first byte differing from the C++ reference" build/js-fixed-opt-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the optionals went red for another reason"; cat build/js-fixed-opt-sabotage/log; exit 1; }
	@echo 'tables JS fixed optional negative control: one inverted present byte reds the reference byte match'


# THE JAVASCRIPT LEG of `make test`: the table accelerator gates and their
# negative controls, the runtime-home gate, and the packet tests in both node modes.
.PHONY: test-js
test-js: toolchain-js generated/js/.stamp generated/js-ludicrous/.stamp generated/bench/js/.stamp
	$(MAKE) tables-js-standalone
	$(MAKE) tables-js-refuses-pointers
	$(MAKE) tables-js-leg
	$(MAKE) tables-js-accessor-negative-control
	$(MAKE) tables-js-slot-negative-control
	$(MAKE) tables-js-fuzz
	$(MAKE) tables-js-fuzz-negative-control
	$(MAKE) tables-js-alloc
	$(MAKE) tables-js-alloc-negative-control
	$(MAKE) tables-js-runtime-home
	$(MAKE) tables-js-runtime-home-negative-control
	$(MAKE) tables-js-fixed-form
	$(MAKE) tables-js-fixed-form-negative-control
	$(MAKE) tables-js-fixed-optional-negative-control
	$(MAKE) tables-js-union-arm-text-negative-control
	cd test/js && $(NODE) main.mjs && NODE_ENV=production $(NODE) main.mjs
	cd test/js-ludicrous && $(NODE) main.mjs && NODE_ENV=production $(NODE) main.mjs

TEST_LEGS         += test-js
TOOLCHAIN_LEGS    += js
TOOLCHAIN_PINS_js  := NODE
CONFORMANCE_LEGS  += $(call unless_skipped,js,build/tables-generated-js/.stamp)
# Both JavaScript packet tiers share the UTF-8 rule and mutation corpus.
build/packet-text/js/.stamp: bin/schema test/packet-text/Narrow.schema
	./bin/schema generate --lang js --out build/packet-text/js test/packet-text/Narrow.schema
	@printf '{"type":"module"}\n' > build/packet-text/js/package.json
	@touch $@

.PHONY: packet-utf8-js packet-utf8-js-negative-control
packet-utf8-js: build/packet-text/js/.stamp build/packet-text/cpp/driver build/packet-text/harness
	@for mode in development production; do for tier in runtime flat; do \
		NODE_ENV=$$mode ./build/packet-text/harness $(NODE) test/packet-text/js/main.mjs $$tier || exit 1; \
	done; done

packet-utf8-js-negative-control: packet-utf8-js
	@mkdir -p build/packet-text/js-negative
	go run ./tools/sabotage -name packet-utf8-js-read -out build/packet-text/js-negative/utf8.gotext internal/codegen/js/utf8.go
	@printf '{"Replace":{"%s/internal/codegen/js/utf8.go":"%s/build/packet-text/js-negative/utf8.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/js-negative/overlay.json
	go run -overlay=build/packet-text/js-negative/overlay.json ./cmd/schema generate --lang js --out build/packet-text/js-negative test/packet-text/Narrow.schema
	cp build/packet-text/js/package.json build/packet-text/js-negative/package.json
	$(NODE) --check build/packet-text/js-negative/Narrow.js
	$(NODE) --check build/packet-text/js-negative/NarrowFlat.js
	@for tier in runtime flat; do \
		if NODE_ENV=production ./build/packet-text/harness -mutations-only $(NODE) test/packet-text/js/main.mjs $$tier "$(CURDIR)/build/packet-text/js-negative" > build/packet-text/js-negative/$$tier.log 2>&1; then echo 'NEGATIVE CONTROL FAILED: JS UTF-8 removal passed'; exit 1; fi; \
		grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/js-negative/$$tier.log || { cat build/packet-text/js-negative/$$tier.log; exit 1; }; \
	done
	@echo 'packet UTF-8 JS negative control: removed read validation fails both tiers in bit-flip agreement'

test-js: packet-utf8-js packet-utf8-js-negative-control


build/packet-wide/js/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema
	./bin/schema generate --lang js --out build/packet-wide/js build/packet-wide/source/WideText.schema
	./bin/schema generate --lang js --out build/packet-wide/js/shapes test/packet-wide/Shapes.schema
	@printf '{"type":"module"}\n' > build/packet-wide/js/package.json
	@touch $@

.PHONY: packet-wide-js packet-wide-js-negative-control
packet-wide-js: build/packet-wide/js/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	@for mode in development production; do for tier in runtime flat; do \
		NODE_ENV=$$mode $(NODE) test/packet-wide/js/main.mjs $$tier --contracts || exit 1; \
		NODE_ENV=$$mode ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver $(NODE) test/packet-wide/js/main.mjs $$tier || exit 1; \
	done; done

packet-wide-js-negative-control: packet-wide-js
	@mkdir -p build/packet-wide/js-negative
	go run ./tools/sabotage -name packet-wide-js-pairing -out build/packet-wide/js-negative/wstring.gotext internal/codegen/js/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/js/wstring.go":"%s/build/packet-wide/js-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/js-negative/overlay.json
	go run -overlay=build/packet-wide/js-negative/overlay.json ./cmd/schema generate --lang js --out build/packet-wide/js-negative build/packet-wide/source/WideText.schema
	cp build/packet-wide/js/package.json build/packet-wide/js-negative/package.json
	$(NODE) --check build/packet-wide/js-negative/WideText.js
	$(NODE) --check build/packet-wide/js-negative/WideTextFlat.js
	@for tier in runtime flat; do \
		if NODE_ENV=production ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only $(NODE) test/packet-wide/js/main.mjs $$tier "$(CURDIR)/build/packet-wide/js-negative" > build/packet-wide/js-negative/$$tier.log 2>&1; then echo 'NEGATIVE CONTROL FAILED: JS wide pairing removal passed'; exit 1; fi; \
		grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/js-negative/$$tier.log || { cat build/packet-wide/js-negative/$$tier.log; exit 1; }; \
	done
	@echo 'packet wide JS negative control: removed pairing fails both tiers in bit-flip agreement'

test-js: packet-wide-js packet-wide-js-negative-control

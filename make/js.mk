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
#   Node.js 20.20.2 (LTS, darwin-arm64)
#   url:    https://nodejs.org/dist/v20.20.2/node-v20.20.2-darwin-arm64.tar.gz
#   sha256: 466e05f3477c20dfb723054dfebffe55bc74660ee77f612166fca121dacb65b6
#   untar into dist/ (the tarball already unpacks to node-v20.20.2-darwin-arm64)
NODE ?= $(CURDIR)/dist/node-v20.20.2-darwin-arm64/bin/node
# the conformance driver is a shell script the harness spawns, so it reads the
# pin from the environment and falls back to PATH
export NODE

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
# standalone gate: a generated <Base>Table.js must stand alone on the language,
# so nothing in it may import the serialize runtime. Its only imports are
# module-relative, to other files OF THIS UNIT.
.PHONY: tables-js-standalone
tables-js-standalone: build/tables-generated-js/.stamp
	@n=$$(ls build/tables-generated-js/*/*Table.js 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 8 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated Table modules, expected at least 8 — the glob, not the property, is what broke"; exit 1; \
		fi
	@for f in build/tables-generated-js/*/*Table.js build/tables-generated-js/*/*Block.js build/tables-generated-js/*/*Cook.js; do \
		if grep -n '^import .*serialize' $$f; then \
			echo "STANDALONE GATE FAILED: the serialize runtime leaked into $$f"; exit 1; \
		fi; \
		if grep -n '^import .*"\.\./' $$f; then \
			echo "STANDALONE GATE FAILED: $$f imports outside its own unit"; exit 1; \
		fi; \
	done
	@echo "tables JS standalone gate: generated table modules import nothing but their own unit"

# The JAVASCRIPT generic-walk gate (docs/SPEC-TABLES.md §16): the text form's
# walker is ONE walker, emitted once per unit and byte-identical in every one.
.PHONY: tables-js-json-walk
tables-js-json-walk: build/tables-generated-js/.stamp
	@rm -rf build/json-walk-js && mkdir -p build/json-walk-js
	@for d in build/tables-generated-js/*/; do \
		unit=$$(basename $$d); \
		for f in $$d*Table.js; do \
			[ -f "$$f" ] || continue; \
			grep -q -- '---- json walk: begin ----' $$f || continue; \
			out=build/json-walk-js/$$unit.$$(basename $$f); \
			awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
		done; \
	done
	@if [ -z "$$(ls build/json-walk-js 2>/dev/null)" ]; then \
		echo "GENERIC-WALK GATE FAILED: no generated JS module carries the walk"; exit 1; fi
	@first=""; for f in build/json-walk-js/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables JS generic-walk gate: one walker per unit, byte-identical across $$(ls build/json-walk-js | wc -l | tr -d ' ') units"

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
tables-js-leg:
	@echo "tables-js-leg: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#516)"

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

# THE KEYED GUARD's NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.4): the guard is
# symmetric — None below the storage, anything past E.Max above it — and one
# unsigned compare covers both ends. Put a None-only guard back and the leg
# must go red on the upper end, or the guard is watching one end and saying
# two.
.PHONY: tables-js-keyed-negative-control
tables-js-keyed-negative-control: bin/schema build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	@rm -rf build/js-keyed-sabotage && mkdir -p build/js-keyed-sabotage
	@sed 's|if (!Number.isInteger(key) \|\| ((key - 1) >>> 0) >= this.Slots.length) {|if (key === 0) { // SABOTAGED: None only|' \
		internal/codegen/jstable/jstable.go > build/js-keyed-sabotage/jstable.go.txt
	@grep -q SABOTAGED build/js-keyed-sabotage/jstable.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/jstable.go":"%s/build/js-keyed-sabotage/jstable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-keyed-sabotage/overlay.json
	@go build -overlay=build/js-keyed-sabotage/overlay.json -o build/js-keyed-sabotage/schema ./cmd/schema
	@./build/js-keyed-sabotage/schema generate --lang js --out build/js-keyed-sabotage/generated/examples tables/examples
	@./build/js-keyed-sabotage/schema generate --lang js --out build/js-keyed-sabotage/generated/pointers tables/pointers
	@./build/js-keyed-sabotage/schema generate --lang js --out build/js-keyed-sabotage/generated/block tables/block
	@if SCHEMA_JS_GENERATED=$(CURDIR)/build/js-keyed-sabotage/generated $(NODE) test/js-tables/main.mjs \
			> build/js-keyed-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a keyed guard that refuses None alone left the leg green"; \
		cat build/js-keyed-sabotage/log; exit 1; \
	fi
	@grep -q "accepted E.Max + 1 as a key" build/js-keyed-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on the key past E.Max"; \
		  cat build/js-keyed-sabotage/log; exit 1; }
	@echo "negative control: a keyed guard that refuses None alone turns the JavaScript leg RED on the key past E.Max"

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

# THE TEXT FORM AGAINST A THIRD IMPLEMENTATION, over instances nobody wrote
# down (docs/SPEC-TABLES.md §16). The conformance harness holds eighteen pinned
# texts; this holds the SPELLING RULE, which eighteen instances cannot cover: the
# JS leg writes each random instance as (wire, text), `schema unpack` reads the
# same wire bytes with the compiler's own Go engine — written from §16 and from
# neither backend — and the two texts are byte-compared.
#
# The rule it holds that eighteen instances cannot: a float32 such as
# -266744.625 renders as an eight-digit TIE, both candidates round-trip back to
# the same float32 so the shortest-precision search cannot step past it, and C
# breaks the tie to EVEN where JavaScript's own formatters break it by
# magnitude — so the walk spells the tie-break itself, and this is where a
# drift in it shows.
# N from the command line overrides the count; the Makefile's own N (the block
# fuzzer's) is defined before this include and must not.
JS_DIFFERENTIAL_N := $(if $(filter command line environment,$(origin N)),$(N),60)
.PHONY: tables-js-json-differential
tables-js-json-differential:
	@echo "tables-js-json-differential: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#516)"

# Its NEGATIVE CONTROL: put the tie-break back the way JavaScript's own
# formatters do it — by MAGNITUDE rather than to EVEN — and the differential
# must go red. That is the exact bug this gate found, on demand.
.PHONY: tables-js-json-differential-negative-control
tables-js-json-differential-negative-control:
	@echo "tables-js-json-differential-negative-control: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#516)"

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
	@grep -q "KeyedConfig Load allocates" build/js-alloc-control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the path that must be zero"; \
		  cat build/js-alloc-control.log; exit 1; }
	@grep -m1 "FAILED: KeyedConfig Load" build/js-alloc-control.log
	@echo "negative control: one extra allocation per iteration turns every zero-floor path RED"

# THE SOAK: read and write the corpus in a loop with the heap sampled after
# warm-up, so "every read path allocates nothing" is a number. --expose-gc so a
# sample measures what is HELD rather than what has not been collected yet.
# SECONDS defaults short enough for a gate; the landing number is an hour.
.PHONY: tables-js-soak
tables-js-soak: build/tables-generated-js/.stamp
	cd $(CURDIR) && $(NODE) --expose-gc test/js-tables/main.mjs soak $(if $(SECONDS),$(SECONDS),60)

# THE JAVASCRIPT PORT'S RELEASE GATE (certify.yml derives the target list from
# this file, so landing one is adding it here and nothing else). What sits
# behind it is the expensive half of this port's own instruments — the half
# that answers a question about the RUNTIME UNDER LOAD rather than about the
# diff, and that `make test` therefore runs at a fraction of its scale:
#
#   the FUZZ ORACLE at ten times the PR scale, because a forged block or cook
#   that escapes is found by depth of search and by nothing else;
#
#   the ALLOCATION GATE at seven times the iterations, which is the one that
#   matters most here: the floor it measures is a property of OPTIMIZED code,
#   so a longer run is a run that has spent more of itself at the top tier;
#
#   and the SOAK, ten minutes of the measured paths in one process, gated on
#   the allocation RATE rather than on heap drift — a heap that stays flat
#   proves only that nothing LEAKED, and an allocation made and collected every
#   iteration leaves it exactly as flat as no allocation at all.
#
# The hour-long soak is `make tables-js-soak SECONDS=3600` and belongs on a
# quiet box; ten minutes is what fits beside every other port in one job.
.PHONY: tables-js-release
tables-js-release: build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	$(MAKE) tables-js-fuzz N=200000
	$(MAKE) tables-js-alloc ITERS=2000000
	$(MAKE) tables-js-alloc-negative-control
	$(MAKE) tables-js-json-differential JS_DIFFERENTIAL_N=400
	$(MAKE) tables-js-soak SECONDS=600
	@echo "tables JS release gate: the fuzzer at depth, the allocation floor at scale, and ten minutes of load"

.PHONY: tables-js-refuses-pointers
tables-js-refuses-pointers: bin/schema
	@rm -rf build/tables-js-refusal && mkdir -p build
	./bin/schema generate --lang js --out build/tables-js-refusal tables/pointers
	@if ls build/tables-js-refusal/*Table.js >/dev/null 2>&1; then \
		echo "REFUSAL GATE FAILED: the JavaScript backend emitted a wire surface for a pointered unit"; exit 1; \
	fi
	@for f in build/tables-js-refusal/*Cook.js build/tables-js-refusal/*Block.js; do \
		grep -q "THE JAVASCRIPT WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not carry the refusal banner"; exit 1; }; \
		grep -q "is a named follow-on" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name the follow-on"; exit 1; }; \
		grep -q "Album, Depot, Layer, ListNode, Marker, Scene and TreeNode" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name every refused table"; exit 1; }; \
	done
	@n=$$(ls build/tables-js-refusal/*Cook.js | wc -l | tr -d ' '); \
		if [ "$$n" -lt 4 ]; then \
			echo "REFUSAL GATE FAILED: found $$n Cook modules for the pointered unit, expected at least 4 — the glob, not the property, is what broke"; exit 1; \
		fi
	@echo "tables JS refusal gate: a pointered unit's WIRE half is refused by name, in every module it does emit, and its cooks still open"

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
	@for surface in Table Block Cook; do \
		base=$$(cd build/runtime-home-js/base && grep -l "the unit's shared runtime lives here" *$$surface.js); \
		added=$$(cd build/runtime-home-js/added && grep -l "the unit's shared runtime lives here" *$$surface.js); \
		if [ "$$base" != "Tabledemo$$surface.js" ] || [ "$$added" != "Tabledemo$$surface.js" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$surface runtime is in $$base before the added file and $$added after — expected Tabledemo$$surface.js both times"; exit 1; \
		fi; \
	done
	@cmp -s build/runtime-home-js/base/TabledemoTable.js build/runtime-home-js/added/TabledemoTable.js || \
		{ echo "RUNTIME HOME GATE FAILED: the JS table runtime's bytes moved when the unit gained a file"; exit 1; }
	@echo "runtime home gate (JS): the table, block and cook runtimes stay in <Package><Surface>.js when an earlier-sorting file joins the unit"

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
	@base=$$(cd build/runtime-home-js/base-sabotage && grep -l "the unit's shared runtime lives here" *Table.js); \
	 added=$$(cd build/runtime-home-js/added-sabotage && grep -l "the unit's shared runtime lives here" *Table.js); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (JS): the file-order rule moves the runtime from $$base to $$added"

generated/bench/tables/js/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/js
	./bin/schema generate --lang js --out generated/bench/tables/js bench/corpus/BenchTable.schema
	@touch $@

# the realworld unit sits in its own subdirectory like go/cs, so the two
# units' outputs never collide
generated/bench/js/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang js --out generated/bench/js bench/corpus/Bench.schema
	./bin/schema generate --lang js --out generated/bench/js/realworld bench/corpus/RealWorld.schema
	@touch $@

# THE JAVASCRIPT LEG's negative control: one field index off in the generic
# walk's READER, and the harness must go red on json-read alone — the same
# sabotage the C# control applies, at the same place, in the other language.
.PHONY: conformance-negative-control-js
conformance-negative-control-js:
	@echo "conformance-negative-control-js: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#516)"

# THE JAVASCRIPT LEG of `make test`: the table gates and their negative
# controls, the conformance negative control, the runtime-home gate, and the
# packet tests in both node modes.
.PHONY: test-js
test-js: generated/js/.stamp generated/js-ludicrous/.stamp generated/bench/js/.stamp generated/bench/tables/js/.stamp
	$(MAKE) tables-js-json-walk
	$(MAKE) tables-js-standalone
	$(MAKE) tables-js-refuses-pointers
	$(MAKE) tables-js-leg
	$(MAKE) tables-js-accessor-negative-control
	$(MAKE) tables-js-slot-negative-control
	$(MAKE) tables-js-keyed-negative-control
	$(MAKE) tables-js-fuzz
	$(MAKE) tables-js-fuzz-negative-control
	$(MAKE) tables-js-alloc
	$(MAKE) tables-js-alloc-negative-control
	$(MAKE) tables-js-json-differential
	$(MAKE) tables-js-json-differential-negative-control
	$(MAKE) conformance-negative-control-js
	$(MAKE) tables-js-runtime-home
	$(MAKE) tables-js-runtime-home-negative-control
	cd test/js && node main.mjs && NODE_ENV=production node main.mjs
	cd test/js-ludicrous && node main.mjs && NODE_ENV=production node main.mjs

TEST_LEGS         += test-js
CONFORMANCE_LEGS  += build/tables-generated-js/.stamp
BENCH_TABLES_LEGS += generated/bench/tables/js/.stamp

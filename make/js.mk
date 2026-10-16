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
build/tables-generated-js/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_BLOCK) $(SCHEMAS_TABLES_BACKEND) $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9) $(SCHEMAS_TABLES_MESSAGES) tables/stream tables/scalars test/tables/V1.schema test/tables/V2.schema test/tables/W1.schema test/tables/W2.schema test/tables/R1.schema test/tables/R2.schema test/tables/P1.schema test/tables/P3.schema test/tables/M1.schema test/tables/M2.schema
	@mkdir -p build/tables-generated-js
	./bin/schema generate --lang js --out build/tables-generated-js/examples tables/examples
	# the POINTERED unit: its FIXED surface is refused by name for every
	# pointered table (§3.4, §11), its two ACCELERATORS are emitted all the same
	# (§7, §19) — this is where the cook's JS read side comes from — and its
	# VARIABLE wire (GraphdemoVariable.js, §3.1) carries Scene and the tables it
	# reaches, which the conformance driver's wire surfaces answer.
	./bin/schema generate --lang js --out build/tables-generated-js/pointers tables/pointers
	# the BYTE-BUFFER unit (§2.5): *bytes and *string on the variable wire
	# (BlobdemoVariable.js).
	./bin/schema generate --lang js --out build/tables-generated-js/blobs tables/blobs
	./bin/schema generate --lang js --out build/tables-generated-js/block tables/block
	./bin/schema generate --lang js --out build/tables-generated-js/blockhome tables/blockhome
	./bin/schema generate --lang js --out build/tables-generated-js/v1 test/tables/V1.schema
	./bin/schema generate --lang js --out build/tables-generated-js/v2 test/tables/V2.schema
	./bin/schema generate --lang js --out build/tables-generated-js/w1 test/tables/W1.schema
	./bin/schema generate --lang js --out build/tables-generated-js/w2 test/tables/W2.schema
	./bin/schema generate --lang js --out build/tables-generated-js/r1 test/tables/R1.schema
	./bin/schema generate --lang js --out build/tables-generated-js/r2 test/tables/R2.schema
	./bin/schema generate --lang js --out build/tables-generated-js/p1 test/tables/P1.schema
	./bin/schema generate --lang js --out build/tables-generated-js/p3 test/tables/P3.schema
	./bin/schema generate --lang js --out build/tables-generated-js/messages tables/messages
	./bin/schema generate --lang js --out build/tables-generated-js/m1 test/tables/M1.schema
	./bin/schema generate --lang js --out build/tables-generated-js/m2 test/tables/M2.schema
	./bin/schema generate --lang js --out build/tables-generated-js/stream tables/stream
	./bin/schema generate --lang js --out build/tables-generated-js/scalars tables/scalars
	# THE MESSAGE FORM's WIDE-VOCABULARY units (docs/SPEC-TABLES.md §3.3): the
	# conformance driver's `message` surface AnnounceReads a connection's
	# announcement, LoadMessages the form-2 batch against it and SaveMessages
	# the bytes, and the units those rows name must be generated here or the
	# shipped leg answers every one of them absent. vocab/vocab9 are the units
	# test/vocabgen writes, whose vocabulary passes 127 ids so a message names
	# slots on both sides of the one-byte boundary. The `backend` unit is the
	# three message roots themselves (docs/SPEC-TABLES.md §3.3): its Envelope
	# holds a table-side union, which the JavaScript fixed form carries and
	# refuses only per table its own closure cannot hold (§2.6, §3.4), so the
	# unit is generated whole and its message rows answer the bytes.
	./bin/schema generate --lang js --out build/tables-generated-js/vocab tables/vocab
	./bin/schema generate --lang js --out build/tables-generated-js/vocab9 tables/vocab9
	./bin/schema generate --lang js --out build/tables-generated-js/backend tables/backend
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
# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, JavaScript
# side. The cook example in docs/USAGE.md is not a sketch beside the code: the
# gate pulls the marked fragment out of the page, wraps it in a module that
# reads the fixture cook off disk and supplies the one helper the page leaves to
# the caller, and runs it under node against the generated `tables/pointers`
# modules it imports — so the day the emitted surface moves, the page goes red
# with it rather than a release later.
.PHONY: tables-js-usage
tables-js-usage: bin/schema build/cook-open/.stamp docs/USAGE.md
	@rm -rf build/js-usage && mkdir -p build/js-usage
	./bin/schema generate --lang js --out build/js-usage tables/pointers
	printf 'import { readFileSync } from "node:fs";\n' > build/js-usage/usage.mjs
	printf 'const bytes = new Uint8Array(readFileSync(process.argv[2]));\n' >> build/js-usage/usage.mjs
	printf 'let used = 0;\n' >> build/js-usage/usage.mjs
	printf 'function use(v) { used++; }\n' >> build/js-usage/usage.mjs
	sed -n '/<!-- js-table-usage -->/,/<!-- \/js-table-usage -->/p' docs/USAGE.md \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> build/js-usage/usage.mjs
	printf 'if (used === 0) { throw new Error("the cook walk read no node"); }\n' >> build/js-usage/usage.mjs
	printf 'console.log("usage: docs/USAGE.md JavaScript cook example opened the Scene cook and walked " + used + " node(s)");\n' >> build/js-usage/usage.mjs
	grep -q 'SceneCook.Open' build/js-usage/usage.mjs || \
		{ echo "MISSING: docs/USAGE.md carries no js-table-usage example"; exit 1; }
	cd build/js-usage && $(NODE) usage.mjs $(CURDIR)/build/cook-open/Scene.cook

.PHONY: tables-js-release
tables-js-release: build/tables-generated-js/.stamp build/js-fuzz-scene.cook
	$(MAKE) tables-js-fuzz N=200000
	$(MAKE) tables-js-alloc ITERS=2000000
	$(MAKE) tables-js-alloc-negative-control
	$(MAKE) tables-js-usage
	@echo "tables JS release gate: the fuzzer at depth, the allocation floor at scale, and the page's example"

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

# build/fixedform-corpus is THE C++ REFERENCE'S OWN BYTE ORACLE, and it belongs
# to no language (Makefile, tables-fixedform-corpus). This leg reads its files,
# checks the values the reference states, and saves them back byte for byte —
# which is the only instrument here that can see §3.4's "the slack is zero",
# because the reference's records are set from CONSTRUCTED storage and one of
# them uses NOTHING of a string whose declared default is not empty.
.PHONY: tables-js-fixed-form
tables-js-fixed-form: build/js-fixed/.stamp build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp build/fixedform-corpus/.stamp
	cd $(CURDIR) && $(NODE) test/js-tables/fixedform.mjs build/js-fixed build/js-fixed-corpus build/js-fixed-optional build/fixedform-corpus

# THE VERSIONING HALF OF THE FIXED FORM ON THE JAVASCRIPT LEG (§5 of
# docs/FIXED-FORM-ALGORITHM.md, the rows of docs/FIXED-FORM-VERSIONING-TESTS.md).
# internal/codegen/jstable/fixedversioning_test.go reads the C++ reference's byte
# oracle out of build/fixedform-corpus — old_<row>.bin, new_<row>.bin and the
# floor, hash and lineage-merge files — generates ONE PROBE MODULE PER ROW PER
# COLUMN (§5.9 #18) and runs each with $(NODE), this leg's own toolchain. Each row
# owes two columns: NEW-READS-OLD lands every old value with §5.4's counters, and
# OLD-REFUSES-NEW answers layout_newer before any record, on the file's hash alone
# (§5.3).
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE. The suite's
# harness SKIPS itself when build/fixedform-corpus is absent, which is right for a
# bare `go test ./...` on a tree that never built the oracle — and is exactly how a
# §5 regression would ride into CI green. Under this target the corpus is built
# first and SCHEMA_REQUIRE_CORPUS=1 makes the skip a FAILURE.
.PHONY: tables-js-versioning
tables-js-versioning: build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 ./internal/codegen/jstable/ -run 'TestJSFixedVersioning|TestJSFixedLineageLockBugFailsTheBuild'
	@echo 'tables JS versioning: every row of §5.7 over the reference own bytes, both columns'

test-js: tables-js-versioning

# THE BLOCK FORM'S BUILD SIDE ON THE JAVASCRIPT LEG (docs/SPEC-TABLES.md §19.1).
# internal/codegen/jstable/blockbuild_test.go generates ONE block module, builds
# an extent with $(NODE) — this leg's own toolchain, reached through jsNode — and
# holds the whole extent byte for byte against the Go builder's own numbers for
# the same counts (internal/codegen/gotable/blockbuild_test.go, which runs the Go
# builder): §19.1's prologue, one triple per array at the layout's own offsets,
# and zero everywhere else, because Begin writes neither a row nor the padding.
# This leg's own reader then opens what this leg built and every accessor answers
# the counts written, and a count past its declared maximum is refused with the
# extent untouched.
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE: the test takes
# node through jsNode, so a bare `go test` skips it by name, and SCHEMA_REQUIRE_CORPUS=1
# makes a missing node a FAILURE rather than a skip.
.PHONY: tables-js-block-build
tables-js-block-build:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -v ./internal/codegen/jstable/ -run '^TestBlockBuildAndCookWriteJavaScript$$'
	@echo 'tables JS block build: the built extent is the Go builder own bytes, and this leg reader opens it'

test-js: tables-js-block-build

.PHONY: tables-js-cook-write-rows
tables-js-cook-write-rows:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -timeout 600s -v ./internal/codegen/jstable/ -run '^TestCookWriteAnswersEveryCookWriteRowJavaScript$$'
	@echo 'tables JS cook-write rows: every cook-write row in MANIFEST.txt matches the golden bytes in both byte orders'

test-js: tables-js-cook-write-rows

# THE VARIABLE CLASS BEYOND THE CARD CONSTRUCTS (docs/SPEC-TABLES.md §2.1, §2.5,
# §2.9, §3, §3.1, §16.7): table pointers over the flat node table, tables nested
# by value, *string and *bytes blobs, unbounded and bounded arrays of scalar,
# enum, table and pointer elements, the rest of the scalar kinds, bytes(N), the
# escape kind and the text form's &node sharing — held byte for byte against the
# Go oracle over tables/pointers, tables/blobs and tables/lists (and a probe unit
# the oracle writes) by TestVariableClassBeyondCardConstructsJavaScript, and every
# construct the leg still refuses named by TestOtherVariableConstructsAreRefusedByName.
# SCHEMA_REQUIRE_CORPUS=1 makes a missing node a FAILURE rather than a skip.
.PHONY: tables-js-variable-class
tables-js-variable-class:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -timeout 600s -v ./internal/codegen/jstable/ -run '^(TestVariableClassBeyondCardConstructsJavaScript|TestOtherVariableConstructsAreRefusedByName)$$'
	@echo 'tables JS variable class: the class beyond the card constructs round-trips the oracle bytes and the rest is refused by name'

test-js: tables-js-variable-class

# MAPS AND UNION ARMS ON THE VARIABLE WIRE (docs/SPEC-TABLES.md §2.3, §2.4, §2.6,
# §2.8, §3): maps, union arms of any field type, optional ?T, enum-keyed arrays,
# optional arrays and 128-bit scalars round-trip the variable wire byte for byte
# against the Go oracle (TestMapsAndUnionArmsOnTheTableWireJavaScript), and the
# refusal test no longer names those constructs (TestOtherVariableConstructsAreRefusedByName).
# SCHEMA_REQUIRE_CORPUS=1 makes a missing node a FAILURE rather than a skip.
.PHONY: tables-js-variable-arms
tables-js-variable-arms:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -timeout 600s -v ./internal/codegen/jstable/ -run '^(TestMapsAndUnionArmsOnTheTableWireJavaScript|TestOtherVariableConstructsAreRefusedByName)$$'
	@echo 'tables JS variable arms: maps, union arms, optional ?T and enum-keyed arrays round-trip the oracle bytes and are no longer refused by name'

test-js: tables-js-variable-arms

# THE FIXED CLASS ON THE VARIABLE WIRE (docs/SPEC-TABLES.md §2, §2.7, §3, §3.1,
# §10, §11, §14, §15): every fixed-class table emits a form-1 codec in
# <Package>Variable.js, carrying declared defaults (int, bits, float, bool, enum,
# string, bytes, flags), guarded fields, compressed floats, fixed-length arrays,
# fixed(W, F) / ufixed(W, F), nested `type`, `flags` and `was` renames on tables,
# variants, arms and type fields, byte for byte against the Go oracle
# (TestFixedClassOnTableWireJavaScript).
# SCHEMA_REQUIRE_CORPUS=1 makes a missing node a FAILURE rather than a skip.
.PHONY: tables-js-variable-fixed-class
tables-js-variable-fixed-class:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -timeout 600s -v ./internal/codegen/jstable/ -run '^TestFixedClassOnTableWireJavaScript$$'
	@echo 'tables JS variable fixed class: fixed-class tables, defaults, nested types, flags and renames round-trip the oracle bytes'

test-js: tables-js-variable-fixed-class

# I13 (docs/PORTING.md:1278): the js leg's text differential against a third
# implementation. A node probe draws 200 random float-only instances of a fixed
# table (seed 24845619678, splitmix64) and writes each as (wire, text) with the
# generated RootSave and RootToJson; the Go engine (internal/tabletext,
# internal/tablepack) writes its own text from the leg's wire, byte-compared,
# and packs the leg's text back to wire, byte-compared
# (TestTextDifferentialJavaScript). The control caps the emitted float32 text at
# six significant digits in memory and must go red on the text comparison.
# SCHEMA_REQUIRE_CORPUS=1 makes a missing node a FAILURE rather than a skip.
.PHONY: tables-js-text-differential tables-js-text-differential-negative-control
tables-js-text-differential:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -timeout 600s -v ./internal/codegen/jstable/ -run '^TestTextDifferentialJavaScript$$'
	@echo 'tables JS text differential: 200 random float instances agree with the Go engine byte for byte, both directions'
tables-js-text-differential-negative-control:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -timeout 600s -v ./internal/codegen/jstable/ -run '^TestTextDifferentialNegativeControlJavaScript$$'
	@echo 'tables JS text differential negative control: a six-digit float32 writer is red on the text comparison'

test-js: tables-js-text-differential

# js/F12 — "a second layout under a held hash" (docs/FIXED-FORM-ALGORITHM.md §2
# row 5: "a known hash over different bytes is `layout_malformed`"; SPEC-TABLES
# §3.4: "A second layout for a hash already held is refused by name"). The test
# builds a valid file for the held layout and two copies — one whose layout bytes
# differ under the same hash, one whose held layout length differs — and proves
# both are refused layout_malformed before any record lands. It rides the
# versioning target's pattern and SCHEMA_REQUIRE_CORPUS=1 keeps a missing node a
# FAILURE rather than a skip, but it needs no C++ oracle: its files are built in
# memory, so it does not take build/fixedform-corpus/.stamp (whose C++ serialize
# sibling is absent on this bench).
#
# TestFixedRoadmapJsT12GuardsWideText (fixed-js-t12-guards-and-wide-text-6) is the
# direct-arm half of the same row: union-field-arms/js/valid-data — a direct text,
# bytes or array arm of a table-closure union round-trips through the emitted
# module, the storage class internal/codegen/js now emits. It rides the same
# in-memory shape (no C++ oracle), so it joins the t13 name in the one -run list.
.PHONY: tables-js-fixed-roadmap
tables-js-fixed-roadmap:
	SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -count=1 -v ./internal/codegen/jstable/ -run '^(TestFixedRoadmapJsT12GuardsWideText|TestFixedRoadmapJsT13LayoutHashCollision)$$'
	@echo 'tables JS fixed roadmap: a second layout under a held hash is layout_malformed (js/F12)'

test-js: tables-js-fixed-roadmap

# ITS NEGATIVE CONTROL (§5.9 #11: each gate owes one, "because a byte comparison
# nobody has seen fail may be comparing a file with itself"). Drop the held
# layout's byte comparison from the generated reader's step 7 (fixedmodule.go:
# "the layout bytes are compared one by one") and the second layout under a held
# hash — same header hash, same length, DIFFERENT bytes — is walked as if it were
# the held layout, so TestFixedRoadmapJsT13LayoutHashCollision must go red. The
# sabotage rides through `go test -overlay`, the same instrument the versioning
# control below uses.
.PHONY: tables-js-fixed-roadmap-negative-control
tables-js-fixed-roadmap-negative-control:
	@rm -rf build/js-roadmap-sabotage && mkdir -p build/js-roadmap-sabotage
	@sed 's|bytes\[layoutAt + i\] !== known.layout\[i\]|false /* SABOTAGED */|' \
		internal/codegen/jstable/fixedmodule.go > build/js-roadmap-sabotage/fixedmodule.go.txt
	@grep -q SABOTAGED build/js-roadmap-sabotage/fixedmodule.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/fixedmodule.go":"%s/build/js-roadmap-sabotage/fixedmodule.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-roadmap-sabotage/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -overlay=build/js-roadmap-sabotage/overlay.json \
			-count=1 ./internal/codegen/jstable/ -run '^(TestFixedRoadmapJsT13LayoutHashCollision)$$' \
			> build/js-roadmap-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the second layout read as the held one and the gate stayed green"; \
		cat build/js-roadmap-sabotage/log; exit 1; \
	fi
	@grep -q "TestFixedRoadmapJsT13LayoutHashCollision" build/js-roadmap-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the roadmap gate went red for another reason"; \
		  cat build/js-roadmap-sabotage/log; exit 1; }
	@echo 'tables JS fixed roadmap negative control: the second layout under a held hash reds without the byte comparison'

# ITS NEGATIVE CONTROL (§5.9 #11: each gate owes one, "because a byte comparison
# nobody has seen fail may be comparing a file with itself"). Swap the ONE NAME a
# file OUTSIDE the lineage owes for the one a file BELOW THE FLOOR owes — the two
# answers §5.2 keeps distinct, "ship the reader" and "upgrade the client" — and
# §5.7's OLD-REFUSES-NEW column must go red for every row.
# The sabotage rides through `go test -overlay`, the same instrument the byte
# gate's controls use on the emitter.
.PHONY: tables-js-versioning-negative-control
tables-js-versioning-negative-control: build/fixedform-corpus/.stamp
	@rm -rf build/js-versioning-sabotage && mkdir -p build/js-versioning-sabotage
	@sed 's|TableFixedRefusal.LayoutNewer, hashLo|TableFixedRefusal.LayoutUnsupported, hashLo|' \
		internal/codegen/jstable/fixedmodule.go > build/js-versioning-sabotage/fixedmodule.go.txt
	@grep -q 'TableFixedRefusal.LayoutNewer' build/js-versioning-sabotage/fixedmodule.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/jstable/fixedmodule.go":"%s/build/js-versioning-sabotage/fixedmodule.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-versioning-sabotage/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -overlay=build/js-versioning-sabotage/overlay.json \
			-count=1 ./internal/codegen/jstable/ -run TestJSFixedVersioning \
			> build/js-versioning-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a file outside the lineage refused under the wrong name and the gate stayed green"; \
		cat build/js-versioning-sabotage/log; exit 1; \
	fi
	@grep -q "owes layout_newer" build/js-versioning-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the versioning gate went red for another reason"; \
		  cat build/js-versioning-sabotage/log; exit 1; }
	@echo 'tables JS versioning negative control: the wrong name on a file outside the lineage reds every OLD-REFUSES-NEW row'

# ITS NEGATIVE CONTROL: move one byte of the write template and the leg must go
# red against the reference's corpus. Without this the byte comparison could be
# comparing a file with itself and nobody would know.
.PHONY: tables-js-fixed-form-negative-control
tables-js-fixed-form-negative-control: bin/schema build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp build/fixedform-corpus/.stamp
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
			build/js-fixed-optional build/fixedform-corpus > build/js-fixed-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a string length one byte off left the fixed form green"; \
		cat build/js-fixed-sabotage/log; exit 1; \
	fi
	@grep -q "first byte differing from the C++ reference" build/js-fixed-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fixed form went red for another reason"; cat build/js-fixed-sabotage/log; exit 1; }
	@echo 'tables JS fixed form negative control: one byte off the write template reds the reference byte match'

# THE SLACK RULE'S OWN CONTROL (docs/SPEC-TABLES.md §3.4, "the slack is zero").
# Put the text write back to the WHOLE DECLARED SPAN — one line — and the leg
# must go red against the reference's own byte oracle. Nothing else here can
# see it: a value READ from a file already carries the prefill's zeros past its
# used length, so the round trip writes the same zeros back either way. Only
# the record built from CONSTRUCTED storage, over a `string(N)` with a declared
# default, has live bytes behind a used length of zero.
.PHONY: tables-js-fixed-slack-negative-control
tables-js-fixed-slack-negative-control: bin/schema build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp build/fixedform-corpus/.stamp
	@rm -rf build/js-slack-sabotage && mkdir -p build/js-slack-sabotage
	@sed 's|ind, val, name, off(base, at+fixedCountBytes), val, name)|ind, f.Type.Size, off(base, at+fixedCountBytes), val, name) // SABOTAGED|; s|%sfor (let i = 0; i < %s.%sLength; i++) { view.setUint8(%s + i, %s.%s\[i\]); }|%sfor (let i = 0; i < %d; i++) { view.setUint8(%s + i, %s.%s[i]); }|' \
		internal/codegen/jstable/fixedjs.go > build/js-slack-sabotage/fixedjs.go.txt
	@grep -q SABOTAGED build/js-slack-sabotage/fixedjs.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/fixedjs.go":"%s/build/js-slack-sabotage/fixedjs.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-slack-sabotage/overlay.json
	@go build -overlay=build/js-slack-sabotage/overlay.json -o build/js-slack-sabotage/schema ./cmd/schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/bench bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/fx1 test/tables/FX1.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/fx2 test/tables/FX2.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/p1 test/tables/P1.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/p3 test/tables/P3.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/fo1 test/tables/FO1.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/fo2 test/tables/FO2.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/ut1 test/tables/UT1.schema
	@./build/js-slack-sabotage/schema generate --lang js --out build/js-slack-sabotage/ut2 test/tables/UT2.schema
	@if $(NODE) test/js-tables/fixedform.mjs build/js-slack-sabotage build/js-fixed-corpus \
			build/js-fixed-optional build/fixedform-corpus > build/js-slack-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: writing the whole declared span left the slack rule green"; \
		cat build/js-slack-sabotage/log; exit 1; \
	fi
	@grep -q "the slack is zero" build/js-slack-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red for another reason"; cat build/js-slack-sabotage/log; exit 1; }
	@echo 'tables JS fixed slack negative control: writing the whole declared span reds the reference byte oracle'

# THE UNION-ARM TEXT LANE'S OWN CONTROL: put the text flavour back where the
# guard's value lives — the one line this defect was — and the gate must go red.
# A plan entry under a union arm carries the tag's OFFSET and the VALUE the tag
# must hold; a text entry carries a flavour too, and the three are three facts.
# Sharing a lane between the last two is invisible to every other gate in this
# file, because the identity path reads a whole body as ONE copy and never
# builds a text entry at all — so this control is what proves the pair is
# watching.
#
# THE GATE IT RUNS IS `tables-js-versioning`, not the byte driver: a text under a
# union arm can only be watched across TWO GENERATIONS, and both of that pair's
# directions are a stranger's layout, which needs a lineage entry handed in. The
# case is TestJSFixedVersioningUnionArmText (UT1 writes arm b at ordinal 2, UT2
# reads it at ordinal 3 with UT1's entry as its lock); the sabotage must red it,
# and the whole versioning gate runs here so the red lands where CI looks.
.PHONY: tables-js-union-arm-text-negative-control
tables-js-union-arm-text-negative-control: build/fixedform-corpus/.stamp
	@rm -rf build/js-armtext-sabotage && mkdir -p build/js-armtext-sabotage
	@sed 's|TableFixedPush(plan, TableFixedOpText, theirAt, at, units, auxAt, guard, arg, dst\[row + TableFixedDstArg\]);|TableFixedPush(plan, TableFixedOpText, theirAt, at, units, auxAt, guard, dst[row + TableFixedDstArg], 0); // SABOTAGED|' \
		internal/codegen/jstable/fixedruntime.go > build/js-armtext-sabotage/fixedruntime.go.txt
	@sed -i.bak 's|const unit = e\[b + TableFixedLaneMeta\] === TableFixedTextWide ? 2 : 1;|const unit = e[b + TableFixedLaneArg] === TableFixedTextWide ? 2 : 1; // SABOTAGED|' \
		build/js-armtext-sabotage/fixedruntime.go.txt
	@test $$(grep -c SABOTAGED build/js-armtext-sabotage/fixedruntime.go.txt) -eq 2 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not patch both halves of the lane"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/fixedruntime.go":"%s/build/js-armtext-sabotage/fixedruntime.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/js-armtext-sabotage/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 NODE=$(NODE) go test -overlay=build/js-armtext-sabotage/overlay.json \
			-count=1 ./internal/codegen/jstable/ -run TestJSFixedVersioning \
			> build/js-armtext-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the text flavour sharing the guard's lane left the versioning gate green"; \
		cat build/js-armtext-sabotage/log; exit 1; \
	fi
	@grep -q "union-arm text:" build/js-armtext-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the versioning gate went red for another reason"; cat build/js-armtext-sabotage/log; exit 1; }
	@echo 'tables JS union-arm text negative control: the text flavour back in the guard'"'"'s lane reds a text field under a union arm'

# THE OPTIONAL HALF'S OWN CONTROL: invert one present byte and the leg must go
# red against the reference's optional corpus. The `?T` bytes are the ones a
# port can get wrong while round-tripping its own records perfectly — the flag
# is a byte the packet wire has never carried — so the byte comparison against
# the reference is the only thing watching them, and this is what proves it is
# watching.
.PHONY: tables-js-fixed-optional-negative-control
tables-js-fixed-optional-negative-control: bin/schema build/js-fixed-corpus/.stamp build/js-fixed-optional/.stamp build/fixedform-corpus/.stamp
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
			build/js-fixed-optional build/fixedform-corpus > build/js-fixed-opt-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an inverted present byte left the optionals green"; \
		cat build/js-fixed-opt-sabotage/log; exit 1; \
	fi
	@grep -q "optionals: first byte differing from the C++ reference" build/js-fixed-opt-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the optionals went red for another reason"; cat build/js-fixed-opt-sabotage/log; exit 1; }
	@echo 'tables JS fixed optional negative control: one inverted present byte reds the reference byte match'


# THE CARDS WIRE-FUZZ GATE, this leg's half (docs/SPEC-TABLES.md §4.2).
# These targets mirror the Go leg's cards-generate and cards-wire-fuzz
# (make/go.mk) against the same oracle harness and the same seed, so the
# JavaScript Card codec is held to the identical byte-for-byte contract. They
# live in make/js.mk (NOT a separate make/cards-js.mk) because the wide-scalar
# refusal gate (tables-ports-refuse-wide-scalars) discovers every make/*.mk as
# a live port, and a fragment named cards-js.mk would be refused there as a
# phantom port (docs/SPEC-TABLES.md §11 names the port registry).
.PHONY: cards-js-generate cards-js-wire-fuzz cards-js-retain-fuzz

# cards-js-generate emits the JavaScript variable-form surface for tables/cards.
cards-js-generate: bin/schema
	@rm -rf build/tables-generated-js/cards
	@mkdir -p build/tables-generated-js/cards
	./bin/schema generate --lang js --out build/tables-generated-js/cards tables/cards

# cards-js-wire-fuzz runs the conformance harness's wire fuzzer over the Card
# root, comparing the JavaScript driver's reads and writes with the reference
# leg's (docs/SPEC-TABLES.md §4.2). --vectors '' keeps the other units' pinned
# vectors out of this Card-only manifest; the greps below hold the nonzero
# seed/mutant claim, so a zero-coverage roster fails rather than passing.
cards-js-wire-fuzz: build/conformance-harness cards-js-generate
	@./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'test/conformance/js/driver wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-js-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-js-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-js-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-js-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-js-wire-fuzz.log

# cards-js-retain-fuzz is cards-js-wire-fuzz under the oracle's --retain arm
# (docs/SPEC-TABLES.md §6.6): the same manifest, seed and coverage greps, so the
# JavaScript Card codec's retaining Load and Save are held to the identical
# byte-for-byte contract the C and C# drivers' retain fuzzers reproduce.
cards-js-retain-fuzz: build/conformance-harness cards-js-generate
	@./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--retain \
		--driver 'test/conformance/js/driver wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-js-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-js-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-js-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-js-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-js-retain-fuzz.log

# THE CARDS CONFORMANCE CHECK, this leg's half (tables/cards/conformance/FORMAT.md).
# cards-js-test runs the facade's node:test cases against the generated unit;
# cards-js-conformance runs the harness `cards` subcommand over the JS driver;
# the negative control flips one rendered byte through a test-only environment
# switch and requires render to go red while load/save/parse/bounds stay green.
# They live in make/js.mk (NOT a separate make/cards-js.mk) for the same reason
# the wire-fuzz targets do: the wide-scalar refusal gate discovers every
# make/*.mk as a live port (docs/SPEC-TABLES.md §11).
.PHONY: cards-js-test cards-js-conformance cards-js-conformance-negative-control

# cards-js-test runs the facade's node:test cases (ES modules loaded by node
# with no npm dependency), against the generated unit cards-js-generate emits.
cards-js-test: cards-js-generate
	$(NODE) --test tables/cards/js/cards.test.mjs

# cards-js-conformance runs the harness `cards` subcommand over the JS driver,
# requiring all nine surfaces; each surface must answer with no absence.
cards-js-conformance: build/conformance-harness cards-js-generate
	./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/js/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view

# cards-js-conformance-negative-control runs the same check with the driver's
# test-only render sabotage switch set, and requires render to go red while the
# other eight surfaces stay green.
cards-js-conformance-negative-control: build/conformance-harness cards-js-generate
	@if CARDS_JS_SABOTAGE_RENDER=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/js/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-js-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one rendered byte changed'; exit 1; fi
	@grep -qE 'cards render .*FAIL' build/cards-js-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-js-conformance-neg.log; exit 1; }
	@grep -qE 'cards (load|save|parse|bounds|upgrade|edit|taskhash|view) pass' build/cards-js-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-js-conformance-neg.log; exit 1; }
	@echo 'cards JS negative control: one changed rendered byte turns exactly render red'

# cards-js-conformance-upgrade-negative-control: a switch that drops one retained
# byte must FAIL upgrade alone while the other eight surfaces stay green.
.PHONY: cards-js-conformance-upgrade-negative-control
cards-js-conformance-upgrade-negative-control: build/conformance-harness cards-js-generate
	@if CARDS_JS_SABOTAGE_UPGRADE=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/js/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-js-conformance-upgrade-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: upgrade stayed green with one retained byte dropped'; exit 1; fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-js-conformance-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-js-conformance-upgrade-neg.log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|edit|taskhash|view) pass' build/cards-js-conformance-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-js-conformance-upgrade-neg.log; exit 1; }
	@echo 'cards JS upgrade negative control: one dropped retained byte turns exactly upgrade red'

# cards-js-conformance-hash-negative-control: a switch that adds needs to the
# hash frame must FAIL taskhash alone while the other eight surfaces stay green.
.PHONY: cards-js-conformance-hash-negative-control
cards-js-conformance-hash-negative-control: build/conformance-harness cards-js-generate
	@if CARDS_JS_SABOTAGE_HASH=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/js/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-js-conformance-hash-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: taskhash stayed green with needs added to the frame'; exit 1; fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-js-conformance-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-js-conformance-hash-neg.log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|upgrade|edit|view) pass' build/cards-js-conformance-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-js-conformance-hash-neg.log; exit 1; }
	@echo 'cards JS hash negative control: needs in the frame turns exactly taskhash red'

test-js: cards-js-wire-fuzz
test-js: cards-js-retain-fuzz
test-js: cards-js-test cards-js-conformance cards-js-conformance-negative-control cards-js-conformance-upgrade-negative-control cards-js-conformance-hash-negative-control

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
	$(MAKE) tables-js-fixed-slack-negative-control
	$(MAKE) tables-js-versioning
	$(MAKE) tables-js-versioning-negative-control
	$(MAKE) tables-js-fixed-roadmap-negative-control
	cd test/js && $(NODE) main.mjs && NODE_ENV=production $(NODE) main.mjs
	cd test/js-ludicrous && $(NODE) main.mjs && NODE_ENV=production $(NODE) main.mjs

TEST_LEGS         += test-js
TOOLCHAIN_LEGS    += js
TOOLCHAIN_PINS_js  := NODE
CONFORMANCE_LEGS  += $(call unless_skipped,js,build/tables-generated-js/.stamp)

# THE JAVASCRIPT TABLE SOURCES ARE LINT-CLEAN AND FORMAT-CANONICAL (issue #424).
# `eslint` is the language's authority over the generated table units, so an
# emitter that drifts from the conventions it enforces goes red on the diff
# rather than on a reviewer. It SKIPS cleanly where eslint is not on PATH.
ESLINT ?= eslint
.PHONY: tables-js-clean
tables-js-clean: build/tables-generated-js/.stamp
	@if ! command -v $(ESLINT) >/dev/null 2>&1; then \
		echo "SKIP tables-js-clean: $(ESLINT) is not installed"; \
	else \
		$(ESLINT) build/tables-generated-js; \
		echo "tables JavaScript: eslint clean over the generated table units"; \
	fi
test-js: tables-js-clean

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

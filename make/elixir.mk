# make/elixir.mk — the Elixir leg (docs/CONTRIBUTING.md, "Adding a language").
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.

# The BEAM toolchain, pinned per project (generated Elixir is self-contained —
# no runtime checkout — so Erlang/OTP + Elixir are the only dependencies).
# The defaults point at the repo-local unpacked toolchain; CI installs the
# same versions and overrides with ELIXIR=elixir MIX=mix. To populate dist/
# (gitignored):
#   Erlang/OTP 29.0.5 (erlef/otp_builds, signed macOS build, aarch64-apple-darwin)
#   url:    https://github.com/erlef/otp_builds/releases/download/OTP-29.0.5/otp-aarch64-apple-darwin.tar.gz
#   sha256: 24b9e00da2b9ad25b1f182e2efd73ff316e46ec4b143c0cc3c69dbd27d5a594d
#   untar into dist/otp-29.0.5
#   Elixir 1.20.4 (precompiled for OTP 29)
#   url:    https://github.com/elixir-lang/elixir/releases/download/v1.20.4/elixir-otp-29.zip
#   sha256: 7863c546cda13fecc949e562e326042451dacf8fd8698a36783cb71eeb223b46
#   unzip into dist/elixir-1.20.4
# The elixir/mix launchers find erl through PATH, so the pinned invocations
# carry both bin directories.
BEAM_PATH ?= $(CURDIR)/dist/otp-29.0.5/bin:$(CURDIR)/dist/elixir-1.20.4/bin
ELIXIR    ?= PATH="$(BEAM_PATH):$$PATH" elixir
MIX       ?= PATH="$(BEAM_PATH):$$PATH" mix
ELIXIRC   ?= PATH="$(BEAM_PATH):$$PATH" elixirc

# the Elixir target: generated modules only, no wiring file at all —
# generated Elixir is self-contained (the port's packing shapes are inlined
# per issue #167), so there is no runtime checkout and no mix project; the
# test legs Code.require_file the generated files by relative path directly
generated/elixir/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang elixir --out generated/elixir examples
	@touch $@

generated/elixir-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang elixir --out generated/elixir-ludicrous examples128
	@touch $@

# THE ELIXIR GENERIC-WALK GATE (docs/SPEC-TABLES.md §16.1): the text form is ONE
# walk over the descriptors, emitted once per UNIT — a unit's Elixir modules
# compile into one application, so a second copy would be a duplicate module
# rather than C++'s harmless re-inclusion behind a guard. This holds the
# runtime's source byte-identical across every unit of the corpus, with nothing
# normalized away but the five-line generated banner and the one line that
# names the unit's own module — a module is named for its package in Elixir,
# and no port can make that line the same in two units.
.PHONY: tables-elixir-walk
tables-elixir-walk: build/tables-generated-elixir/.stamp
	@rm -rf build/elixir-walk && mkdir -p build/elixir-walk
	@for f in build/tables-generated-elixir/*/TableRuntime.ex; do \
		out=build/elixir-walk/$$(echo $$f | tr / _); \
		tail -n +6 $$f | sed 's/^defmodule .*\.TableRuntime do$$/defmodule <Package>.TableRuntime do/' > $$out; \
		if [ ! -s $$out ]; then echo "ELIXIR GENERIC-WALK GATE FAILED: no runtime in $$f"; exit 1; fi; \
	done
	@first=""; for f in build/elixir-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "ELIXIR GENERIC-WALK GATE FAILED: the runtime in $$f is not the runtime in $$first"; exit 1; }; \
		fi; \
	done
	@echo "elixir generic-walk gate: one table runtime, the same bytes in every unit"

generated/bench/tables/elixir/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/elixir
	./bin/schema generate --lang elixir --out generated/bench/tables/elixir bench/corpus/BenchTable.schema
	@touch $@

generated/bench/elixir/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang elixir --out generated/bench/elixir bench/corpus/Bench.schema
	./bin/schema generate --lang elixir --out generated/bench/elixir/realworld bench/corpus/RealWorld.schema
	@touch $@

# THE ELIXIR TABLE CORPUS: one generated directory per unit, all compiled into
# ONE ebin, because a unit's namespace is its package and the corpus's packages
# are distinct. The conformance driver starts a BEAM per surface over these
# .beam files, so nothing is compiled at driver time.
ELIXIR_TABLE_UNITS := tabledemo:tables/examples graphdemo:tables/pointers \
	blockdemo:tables/block blockhome:tables/blockhome \
	tblv1:test/tables/V1.schema tblv2:test/tables/V2.schema \
	tblp1:test/tables/P1.schema tblp2:test/tables/P2.schema \
	tblp3:test/tables/P3.schema jsonkeys:test/tables/JsonKeys.schema

build/tables-generated-elixir/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema
	@mkdir -p build/tables-generated-elixir
	@for unit in $(ELIXIR_TABLE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		rm -rf build/tables-generated-elixir/$$name; \
		./bin/schema generate --lang elixir --out build/tables-generated-elixir/$$name $$path || exit 1; \
	done
	@touch $@

build/elixir-tables-ebin/.stamp: build/tables-generated-elixir/.stamp test/conformance/elixir/driver_impl.ex
	@rm -rf build/elixir-tables-ebin && mkdir -p build/elixir-tables-ebin
	$(ELIXIRC) -o build/elixir-tables-ebin build/tables-generated-elixir/*/*.ex \
		test/conformance/elixir/driver_impl.ex
	@touch $@

.PHONY: build-conformance-elixir
build-conformance-elixir: build/elixir-tables-ebin/.stamp

# THE ELIXIR LEG's own gates, beside the harness's matrix row. They read the
# DERIVED manifest the harness writes, so each is `conformance` plus one run.
#
# THE ALLOCATION AUDIT (docs/SPEC-TABLES.md §16.1's Rust paragraph, in the shape
# the BEAM allows): the COUNT of heap words and refc binary bytes one iteration
# allocates, measured in a process large enough that no garbage collection
# happens, so the heap grows by exactly what the loop allocated. The floor is
# not zero and is not claimed to be — Elixir has no caller-owned buffer and no
# mutable struct — so the gate is a PINNED BUDGET per case, re-pinned
# deliberately the way a wire golden is.
# THE DERIVED MANIFEST, made by an ELIXIR-ONLY harness run. The leg's own gates
# need the materialized fixtures and the manifest, not the other four legs'
# verdicts — and rebuilding those to read one file is a minute nobody gets back
# every time this chain runs. `make conformance` writes the same file, so
# whichever ran last serves.
build/conformance/manifest.txt: build/conformance-harness build-conformance-elixir
	./build/conformance-harness run --only elixir > /dev/null

.PHONY: tables-elixir-alloc-audit
tables-elixir-alloc-audit:
	@echo "tables-elixir-alloc-audit: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#515)"

.PHONY: tables-elixir-alloc-pin
tables-elixir-alloc-pin:
	@echo "tables-elixir-alloc-pin: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#515)"

# A SABOTAGED BUILD OF THE CORPUS, which every Elixir negative control below
# runs its gate against: the emitter source $(2) with the sed program held in
# the variable NAMED $(3) applied, built into a second compiler through
# `go build -overlay`, the corpus generated by that compiler and compiled to
# $(1)/ebin, which the driver takes through ELIXIR_TABLES_EBIN. The controls
# sabotage the EMITTER rather than the driver, which is what the Rust and Java
# controls do and what makes a control independent of the thing it tests: the
# gate has to FIND a defect in generated code. The sabotage is checked to have
# APPLIED, because a sed that silently matched nothing is a green light and not
# a control.
define ELIXIR_SABOTAGED_BUILD
	@rm -rf $(1) && mkdir -p $(1)
	@sed $($(3)) $(2) > $(1)/sabotaged.go.txt
	@cmp -s $(2) $(1)/sabotaged.go.txt && \
		{ echo "NEGATIVE CONTROL: the sabotage of $(2) did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/$(2)":"%s/$(1)/sabotaged.go.txt"}}\n' "$(CURDIR)" "$(CURDIR)" > $(1)/overlay.json
	go build -overlay $(1)/overlay.json -o $(1)/schema ./cmd/schema
	@for unit in $(ELIXIR_TABLE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		$(1)/schema generate --lang elixir --out $(1)/generated/$$name $$path || exit 1; \
	done
	@mkdir -p $(1)/ebin
	$(ELIXIRC) -o $(1)/ebin $(1)/generated/*/*.ex test/conformance/elixir/driver_impl.ex
endef

# THE AUDIT'S NEGATIVE CONTROL, and it is what makes the audit an instrument
# rather than a number: every generated load gains sixteen refc binaries and a
# thousand-cell list, freed
# again at once, and every case must go over its budget on the heap-word column
# AND on the binary-call column — each named in the audit's own output, so a
# column that lost its teeth is found on its own. A gate that cannot go red is
# not a gate.
CONFORMANCE_NEGATIVE_ELIXIR_ALLOC := build/elixir-alloc-negative
ELIXIR_ALLOC_SED := -e 's|  fields_%s(data, %s(), report)\\n|  _ = Enum.map(1..16, fn _ -> :binary.copy(<<0>>, 65) end) ++ List.duplicate(0, 1024)\\n  fields_%s(data, %s(), report)\\n|'

.PHONY: tables-elixir-alloc-negative-control
tables-elixir-alloc-negative-control:
	@echo "tables-elixir-alloc-negative-control: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#515)"

.PHONY: tables-elixir-soak
tables-elixir-soak:
	@echo "tables-elixir-soak: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#515)"

# THE FUZZER'S ORACLE over the two READERS: for ANY bytes, Open either refuses
# or opens, and an opened image is one every accessor walks without leaving the
# buffer. An index out of bounds is a REFUSAL, never an exception that escapes
# — which on the BEAM has teeth, because a bad binary match raises.
ELIXIR_FUZZ_N ?= 20000

# THE SOAK's OWN negative control, and it is a DIFFERENT sabotage from the
# audit's on purpose: the audit's extra allocation is freed every iteration and
# lifts no floor, which is exactly why the two instruments both exist. This one
# RETAINS — every generated load caches a copy of its bytes under a fresh key
# and never evicts, the shape a leak in generated code takes on the BEAM — so
# both floors rise, and the soak must name each arm.
CONFORMANCE_NEGATIVE_ELIXIR_SOAK := build/elixir-soak-negative
ELIXIR_SOAK_SED := -e 's|  fields_%s(data, %s(), report)\\n|  :erlang.put({:cache, :erlang.unique_integer()}, :binary.copy(data))\\n  fields_%s(data, %s(), report)\\n|'

.PHONY: tables-elixir-soak-negative-control
tables-elixir-soak-negative-control:
	@echo "tables-elixir-soak-negative-control: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#515)"

.PHONY: tables-elixir-fuzz
tables-elixir-fuzz: build/conformance/manifest.txt
	SEED=$(SEED) BEAM_PATH="$(BEAM_PATH)" ./test/conformance/elixir/driver \
		build/conformance/manifest.txt fuzz $(ELIXIR_FUZZ_N)

# THE FUZZ ORACLE'S NEGATIVE CONTROL, and it sabotages the EMITTER rather than
# the driver, which is what the Rust and Java controls do and what makes a
# control independent of the thing it tests: the generated reader loses a bound,
# and the gate has to FIND it.
#
# IT REMOVES BOTH EXTENT BOUNDS, and the reason is a measurement rather than a
# convenience. Removing only the first — the rows against the caller's extent —
# leaves the oracle GREEN with an identical mutant count under the seed this leg
# shipped pinned to, because the padding check downstream absorbs it; under
# SEED=$(SEED) the same one-bound build reds on mutant 1. A control whose
# verdict depends on which mutant the seed reaches first is not a control, so
# this one removes the whole layer — and that measurement is half the argument
# for the SEED knob above.
CONFORMANCE_NEGATIVE_ELIXIR_FUZZ := build/elixir-fuzz-negative
ELIXIR_FUZZ_SED := -e 's|rows > bytes - offset_of -> {:halt, :error}|true -> {:cont, used}|' \
	-e 's|if padding > bytes - used do|if false do|'

.PHONY: tables-elixir-fuzz-negative-control
tables-elixir-fuzz-negative-control: build/conformance/manifest.txt
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_FUZZ),internal/codegen/elixirtable/block.go,ELIXIR_FUZZ_SED)
	@if SEED=$(SEED) ELIXIR_TABLES_EBIN=$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_FUZZ)/ebin \
			BEAM_PATH="$(BEAM_PATH)" ./test/conformance/elixir/driver \
			build/conformance/manifest.txt fuzz $(ELIXIR_FUZZ_N) > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a reader with no extent bound left the fuzz oracle green"; \
		exit 1; \
	else \
		echo "elixir fuzz negative control: a block reader with both extent bounds removed reds the oracle"; \
	fi

# THE BASE-ALIGNMENT GATE (docs/SPEC-TABLES.md §19.1, §19.2): every committed
# block image at every lead in 0..64, and the alignment rule exactly — 0 and 64
# open, 1..63 refuse. §19.2 checks the base's alignment and this leg carries it
# as the caller's stated `lead`, so a stated fact nothing checks would be a
# comment rather than a check.
.PHONY: tables-elixir-block-lead
tables-elixir-block-lead: build/conformance/manifest.txt
	BEAM_PATH="$(BEAM_PATH)" ./test/conformance/elixir/driver \
		build/conformance/manifest.txt block-lead

# and its control, on the EMITTER for the fuzz control's reason
CONFORMANCE_NEGATIVE_ELIXIR_LEAD := build/elixir-lead-negative
ELIXIR_LEAD_SED := -e 's|or rem(lead, B.align()) != 0 do|or lead < 0 do|'

.PHONY: tables-elixir-block-lead-negative-control
tables-elixir-block-lead-negative-control: build/conformance/manifest.txt
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_LEAD),internal/codegen/elixirtable/block.go,ELIXIR_LEAD_SED)
	@if ELIXIR_TABLES_EBIN=$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_LEAD)/ebin \
			BEAM_PATH="$(BEAM_PATH)" ./test/conformance/elixir/driver \
			build/conformance/manifest.txt block-lead > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a reader that does not check the base left the gate green"; \
		exit 1; \
	else \
		echo "elixir base-alignment negative control: dropping the check reds the gate"; \
	fi

# THE ELIXIR LEG's BENCH GATE: the leg builds, and its GOLDEN GATE answers
# before any clock does — variant 0 is byte-compared to the pinned instance and
# every one of the 64 variants must load, re-save at the same length and come
# back byte-identical, so a leg that fails refuses to produce numbers. It is a
# correctness check wearing a bench's clothes, which is why it belongs on a gate
# at all — and `--gate` is this leg's own verb for it, which stops there rather
# than spending eight timed runs to learn the same thing.
.PHONY: tables-elixir-bench-gate
tables-elixir-bench-gate: generated/bench/tables/elixir/.stamp
	bench/tables/elixir/leg build
	bench/tables/elixir/leg run --gate

# THE ELIXIR RELEASE GATE (certify.yml's release-gates job finds it BY NAME, so
# landing it is this target and nothing else — no edit to that file).
#
# It is the leg's expensive half, and the split is the one the CI files draw: an
# ITERATION gate answers "is this diff right" and rides the pull request; this
# answers "does the runtime still hold under load", which is measured in minutes
# and fires on a toolchain change as readily as on a code one.
#
# The soak here is BOUNDED at five minutes, and `make tables-elixir-soak` is the
# hour. The two answer the same question at two costs, and a certification job
# sharing a runner with every other port's gate is not where an hour belongs.
ELIXIR_RELEASE_SOAK_SECONDS ?= 300
ELIXIR_RELEASE_FUZZ_N ?= 200000

.PHONY: tables-elixir-release
tables-elixir-release:
	$(MAKE) tables-elixir-fuzz ELIXIR_FUZZ_N=$(ELIXIR_RELEASE_FUZZ_N)
	$(MAKE) tables-elixir-fuzz-negative-control
	$(MAKE) tables-elixir-block-lead
	$(MAKE) tables-elixir-block-lead-negative-control
	$(MAKE) tables-elixir-alloc-audit
	$(MAKE) tables-elixir-alloc-negative-control
	$(MAKE) tables-elixir-soak SOAK_SECONDS=$(ELIXIR_RELEASE_SOAK_SECONDS)
	$(MAKE) tables-elixir-soak-negative-control
	$(MAKE) tables-elixir-bench-gate

# THE GO WALKER's NEGATIVE CONTROL (docs/SPEC-TABLES.md §16.5), and it is a
# DIFFERENT sabotage from conformance-negative-control-go above, on purpose.
# That one flips a byte of an ANSWER and proves the harness can see a wrong
# answer; this one breaks the WALK's own offset arithmetic and proves it can see
# a wrong walk — the shape §16.5 asks every backend for, and the shape the C#
# control already has.
#
# It is sabotaged IN THE EMITTER and generated afresh, because the walker IS
# emitter source — one constant in internal/codegen/gotable/json.go — so
# patching the emitter is patching the walk itself rather than an artifact of
# it. No tracked file is written to: the sed lands in build/ and a Go build
# overlay points the compiler at it.
#
# THE SABOTAGE is the C++ control's, in Go's spelling: a field's STORAGE OFFSET
# xor 4 on the READ path only. Go has real offsets — the descriptors carry
# unsafe.Offsetof — so unlike C#, whose twin had to perturb a field INDEX
# instead, that arithmetic ports directly. The sed is bounded to
# tableJsonReadField's own body, because the writer's line is the same text and
# a control that broke both would not localise the READER.
#
# The second half is the point, as it is for every control here: json-read must
# go RED and every other surface must stay GREEN.
# THE ELIXIR WALK's negative control (docs/SPEC-TABLES.md §16.5): with the READ
# path's placement sabotaged by one key, the text a walk reads lands somewhere
# the writer never looks, and json-read goes red.
#
# WHAT STANDS IN FOR AN OFFSET HERE. C++ sabotages the walker's offset
# arithmetic and C# the field INDEX a descriptor is looked up by. An Elixir
# struct has neither: a field is reached by its KEY, so the key is what the
# control breaks — the read places every scalar under a name the instance does
# not have, which is precisely "one key's value lands in its neighbour's" in the
# only vocabulary this language has for it.
#
# The second half is the point, as it is for every control here: json-read must
# go RED and every other surface must stay GREEN. json-write staying green is
# what says the break is the READER's.
# (the marker is inside the ATOM: a Makefile variable cannot carry a "#", which
# starts a comment, so the sabotage names itself in the only place it can)
.PHONY: conformance-negative-control-elixir
conformance-negative-control-elixir:
	@echo "conformance-negative-control-elixir: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#515)"

# THE SOAK: the whole corpus read and written in a loop, with the bytes
# compared every iteration so a run that drifted STOPS rather than merely
# getting slower, and the live heap and binary memory printed against the warm
# baseline. This is the LEAK half; the audit above is the COUNT half, and the
# two answer different questions. Its length is the Makefile's SOAK_SECONDS.

# THE ELIXIR LEG of `make test`: the generic-walk gate, the conformance
# negative control, THE ELIXIR PORT's own instruments (docs/SPEC-TABLES.md) —
# the reading tier's allocation BUDGET and its negative control (the BEAM has
# no caller-owned buffer, so the claim is that the count does not move rather
# than that it is zero), the forgery fuzzer over both readers, and the block
# lead gate; the hour is `make tables-elixir-soak` — then the format check and
# the packet tests.
.PHONY: test-elixir
test-elixir: generated/bench/tables/elixir/.stamp generated/elixir/.stamp generated/elixir-ludicrous/.stamp generated/bench/elixir/.stamp
	$(MAKE) tables-elixir-walk
	$(MAKE) conformance-negative-control-elixir
	$(MAKE) tables-elixir-alloc-audit
	$(MAKE) tables-elixir-alloc-negative-control
	$(MAKE) tables-elixir-fuzz
	$(MAKE) tables-elixir-block-lead
	$(MIX) format --check-formatted generated/elixir/*.ex generated/elixir-ludicrous/*.ex generated/bench/elixir/*.ex generated/bench/elixir/realworld/*.ex
	cd test/elixir && $(ELIXIR) main.exs
	cd test/elixir-ludicrous && $(ELIXIR) main.exs

TEST_LEGS         += test-elixir
CONFORMANCE_LEGS  += build-conformance-elixir
BENCH_TABLES_LEGS += generated/bench/tables/elixir/.stamp

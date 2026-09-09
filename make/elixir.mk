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

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"). The three pins carry an
# environment prefix rather than a path, so each probe resolves the launcher
# UNDER that prefix: BEAM_PATH pointing into an empty dist/ and elixir on PATH
# is what CI has, and it resolves, which is the state the gate has to pass.
.PHONY: toolchain-elixir
toolchain-elixir:
	@$(call toolchain_probe,elixir,ELIXIR,$(ELIXIR),PATH="$(BEAM_PATH):$$PATH" command -v $(lastword $(ELIXIR)))
	@$(call toolchain_probe,elixir,ELIXIRC,$(ELIXIRC),PATH="$(BEAM_PATH):$$PATH" command -v $(lastword $(ELIXIRC)))
	@$(call toolchain_probe,elixir,MIX,$(MIX),PATH="$(BEAM_PATH):$$PATH" command -v $(lastword $(MIX)))
build/packet-defaults/elixir/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/elixir.mk
	./bin/schema generate --lang elixir --out build/packet-defaults/elixir/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang elixir --out build/packet-defaults/elixir/plain test/packet-defaults/Plain.schema
	@touch $@

.PHONY: packet-defaults-elixir packet-defaults-elixir-negative-control
packet-defaults-elixir: build/packet-defaults/elixir/.stamp packet-defaults-cpp
	$(MIX) format --check-formatted build/packet-defaults/elixir/defaults/*.ex build/packet-defaults/elixir/plain/*.ex test/packet-defaults/elixir/*.exs
	$(ELIXIR) test/packet-defaults/elixir/main.exs testdata/wire/packet-defaults

packet-defaults-elixir-negative-control: packet-defaults-elixir
	@mkdir -p build/packet-defaults/elixir-negative/beam
	go run ./tools/sabotage -name packet-defaults-elixir-constructor-bytes \
		-out build/packet-defaults/elixir-negative/elixir.gotext internal/codegen/elixir/elixir.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/elixir.go":"%s/build/packet-defaults/elixir-negative/elixir.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/elixir-negative/overlay.json
	go build -overlay=build/packet-defaults/elixir-negative/overlay.json -o build/packet-defaults/elixir-negative/schema ./cmd/schema
	./build/packet-defaults/elixir-negative/schema generate --lang elixir --out build/packet-defaults/elixir-negative/generated test/packet-defaults/Defaults.schema
	$(ELIXIRC) --warnings-as-errors -o build/packet-defaults/elixir-negative/beam build/packet-defaults/elixir-negative/generated/*.ex
	@if $(ELIXIR) test/packet-defaults/elixir/main.exs testdata/wire/packet-defaults "$(CURDIR)/build/packet-defaults/elixir-negative/generated" > build/packet-defaults/elixir-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in Elixir'; exit 1; fi
	@grep -Fq 'FAILED: packet-default constructor bytes' build/packet-defaults/elixir-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Elixir failed for another reason'; cat build/packet-defaults/elixir-negative/log; exit 1; }
	@echo 'packet defaults Elixir negative control: missing constructor bytes fail the runtime check'

test-elixir: packet-defaults-elixir packet-defaults-elixir-negative-control

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
# THE DERIVED MANIFEST, made by an ELIXIR-ONLY harness run. The leg's own gates
# need the materialized fixtures and the manifest, not the other four legs'
# verdicts — and rebuilding those to read one file is a minute nobody gets back
# every time this chain runs. `make conformance` writes the same file, so
# whichever ran last serves.
build/conformance/manifest.txt: build/conformance-harness build-conformance-elixir
	./build/conformance-harness run --only elixir > /dev/null

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

# THE FUZZER'S ORACLE over the two READERS: for ANY bytes, Open either refuses
# or opens, and an opened image is one every accessor walks without leaving the
# buffer. An index out of bounds is a REFUSAL, never an exception that escapes
# — which on the BEAM has teeth, because a bad binary match raises.
ELIXIR_FUZZ_N ?= 20000

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

# THE ELIXIR RELEASE GATE (certify.yml's release-gates job finds it BY NAME, so
# landing it is this target and nothing else — no edit to that file).
#
# It is the leg's expensive half, and the split is the one the CI files draw: an
# ITERATION gate answers "is this diff right" and rides the pull request; this
# answers "do the readers still hold under load", which is measured in minutes
# and fires on a toolchain change as readily as on a code one. It is the long
# fuzz over both readers and the base-alignment gate, each with its control;
# the soak, the audit and the bench gate that once sat beside them went with
# the table wire they measured (schema#515 brings the wire back, and them).
ELIXIR_RELEASE_FUZZ_N ?= 200000

.PHONY: tables-elixir-release
tables-elixir-release:
	$(MAKE) tables-elixir-fuzz ELIXIR_FUZZ_N=$(ELIXIR_RELEASE_FUZZ_N)
	$(MAKE) tables-elixir-fuzz-negative-control
	$(MAKE) tables-elixir-block-lead
	$(MAKE) tables-elixir-block-lead-negative-control

# THE ELIXIR LEG of `make test`: THE ELIXIR PORT's own instruments over the
# two readers it emits (docs/SPEC-TABLES.md §7, §19) — the forgery fuzzer over
# both and the block lead gate — then the format check and the packet tests.
# The port emits no table wire (schema#515), so there is no walk gate, no
# text-form control, no allocation audit and no soak here: each measured the
# wire's previous form and went with it.
.PHONY: test-elixir
test-elixir: toolchain-elixir generated/elixir/.stamp generated/elixir-ludicrous/.stamp generated/bench/elixir/.stamp
	$(MAKE) tables-elixir-fuzz
	$(MAKE) tables-elixir-block-lead
	$(MIX) format --check-formatted generated/elixir/*.ex generated/elixir-ludicrous/*.ex generated/bench/elixir/*.ex generated/bench/elixir/realworld/*.ex
	cd test/elixir && $(ELIXIR) main.exs
	cd test/elixir-ludicrous && $(ELIXIR) main.exs

TEST_LEGS            += test-elixir
TOOLCHAIN_LEGS       += elixir
TOOLCHAIN_PINS_elixir := ELIXIR ELIXIRC MIX
CONFORMANCE_LEGS     += $(call unless_skipped,elixir,build-conformance-elixir)
# Packet UTF-8 content validation, including a compiled mutation control.
build/packet-text/elixir/.stamp: bin/schema test/packet-text/Narrow.schema
	@mkdir -p build/packet-text/elixir/source
	cp test/packet-text/Narrow.schema build/packet-text/elixir/source/Text.schema
	./bin/schema generate --lang elixir --out build/packet-text/elixir build/packet-text/elixir/source/Text.schema
	@touch $@

.PHONY: packet-utf8-elixir packet-utf8-elixir-negative-control
packet-utf8-elixir: build/packet-text/elixir/.stamp build/packet-text/cpp/driver build/packet-text/harness
	$(MIX) format --check-formatted build/packet-text/elixir/*.ex test/packet-text/elixir/*.exs
	./build/packet-text/harness env $(ELIXIR) test/packet-text/elixir/main.exs

packet-utf8-elixir-negative-control: packet-utf8-elixir
	@mkdir -p build/packet-text/elixir-negative/beam
	go run ./tools/sabotage -name packet-utf8-elixir-read -out build/packet-text/elixir-negative/functions.gotext internal/codegen/elixir/functions.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/functions.go":"%s/build/packet-text/elixir-negative/functions.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/elixir-negative/overlay.json
	go run -overlay=build/packet-text/elixir-negative/overlay.json ./cmd/schema generate --lang elixir --out build/packet-text/elixir-negative build/packet-text/elixir/source/Text.schema
	$(ELIXIRC) --warnings-as-errors -o build/packet-text/elixir-negative/beam build/packet-text/elixir-negative/*.ex
	@if ./build/packet-text/harness -mutations-only env $(ELIXIR) test/packet-text/elixir/main.exs "$(CURDIR)/build/packet-text/elixir-negative" > build/packet-text/elixir-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Elixir UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/elixir-negative/log || { cat build/packet-text/elixir-negative/log; exit 1; }
	@echo 'packet UTF-8 Elixir negative control: removed read validation fails bit-flip agreement'

test-elixir: packet-utf8-elixir packet-utf8-elixir-negative-control


build/packet-wide/elixir/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema
	./bin/schema generate --lang elixir --out build/packet-wide/elixir build/packet-wide/source/WideText.schema
	./bin/schema generate --lang elixir --out build/packet-wide/elixir/shapes test/packet-wide/Shapes.schema
	@touch $@

.PHONY: packet-wide-elixir packet-wide-elixir-negative-control
packet-wide-elixir: build/packet-wide/elixir/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	$(MIX) format --check-formatted build/packet-wide/elixir/*.ex build/packet-wide/elixir/shapes/*.ex test/packet-wide/elixir/*.exs
	$(ELIXIR) test/packet-wide/elixir/main.exs --contracts
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver env $(ELIXIR) test/packet-wide/elixir/main.exs

packet-wide-elixir-negative-control: packet-wide-elixir
	@mkdir -p build/packet-wide/elixir-negative/beam
	go run ./tools/sabotage -name packet-wide-elixir-pairing -out build/packet-wide/elixir-negative/wstring.gotext internal/codegen/elixir/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/wstring.go":"%s/build/packet-wide/elixir-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/elixir-negative/overlay.json
	go run -overlay=build/packet-wide/elixir-negative/overlay.json ./cmd/schema generate --lang elixir --out build/packet-wide/elixir-negative build/packet-wide/source/WideText.schema
	$(ELIXIRC) --warnings-as-errors -o build/packet-wide/elixir-negative/beam build/packet-wide/elixir-negative/*.ex
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only env $(ELIXIR) test/packet-wide/elixir/main.exs "$(CURDIR)/build/packet-wide/elixir-negative" > build/packet-wide/elixir-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Elixir wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/elixir-negative/log || { cat build/packet-wide/elixir-negative/log; exit 1; }
	@echo 'packet wide Elixir negative control: removed pairing fails bit-flip agreement'

test-elixir: packet-wide-elixir packet-wide-elixir-negative-control

# ---------------------------------------------------------------------------
# THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
# ---------------------------------------------------------------------------
#
# THE BYTES ARE THE C++ REFERENCE'S, and this gate measures that rather than
# asserting it. `make tables-fixedform-corpus` has the reference write one
# form-3 FILE per root — FX1, FX2, P1, P3, KeyedConfig and PackConfig, values
# set by hand — and the leg reads each one, saves it back, and requires the
# result BYTE FOR BYTE. That reaches every field, the layout, the hash and every
# byte of declared slack, because a byte this port encodes differently is a byte
# that does not come back.
#
# Beside it rides the VERSIONING CONFORMANCE §3.4 names — a widened field, a
# rename arriving under `was`, a field this reader cannot name, a field the
# writer does not carry, a whole nested TYPE stepped over by its layout size, an
# optional against a value, and an ENUM and a UNION that each gained a variant
# in the middle so their ordinals SLIDE — and the negative controls, of which
# the one §3.4 names is a reader given the WRONG PLAN for a record.
ELIXIR_FIXED_UNITS := fx1:test/tables/FX1.schema fx2:test/tables/FX2.schema \
	fe1:test/tables/FE1.schema fe2:test/tables/FE2.schema \
	p1:test/tables/P1.schema p3:test/tables/P3.schema \
	examples:tables/examples

ELIXIR_FIXED_SCHEMAS := test/tables/FX1.schema test/tables/FX2.schema \
	test/tables/FE1.schema test/tables/FE2.schema \
	test/tables/P1.schema test/tables/P3.schema $(SCHEMAS_TABLES)

build/elixir-fixedform/.stamp: bin/schema $(ELIXIR_FIXED_SCHEMAS) make/elixir.mk
	@rm -rf build/elixir-fixedform
	@mkdir -p build/elixir-fixedform/ebin
	@for unit in $(ELIXIR_FIXED_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		./bin/schema generate --lang elixir --out build/elixir-fixedform/gen/$$name $$path || exit 1; \
	done
	$(ELIXIRC) --warnings-as-errors -o build/elixir-fixedform/ebin build/elixir-fixedform/gen/*/*.ex
	@touch $@

.PHONY: tables-elixir-fixed-form
tables-elixir-fixed-form: build/elixir-fixedform/.stamp build/fixedform-corpus/.stamp
	# THE GENERATED ELIXIR IS `mix format`'s OWN SHAPE, emitted that way rather
	# than checked afterwards — the emitter breaks every expression where the
	# formatter would (internal/codegen/elixirtable/fixedrender.go).
	$(MIX) format --check-formatted build/elixir-fixedform/gen/*/*Fixed.ex \
		build/elixir-fixedform/gen/*/FixedRuntime.ex test/elixir-fixedform/main.exs
	$(ELIXIR) -pa build/elixir-fixedform/ebin test/elixir-fixedform/main.exs build/fixedform-corpus

# THE PAIRED CORPUS (docs/SPEC-TABLES.md §3.4): the bench's own sixty-four
# logical records on the PACKET wire and on this one. The C++ reference writes
# the form-3 file out of the canonical packet corpus, and the Elixir leg reads
# the same sixty-four records twice — off the packet corpus with the packet
# codec, off this file with the fixed one — and requires the two to agree field
# for field, then writes the fixed file back byte for byte.
#
# IT IS ALSO WHERE THE WIDE KINDS ARE MEASURED. BenchMixed carries `int128`,
# `uint128` and two fixed-point fields, which the two ACCELERATORS refuse in
# this backend (schema#366) and which §3.4's constant-size table carries fine —
# so the refusal is scoped to the accelerators and this is the gate that says
# the scoping was right.
#
# The unit's schema files are COPIED under other basenames, and only that: a
# declaration named FixedTable in a file named FixedTable.schema collides with
# the module this backend writes for that file (§11), which is the checker
# working rather than a problem. No declaration moves, so no id and no layout
# byte moves either.
build/elixir-fixed-bench/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema make/elixir.mk
	@rm -rf build/elixir-fixed-bench
	@mkdir -p build/elixir-fixed-bench/source build/elixir-fixed-bench/ebin
	cp bench/corpus/Bench.schema build/elixir-fixed-bench/source/Bench.schema
	cp bench/corpus/FixedTable.schema build/elixir-fixed-bench/source/Wrap.schema
	./bin/schema generate --lang elixir --out build/elixir-fixed-bench/gen \
		build/elixir-fixed-bench/source/Bench.schema build/elixir-fixed-bench/source/Wrap.schema
	$(ELIXIRC) --warnings-as-errors -o build/elixir-fixed-bench/ebin build/elixir-fixed-bench/gen/*.ex
	@touch $@

build/elixir-fixed-bench/bench_fixed.bin: generated/bench/paired/cpp/.stamp test/bench/fixedform_corpus.cpp bench/corpus/variants/bench_mixed.variants.bin
	@mkdir -p build/elixir-fixed-bench
	$(CXX) $(CXXFLAGS) -Igenerated/bench/paired/cpp test/bench/fixedform_corpus.cpp -o build/elixir-fixed-bench/corpus
	./build/elixir-fixed-bench/corpus bench/corpus/variants/bench_mixed.variants.bin $@

# THE PAIRED BENCH LEG's generated unit (bench/paired/README.md,
# bench/tables/README.md). It is the SAME unit tables-elixir-fixed-bench builds
# in build/, generated into the committed tree because that is where every
# other paired leg's generated code lives and because `generated-current`
# refuses a generated file nobody regenerates. The copy is for the same reason
# as above: a declaration whose name is its own file's basename collides with
# the module this backend writes for that file (§11).
#
# bench/paired/main.go's `-mode build -langs elixir` writes the same files from
# the same copies, so a build there and a `make test` here cannot disagree.
generated/bench/paired/elixir/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema make/elixir.mk
	@mkdir -p generated/bench/paired/elixir build/paired/elixir-src
	cp bench/corpus/Bench.schema build/paired/elixir-src/Bench.schema
	cp bench/corpus/FixedTable.schema build/paired/elixir-src/Wrap.schema
	./bin/schema generate --lang elixir --out generated/bench/paired/elixir \
		build/paired/elixir-src/Bench.schema build/paired/elixir-src/Wrap.schema
	@touch $@

# THE PAIRED LEG's own gate, with no clock: the fixed corpus's layout compared
# byte for byte against this build's, the file loaded clean, saved back and
# compared, and then loaded and saved twice more into fresh terms. It is
# bench/tables/elixir's `--gate` mode, which is what bench/paired's gate runs,
# so `make test` and the paired driver hold this leg to the same bytes.
.PHONY: tables-elixir-paired-gate
tables-elixir-paired-gate: generated/bench/paired/elixir/.stamp bench-paired-check
	$(MIX) format --check-formatted generated/bench/paired/elixir/*.ex bench/tables/elixir/*.exs
	$(ELIXIRC) --warnings-as-errors -o build/paired/table-elixir/ebin generated/bench/paired/elixir/*.ex
	$(ELIXIR) bench/tables/elixir/main.exs --gate --indexed \
		--wire-dir bench/paired/corpus --variant-dir bench/paired/corpus

.PHONY: tables-elixir-fixed-bench
tables-elixir-fixed-bench: build/elixir-fixed-bench/.stamp build/elixir-fixed-bench/bench_fixed.bin
	$(MIX) format --check-formatted build/elixir-fixed-bench/gen/*Fixed.ex \
		build/elixir-fixed-bench/gen/FixedRuntime.ex test/elixir-fixedform/bench.exs
	$(ELIXIR) -pa build/elixir-fixed-bench/ebin test/elixir-fixedform/bench.exs \
		bench/corpus/variants/bench_mixed.variants.bin build/elixir-fixed-bench/bench_fixed.bin

# ITS NEGATIVE CONTROL: move ONE byte of the write template and the leg must go
# red against the reference's corpus. Without it the byte comparison could be
# comparing a file with itself and nobody would know. It sabotages the EMITTER
# and not the driver, which is what every other Elixir control here does.
CONFORMANCE_NEGATIVE_ELIXIR_FIXED := build/elixir-fixed-negative
ELIXIR_FIXED_SED := -e 's|fmt.Sprintf("byte_size(%s)::little-signed-32", name)|fmt.Sprintf("byte_size(%s) + 1::little-signed-32", name)|'

.PHONY: tables-elixir-fixed-form-negative-control
tables-elixir-fixed-form-negative-control: build/fixedform-corpus/.stamp
	@rm -rf $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)
	@mkdir -p $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/ebin
	@sed $(ELIXIR_FIXED_SED) internal/codegen/elixirtable/fixedelixir.go > $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/sabotaged.go.txt
	@cmp -s internal/codegen/elixirtable/fixedelixir.go $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/sabotaged.go.txt && \
		{ echo "NEGATIVE CONTROL: the sabotage of the fixed emitter did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/elixirtable/fixedelixir.go":"%s/$(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/sabotaged.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/overlay.json
	go build -overlay $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/overlay.json -o $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/schema ./cmd/schema
	@for unit in $(ELIXIR_FIXED_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		$(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/schema generate --lang elixir --out $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/gen/$$name $$path || exit 1; \
	done
	$(ELIXIRC) -o $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/ebin $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/gen/*/*.ex
	@if $(ELIXIR) -pa $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/ebin test/elixir-fixedform/main.exs \
			build/fixedform-corpus > $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a text length one byte off left the fixed form green"; \
		cat $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/log; exit 1; \
	fi
	@grep -q "first byte differing from the C++ reference" $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fixed form went red for another reason"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_FIXED)/log; exit 1; }
	@echo 'elixir fixed form negative control: one byte off the write template reds the reference byte match'

test-elixir: tables-elixir-fixed-form tables-elixir-fixed-form-negative-control tables-elixir-fixed-bench
test-elixir: tables-elixir-paired-gate

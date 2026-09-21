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

# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, Elixir
# side. The page's cook example is pulled from docs/USAGE.md into a script that
# reads the fixture cook and evaluates the fragment verbatim against the
# compiled `graphdemo` modules — so a module name the packet emitter does not
# write, the drift the register's measured effect names, goes red here.
.PHONY: tables-elixir-usage
tables-elixir-usage: build/elixir-tables-ebin/.stamp build/cook-open/.stamp docs/USAGE.md
	@rm -rf build/elixir-usage && mkdir -p build/elixir-usage
	printf 'bytes = File.read!(System.argv() |> List.first())\n\n' > build/elixir-usage/usage.exs
	sed -n '/<!-- elixir-table-usage -->/,/<!-- \/elixir-table-usage -->/p' docs/USAGE.md \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> build/elixir-usage/usage.exs
	printf '\nIO.puts("usage: docs/USAGE.md Elixir cook example opened the Scene cook and walked its chain")\n' >> build/elixir-usage/usage.exs
	grep -q 'cook_open_scene' build/elixir-usage/usage.exs || \
		{ echo "MISSING: docs/USAGE.md carries no elixir-table-usage example"; exit 1; }
	$(ELIXIR) -pa build/elixir-tables-ebin build/elixir-usage/usage.exs $(CURDIR)/build/cook-open/Scene.cook

.PHONY: tables-elixir-release
tables-elixir-release:
	$(MAKE) tables-elixir-fuzz ELIXIR_FUZZ_N=$(ELIXIR_RELEASE_FUZZ_N)
	$(MAKE) tables-elixir-fuzz-negative-control
	$(MAKE) tables-elixir-block-lead
	$(MAKE) tables-elixir-block-lead-negative-control
	$(MAKE) tables-elixir-usage

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

# THE ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md §J1, issue #421).
# The leg emits two derivations of one layout — the TYPED row/slot accessors
# beside the block-record and cook descriptors — and this gate reads the same
# SYNTHETIC bytes twice and requires agreement. It is VALUE agreement, not
# offset agreement: on Go the accessor's offset is a number unsafe.Offsetof
# will tell you, and on the BEAM the accessor's offset is a literal baked into
# a generated function body that nothing can read back. The bytes are
# synthetic, so this gate needs no harness run and its prerequisite is the
# compiled corpus alone — build/elixir-tables-ebin/.stamp, not the derived
# manifest — which is a minute per invocation it does not spend.
#
# SCOPE: the walk holds PLAIN SCALAR block-record fields and POINTER cook
# slots, and nothing else — exactly what the two controls below move. Every
# other descriptor class is counted and named `not yet held` by the script,
# never silently skipped.
.PHONY: tables-elixir-accessor-descriptor-agreement
tables-elixir-accessor-descriptor-agreement: build/elixir-tables-ebin/.stamp
	$(MIX) format --check-formatted test/elixir-tables/*.exs
	ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin $(ELIXIR) test/elixir-tables/accessor_descriptor.exs

# The two controls for that gate, on the EMITTER for the fuzz control's reason:
# each moves ONE derivation — a generated block scalar accessor four bytes, a
# cook pointer slot eight — through `go build -overlay`, and the gate must find
# the defect in generated code and red on its own disagreement message.
CONFORMANCE_NEGATIVE_ELIXIR_ACCESSOR := build/elixir-accessor-negative
ELIXIR_ACCESSOR_SED := -e 's|b.readAt(f, fmt.Sprint(fl.Offset), size)|b.readAt(f, fmt.Sprint(fl.Offset+4), size)|'

.PHONY: tables-elixir-accessor-negative-control
tables-elixir-accessor-negative-control:
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_ACCESSOR),internal/codegen/elixirtable/block.go,ELIXIR_ACCESSOR_SED)
	@if ELIXIR_TABLES_EBIN=$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_ACCESSOR)/ebin \
			$(ELIXIR) test/elixir-tables/accessor_descriptor.exs \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_ACCESSOR)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a generated accessor four bytes off left the gate green"; \
		exit 1; \
	fi
	@grep -q "the accessor and the descriptor disagree about the value" $(CONFORMANCE_NEGATIVE_ELIXIR_ACCESSOR)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the accessor/descriptor disagreement"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_ACCESSOR)/log; exit 1; }
	@echo "elixir accessor negative control: a generated accessor four bytes off reds the agreement gate"

CONFORMANCE_NEGATIVE_ELIXIR_SLOT := build/elixir-slot-negative
ELIXIR_SLOT_SED := -e 's|do: C.deref(region, node + %d)|do: C.deref(region, node + 8 + %d)|'

.PHONY: tables-elixir-slot-negative-control
tables-elixir-slot-negative-control:
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_SLOT),internal/codegen/elixirtable/cook.go,ELIXIR_SLOT_SED)
	@if ELIXIR_TABLES_EBIN=$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_SLOT)/ebin \
			$(ELIXIR) test/elixir-tables/accessor_descriptor.exs \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_SLOT)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a pointer slot eight bytes off left the gate green"; \
		exit 1; \
	fi
	@grep -q "the slot accessor and the descriptor disagree about the delta" $(CONFORMANCE_NEGATIVE_ELIXIR_SLOT)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the pointer slot"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_SLOT)/log; exit 1; }
	@echo "elixir slot negative control: a pointer slot eight bytes off reds the agreement gate"

# THE SPLIT the CI files draw: the cheap agreement gate rides `test-elixir`, the
# two controls ride `tables-elixir-release` beside the fuzz and block-lead
# controls, because each does a whole `go build -overlay` plus a regeneration
# plus an `elixirc` of the entire corpus.
test-elixir: tables-elixir-accessor-descriptor-agreement
tables-elixir-release: tables-elixir-accessor-negative-control tables-elixir-slot-negative-control

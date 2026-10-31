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
# The strict compile every clean gate uses. Elixir 1.20.4 on OTP 29 accepts
# `--warnings-as-errors` but exits 0 even when warnings are printed (the
# diagnostics go out through the logger, not the compiler's warning counter), so
# a gate that passes that flag never fails on a warning. ELIXIRC_STRICT runs
# elixirc through make/elixir-strict.sh, which fails when the output carries a
# warning or the compile fails. It is used by every gate line that must be clean.
ELIXIRC_STRICT ?= PATH="$(BEAM_PATH):$$PATH" sh $(CURDIR)/make/elixir-strict.sh elixirc

# THE PINNED RUNTIME THIS LEG CERTIFIES ON (docs/PORTING.md I14). The two
# versions above live in a comment and in dist/ path names; these are the two
# the gate COMPARES AGAINST, and they are the same strings
# make/negative-controls.json and test/conformance/elixir/ci.json carry.
ELIXIR_PIN_OTP    ?= 29.0.5
ELIXIR_PIN_ELIXIR ?= 1.20.4

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
	$(ELIXIRC_STRICT) -o build/packet-defaults/elixir-negative/beam build/packet-defaults/elixir-negative/generated/*.ex
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
#
# graphdemo (tables/pointers) exercises the pointered variable class
# (docs/SPEC-TABLES.md §2.1); blobdemo (tables/blobs) the byte buffers (§2.5);
# listdemo (tables/lists) the unbounded arrays (§2.9); tblp1/tblp2/tblp3 the
# evolution rows; mapdemo (tables/maps) the maps (§2.8); messagedemo
# (tables/messages) union arms of every field type, arrays of unions and
# optional arrays (§2.3, §2.6); scalars (tables/scalars) the fixed-point and
# 128-bit kinds and an enum-keyed array (§2.4, §3); widedemo (examples-wide)
# wstring(N) on the variable wire (§3); vocabdemo, vocab9demo and backenddemo
# (tables/vocab, tables/vocab9, tables/backend) the message surface's three
# connections (§3.3); streamdemo (tables/stream) a variable-length arm, a
# pointer arm and arrays of pointers (§2.1, §2.6); tblm1/tblm2 and tbla1/tbla2
# the union-arm evolution pairs (§4), tblk1/tblk2 the enum-kind pair,
# tblscalars2 the second scalars version and tblrt1/tblrt2/tblrt3 the retain
# units (§6.6). The variable-class surfaces (wire,
# json-read, json-write, report) answer every carried instance; a construct
# outside the class writes <case>.absent rather than a wrong answer.
ELIXIR_TABLE_UNITS := tabledemo:tables/examples graphdemo:tables/pointers \
	blockdemo:tables/block blockhome:tables/blockhome \
	tblv1:test/tables/V1.schema tblv2:test/tables/V2.schema \
	tblw1:test/tables/W1.schema tblw2:test/tables/W2.schema \
	tblr1:test/tables/R1.schema tblr2:test/tables/R2.schema \
	tblp1:test/tables/P1.schema \
	tblp3:test/tables/P3.schema tblrt1:test/tables/RT1.schema jsonkeys:test/tables/JsonKeys.schema \
	blobdemo:tables/blobs listdemo:tables/lists tblp2:test/tables/P2.schema \
	mapdemo:tables/maps messagedemo:tables/messages scalars:tables/scalars \
	widedemo:examples-wide \
	vocabdemo:tables/vocab vocab9demo:tables/vocab9 backenddemo:tables/backend \
	streamdemo:tables/stream tblm1:test/tables/M1.schema tblm2:test/tables/M2.schema \
	tbla1:test/tables/A1.schema tbla2:test/tables/A2.schema \
	tblk1:test/tables/K1.schema tblk2:test/tables/K2.schema \
	tblscalars2:test/tables/Scalars2.schema \
	tblrt1:test/tables/RT1.schema tblrt2:test/tables/RT2.schema tblrt3:test/tables/RT3.schema \
	tblg1:test/tables/G1.schema

build/tables-generated-elixir/.stamp: bin/schema make/elixir.mk $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_SCALARS) $(wildcard examples-wide/*.schema) test/tables/V1.schema test/tables/V2.schema test/tables/W1.schema test/tables/W2.schema test/tables/R1.schema test/tables/R2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema $(wildcard tables/stream/*.schema) test/tables/M1.schema test/tables/M2.schema test/tables/A1.schema test/tables/A2.schema test/tables/K1.schema test/tables/K2.schema test/tables/Scalars2.schema test/tables/RT1.schema test/tables/RT2.schema test/tables/RT3.schema test/tables/G1.schema $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9) $(SCHEMAS_TABLES_BACKEND)
	@mkdir -p build/tables-generated-elixir
	@for unit in $(ELIXIR_TABLE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		rm -rf build/tables-generated-elixir/$$name; \
		./bin/schema generate --lang elixir --out build/tables-generated-elixir/$$name $$path || exit 1; \
	done
	# Elixir 1.20.4 is the formatting authority (issue #424), and every table
	# emitter writes its shape (issue #547, tables-elixir-emit-format-clean), so
	# the stamp formats nothing: tables-elixir-clean checks the raw output.
	@touch $@

build/elixir-tables-ebin/.stamp: build/tables-generated-elixir/.stamp test/conformance/elixir/driver_impl.ex
	@rm -rf build/elixir-tables-ebin && mkdir -p build/elixir-tables-ebin
	$(ELIXIRC) -o build/elixir-tables-ebin build/tables-generated-elixir/*/*.ex \
		test/conformance/elixir/driver_impl.ex
	@touch $@

# THE STANDALONE GATE, Elixir — the M4b twin of the gates above
# (docs/PORTING.md). A generated table module INLINES its bitstring segments
# rather than importing a runtime, so the unit compiles on the BEAM toolchain
# ALONE: every generated .ex file is handed to elixirc with no driver, no test
# support and no sibling runtime checkout, and the unit that generates is the
# unit that compiles. A module that reached for a dependency would not resolve
# here.
.PHONY: tables-elixir-standalone
tables-elixir-standalone: build/tables-generated-elixir/.stamp
	@n=$$(ls build/tables-generated-elixir/*/*.ex 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 8 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated table modules, expected at least 8 — the glob, not the property, is what broke"; exit 1; \
		fi
	@rm -rf build/elixir-standalone-ebin && mkdir -p build/elixir-standalone-ebin
	$(ELIXIRC) -o build/elixir-standalone-ebin build/tables-generated-elixir/*/*.ex
	@echo "tables Elixir standalone gate: every generated module compiles on the BEAM toolchain alone"
test-elixir: tables-elixir-standalone

# THE ELIXIR TABLE SOURCES ARE FORMAT-CANONICAL (issue #424). `mix format` is
# the language's one formatting authority; the packet units under generated/
# were already held to it, and this holds the TABLE units under
# build/tables-generated-elixir to the same formatter.
.PHONY: tables-elixir-clean
tables-elixir-clean: build/tables-generated-elixir/.stamp
	@set -e; files=""; \
	for unit in $(ELIXIR_TABLE_UNITS); do \
		name=$${unit%%:*}; \
		files="$$files build/tables-generated-elixir/$$name/"*.ex; \
	done; \
	$(MIX) format --check-formatted $$files
	@echo "tables Elixir: mix format clean over the generated table units"
test-elixir: tables-elixir-clean

# THE EMITTERS' RAW OUTPUT IS FORMAT-CANONICAL (issue #547, the formatter half).
# tables-elixir-clean above checks what a stamp FORMATTED before the check, so
# it can never see an emitter that drifts from Elixir 1.20.4's shape. This gate
# generates the whole table corpus — the table units, the fixed-form units, the
# paired and bench units and the cards unit — with the compiler into
# $(ELIXIR_EMIT_FORMAT_DIR) and hands the RAW files to `mix format
# --check-formatted`, with nothing in between that formats.
#
# ELIXIR_EMIT_FORMAT_PENDING names the files the gate does NOT yet hold, by
# basename, and the recipe prints the list so the exclusion is never silent.
# It is EMPTY: the variable form's Table.ex and TableRuntime.ex, the last two
# it held, are canonical since elixir-variable-form-emits-mix-format-canonical-source,
# and TestVariableFormEmitsFormatCanonicalSource holds it empty.
#
# ELIXIR_EMIT_FORMAT_SCHEMA is the compiler the gate generates with, so the
# negative control below runs this same recipe over a sabotaged compiler.
ELIXIR_EMIT_FORMAT_PENDING :=

ELIXIR_EMIT_FORMAT_DIR     ?= build/elixir-emit-format
ELIXIR_EMIT_FORMAT_SCHEMA  ?= ./bin/schema

.PHONY: tables-elixir-emit-format-clean
tables-elixir-emit-format-clean: bin/schema
	@rm -rf $(ELIXIR_EMIT_FORMAT_DIR) && mkdir -p $(ELIXIR_EMIT_FORMAT_DIR)/gen $(ELIXIR_EMIT_FORMAT_DIR)/src
	@for unit in $(addprefix table-,$(ELIXIR_TABLE_UNITS)) $(addprefix fixed-,$(ELIXIR_FIXED_UNITS)) \
			cards:tables/cards bench:bench/corpus/Bench.schema realworld:bench/corpus/RealWorld.schema; do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		$(ELIXIR_EMIT_FORMAT_SCHEMA) generate --lang elixir --out $(ELIXIR_EMIT_FORMAT_DIR)/gen/$$name $$path > /dev/null || exit 1; \
	done
	@cp bench/corpus/Bench.schema $(ELIXIR_EMIT_FORMAT_DIR)/src/Bench.schema
	@cp bench/corpus/FixedTable.schema $(ELIXIR_EMIT_FORMAT_DIR)/src/Wrap.schema
	@$(ELIXIR_EMIT_FORMAT_SCHEMA) generate --lang elixir --out $(ELIXIR_EMIT_FORMAT_DIR)/gen/paired \
		$(ELIXIR_EMIT_FORMAT_DIR)/src/Bench.schema $(ELIXIR_EMIT_FORMAT_DIR)/src/Wrap.schema > /dev/null
	@echo "tables-elixir-emit-format-clean: PENDING (not held): $(ELIXIR_EMIT_FORMAT_PENDING)"
	@set -e; files=""; \
	for f in $(ELIXIR_EMIT_FORMAT_DIR)/gen/*/*.ex; do \
		case " $(ELIXIR_EMIT_FORMAT_PENDING) " in *" $$(basename $$f) "*) continue ;; esac; \
		files="$$files $$f"; \
	done; \
	n=$$(echo $$files | wc -w | tr -d ' '); \
	if [ "$$n" -lt 100 ]; then echo "tables-elixir-emit-format-clean: found $$n raw files, expected at least 100 — the glob, not the property, broke"; exit 1; fi; \
	echo "tables-elixir-emit-format-clean: checking $$n raw emitter files"; \
	$(MIX) format --check-formatted $$files
	@echo "tables Elixir emitters: the raw output of every table emitter but the pending ones is mix format clean"
test-elixir: tables-elixir-emit-format-clean

# ITS NEGATIVE CONTROL, on the EMITTER for the fuzz control's reason (below):
# the cook runtime's `int(region, at + 4, 4)` (the high half of a cook
# delta) loses its call parentheses — still
# valid Elixir, which the sabotaged build compiles, and a pair the formatter
# would add back — and the gate, run over the sabotaged compiler's raw output,
# must go red NAMING CookRuntime.ex. A gate that formatted before it checked,
# the shape the older stamps had, would stay green here.
CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT := build/elixir-emit-format-negative
ELIXIR_EMIT_FORMAT_SED := -e 's|high = int(region, at + 4, 4)|high = int region, at + 4, 4|'

.PHONY: tables-elixir-emit-format-negative-control
tables-elixir-emit-format-negative-control: bin/schema
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT),internal/codegen/elixirtable/cook.go,ELIXIR_EMIT_FORMAT_SED)
	@if $(MAKE) --no-print-directory tables-elixir-emit-format-clean \
			ELIXIR_EMIT_FORMAT_SCHEMA=$(CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT)/schema \
			ELIXIR_EMIT_FORMAT_DIR=$(CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT)/emit-format \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a cook runtime call without its parentheses left the raw-format gate green"; \
		cat $(CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT)/log; exit 1; \
	fi
	@grep -q 'emit-format/gen/[^/]*/CookRuntime\.ex' $(CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the raw-format gate went red, but did not name CookRuntime.ex"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_EMIT_FORMAT)/log; exit 1; }
	@echo 'elixir emit-format negative control: dropping a call'"'"'s parentheses in the cook emitter reds the raw-format gate on CookRuntime.ex'
test-elixir: tables-elixir-emit-format-negative-control

# THE JSON-HOSTILE ROWS' NEGATIVE CONTROLS (docs/SPEC-TABLES.md §16.2). Each
# sabotages the table emitter, rebuilds the corpus, runs the harness's elixir
# leg against that build (the driver takes its modules through
# ELIXIR_TABLES_EBIN) and requires the json-hostile surface red on EXACTLY the
# rows its rule owns: a control that reds more has found a second defect, and
# one that reds fewer has not found its own.
#   dup: an enum-keyed array's repeated slot name stops counting `duplicate`
#        (dup-key-rejected-keyed, keyed-duplicate-mixed, keyed-duplicate-slot);
#   f32: a float32 field or element past the largest float32's rounding
#        midpoint is stored instead of a kind mismatch (num-float32-midpoint
#        by integer token, num-float32-overflow by decimal token).
CONFORMANCE_NEGATIVE_ELIXIR_HOSTILE_DUP := build/elixir-hostile-dup-negative
ELIXIR_HOSTILE_DUP_SED := -e 's|if Map.has_key?(acc, name),$$|if false and Map.has_key?(acc, name),|'
ELIXIR_HOSTILE_DUP_ROWS := dup-key-rejected-keyed keyed-duplicate-mixed keyed-duplicate-slot
CONFORMANCE_NEGATIVE_ELIXIR_HOSTILE_F32 := build/elixir-hostile-f32-negative
ELIXIR_HOSTILE_F32_SED := -e 's|abs(raw) >= 340_282_356_779_733_661_637_539_395_458_142_568_448|abs(raw) >= 3_402_823_567_797_336_616_375_393_954_581_425_684_480|' \
	-e 's|if abs(raw) >= 3.4028235677973366e38, do: :error|if abs(raw) >= 3.4028235677973366e48, do: :error|'
ELIXIR_HOSTILE_F32_ROWS := num-float32-midpoint num-float32-overflow

# $(call ELIXIR_HOSTILE_CONTROL,dir,sedvar,rows,what)
define ELIXIR_HOSTILE_CONTROL
	$(call ELIXIR_SABOTAGED_BUILD,$(1),internal/codegen/elixirtable/variableform.go,$(2))
	@if ELIXIR_TABLES_EBIN=$(CURDIR)/$(1)/ebin ./build/conformance-harness run --only elixir > $(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(4) left the json-hostile surface green"; exit 1; \
	fi
	@got=$$(sed -n 's|^  elixir / json-hostile: \([^:]*\):.*|\1|p' $(1)/log | sort | tr '\n' ' '); \
		want="$$(echo $(3) | tr ' ' '\n' | sort | tr '\n' ' ')"; \
		if [ "$$got" != "$$want" ]; then \
			echo "NEGATIVE CONTROL FAILED: $(4) reds [$$got], want exactly [$$want]"; cat $(1)/log; exit 1; \
		fi
endef

.PHONY: tables-elixir-json-hostile-dup-negative-control tables-elixir-json-hostile-f32-negative-control
tables-elixir-json-hostile-dup-negative-control: build/conformance-harness
	$(call ELIXIR_HOSTILE_CONTROL,$(CONFORMANCE_NEGATIVE_ELIXIR_HOSTILE_DUP),ELIXIR_HOSTILE_DUP_SED,$(ELIXIR_HOSTILE_DUP_ROWS),an uncounted repeated enum-keyed slot)
	@echo "elixir json-hostile dup negative control: an uncounted repeated slot reds exactly its three rows"

tables-elixir-json-hostile-f32-negative-control: build/conformance-harness
	$(call ELIXIR_HOSTILE_CONTROL,$(CONFORMANCE_NEGATIVE_ELIXIR_HOSTILE_F32),ELIXIR_HOSTILE_F32_SED,$(ELIXIR_HOSTILE_F32_ROWS),a float32 stored past its rounding midpoint)
	@echo "elixir json-hostile f32 negative control: a stored float32 overflow reds exactly its two rows"

test-elixir: tables-elixir-json-hostile-dup-negative-control tables-elixir-json-hostile-f32-negative-control

# THE ELIXIR LEG's CONFORMANCE NEGATIVE CONTROLS (docs/PORTING.md I11): a
# harness that has never gone red on a leg is not watching that leg. Each one
# sabotages the table EMITTER through a Go overlay (ELIXIR_SABOTAGED_BUILD
# below), regenerates and recompiles the whole corpus with that compiler, runs
# the harness's Elixir leg against the sabotaged build in a work directory of
# its own, and requires the matrix red on the surfaces that read the broken
# code and green on the surfaces that do not, so the red is localized to the
# defect and is never a build failure. Nothing tracked is written to.
#   walk   (I11) the text reader stores the negation of every bool field:
#          json-read red; json-write and wire green.
#   shared (M6)  the pack walk's identity check is gone, so a node two
#          pointers name is written twice: wire and json-read (whose
#          answer is the saved wire) red; json-write and report green.
#   label  (M14) the text writer never labels a shared node and writes it in
#          full at every pointer: json-write red; json-read and wire green.
#   ids    (M20) the id table is written in reverse first-use order while the
#          body keeps its references: wire red; json-write green.
#   arm    (M18) an arm header under another kind reads as the declared arm
#          instead of a counted kind mismatch: report red; json-write green.
#   order  (I7)  a byte-swapped block magic is no longer refused by name as a
#          foreign order: block-reason red; block green.
#   mapsort (M19) the writer sorts a map's entries DESCENDING: wire red;
#          report green.
ELIXIR_CONFORMANCE_WALK_SED   := -e 's|defp json_read_field(%{t: :bool}, raw, report) when is_boolean(raw), do: {raw, report}|defp json_read_field(%{t: :bool}, raw, report) when is_boolean(raw), do: {not raw, report}|'
ELIXIR_CONFORMANCE_SHARED_SED := -e 's|    if Enum.any?(acc, fn n -> node_content(n) == c end) do|    if false and Enum.any?(acc, fn n -> node_content(n) == c end) do|'
ELIXIR_CONFORMANCE_LABEL_SED  := -e 's|^    if n < 2 do$$|    if true or n < 2 do|'
ELIXIR_CONFORMANCE_IDS_SED    := -e 's|        {ref, Map.put(ids, id, ref), \[id \| order\]}|        {ref, Map.put(ids, id, ref), order ++ [id]}|'
ELIXIR_CONFORMANCE_ARM_SED    := -e 's| when byte_size(content) >= l and k == a.arm_kind ->| when byte_size(content) >= l ->|'
ELIXIR_CONFORMANCE_MAPSORT_SED := -e 's~^    |> Enum.sort_by(fn {key, _value} -> map_sort_key(f, key) end)$$~    |> Enum.sort_by(fn {key, _value} -> map_sort_key(f, key) end, :desc)~'
ELIXIR_CONFORMANCE_ORDER_SED  := -e 's|magic == B.foreign_magic() -> {:error, :foreign_order}|false -> {:error, :foreign_order}|'

# $(call ELIXIR_CONFORMANCE_CONTROL,dir,emitter,sedvar,red surfaces,green surfaces,what)
define ELIXIR_CONFORMANCE_CONTROL
	$(call ELIXIR_SABOTAGED_BUILD,$(1),$(2),$(3))
	@if ELIXIR_TABLES_EBIN=$(CURDIR)/$(1)/ebin ./build/conformance-harness run --only elixir \
			--work $(1)/work > $(1)/log 2>&1; then \
		cat $(1)/log; echo "NEGATIVE CONTROL FAILED: $(6) left the Elixir leg green"; exit 1; \
	fi
	@for s in $(4); do \
		grep -qE "^$$s +FAIL" $(1)/log || { cat $(1)/log; echo "NEGATIVE CONTROL FAILED: $(6) did not red $$s"; exit 1; }; \
	done
	@for s in $(5); do \
		grep -qE "^$$s +pass" $(1)/log || { cat $(1)/log; echo "NEGATIVE CONTROL FAILED: $(6) also broke $$s, which never runs the sabotaged code"; exit 1; }; \
	done
	@grep -E '^[a-z-]+ +(FAIL|pass)' $(1)/log
	@echo "elixir conformance negative control: $(6) reds [$(4)] and leaves [$(5)] green"
endef

.PHONY: conformance-negative-control-elixir-walk conformance-negative-control-elixir-shared \
	conformance-negative-control-elixir-label conformance-negative-control-elixir-ids \
	conformance-negative-control-elixir-arm conformance-negative-control-elixir-order \
	conformance-negative-control-elixir-mapsort
conformance-negative-control-elixir-walk: build/conformance-harness
	$(call ELIXIR_CONFORMANCE_CONTROL,build/elixir-conformance-negative-walk,internal/codegen/elixirtable/variableform.go,ELIXIR_CONFORMANCE_WALK_SED,json-read,json-write wire,a text reader that negates every bool)

conformance-negative-control-elixir-shared: build/conformance-harness
	$(call ELIXIR_CONFORMANCE_CONTROL,build/elixir-conformance-negative-shared,internal/codegen/elixirtable/variableform.go,ELIXIR_CONFORMANCE_SHARED_SED,wire json-read,json-write report,a pack walk that writes a shared node twice)

conformance-negative-control-elixir-label: build/conformance-harness
	$(call ELIXIR_CONFORMANCE_CONTROL,build/elixir-conformance-negative-label,internal/codegen/elixirtable/variableform.go,ELIXIR_CONFORMANCE_LABEL_SED,json-write,json-read wire,a text writer that never labels a shared node)

conformance-negative-control-elixir-ids: build/conformance-harness
	$(call ELIXIR_CONFORMANCE_CONTROL,build/elixir-conformance-negative-ids,internal/codegen/elixirtable/variableform.go,ELIXIR_CONFORMANCE_IDS_SED,wire,json-write,an id table written in reverse first-use order)

conformance-negative-control-elixir-arm: build/conformance-harness
	$(call ELIXIR_CONFORMANCE_CONTROL,build/elixir-conformance-negative-arm,internal/codegen/elixirtable/variableform.go,ELIXIR_CONFORMANCE_ARM_SED,report,json-write,an arm header under another kind read as the declared arm)

conformance-negative-control-elixir-order: build/conformance-harness
	$(call ELIXIR_CONFORMANCE_CONTROL,build/elixir-conformance-negative-order,internal/codegen/elixirtable/block.go,ELIXIR_CONFORMANCE_ORDER_SED,block-reason,block,a block reader that no longer names a foreign byte order)

conformance-negative-control-elixir-mapsort: build/conformance-harness
	$(call ELIXIR_CONFORMANCE_CONTROL,build/elixir-conformance-negative-mapsort,internal/codegen/elixirtable/variableform.go,ELIXIR_CONFORMANCE_MAPSORT_SED,wire,report,a map writer that sorts its entries descending)

test-elixir: conformance-negative-control-elixir-walk conformance-negative-control-elixir-shared \
	conformance-negative-control-elixir-label conformance-negative-control-elixir-ids \
	conformance-negative-control-elixir-arm conformance-negative-control-elixir-order \
	conformance-negative-control-elixir-mapsort

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
ELIXIR_FUZZ_SED := -e 's|rows > bytes - offset_of -> {:halt, :bad_layout}|true -> {:cont, used}|' \
	-e 's|padding > bytes - used -> {:error, :truncated}|false -> {:error, :truncated}|'

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

# THE RETAIN SURFACES' NEGATIVE CONTROL (docs/SPEC-TABLES.md §6.6): a copy of
# the emitter whose retaining save finds the retained records of the ROOT body
# only, so every record read from a nested body, an array or keyed element, a
# union arm, a map entry or a node is left behind. The corpus regenerated by it,
# and the driver compiled against that, must turn `retain-save` red (the bytes)
# and `retain` with it (the save's own retain_lost), while every other surface
# stays green, so the red is the retention and nothing else. The registry is
# the committed one with the Elixir leg's driver pointed at the sabotaged
# corpus, and the run is the Elixir leg alone.
CONFORMANCE_NEGATIVE_ELIXIR_RETAIN := build/elixir-retain-negative
ELIXIR_RETAIN_SED := -e 's|    records = Map.get(retain.by_path, path, \[\])|    records = if path == [], do: Map.get(retain.by_path, path, []), else: []|'

.PHONY: tables-elixir-retain-negative-control
tables-elixir-retain-negative-control: build/conformance-harness
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN),internal/codegen/elixirtable/variableform.go,ELIXIR_RETAIN_SED)
	@mkdir -p $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/drivers/elixir
	@for lang in c cpp cs dart go java js rust; do \
		mkdir -p $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/drivers/$$lang; \
		ln -sf $(CURDIR)/test/conformance/$$lang/driver $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/drivers/$$lang/driver; \
	done
	@printf '#!/bin/sh\nELIXIR_TABLES_EBIN=%s exec %s "$$@"\n' \
		"$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/ebin" "$(CURDIR)/test/conformance/elixir/driver" \
		> $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/drivers/elixir/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/drivers/elixir/driver
	@if ./build/conformance-harness run --only elixir --drivers $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/drivers \
		--work $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/work > $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log 2>&1; then \
		cat $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log; \
		echo "NEGATIVE CONTROL FAILED: the Elixir leg stayed green with the retained records of every nested body left behind"; exit 1; \
	fi
	@grep -qE '^retain +FAIL' $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log || \
		{ cat $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log; echo "NEGATIVE CONTROL FAILED: retain did not go red"; exit 1; }
	@grep -qE '^retain-save +FAIL' $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log || \
		{ cat $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log; echo "NEGATIVE CONTROL FAILED: retain-save did not go red"; exit 1; }
	@if grep -E '^[a-z-]+ +FAIL' $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log | grep -vqE '^retain(-save)? '; then \
		cat $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log; echo "NEGATIVE CONTROL FAILED: a surface other than the two retain surfaces went red"; exit 1; \
	fi
	@grep -E '^retain(-save)? ' $(CONFORMANCE_NEGATIVE_ELIXIR_RETAIN)/log
	@echo "elixir retain negative control: a retaining save that finds the root body's records only reds the retain surfaces and only them"

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
ELIXIR_LEAD_SED := -e 's|rem(lead, B.align()) != 0 -> {:error, :unaligned_base}|lead < 0 -> {:error, :unaligned_base}|'

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

# THE OFFSETS/COOK-DELTA COMPOSITION GATE (docs/PORTING.md M2): the block's
# offset_of and the cook's self-relative delta are composed from two 32-bit
# halves, so no bignum is formed before the bound that refuses it runs.
# TestOffsetsComposeFromHalvesElixir measures (a) that every corpus block
# open, every row of its arrays and every reference slot of three corpus cooks
# allocates no more heap words per call than a same-shaped immediate-value
# control, against a forced-boxing positive control that must allocate more,
# and (b) that a FORGED offset_of or delta word (high half 0x7FFFFFFF, and the
# delta's negative twin) is refused with no more minor collections than an
# immediate-high-half twin (a bignum would add some). It is a FUNCTIONAL test:
# SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain into a failure, never a skip.
.PHONY: tables-elixir-offsets
tables-elixir-offsets:
	@mkdir -p build
	@SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestOffsetsComposeFromHalvesElixir$$' \
		> build/tables-elixir-offsets.log 2>&1; \
		status=$$?; cat build/tables-elixir-offsets.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@if grep -q -- '--- SKIP' build/tables-elixir-offsets.log; then \
		echo "tables-elixir-offsets: the gate SKIPPED a subtest; under SCHEMA_REQUIRE_CORPUS=1 a missing toolchain or corpus is a failure"; \
		exit 1; \
	fi
	@echo 'tables Elixir offsets: ordinary opens, rows and cook slots allocate no more than the immediate-value control; forged offset_of and cook delta refused with no extra collections'

# ITS NEGATIVE CONTROL: sabotage the block's composed read back to the
# whole-word read, and the forged-offset assertion must go red (a bignum gets
# allocated before the bound refuses).
CONFORMANCE_NEGATIVE_ELIXIR_OFFSETS := build/elixir-offsets-negative
ELIXIR_OFFSETS_SED := -e 's|B.u64halves(data, a.offset_of_offset)|B.u64(data, a.offset_of_offset)|'

.PHONY: tables-elixir-offsets-negative-control
tables-elixir-offsets-negative-control:
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_OFFSETS),internal/codegen/elixirtable/block.go,ELIXIR_OFFSETS_SED)
	@if SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
			go test -overlay $(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_OFFSETS)/overlay.json \
			./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestOffsetsComposeFromHalvesElixir$$' \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_OFFSETS)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a whole-word offset_of read left the forged-offset assertion green"; \
		exit 1; \
	fi
	@grep -q "forged" $(CONFORMANCE_NEGATIVE_ELIXIR_OFFSETS)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the offsets gate red for another reason"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_OFFSETS)/log; exit 1; }
	@echo "elixir offsets negative control: a whole-word offset_of read reds the forged-offset check"

# AND THE COOK'S: sabotage the cook runtime's deref back to a whole-word read
# of the signed 64-bit delta (the delta is formed as one 64-bit integer, then bounded), and the
# forged-cook-delta assertion must go red (a bignum gets allocated before the
# bound refuses it, which shows as extra minor collections against the
# immediate twin).
CONFORMANCE_NEGATIVE_ELIXIR_COOK := build/elixir-cook-negative
ELIXIR_COOK_SED := \
	-e 's|    high = int(region, at + 4, 4)|    high = int(region, at, 8)|' \
	-e 's|high >= 0x08000000 or high <= -0x08000000 -> :error|high >= 0x0800000000000000 or high <= -0x0800000000000000 -> :error|' \
	-e 's|true -> inside(region, at + high \* 0x100000000 + low)|true -> inside(region, at + high)|'

.PHONY: tables-elixir-cook-negative-control
tables-elixir-cook-negative-control:
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_COOK),internal/codegen/elixirtable/cook.go,ELIXIR_COOK_SED)
	@if SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
			go test -overlay $(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_COOK)/overlay.json \
			./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestOffsetsComposeFromHalvesElixir$$' \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_COOK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a whole-word cook delta read left the forged-delta assertion green"; \
		exit 1; \
	fi
	@grep -q "forged cook delta" $(CONFORMANCE_NEGATIVE_ELIXIR_COOK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the cook gate red for another reason"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_COOK)/log; exit 1; }
	@echo "elixir cook negative control: a whole-word cook delta read reds the forged-delta check"

test-elixir: tables-elixir-offsets tables-elixir-offsets-negative-control tables-elixir-cook-negative-control

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
ELIXIR_SLOT_SED := -e 's|"C.deref(region, node + %s)"|"C.deref(region, node + 8 + %s)"|'

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

.PHONY: tables-elixir-runtime-pin
tables-elixir-runtime-pin:
	$(ELIXIR) test/elixir/runtime_pin.exs $(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR)

.PHONY: tables-elixir-runtime-pin-negative-control
tables-elixir-runtime-pin-negative-control:
	@mkdir -p build/elixir-runtime-pin
	@$(ELIXIR) -e 'IO.puts(System.version())' > build/elixir-runtime-pin/running.txt
	@if $(ELIXIR) test/elixir/runtime_pin.exs 0.0.0 0.0.0 > build/elixir-runtime-pin/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the gate certified on a pin nothing can be running"; \
		cat build/elixir-runtime-pin/log; exit 1; fi
	@grep -Fq '0.0.0' build/elixir-runtime-pin/log || \
		{ echo "NEGATIVE CONTROL FAILED: the refusal did not name the pin it wanted"; cat build/elixir-runtime-pin/log; exit 1; }
	@grep -Fq "$$(cat build/elixir-runtime-pin/running.txt)" build/elixir-runtime-pin/log || \
		{ echo "NEGATIVE CONTROL FAILED: the refusal did not name the Elixir it is actually running"; cat build/elixir-runtime-pin/log; exit 1; }
	@echo 'elixir runtime pin negative control: a pin nothing is running is refused, naming both versions'

# ---------------------------------------------------------------------------
# THE ALLOCATION AUDIT, behind the pin (docs/PORTING.md I1 and I14, issue #420)
# ---------------------------------------------------------------------------
#
# test/elixir-alloc/main.exs over the derived manifest and the generated ebin:
# for every `instance` whose unit has a <Unit>.Table module, one iteration is
# one wire load, one wire save, one text read and one text write — the calls
# the conformance driver's wire, json-read and json-write surfaces make — and
# each case prints heap words per iteration (per process, :erlang.trace/3 on
# :garbage_collection), refc binary allocations per iteration (binary_alloc's
# VM-wide call counter, slack 8) and reductions (printed, not gated). The gate
# is a PINNED BUDGET per case in test/conformance/elixir/alloc-budget.txt; the
# floor is not zero and is not claimed to be. A case with no pinned line is
# listed `unpinned` and does not fail.
#
# THE PIN COMES FIRST: the script's first act is test/elixir/runtime_pin.exs
# with ELIXIR_PIN_OTP and ELIXIR_PIN_ELIXIR, so on any other runtime it prints
# FAILED and exits non-zero before a case is measured; SCHEMA_ELIXIR_ANY_OTP=1
# prints the numbers under NOT CERTIFIED and exits 0, and the pin target refuses
# to write a budget under it. Re-pinning is deliberate, as a wire golden is.
ELIXIR_ALLOC_BUDGET := test/conformance/elixir/alloc-budget.txt

.PHONY: tables-elixir-alloc-audit
tables-elixir-alloc-audit: build/conformance/manifest.txt
	$(MIX) format --check-formatted test/elixir-alloc/main.exs
	ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin $(ELIXIR) test/elixir-alloc/main.exs \
		build/conformance/manifest.txt $(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR) audit $(ELIXIR_ALLOC_BUDGET)

.PHONY: tables-elixir-alloc-pin
tables-elixir-alloc-pin: build/conformance/manifest.txt
	ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin $(ELIXIR) test/elixir-alloc/main.exs \
		build/conformance/manifest.txt $(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR) pin $(ELIXIR_ALLOC_BUDGET)

# THE AUDIT'S NEGATIVE CONTROL, and it is what makes the audit an instrument
# rather than a number: the EMITTER is sabotaged (variableform.go, through
# ELIXIR_SABOTAGED_BUILD) so every generated load also builds sixteen refc
# binaries and a thousand-cell list and frees them at once, and every pinned
# case must go over its budget on the heap-word column AND on the binary-call
# column — each counted on its own from the audit's output, so a column that
# lost its teeth is found on its own. A gate that cannot go red is not a gate.
CONFORMANCE_NEGATIVE_ELIXIR_ALLOC := build/elixir-alloc-negative
ELIXIR_ALLOC_SED := -e 's|^  def load(table, data) when is_binary(data) do$$|&\n    _ = Enum.map(1..16, fn _ -> :binary.copy(<<0>>, 65) end) ++ List.duplicate(0, 1000)|'

.PHONY: tables-elixir-alloc-negative-control
tables-elixir-alloc-negative-control: build/conformance/manifest.txt
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_ALLOC),internal/codegen/elixirtable/variableform.go,ELIXIR_ALLOC_SED)
	@if ELIXIR_TABLES_EBIN=$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_ALLOC)/ebin $(ELIXIR) test/elixir-alloc/main.exs \
			build/conformance/manifest.txt $(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR) audit $(ELIXIR_ALLOC_BUDGET) \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_ALLOC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the audit passed with sixteen refc binaries and a thousand-cell list added to every generated load"; \
		cat $(CONFORMANCE_NEGATIVE_ELIXIR_ALLOC)/log; exit 1; fi
	@cases=$$(grep -c '^[a-z]' $(ELIXIR_ALLOC_BUDGET)); \
	over=$$(grep 'over its pinned budget' $(CONFORMANCE_NEGATIVE_ELIXIR_ALLOC)/log); \
	[ "$$cases" -gt 0 ] || { echo "NEGATIVE CONTROL FAILED: $(ELIXIR_ALLOC_BUDGET) pins no case"; exit 1; }; \
	[ "$$(echo "$$over" | grep -c 'heap words')" -eq "$$cases" ] || \
		{ echo "NEGATIVE CONTROL FAILED: the heap words column did not red every one of the $$cases pinned cases"; cat $(CONFORMANCE_NEGATIVE_ELIXIR_ALLOC)/log; exit 1; }; \
	[ "$$(echo "$$over" | grep -c 'binary calls')" -eq "$$cases" ] || \
		{ echo "NEGATIVE CONTROL FAILED: the binary calls column did not red every one of the $$cases pinned cases"; cat $(CONFORMANCE_NEGATIVE_ELIXIR_ALLOC)/log; exit 1; }; \
	echo "elixir alloc negative control: sixteen refc binaries and a thousand-cell list per generated load red the heap words and binary calls columns of all $$cases pinned cases"

# THE AUDIT'S RUNTIME CONTROL, as tables-elixir-runtime-pin-negative-control is
# the pin's: the audit run with a pin nothing is running must refuse, name the
# pin it wanted, and measure nothing. It needs no corpus: the refusal comes
# before the manifest is read, and a corpus it did read would be a measurement.
.PHONY: tables-elixir-alloc-runtime-negative-control
tables-elixir-alloc-runtime-negative-control:
	@mkdir -p build/elixir-alloc-runtime
	@if ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin $(ELIXIR) test/elixir-alloc/main.exs \
			build/conformance/manifest.txt 0.0.0 0.0.0 audit $(ELIXIR_ALLOC_BUDGET) \
			> build/elixir-alloc-runtime/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the audit certified on a pin nothing can be running"; \
		cat build/elixir-alloc-runtime/log; exit 1; fi
	@grep -Fq 'FAILED' build/elixir-alloc-runtime/log && grep -Fq '0.0.0' build/elixir-alloc-runtime/log || \
		{ echo "NEGATIVE CONTROL FAILED: the refusal did not name the pin it wanted"; cat build/elixir-alloc-runtime/log; exit 1; }
	@if grep -Eq 'measuring|heap words' build/elixir-alloc-runtime/log; then \
		echo "NEGATIVE CONTROL FAILED: the audit measured before it refused"; cat build/elixir-alloc-runtime/log; exit 1; fi
	@echo 'elixir alloc runtime negative control: the audit on a pin nothing is running refuses, names 0.0.0 and measures nothing'

test-elixir: tables-elixir-alloc-audit tables-elixir-alloc-negative-control tables-elixir-alloc-runtime-negative-control

# THE ELIXIR TABLES SOAK (docs/PORTING.md I9), test/elixir-soak/main.exs over
# the derived manifest and the generated ebin: one PASS is one iteration of
# every pinned case of the allocation audit above, TABLES_ELIXIR_SOAK_ITERATIONS
# passes (a count, never a clock: `make shape-gate` refuses a timing primitive
# outside the sanctioned bench), cut into samples of 200, the audit's own
# window. Every sample is the audit's own measurement of every case, heap words
# and binary calls, each held to its pinned budget in $(ELIXIR_ALLOC_BUDGET)
# with the audit's slack; the VM's binary, ETS, persistent-term and atom drift
# is printed beside it as the weaker instrument and bounds retention. The pin comes first, as in the audit. THE LENGTH CHECK rides
# every run: the soak must report exactly the passes and samples its count
# names, so a soak that ignored its count is red, not silently green. The
# default count is the release act, about an hour on vision (a pass over the
# pinned corpus is about 0.4 s there); `test-elixir` runs the
# short form, two samples.
TABLES_ELIXIR_SOAK_ITERATIONS ?= 9000
ELIXIR_SOAK_SHORT := 400
ELIXIR_SOAK_LOG := build/elixir-soak/log

.PHONY: tables-elixir-soak tables-elixir-soak-short tables-elixir-soak-negative-control
tables-elixir-soak: build/conformance/manifest.txt
	$(MIX) format --check-formatted test/elixir-soak/main.exs
	@mkdir -p $(dir $(ELIXIR_SOAK_LOG))
	@ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin $(ELIXIR) test/elixir-soak/main.exs \
		build/conformance/manifest.txt $(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR) \
		$(TABLES_ELIXIR_SOAK_ITERATIONS) $(ELIXIR_ALLOC_BUDGET) > $(ELIXIR_SOAK_LOG) 2>&1; \
		status=$$?; cat $(ELIXIR_SOAK_LOG); exit $$status
	@grep -q "soak: worker ran $(TABLES_ELIXIR_SOAK_ITERATIONS) passes, driver took $$(( $(TABLES_ELIXIR_SOAK_ITERATIONS) / 200 )) samples" $(ELIXIR_SOAK_LOG) || \
		{ echo "SOAK LENGTH FAILED: the soak did not run its $(TABLES_ELIXIR_SOAK_ITERATIONS) passes"; exit 1; }
	@echo "tables Elixir soak: $(TABLES_ELIXIR_SOAK_ITERATIONS) passes, every sample inside the pinned allocation budget"

tables-elixir-soak-short:
	$(MAKE) tables-elixir-soak TABLES_ELIXIR_SOAK_ITERATIONS=$(ELIXIR_SOAK_SHORT)

# THE SOAK'S NEGATIVE CONTROL: the emitter sabotage the audit's control plants
# (ELIXIR_ALLOC_SED: every generated load builds sixteen refc binaries and a
# thousand-cell list and drops them at once), a matched allocate-and-free per
# pass that retains nothing. Two samples over four pinned cases keep it inside
# the CI job's ceiling; the soak must go red on the count at both samples, and
# the drift line must stay [PASS], which is what says the count saw what drift
# cannot.
CONFORMANCE_NEGATIVE_ELIXIR_SOAK := build/elixir-soak-negative
ELIXIR_SOAK_CONTROL_CASES := root_full,archive,keyed_config,profile_elide

tables-elixir-soak-negative-control: build/conformance/manifest.txt
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_SOAK),internal/codegen/elixirtable/variableform.go,ELIXIR_ALLOC_SED)
	@if ELIXIR_SOAK_CASES=$(ELIXIR_SOAK_CONTROL_CASES) \
			ELIXIR_TABLES_EBIN=$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/ebin $(ELIXIR) test/elixir-soak/main.exs \
			build/conformance/manifest.txt $(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR) 400 $(ELIXIR_ALLOC_BUDGET) \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a matched allocate-and-free per generated load left the soak green"; \
		cat $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log; exit 1; fi
	@grep -q '^soak: 400 passes over 4 pinned cases' $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the control did not soak its four cases"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log; exit 1; }
	@[ "$$(grep -c '^SOAK FAILED: sample [12]: 4 cases' $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log)" -eq 2 ] || \
		{ echo "NEGATIVE CONTROL FAILED: the count did not red all four cases at both samples"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log; exit 1; }
	@grep -qE '^soak: drift .*\[PASS\]' $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: drift went red too, so the control does not show the count seeing what drift cannot"; \
		  cat $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log; exit 1; }
	@grep -m1 '^SOAK FAILED: sample' $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log
	@grep '^soak: drift' $(CONFORMANCE_NEGATIVE_ELIXIR_SOAK)/log
	@echo "elixir soak negative control: a matched allocate-and-free per load reds the count at both samples while drift stays silent"

test-elixir: tables-elixir-soak-short tables-elixir-soak-negative-control

# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, Elixir
# side. The page's cook example is pulled from docs/USAGE.md into a script that
# reads the fixture cook and evaluates the fragment verbatim against the
# compiled `graphdemo` modules — so a module name the packet emitter does not
# write, the drift the register's measured effect names, goes red here.
ELIXIR_USAGE_EBIN ?= build/elixir-tables-ebin

.PHONY: tables-elixir-usage
tables-elixir-usage: build/elixir-tables-ebin/.stamp build/cook-open/.stamp docs/USAGE.md
	@rm -rf build/elixir-usage && mkdir -p build/elixir-usage
	printf 'bytes = File.read!(System.argv() |> List.first())\n\n' > build/elixir-usage/usage.exs
	sed -n '/<!-- elixir-table-usage -->/,/<!-- \/elixir-table-usage -->/p' docs/USAGE.md \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> build/elixir-usage/usage.exs
	printf '\nIO.puts("usage: docs/USAGE.md Elixir cook example opened the Scene cook and walked its chain")\n' >> build/elixir-usage/usage.exs
	grep -q 'cook_open_scene' build/elixir-usage/usage.exs || \
		{ echo "MISSING: docs/USAGE.md carries no elixir-table-usage example"; exit 1; }
	$(ELIXIR) -pa $(ELIXIR_USAGE_EBIN) build/elixir-usage/usage.exs $(CURDIR)/build/cook-open/Scene.cook

# ITS NEGATIVE CONTROL (docs/PORTING.md I12), on the EMITTER for the fuzz
# control's reason: the cook emitter renames the page's entry point
# (cook_open_<table>/1), the corpus is rebuilt through that compiler, and the
# page's example run against it must go red naming cook_open_scene. A gate that
# ran something other than the page, or the committed corpus, would stay green.
CONFORMANCE_NEGATIVE_ELIXIR_USAGE := build/elixir-usage-negative
ELIXIR_USAGE_SED := -e 's|exDefDo("  ", "def cook_open_"+lo+"(data)"|exDefDo("  ", "def cook_open_renamed_"+lo+"(data)"|'

.PHONY: tables-elixir-usage-negative-control
tables-elixir-usage-negative-control: build/cook-open/.stamp docs/USAGE.md
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_USAGE),internal/codegen/elixirtable/cook.go,ELIXIR_USAGE_SED)
	@if $(MAKE) --no-print-directory tables-elixir-usage ELIXIR_USAGE_EBIN=$(CONFORMANCE_NEGATIVE_ELIXIR_USAGE)/ebin \
			> $(CONFORMANCE_NEGATIVE_ELIXIR_USAGE)/log 2>&1; then \
		cat $(CONFORMANCE_NEGATIVE_ELIXIR_USAGE)/log; \
		echo "NEGATIVE CONTROL FAILED: the page's example ran against a corpus with no cook_open_scene/1"; exit 1; \
	fi
	@grep -q 'cook_open_scene' $(CONFORMANCE_NEGATIVE_ELIXIR_USAGE)/log || \
		{ cat $(CONFORMANCE_NEGATIVE_ELIXIR_USAGE)/log; echo "NEGATIVE CONTROL FAILED: the usage gate went red, but not on cook_open_scene"; exit 1; }
	@echo "elixir usage negative control: a renamed cook entry point reds the page's example on cook_open_scene"

test-elixir: tables-elixir-usage-negative-control

# J2 (docs/PORTING.md) — THE RUNTIME HOME, Elixir side. A unit's shared runtime
# modules are files named by the module, never by the schema file that sorts
# first, so a file that sorts earlier relocates nothing: the runtime files
# (ELIXIR_RUNTIME_HOME_FILES) are present and byte-identical across the two
# trees, and the unit-level modules whose CONTENT is the unit's (the Table
# descriptors, the build version, the view: ELIXIR_RUNTIME_HOME_NAMED) are
# present under the same names in both. The gate
# runs over the compiler ELIXIR_RUNTIME_HOME_SCHEMA so its control can run the
# same recipe over a sabotaged one.
ELIXIR_RUNTIME_HOME_SCHEMA ?= ./bin/schema
ELIXIR_RUNTIME_HOME_DIR    ?= build/runtime-home-elixir
ELIXIR_RUNTIME_HOME_FILES  := TableRuntime.ex BlockRuntime.ex CookRuntime.ex FixedRuntime.ex
ELIXIR_RUNTIME_HOME_NAMED  := Table.ex BuildVersion.ex UnitView.ex

.PHONY: tables-elixir-runtime-home
tables-elixir-runtime-home: bin/schema
	@rm -rf $(ELIXIR_RUNTIME_HOME_DIR) && mkdir -p $(ELIXIR_RUNTIME_HOME_DIR)/src
	@cp tables/examples/*.schema $(ELIXIR_RUNTIME_HOME_DIR)/src/
	@printf 'package tabledemo\n\ntype AaaPacket\n{\n    tag uint8\n}\n' > $(ELIXIR_RUNTIME_HOME_DIR)/src/Aaa.schema
	@$(ELIXIR_RUNTIME_HOME_SCHEMA) generate --lang elixir --out $(ELIXIR_RUNTIME_HOME_DIR)/base tables/examples > /dev/null 2>&1
	@$(ELIXIR_RUNTIME_HOME_SCHEMA) generate --lang elixir --out $(ELIXIR_RUNTIME_HOME_DIR)/added $(ELIXIR_RUNTIME_HOME_DIR)/src > /dev/null 2>&1
	@for n in $(ELIXIR_RUNTIME_HOME_NAMED); do \
		for tree in base added; do \
			test -f $(ELIXIR_RUNTIME_HOME_DIR)/$$tree/$$n || \
				{ echo "RUNTIME HOME GATE FAILED: the $$tree tree has no $$n (a unit-level module is named by the module, not by a schema file)"; ls $(ELIXIR_RUNTIME_HOME_DIR)/$$tree; exit 1; }; \
		done; \
	done
	@for n in $(ELIXIR_RUNTIME_HOME_FILES); do \
		for tree in base added; do \
			test -f $(ELIXIR_RUNTIME_HOME_DIR)/$$tree/$$n || \
				{ echo "RUNTIME HOME GATE FAILED: the $$tree tree has no $$n (a runtime module is named by the module, not by a schema file)"; ls $(ELIXIR_RUNTIME_HOME_DIR)/$$tree; exit 1; }; \
		done; \
		cmp -s $(ELIXIR_RUNTIME_HOME_DIR)/base/$$n $(ELIXIR_RUNTIME_HOME_DIR)/added/$$n || \
			{ echo "RUNTIME HOME GATE FAILED: the runtime file $$n changed when the unit gained an earlier-sorting file"; exit 1; }; \
	done
	@echo "runtime home gate (Elixir): $(ELIXIR_RUNTIME_HOME_FILES) stay where they are and byte-identical, and $(ELIXIR_RUNTIME_HOME_NAMED) keep their names, when an earlier-sorting file joins the unit"

# Its NEGATIVE CONTROL: put the file-order rule back (the table runtime named
# by the file that sorts first, ir.ProtocolIdHome) and the gate, run over that
# compiler, must go red on TableRuntime.ex.
.PHONY: tables-elixir-runtime-home-negative-control
tables-elixir-runtime-home-negative-control: bin/schema
	@mkdir -p build/elixir-runtime-home-negative
	@sed 's|out\[TableRuntimeModule+".ex"\] = tableRuntimeModule(u, ns)|out[ir.ProtocolIdHome(u)+TableRuntimeModule+".ex"] = tableRuntimeModule(u, ns) // SABOTAGED: back to the file order|' \
		internal/codegen/elixirtable/elixirtable.go > build/elixir-runtime-home-negative/elixirtable.go.txt
	@grep -q SABOTAGED build/elixir-runtime-home-negative/elixirtable.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/elixirtable/elixirtable.go":"%s/build/elixir-runtime-home-negative/elixirtable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/elixir-runtime-home-negative/overlay.json
	go build -overlay=build/elixir-runtime-home-negative/overlay.json -o build/elixir-runtime-home-negative/schema ./cmd/schema
	@if $(MAKE) --no-print-directory tables-elixir-runtime-home \
			ELIXIR_RUNTIME_HOME_SCHEMA=build/elixir-runtime-home-negative/schema \
			ELIXIR_RUNTIME_HOME_DIR=build/elixir-runtime-home-negative/tree > build/elixir-runtime-home-negative/log 2>&1; then \
		cat build/elixir-runtime-home-negative/log; \
		echo "NEGATIVE CONTROL FAILED: the file-order rule left the runtime home gate green"; exit 1; \
	fi
	@grep -q 'TableRuntime.ex' build/elixir-runtime-home-negative/log || \
		{ cat build/elixir-runtime-home-negative/log; echo "NEGATIVE CONTROL FAILED: the gate went red, but not on TableRuntime.ex"; exit 1; }
	@echo "runtime home negative control (Elixir): the file-order rule moves TableRuntime.ex and reds the gate"

test-elixir: tables-elixir-runtime-home tables-elixir-runtime-home-negative-control

# THE ELIXIR RELEASE GATE (certify.yml's release-gates job finds it BY NAME, so
# landing it is this target and nothing else — no edit to that file).
#
# It is the leg's expensive half, and the split is the one the CI files draw: an
# ITERATION gate answers "is this diff right" and rides the pull request; this
# answers "do the readers still hold under load", which is measured in minutes
# and fires on a toolchain change as readily as on a code one. It is the long
# fuzz over both readers and the base-alignment gate, each with its control;
# the soak, the audit and the bench gate that once sat beside them went with
# the table wire they measured. The variable form is emitted again
# (tables-elixir-variable-class); the AUDIT has returned on that wire, behind
# the runtime pin (tables-elixir-alloc-audit, riding test-elixir), and the soak
# and the bench gate have not.
ELIXIR_RELEASE_FUZZ_N ?= 200000

.PHONY: tables-elixir-release
tables-elixir-release:
	$(MAKE) tables-elixir-runtime-pin
	$(MAKE) tables-elixir-runtime-pin-negative-control
	$(MAKE) tables-elixir-fuzz ELIXIR_FUZZ_N=$(ELIXIR_RELEASE_FUZZ_N)
	$(MAKE) tables-elixir-fuzz-negative-control
	$(MAKE) tables-elixir-block-lead
	$(MAKE) tables-elixir-block-lead-negative-control
	$(MAKE) tables-elixir-usage

# THE ELIXIR LEG of `make test`: THE ELIXIR PORT's own instruments over the
# two readers it emits (docs/SPEC-TABLES.md §7, §19) — the forgery fuzzer over
# both and the block lead gate — then the format check and the packet tests.
# The port emits the whole variable class (docs/SPEC-TABLES.md §2, §3, §16),
# whose own gate is tables-elixir-variable-class above.
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
	$(ELIXIRC_STRICT) -o build/packet-text/elixir-negative/beam build/packet-text/elixir-negative/*.ex
	@if ./build/packet-text/harness -mutations-only env $(ELIXIR) test/packet-text/elixir/main.exs "$(CURDIR)/build/packet-text/elixir-negative" > build/packet-text/elixir-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Elixir UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/elixir-negative/log || { cat build/packet-text/elixir-negative/log; exit 1; }
	@echo 'packet UTF-8 Elixir negative control: removed read validation fails bit-flip agreement'

test-elixir: packet-utf8-elixir packet-utf8-elixir-negative-control

# Packet finite floats (SPEC §4.3): writes refuse a non-float as
# non_finite_float, reads refuse a NaN or an infinity on its raw bits, and -0
# and subnormals ride bit exact. The control removes the runtime's read checks
# and requires the crafted-bytes vectors to red.
build/packet-finite/elixir/.stamp: bin/schema test/packet-finite/elixir/Finite.schema
	./bin/schema generate --lang elixir --out build/packet-finite/elixir test/packet-finite/elixir/Finite.schema
	@touch $@

.PHONY: packet-finite-elixir packet-finite-elixir-negative-control
packet-finite-elixir: build/packet-finite/elixir/.stamp
	$(MIX) format --check-formatted build/packet-finite/elixir/*.ex test/packet-finite/elixir/*.exs
	@mkdir -p build/packet-finite/elixir-beam
	$(ELIXIRC_STRICT) -o build/packet-finite/elixir-beam build/packet-finite/elixir/*.ex
	$(ELIXIR) test/packet-finite/elixir/main.exs

packet-finite-elixir-negative-control: packet-finite-elixir
	@mkdir -p build/packet-finite/elixir-negative/beam
	go run ./tools/sabotage -name packet-finite-elixir-read -out build/packet-finite/elixir-negative/runtime.gotext internal/codegen/elixir/runtime.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/runtime.go":"%s/build/packet-finite/elixir-negative/runtime.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-finite/elixir-negative/overlay.json
	go run -overlay=build/packet-finite/elixir-negative/overlay.json ./cmd/schema generate --lang elixir --out build/packet-finite/elixir-negative/generated test/packet-finite/elixir/Finite.schema
	$(ELIXIRC_STRICT) -o build/packet-finite/elixir-negative/beam build/packet-finite/elixir-negative/generated/*.ex
	@if $(ELIXIR) test/packet-finite/elixir/main.exs "$(CURDIR)/build/packet-finite/elixir-negative/generated" > build/packet-finite/elixir-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Elixir non_finite_float read check removal passed'; exit 1; fi
	@grep -Fq 'FAILED: non_finite_float read ' build/packet-finite/elixir-negative/log || { cat build/packet-finite/elixir-negative/log; exit 1; }
	@if grep -F 'FAILED: ' build/packet-finite/elixir-negative/log | grep -Fvq 'FAILED: non_finite_float read '; then cat build/packet-finite/elixir-negative/log; echo 'NEGATIVE CONTROL FAILED: a check other than the read refusal went red'; exit 1; fi
	@echo 'packet finite Elixir negative control: removed raw-bit read checks red the non_finite_float read vectors'

test-elixir: packet-finite-elixir packet-finite-elixir-negative-control


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
	go run ./tools/sabotage -name packet-wide-elixir-pairing -out build/packet-wide/elixir-negative/runtime.gotext internal/codegen/elixir/runtime.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/runtime.go":"%s/build/packet-wide/elixir-negative/runtime.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/elixir-negative/overlay.json
	go run -overlay=build/packet-wide/elixir-negative/overlay.json ./cmd/schema generate --lang elixir --out build/packet-wide/elixir-negative build/packet-wide/source/WideText.schema
	$(ELIXIRC_STRICT) -o build/packet-wide/elixir-negative/beam build/packet-wide/elixir-negative/*.ex
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only env $(ELIXIR) test/packet-wide/elixir/main.exs "$(CURDIR)/build/packet-wide/elixir-negative" > build/packet-wide/elixir-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Elixir wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/elixir-negative/log || { cat build/packet-wide/elixir-negative/log; exit 1; }
	@echo 'packet wide Elixir negative control: removed pairing fails bit-flip agreement'

test-elixir: packet-wide-elixir packet-wide-elixir-negative-control

# ---------------------------------------------------------------------------
# THE VARIABLE CLASS ON THE TABLE WIRE (docs/SPEC-TABLES.md §2, §3, §16)
# ---------------------------------------------------------------------------
#
# THE VARIABLE FORM'S OWN GATE, the constructs beyond the card record: pointers
# to tables and the flat node table (§2.1, §3.1), blobs (*bytes) (§2.5),
# unbounded arrays of scalar, string and enum elements and arrays of pointers
# (§2.9), the 64-bit id-table wire with the enum and escape kinds (§3), and the
# variable-class text form (§16). TestVariableClassBeyondCardConstructsElixir
# round-trips tables/pointers, tables/blobs, tables/lists, tblp2 and the
# fixed-only form-1 units tblp1 and tblp3 through the generated Elixir against
# the Go oracle bytes and text form; it is a FUNCTIONAL
# test (it runs the leg's own toolchain through elixirBinary), so
# SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain or corpus into a FAILURE,
# never a skip. TestListdemoNamesUncarriedRoots (the roots the cook write and
# the wire-fuzz roster leave uncarried) rides the same -run; the refusal gate
# TestOtherVariableConstructsAreRefusedByName retired with
# tables-elixir-variable-arms below, which carries every construct it named. The conformance run answers the wire, json-read,
# json-write and report surfaces for the variable-class instances.
.PHONY: tables-elixir-variable-class
tables-elixir-variable-class: build/conformance/manifest.txt
	SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run 'TestVariableClassBeyondCardConstructsElixir|TestListdemoNamesUncarriedRoots|TestOptionalVariableClosureElixir'
	./build/conformance-harness run --only elixir

test-elixir: tables-elixir-variable-class

# THE FIXED CLASS ON THE TABLE WIRE (docs/SPEC-TABLES.md §3, §5): a closure
# that HOLDS a fixed table rides form byte 1, and the table-declaration `was`
# (Vessel -> Ship) reads across the rename on form 1 alone. TestFixedClassOn-
# TableWireElixir generates tblw1/tblw2 and tblr1 through the COMPILER path and
# round-trips the committed oracle bytes and text through the generated Elixir.
# It is a FUNCTIONAL test, so SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain
# into a failure, never a skip. The conformance run answers wire and report for
# the tblv1/tblv2, tblw1/tblw2 and tblr1 instances, whose report rows
# w1_fleet_as_w2 and w1_fleet_default_as_w2 equal the Go leg's, and tblr2's
# r1_cfg_as_r2 / r2_cfg_as_r1 equal the Go leg's. TestDefaultsAndTableRename-
# Elixir pins the string, bytes and flags defaults on the wire and in the text
# (§4.2) and every table, variant, arm and type-field rename reading an older
# writer's bytes as the engine reads them (§5).
.PHONY: tables-elixir-variable-fixed-class
tables-elixir-variable-fixed-class: build/conformance/manifest.txt
	SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run 'TestFixedClassOnTableWireElixir|TestDefaultsAndTableRenameElixir'
	./build/conformance-harness run --only elixir

test-elixir: tables-elixir-variable-fixed-class

# THE `was` ROWS (docs/SPEC-TABLES.md §5) on the Elixir leg. TestElixirWasRowsCarried
# renaming one enum variant, one packet-union arm and one type field, feeds the new
# build an older writer's bytes (the engine over the old unit), and requires the new
# build to read and re-save them byte for byte. It is a FUNCTIONAL test (it runs the
# leg's own toolchain through elixirBinary), so it runs under
# SCHEMA_REQUIRE_CORPUS=1 and fails on any SKIP.
.PHONY: tables-elixir-wasrows
tables-elixir-wasrows:
	SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestElixirWasRowsCarried$$'
test-elixir: tables-elixir-wasrows

# THE FILE-MODULE STEP-ASIDE GATE (docs/SPEC.md, the Elixir backend list):
# a schema file whose base is also a declaration's name (tables/arms/Carry.schema
# declares union Carry) makes the packet emitter's file-scope module step aside
# from <Ns>.<Base> to <Ns>.<Base>File. This generates the unit out of tree,
# compiles the output with $(ELIXIRC_STRICT), and runs
# test/elixir-filemodule/main.exs, which calls a function of the renamed file
# module and a function of the union's own module and asserts both resolve — two
# distinct modules.
.PHONY: tables-elixir-file-module
tables-elixir-file-module: bin/schema
	@rm -rf build/elixir-filemodule && mkdir -p build/elixir-filemodule/gen build/elixir-filemodule/ebin
	./bin/schema generate --lang elixir --out build/elixir-filemodule/gen tables/arms
	$(ELIXIRC_STRICT) -o build/elixir-filemodule/ebin build/elixir-filemodule/gen/Carry.ex
	$(ELIXIR) -pa build/elixir-filemodule/ebin test/elixir-filemodule/main.exs

test-elixir: tables-elixir-file-module

# I13 (docs/PORTING.md, the text differential against a third implementation)
# on the Elixir leg. TestTextDifferentialElixir generates a float-only probe
# unit, has the generated Elixir draw 200 instances from a splitmix64 stream
# and write each as (Table.save wire, Table.to_json text), then has the
# compiler's own engine (internal/tabletext, internal/tablepack) read every wire
# and byte-compare its text with the leg's, and pack every leg text back and
# byte-compare the wire. The probe builds its own unit, so no corpus is a
# prerequisite. It is a FUNCTIONAL test (it runs elixir), so the target runs it
# under SCHEMA_REQUIRE_CORPUS=1 and fails on any SKIP. The control caps a
# float32 at six significant digits in the emitted json_float, in memory, and
# requires the run red on the text comparison and not on a compile error.
.PHONY: tables-elixir-json-differential tables-elixir-json-differential-negative-control
tables-elixir-json-differential:
	@out=$$(SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestTextDifferentialElixir$$' 2>&1); status=$$?; \
	printf '%s\n' "$$out"; \
	if [ $$status -ne 0 ]; then exit $$status; fi; \
	if printf '%s\n' "$$out" | grep -q -- '--- SKIP'; then echo 'tables-elixir-json-differential: a SKIP is a failure here'; exit 1; fi; \
	printf '%s\n' "$$out" | grep -q -- '--- PASS: TestTextDifferentialElixir' || { echo 'tables-elixir-json-differential: TestTextDifferentialElixir did not run'; exit 1; }
	@echo 'tables Elixir json differential: 200 of 200 instances agree with the engine in both directions'

tables-elixir-json-differential-negative-control:
	@out=$$(SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestTextDifferentialNegativeControlElixir$$' 2>&1); status=$$?; \
	printf '%s\n' "$$out"; \
	if [ $$status -ne 0 ]; then exit $$status; fi; \
	if printf '%s\n' "$$out" | grep -q -- '--- SKIP'; then echo 'tables-elixir-json-differential-negative-control: a SKIP is a failure here'; exit 1; fi; \
	printf '%s\n' "$$out" | grep -q -- '--- PASS: TestTextDifferentialNegativeControlElixir' || { echo 'tables-elixir-json-differential-negative-control: the control did not run'; exit 1; }
	@echo 'tables Elixir json differential negative control: a float32 capped at six digits went red on the text comparison'

test-elixir: tables-elixir-json-differential tables-elixir-json-differential-negative-control

# FINITE TABLE FLOATS on the Elixir leg (docs/SPEC-TABLES.md §3.3; Glenn,
# 2026-10-27: "Tables go finite-only too, same rule as packets"): every read,
# variable, retaining, message and fixed, refuses a NaN or an infinity on its
# raw bits as non_finite_float, every writer refuses one, and -0 rides by its
# bits. TestElixirFiniteFloat generates its own three units and runs a probe
# over every float position, so no corpus is a prerequisite. It is a FUNCTIONAL
# test (it runs elixir), so the target runs it under SCHEMA_REQUIRE_CORPUS=1 and
# fails on any SKIP. The control sabotages the variable read's bit test at each
# width, the retained unknown's test, the message read's, the fixed read's and
# the variable writer's, one at a time, and each must red exactly its own rows.
.PHONY: tables-elixir-finite-float tables-elixir-finite-float-negative-control
tables-elixir-finite-float:
	@out=$$(SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestElixirFiniteFloat$$' 2>&1); status=$$?; \
	printf '%s\n' "$$out"; \
	if [ $$status -ne 0 ]; then exit $$status; fi; \
	if printf '%s\n' "$$out" | grep -q -- '--- SKIP'; then echo 'tables-elixir-finite-float: a SKIP is a failure here'; exit 1; fi; \
	printf '%s\n' "$$out" | grep -q -- '--- PASS: TestElixirFiniteFloat' || { echo 'tables-elixir-finite-float: TestElixirFiniteFloat did not run'; exit 1; }
	@echo 'tables Elixir finite floats: every read refuses a NaN or an infinity, every writer refuses one, -0 rides'

tables-elixir-finite-float-negative-control:
	@out=$$(SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestElixirFiniteFloatNegativeControl$$' 2>&1); status=$$?; \
	printf '%s\n' "$$out"; \
	if [ $$status -ne 0 ]; then exit $$status; fi; \
	if printf '%s\n' "$$out" | grep -q -- '--- SKIP'; then echo 'tables-elixir-finite-float-negative-control: a SKIP is a failure here'; exit 1; fi; \
	printf '%s\n' "$$out" | grep -q -- '--- PASS: TestElixirFiniteFloatNegativeControl' || { echo 'tables-elixir-finite-float-negative-control: the control did not run'; exit 1; }
	@echo 'tables Elixir finite floats negative control: each sabotaged refusal reds exactly its own rows'

test-elixir: tables-elixir-finite-float tables-elixir-finite-float-negative-control

# THE VARIABLE CLASS'S REMAINING CONSTRUCTS (docs/SPEC-TABLES.md §2.3, §2.4,
# §2.6, §2.8, §3, §3.2, §4.12): maps, union arms of every field type and
# arrays of unions, optional fields and optional arrays, enum-keyed arrays,
# the fixed-point and 128-bit kinds, and wstring(N).
# TestMapsAndUnionArmsOnTheTableWireElixir generates tables/maps,
# tables/messages, tables/scalars and examples-wide through the COMPILER path
# (`schema generate --lang elixir`) and round-trips every committed instance,
# and the tables/arms goldens, through the generated Elixir against the Go
# oracle's wire and text byte for byte. It is a FUNCTIONAL test (it runs the
# leg's own toolchain), so SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain
# into a failure, never a skip. The conformance run answers wire, report,
# json-read and json-write for the mapdemo, messagedemo, scalars and widedemo
# instances.
.PHONY: tables-elixir-variable-arms
tables-elixir-variable-arms: build/conformance/manifest.txt
	SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run TestMapsAndUnionArmsOnTheTableWireElixir
	./build/conformance-harness run --only elixir

test-elixir: tables-elixir-variable-arms

# THE BUILDER SIDE (docs/SPEC-TABLES.md §19.1, §7.6). TestBlockBuildAndCookWriteElixir
# builds the pinned block golden and cooks the carried instances to `schema cook`
# in both byte orders; a short buffer returns false and writes nothing. It is a
# FUNCTIONAL test, so SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain into a
# failure. The conformance run answers cook-write for the instances this leg
# carries and writes <case>.absent for the rest.
.PHONY: tables-elixir-block-build
tables-elixir-block-build: build/conformance/manifest.txt
	SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run TestBlockBuildAndCookWriteElixir
	./build/conformance-harness run --only elixir

test-elixir: tables-elixir-block-build

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
#
# FU1/FU2 is the TEXT-UNDER-AN-ARM pin: a `string(N)` inside a union's SECOND
# arm, read once through the identity plan and once through a COMPILED one, so
# a plan entry that spent ONE lane on both "which arm guards me" and "which
# flavour of text am I" cannot pass here (reference-fix 12).
ELIXIR_FIXED_UNITS := fx1:test/tables/FX1.schema fx2:test/tables/FX2.schema \
	fe1:test/tables/FE1.schema fe2:test/tables/FE2.schema \
	fu1:test/tables/FU1.schema fu2:test/tables/FU2.schema \
	p1:test/tables/P1.schema p3:test/tables/P3.schema \
	examples:tables/examples

ELIXIR_FIXED_SCHEMAS := test/tables/FX1.schema test/tables/FX2.schema \
	test/tables/FE1.schema test/tables/FE2.schema \
	test/tables/FU1.schema test/tables/FU2.schema \
	test/tables/P1.schema test/tables/P3.schema $(SCHEMAS_TABLES)

build/elixir-fixedform/.stamp: bin/schema $(ELIXIR_FIXED_SCHEMAS) make/elixir.mk
	@rm -rf build/elixir-fixedform
	@mkdir -p build/elixir-fixedform/ebin
	@for unit in $(ELIXIR_FIXED_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		./bin/schema generate --lang elixir --out build/elixir-fixedform/gen/$$name $$path || exit 1; \
	done
	# The emitters write mix format's shape (tables-elixir-emit-format-clean),
	# so the stamp compiles their raw output.
	$(ELIXIRC_STRICT) -o build/elixir-fixedform/ebin build/elixir-fixedform/gen/*/*.ex
	@touch $@

.PHONY: tables-elixir-fixed-form
tables-elixir-fixed-form: build/elixir-fixedform/.stamp build/fixedform-corpus/.stamp
	# The fixed modules and FixedRuntime.ex are the emitters' raw output;
	# this holds both, and the hand-written driver,
	# to Elixir 1.20.4.
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
	# Raw emitter output (tables-elixir-emit-format-clean).
	$(ELIXIRC_STRICT) -o build/elixir-fixed-bench/ebin build/elixir-fixed-bench/gen/*.ex
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
# bench/paired/main.go's `-mode build -langs elixir` writes the same emitter
# bytes from the same copies. Every committed file is the emitter's raw
# output, which is the shape tables-elixir-paired-gate checks.
generated/bench/paired/elixir/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema make/elixir.mk
	@mkdir -p generated/bench/paired/elixir build/paired/elixir-src
	cp bench/corpus/Bench.schema build/paired/elixir-src/Bench.schema
	cp bench/corpus/FixedTable.schema build/paired/elixir-src/Wrap.schema
	./bin/schema generate --lang elixir --out generated/bench/paired/elixir \
		build/paired/elixir-src/Bench.schema build/paired/elixir-src/Wrap.schema
	@touch $@

# THE TABLES BOARD UNIT, for bench/tables/elixir/leg (bench/tables/README.md):
# the VARIABLE form (form 1) of the board's representative corpus. Committed,
# like every other language's generated/bench/tables/<lang>. The file's
# declaration and basename do not collide (docs/SPEC-TABLES.md §11), so no copy.
generated/bench/tables/elixir/.stamp: bin/schema bench/corpus/BenchTable.schema make/elixir.mk
	@mkdir -p generated/bench/tables/elixir
	./bin/schema generate --lang elixir --out generated/bench/tables/elixir bench/corpus/BenchTable.schema
	@touch $@

# THE BENCH LEG's golden gate, with no clock: the golden, all 64 variants load
# clean and re-save byte-identically, and two rotations (docs/PORTING.md J5).
.PHONY: tables-elixir-bench-gate
tables-elixir-bench-gate: generated/bench/tables/elixir/.stamp
	$(MIX) format --check-formatted bench/tables/elixir/form1_*.exs
	BEAM_PATH="$(BEAM_PATH)" bench/tables/elixir/leg build
	BEAM_PATH="$(BEAM_PATH)" bench/tables/elixir/leg run --gate
test-elixir: tables-elixir-bench-gate
BENCH_TABLES_LEGS += generated/bench/tables/elixir/.stamp

# THE PAIRED LEG's own gate, with no clock: the fixed corpus's layout compared
# byte for byte against this build's, the file loaded clean, saved back and
# compared, and then loaded and saved twice more into fresh terms. It is
# bench/tables/elixir's `--gate` mode, which is what bench/paired's gate runs,
# so `make test` and the paired driver hold this leg to the same bytes.
.PHONY: tables-elixir-paired-gate
tables-elixir-paired-gate: generated/bench/paired/elixir/.stamp bench-paired-check
	$(MIX) format --check-formatted generated/bench/paired/elixir/*.ex bench/tables/elixir/*.exs
	$(ELIXIRC_STRICT) -o build/paired/table-elixir/ebin generated/bench/paired/elixir/*.ex
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

# THE VERSIONING HALF OF THE ELIXIR LEG'S GATE (docs/FIXED-FORM-ALGORITHM.md
# §5.7 step 6, §5.9 #11: every leg owes TWO gates — the BYTES against the
# reference's corpus, which is tables-elixir-fixed-form above, and §5.7's ROWS
# against the versioning corpus, which is this one). The rows live in
# internal/codegen/elixirtable/fixedversioning_test.go, which plays the lock,
# generates the reader's Elixir with the older generation's locked entry as its
# lineage, writes ONE PROBE per row per column beside it and runs it with this
# leg's own toolchain (§5.9 #18).
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE: the harness
# SKIPS itself when build/fixedform-corpus or the Elixir in dist/ is absent,
# which is right for a bare `go test ./...` and is exactly how a §5 regression
# would ride into a green. So the target builds the oracle first and sets
# SCHEMA_REQUIRE_CORPUS=1, under which that skip is a FAILURE.
#
# ELIXIR_BIN and ERL_BIN are PLAIN PATHS and not the PATH-prefixed launchers the
# rest of this file uses, because the harness spawns the binary itself: empty is
# the default, and the harness then takes the Elixir and the OTP this file pins
# in dist/, or the ones on PATH when dist/ holds none — which is the CI shape,
# where setup-beam installs the pinned versions on PATH and the negative
# controls group passes ELIXIR=elixir and no ELIXIR_BIN at all.
ELIXIR_BIN ?=
ERL_BIN    ?=

.PHONY: tables-elixir-versioning
tables-elixir-versioning: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -run 'TestFixedVersioning'
	@echo 'tables Elixir versioning: §5 read both columns of every row against the C++ reference bytes'

# ITS NEGATIVE CONTROL (§5.9 #11: a gate nobody has watched fail may be
# comparing a file with itself). The sabotage replaces the ONE name that carries
# §5's whole direction — a hash no lineage entry holds is `layout_newer`, ship
# the reader — with `layout_malformed`, on a file OUTSIDE the lineage, and the
# OLD-REFUSES-NEW column must go red naming it.
.PHONY: tables-elixir-versioning-negative-control
tables-elixir-versioning-negative-control: tables-fixedform-corpus
	@rm -rf build/elixir-versioning-nc && mkdir -p build/elixir-versioning-nc
	@sed 's|{:error, :layout_newer, hash}|{:error, :layout_malformed, hash}|' \
		internal/codegen/elixirtable/fixedruntime.go > build/elixir-versioning-nc/fixedruntime.go.txt
	@cmp -s internal/codegen/elixirtable/fixedruntime.go build/elixir-versioning-nc/fixedruntime.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/elixirtable/fixedruntime.go":"%s/build/elixir-versioning-nc/fixedruntime.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/elixir-versioning-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
			go test -overlay build/elixir-versioning-nc/overlay.json \
			./internal/codegen/elixirtable/ -count=1 -timeout 600s -run 'TestFixedVersioningOldRefusesNew' \
			> build/elixir-versioning-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the wrong refusal name left the versioning gate green"; \
		cat build/elixir-versioning-nc/log; exit 1; \
	fi
	@grep -q 'owes layout_newer' build/elixir-versioning-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the versioning gate went red for another reason"; \
		  cat build/elixir-versioning-nc/log; exit 1; }
	@echo 'negative control: the wrong name for a hash outside the lineage reds the Elixir versioning gate'

# elixir/F12: a second layout for a held hash (docs/SPEC-TABLES.md §3.4,
# docs/FIXED-FORM-ALGORITHM.md §5.3 step 5). The row script builds its vector
# from the generated module's OWN facts — the held hash, the lock's layout
# bytes and the file header — with no corpus file, so this target needs only
# the compiled fixed-form beams. It is the leg's on-GENERATED-code assertion
# that a held hash over different bytes is refused `:layout_malformed`, changes
# nothing and is never `:layout_newer`.
.PHONY: tables-elixir-row-f12
tables-elixir-row-f12: build/elixir-fixedform/.stamp
	$(ELIXIR) test/conformance/elixir/rows/F12.exs

# The same law as a Go functional probe (fixedroadmap_t13_test.go), which draws
# its vectors from T.lineage_fixed_known/0 rather than the corpus: every byte of
# the held layout bent in turn, the other lock entry's real layout under this
# build's hash, and this build's layout under the older entry's hash, each
# `:layout_malformed`, plus the `:layout_newer` contrast. It runs the leg's own
# Elixir, so it is gated exactly like tables-elixir-versioning: the corpus skip
# is a FAILURE under SCHEMA_REQUIRE_CORPUS=1.
.PHONY: tables-elixir-t13
tables-elixir-t13:
	SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run 'TestFixedRoadmapElixirT13SecondLayoutUnderAHeldHash'
	@echo 'tables Elixir T13: a second layout under a held hash is layout_malformed, no counter moved'

# THE MESSAGE FORM'S BATCH ON THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3).
# TestMessageFormBatchElixir reads the `message` rows of the conformance
# manifest and runs the generated Elixir against the leg's own toolchain, so it
# is a FUNCTIONAL test: elixirBinary SKIPS when no Elixir is found and only
# FAILS by name under SCHEMA_REQUIRE_CORPUS — which is the shape
# tables-elixir-versioning (above) established: a bare `go test ./...` is green
# with no toolchain, so the leg's gate is never satisfied by absence. This
# target is that gate for the batch: it arms SCHEMA_REQUIRE_CORPUS=1, pins the
# one test, and reds on any `--- SKIP` in the -v output.
#
# ERL_FLAGS CAPS EACH BEAM'S SCHEDULER THREADS. The batch runs one Elixir VM per
# parallel subtest, and a default BEAM starts one scheduler thread per core plus
# its dirty schedulers — on a 64-core bench with the per-user thread limit at
# ~1870 that is EAGAIN inside the VM ("Failed to create scheduler thread"), not
# a verdict about the bytes. One scheduler of each class is all the generated
# probe uses, and the VM still runs the whole probe.
.PHONY: tables-elixir-message-batch
tables-elixir-message-batch:
	@mkdir -p build
	@SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_FLAGS='+S 1:1 +SDcpu 1 +SDio 1' \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestMessageFormBatchElixir$$' \
		> build/tables-elixir-message-batch.log 2>&1; \
		status=$$?; cat build/tables-elixir-message-batch.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@if grep -q -- '--- SKIP' build/tables-elixir-message-batch.log; then \
		echo "tables-elixir-message-batch: the batch gate SKIPPED a subtest; under SCHEMA_REQUIRE_CORPUS=1 a missing toolchain or corpus is a failure"; \
		exit 1; \
	fi
	@echo 'tables Elixir message batch: TestMessageFormBatchElixir ran under test-elixir with no subtest skipped'

# THE MESSAGE SURFACE ON THE HARNESS (test/conformance/README.md, "The
# surfaces"; docs/SPEC-TABLES.md §3.3): the driver answers every manifest
# `message` row whose root is a fixed table, byte-equal to the C++ reference's
# golden, and writes graph_tree.absent (its root Scene is a plain `table`, which
# the form refuses by name). TestMessageFormConformanceElixir's harness clause
# runs `./build/conformance-harness run --only elixir` over the built leg; the
# target arms SCHEMA_REQUIRE_CORPUS=1 and reds on any `--- SKIP`.
.PHONY: tables-elixir-message-conformance
tables-elixir-message-conformance: build/conformance-harness build-conformance-elixir
	@mkdir -p build
	@SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestMessageFormConformanceElixir$$' \
		> build/tables-elixir-message-conformance.log 2>&1; \
		status=$$?; cat build/tables-elixir-message-conformance.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@if grep -q -- '--- SKIP' build/tables-elixir-message-conformance.log; then \
		echo "tables-elixir-message-conformance: a clause SKIPPED; under SCHEMA_REQUIRE_CORPUS=1 a missing toolchain or build is a failure"; \
		exit 1; \
	fi
	@echo 'tables Elixir message conformance: the driver answers the message surface, graph_tree the one named absence'
test-elixir: tables-elixir-message-conformance

.PHONY: tables-elixir-fixed-roadmap-t12
tables-elixir-fixed-roadmap-t12: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 SCHEMA_SLOW=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run 'TestFixedRoadmapElixirT12OrdinalsArms'
	@echo 'tables Elixir T12: R27 manifest oracle, union-field-arms and payload-free-arms verified'

# M5 (#407): 1-based keyed array accessors (<field>_at/2, put_<field>_at/3,
# <field>_entries/1) on both table structs and type structs, refusing None (0)
# and keys past E.Max with ArgumentError.
.PHONY: tables-elixir-keyed
tables-elixir-keyed:
	@mkdir -p build
	@SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) ERL_FLAGS='+S 1:1 +SDcpu 1 +SDio 1' \
		go test ./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestKeyedAccessorCoversBothEndsElixir$$' \
		> build/tables-elixir-keyed.log 2>&1; \
		status=$$?; cat build/tables-elixir-keyed.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@if grep -q -- '--- SKIP' build/tables-elixir-keyed.log; then \
		echo "tables-elixir-keyed: the keyed array gate SKIPPED; under SCHEMA_REQUIRE_CORPUS=1 a missing toolchain is a failure"; \
		exit 1; \
	fi
	@echo 'tables Elixir keyed: 1-based keyed array accessors refuse None (0) and keys past E.Max on both table and type structs'

ELIXIR_KEYED_NONE_SED := -e 's|key >= 1 and ||'

.PHONY: tables-elixir-keyed-none-negative-control
tables-elixir-keyed-none-negative-control:
	@rm -rf build/elixir-keyed-none-nc && mkdir -p build/elixir-keyed-none-nc
	@sed $(ELIXIR_KEYED_NONE_SED) \
		internal/codegen/elixir/elixir.go > build/elixir-keyed-none-nc/elixir.go.txt
	@cmp -s internal/codegen/elixir/elixir.go build/elixir-keyed-none-nc/elixir.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: ELIXIR_KEYED_NONE_SED matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/elixir/elixir.go":"%s/build/elixir-keyed-none-nc/elixir.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/elixir-keyed-none-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) ERL_FLAGS='+S 1:1 +SDcpu 1 +SDio 1' \
			go test -overlay build/elixir-keyed-none-nc/overlay.json \
			./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestKeyedAccessorCoversBothEndsElixir$$' \
			> build/elixir-keyed-none-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: dropping the key >= 1 guard left TestKeyedAccessorCoversBothEndsElixir green"; \
		cat build/elixir-keyed-none-nc/log; exit 1; \
	fi
	@grep -q 'key 0 (None) was not refused' build/elixir-keyed-none-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: TestKeyedAccessorCoversBothEndsElixir went red for another reason"; \
		  cat build/elixir-keyed-none-nc/log; exit 1; }
	@echo 'negative control: dropping the key >= 1 guard reds TestKeyedAccessorCoversBothEndsElixir on key 0 (None)'

ELIXIR_KEYED_MAX_SED := -e 's| and key <= " + maxLit + "||'

.PHONY: tables-elixir-keyed-max-negative-control
tables-elixir-keyed-max-negative-control:
	@rm -rf build/elixir-keyed-max-nc && mkdir -p build/elixir-keyed-max-nc
	@sed $(ELIXIR_KEYED_MAX_SED) \
		internal/codegen/elixir/elixir.go > build/elixir-keyed-max-nc/elixir.go.txt
	@cmp -s internal/codegen/elixir/elixir.go build/elixir-keyed-max-nc/elixir.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: ELIXIR_KEYED_MAX_SED matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/elixir/elixir.go":"%s/build/elixir-keyed-max-nc/elixir.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/elixir-keyed-max-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) ERL_FLAGS='+S 1:1 +SDcpu 1 +SDio 1' \
			go test -overlay build/elixir-keyed-max-nc/overlay.json \
			./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestKeyedAccessorCoversBothEndsElixir$$' \
			> build/elixir-keyed-max-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: dropping the key <= max guard left TestKeyedAccessorCoversBothEndsElixir green"; \
		cat build/elixir-keyed-max-nc/log; exit 1; \
	fi
	@grep -q 'key past max' build/elixir-keyed-max-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: TestKeyedAccessorCoversBothEndsElixir went red for another reason"; \
		  cat build/elixir-keyed-max-nc/log; exit 1; }
	@echo 'negative control: dropping the key <= max guard reds TestKeyedAccessorCoversBothEndsElixir on key past max'

# THE COOK NUMBERS A BLOB BY ITS SLOT (docs/SPEC-TABLES.md §2.5, §3.1, §7.6):
# two slots holding equal bytes are two blob nodes in a cook as on the wire.
# The control keys the cook's blob mark by the bytes instead of the slot, which
# makes the equal-bytes slots one node, and requires
# TestCookNumbersABlobByItsSlotElixir to red on blob_equal_bytes alone.
ELIXIR_COOK_BLOB_SED := -e 's|    slot = {:cook_blob, length(acc)}|    slot = {:cook_blob, v}|'

.PHONY: tables-elixir-cook-blob-negative-control
tables-elixir-cook-blob-negative-control:
	@rm -rf build/elixir-cook-blob-nc && mkdir -p build/elixir-cook-blob-nc
	@sed $(ELIXIR_COOK_BLOB_SED) \
		internal/codegen/elixirtable/cookwrite.go > build/elixir-cook-blob-nc/cookwrite.go.txt
	@cmp -s internal/codegen/elixirtable/cookwrite.go build/elixir-cook-blob-nc/cookwrite.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: ELIXIR_COOK_BLOB_SED matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/elixirtable/cookwrite.go":"%s/build/elixir-cook-blob-nc/cookwrite.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/elixir-cook-blob-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 ELIXIR=$(ELIXIR_BIN) ERL_BIN=$(ERL_BIN) ERL_FLAGS='+S 1:1 +SDcpu 1 +SDio 1' \
			go test -overlay build/elixir-cook-blob-nc/overlay.json \
			./internal/codegen/elixirtable/ -count=1 -timeout 600s -v -run '^TestCookNumbersABlobByItsSlotElixir$$' \
			> build/elixir-cook-blob-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: keying the cook's blob by its bytes left TestCookNumbersABlobByItsSlotElixir green"; \
		cat build/elixir-cook-blob-nc/log; exit 1; \
	fi
	@grep -q 'FAIL blob_equal_bytes: ' build/elixir-cook-blob-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: TestCookNumbersABlobByItsSlotElixir went red for another reason"; \
		  cat build/elixir-cook-blob-nc/log; exit 1; }
	@if grep 'FAIL ' build/elixir-cook-blob-nc/log | grep -v -q 'FAIL blob_equal_bytes'; then \
		echo "NEGATIVE CONTROL FAILED: keying the blob by its bytes red more than blob_equal_bytes"; \
		cat build/elixir-cook-blob-nc/log; exit 1; \
	fi
	@echo 'negative control: keying the cook blob by its bytes reds TestCookNumbersABlobByItsSlotElixir on blob_equal_bytes'

test-elixir: tables-elixir-cook-blob-negative-control
test-elixir: tables-elixir-fixed-form tables-elixir-fixed-form-negative-control tables-elixir-fixed-bench
test-elixir: tables-elixir-versioning tables-elixir-versioning-negative-control
test-elixir: tables-elixir-message-batch
test-elixir: tables-elixir-paired-gate
test-elixir: tables-elixir-row-f12 tables-elixir-t13 tables-elixir-fixed-roadmap-t12
test-elixir: tables-elixir-keyed tables-elixir-keyed-none-negative-control tables-elixir-keyed-max-negative-control

# ---------------------------------------------------------------------------
# THE CARDS UNIT'S ELIXIR LEG (docs/SPEC-TABLES.md §2)
# ---------------------------------------------------------------------------
#
# These targets are the Elixir side of the cards wire-fuzz gate: they mirror the
# Go leg's cards-generate and cards-wire-fuzz (make/go.mk) against the same
# oracle harness and the same seed, so the Elixir Card codec is held to the
# identical byte-for-byte contract. They live in make/elixir.mk (NOT a separate
# make/cards-elixir.mk or make/cards-elixir-retain.mk) because the wide-scalar
# refusal gate (tables-ports-refuse-wide-scalars) discovers every make/*.mk as a
# live port, and a fragment named cards-elixir.mk or cards-elixir-retain.mk
# would be refused there as a phantom port.
.PHONY: cards-elixir-generate cards-elixir-wire-fuzz cards-elixir-retain-fuzz

# cards-elixir-generate emits the Elixir table surface for tables/cards.
# The packet emitter's name check no longer refuses the `Card` table for
# colliding with the module it writes for schema file Card.schema
# (docs/SPEC-TABLES.md §11): a table lowers to a module in the TABLE backend,
# so the packet emitter skips it and the command reaches elixirtable.
cards-elixir-generate:
	@rm -rf build/tables-generated-elixir/cards
	@mkdir -p build/tables-generated-elixir/cards
	./bin/schema generate --lang elixir --out build/tables-generated-elixir/cards tables/cards

# the cards corpus and the driver compiled into one ebin, so the wire-fuzz
# driver's one BEAM can name the Cards.Table codec it drives.
build/elixir-cards-ebin/.stamp: cards-elixir-generate test/conformance/elixir/driver_impl.ex
	@rm -rf build/elixir-cards-ebin && mkdir -p build/elixir-cards-ebin
	$(ELIXIRC) -o build/elixir-cards-ebin build/tables-generated-elixir/cards/*.ex \
		test/conformance/elixir/driver_impl.ex
	@touch $@

# cards-elixir-wire-fuzz runs the conformance harness's wire fuzzer over the
# Card root, comparing the Elixir driver's reads and writes with the Go
# reference leg's. It requires cards-elixir-generate to have succeeded first.
# The gate refuses a zero-coverage roster: the harness errors when the leg has
# a codec for none of the named roots, and the greps below hold the nonzero
# seed/mutant claim explicitly.
cards-elixir-wire-fuzz: build/conformance-harness build/elixir-cards-ebin/.stamp
	@ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-cards-ebin \
		./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'test/conformance/elixir/driver wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-elixir-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-elixir-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-elixir-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-elixir-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-elixir-wire-fuzz.log

test-elixir: cards-elixir-wire-fuzz

# cards-elixir-retain-fuzz is the SAME fuzzer through the RETENTION ARM
# (docs/SPEC-TABLES.md §6.6): every mutant is read and written back through the
# Card codec's retaining pair, and the two retention counters ride beside the
# six. The seed, count and coverage greps are cards-elixir-wire-fuzz's, plus
# --retain, so a retained save is held to the Go reference leg byte for byte.
cards-elixir-retain-fuzz: build/conformance-harness build/elixir-cards-ebin/.stamp
	@ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-cards-ebin \
		./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'test/conformance/elixir/driver wire-fuzz' --seed 24845619678 --n 100 --retain \
		> build/cards-elixir-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-elixir-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-elixir-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-elixir-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-elixir-retain-fuzz.log

test-elixir: cards-elixir-retain-fuzz

# THE WIRE FUZZER OVER EVERY ROOT (docs/SPEC-TABLES.md §4.2; test/conformance/
# README.md, "The wire-fuzz driver"): the plain run of the same fuzzer the cards
# targets above drive, over the harness's DEFAULT roster
# (testdata/conformance/tables/MANIFEST.txt, the default in
# test/conformance/harness/main.go) rather than the Card manifest: no --manifest,
# the roster tables-go-wire-fuzz runs. The coverage greps are the card targets',
# so a roster with no live root fails. The roots the leg cannot open are held to
# a pinned list BY NAME: the driver writes each roster entry it answers 0 for to
# $(ELIXIR_WIRE_FUZZ_ABSENT_LOG), and the recipe fails on any difference from
# test/conformance/elixir/wire-fuzz-absent.txt and on any count other than
# ELIXIR_WIRE_FUZZ_ABSENT_COUNT, naming each root that moved. The absent check
# runs before the harness status is read, so a red run still names them. The
# --retain arm of this roster is its own gate.
ELIXIR_WIRE_FUZZ_ABSENT_COUNT := 33
ELIXIR_WIRE_FUZZ_ABSENT_LOG = build/tables-elixir-wire-fuzz-all.absent
.PHONY: tables-elixir-wire-fuzz-all
tables-elixir-wire-fuzz-all: build/conformance-harness build/elixir-tables-ebin/.stamp
	@rm -f $(ELIXIR_WIRE_FUZZ_ABSENT_LOG)
	@ELIXIR_WIRE_FUZZ_ABSENT=$(CURDIR)/$(ELIXIR_WIRE_FUZZ_ABSENT_LOG) \
		ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin ./build/conformance-harness wire-fuzz --driver 'test/conformance/elixir/driver wire-fuzz' --seed $(SEED) --n $(N) \
		> build/tables-elixir-wire-fuzz-all.log 2>&1; \
		status=$$?; cat build/tables-elixir-wire-fuzz-all.log; \
		if [ ! -f $(ELIXIR_WIRE_FUZZ_ABSENT_LOG) ]; then \
			echo "tables-elixir-wire-fuzz-all: the driver wrote no absent-root list"; exit 1; fi; \
		sort $(ELIXIR_WIRE_FUZZ_ABSENT_LOG) > $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).sorted; \
		grep -v '^\#' test/conformance/elixir/wire-fuzz-absent.txt | sort > $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).pinned; \
		n=$$(wc -l < $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).sorted | tr -d ' '); \
		if ! cmp -s $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).sorted $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).pinned || \
			[ "$$n" -ne $(ELIXIR_WIRE_FUZZ_ABSENT_COUNT) ]; then \
			echo "tables-elixir-wire-fuzz-all: $$n roots absent, $(ELIXIR_WIRE_FUZZ_ABSENT_COUNT) pinned (test/conformance/elixir/wire-fuzz-absent.txt)"; \
			comm -23 $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).sorted $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).pinned | sed 's/^/  the leg cannot open: /'; \
			comm -13 $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).sorted $(ELIXIR_WIRE_FUZZ_ABSENT_LOG).pinned | sed 's/^/  pinned absent but open now (unpin it): /'; \
			exit 1; fi; \
		echo "tables-elixir-wire-fuzz-all: $$n roots absent, as pinned"; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/tables-elixir-wire-fuzz-all.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/tables-elixir-wire-fuzz-all.log
	@grep -qE '[1-9][0-9]* mutants' build/tables-elixir-wire-fuzz-all.log

test-elixir: tables-elixir-wire-fuzz-all

# cards-elixir-test compiles the facade (tables/cards/elixir/cards.ex) against
# the generated codec ebin and runs the ExUnit tests (cards_test.exs). The
# facade is the owning layer over Cards.Table, ported from tables/cards/cards.go.
build/elixir-cards-facade/.stamp: build/elixir-cards-ebin/.stamp tables/cards/elixir/cards.ex
	@rm -rf build/elixir-cards-facade && mkdir -p build/elixir-cards-facade
	$(ELIXIRC) -pa build/elixir-cards-ebin -o build/elixir-cards-facade tables/cards/elixir/cards.ex
	@touch $@

.PHONY: cards-elixir-test
cards-elixir-test: build/elixir-cards-facade/.stamp
	$(ELIXIR) -pa build/elixir-cards-ebin -pa build/elixir-cards-facade tables/cards/elixir/cards_test.exs

test-elixir: cards-elixir-test

# The card conformance driver, compiled against the facade and the codec.
build/elixir-cards-driver/.stamp: build/elixir-cards-facade/.stamp tables/cards/elixir/driver.exs
	@rm -rf build/elixir-cards-driver && mkdir -p build/elixir-cards-driver
	$(ELIXIRC) -pa build/elixir-cards-ebin -pa build/elixir-cards-facade -o build/elixir-cards-driver tables/cards/elixir/driver.exs
	@touch $@

# cards-elixir-conformance runs the harness's cards subcommand over the Elixir
# driver and requires all nine surfaces (tables/cards/conformance/FORMAT.md).
# Absence is a failure: a surface left out of the driver's list, or answered
# with exit 2, reds the required check.
.PHONY: cards-elixir-conformance
cards-elixir-conformance: build/conformance-harness build/elixir-cards-driver/.stamp
	./build/conformance-harness cards --corpus tables/cards/conformance \
		--driver tables/cards/elixir/driver \
		--require load,save,render,parse,bounds,upgrade,edit,taskhash,view

# cards-elixir-conformance-negative-control flips one rendered byte through a
# test-only environment switch and requires the check to fail on render while
# load, save, parse and bounds pass.
.PHONY: cards-elixir-conformance-negative-control
cards-elixir-conformance-negative-control: build/conformance-harness build/elixir-cards-driver/.stamp
	@if CARDS_ELIXIR_NEGATIVE=1 ./build/conformance-harness cards --corpus tables/cards/conformance \
			--driver tables/cards/elixir/driver --require load,save,render,parse,bounds \
			> build/cards-elixir-conformance-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a changed render byte left the check green"; exit 1; fi
	@grep -q 'cards render FAIL' build/cards-elixir-conformance-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the check went red for another reason"; cat build/cards-elixir-conformance-negative.log; exit 1; }
	@grep -q 'cards load pass' build/cards-elixir-conformance-negative.log
	@grep -q 'cards save pass' build/cards-elixir-conformance-negative.log
	@grep -q 'cards parse pass' build/cards-elixir-conformance-negative.log
	@grep -q 'cards bounds pass' build/cards-elixir-conformance-negative.log
	@echo 'cards Elixir negative control: one changed render byte reds render while load, save, parse and bounds pass'

# cards-elixir-conformance-upgrade-negative-control drops one retained field
# through a test-only switch and requires the check to fail on upgrade alone.
.PHONY: cards-elixir-conformance-upgrade-negative-control
cards-elixir-conformance-upgrade-negative-control: build/conformance-harness build/elixir-cards-driver/.stamp
	@if CARDS_ELIXIR_UPGRADE_NEGATIVE=1 ./build/conformance-harness cards --corpus tables/cards/conformance \
			--driver tables/cards/elixir/driver \
			--require load,save,render,parse,bounds,upgrade,edit,taskhash,view \
			> build/cards-elixir-conformance-upgrade-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: dropping one retained field left the check green"; exit 1; fi
	@grep -q 'cards upgrade FAIL' build/cards-elixir-conformance-upgrade-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the check went red for another reason"; cat build/cards-elixir-conformance-upgrade-negative.log; exit 1; }
	@grep -q 'cards load pass' build/cards-elixir-conformance-upgrade-negative.log
	@grep -q 'cards save pass' build/cards-elixir-conformance-upgrade-negative.log
	@grep -q 'cards render pass' build/cards-elixir-conformance-upgrade-negative.log
	@grep -q 'cards parse pass' build/cards-elixir-conformance-upgrade-negative.log
	@grep -q 'cards bounds pass' build/cards-elixir-conformance-upgrade-negative.log
	@grep -q 'cards edit pass' build/cards-elixir-conformance-upgrade-negative.log
	@grep -q 'cards taskhash pass' build/cards-elixir-conformance-upgrade-negative.log
	@grep -q 'cards view pass' build/cards-elixir-conformance-upgrade-negative.log
	@echo 'cards Elixir upgrade negative control: dropping one retained field reds upgrade alone'

# cards-elixir-conformance-hash-negative-control adds needs to the task hash
# through a test-only switch and requires the check to fail on taskhash alone.
.PHONY: cards-elixir-conformance-hash-negative-control
cards-elixir-conformance-hash-negative-control: build/conformance-harness build/elixir-cards-driver/.stamp
	@if CARDS_ELIXIR_HASH_NEGATIVE=1 ./build/conformance-harness cards --corpus tables/cards/conformance \
			--driver tables/cards/elixir/driver \
			--require load,save,render,parse,bounds,upgrade,edit,taskhash,view \
			> build/cards-elixir-conformance-hash-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: folding needs into the hash left the check green"; exit 1; fi
	@grep -q 'cards taskhash FAIL' build/cards-elixir-conformance-hash-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the check went red for another reason"; cat build/cards-elixir-conformance-hash-negative.log; exit 1; }
	@grep -q 'cards load pass' build/cards-elixir-conformance-hash-negative.log
	@grep -q 'cards save pass' build/cards-elixir-conformance-hash-negative.log
	@grep -q 'cards render pass' build/cards-elixir-conformance-hash-negative.log
	@grep -q 'cards parse pass' build/cards-elixir-conformance-hash-negative.log
	@grep -q 'cards bounds pass' build/cards-elixir-conformance-hash-negative.log
	@grep -q 'cards upgrade pass' build/cards-elixir-conformance-hash-negative.log
	@grep -q 'cards edit pass' build/cards-elixir-conformance-hash-negative.log
	@grep -q 'cards view pass' build/cards-elixir-conformance-hash-negative.log
	@echo 'cards Elixir hash negative control: folding needs into the hash reds taskhash alone'

test-elixir: cards-elixir-conformance cards-elixir-conformance-negative-control

# THE CLEAN-GATE PLANTED-WARNING NEGATIVE CONTROL. A gate that is supposed to
# fail on a warning has to prove it can: this compiles a planted warning (an
# unused variable and an unused alias) through the same strict invocation the
# gates use and requires the compile to exit NON-ZERO with the warning text. It
# passes only when the strict compile fails, and fails by name when the compile
# exits 0. Without a toolchain it skips by name; under SCHEMA_REQUIRE_CORPUS=1
# a missing toolchain is a failure, not a skip.
.PHONY: tables-elixir-warning-control
tables-elixir-warning-control:
	@if ! command -v $(lastword $(ELIXIRC)) >/dev/null 2>&1; then \
		if [ "$$SCHEMA_REQUIRE_CORPUS" = "1" ]; then \
			echo "tables-elixir-warning-control: elixirc missing and SCHEMA_REQUIRE_CORPUS=1"; exit 1; \
		fi; \
		echo "SKIP tables-elixir-warning-control (no elixirc on PATH)"; exit 0; \
	fi
	@mkdir -p build/elixir-warning-control
	@if $(ELIXIRC_STRICT) -o build/elixir-warning-control test/elixir-warning-control/planted.ex > build/elixir-warning-control/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the planted warning compiled clean (exit 0)'; \
		cat build/elixir-warning-control/log; exit 1; \
	fi
	@grep -q 'warning:' build/elixir-warning-control/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the strict compile failed without the warning text'; \
		  cat build/elixir-warning-control/log; exit 1; }
	@echo 'planted warning makes the strict Elixir compile exit non-zero'
test-elixir: tables-elixir-warning-control
test-elixir: cards-elixir-conformance-upgrade-negative-control cards-elixir-conformance-hash-negative-control

# ---------------------------------------------------------------------------
# THE UNIT REGISTRY ON THE ELIXIR LEG (docs/SPEC-TABLES.md §8.3, §8.5, §8.7)
# ---------------------------------------------------------------------------
#
# The tables-cs-view twin. EVERY VIEW_CORPUS unit (Makefile) is listed: each
# unit's <Ns>.UnitView.unit_view/0 is printed in canonical listing form by
# test/elixir-view/view_main.exs and checked byte-for-byte against
# internal/viewlisting's IR listing, all fifteen, with no
# SCHEMA_VIEW_LISTING_UNITS narrowing.
#
# Ten units come from `schema generate --lang elixir`, the whole unit. Five the
# compiler still refuses for elixir by name (VIEW_ELIXIR_REGISTRY_ONLY: messages
# and wide, `was` on an enum variant, a union arm or a type's field; stream and
# backend, a union a table closure holds; arms, the declaration Carry colliding
# with the module written for Carry.schema). For those the registry alone is
# emitted by test/elixir-view/emit, the same unitViewModule the generator
# calls, so the listing still covers them. The --unflattened run on tabledemo
# is the negative control: a damaged listing must turn the oracle red.
VIEW_ELIXIR_REGISTRY_ONLY := messages stream arms backend wide

.PHONY: tables-elixir-view
tables-elixir-view: bin/schema test/elixir-view/view_main.exs
	@rm -rf build/elixir-view
	@mkdir -p build/elixir-view
	go build -o build/elixir-view/emit ./test/elixir-view/emit
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		source=tables/$$dir; [ "$$dir" != wide ] || source=examples-wide; \
		gen=build/elixir-view/generated/$$dir; mkdir -p $$gen; \
		case " $(VIEW_ELIXIR_REGISTRY_ONLY) " in \
			*" $$dir "*) ./build/elixir-view/emit "$$source" $$gen/UnitView.ex ;; \
			*) ./bin/schema generate --lang elixir --out $$gen "$$source" ;; \
		esac; \
		$(ELIXIR) -r $$gen/UnitView.ex test/elixir-view/view_main.exs $$pkg > build/elixir-view/$$pkg.listing; \
		if [ "$$pkg" = tabledemo ]; then $(ELIXIR) -r $$gen/UnitView.ex test/elixir-view/view_main.exs $$pkg --unflattened > build/elixir-view/unflattened.listing; fi; \
	done
	SCHEMA_VIEW_LISTING_DIR=$(CURDIR)/build/elixir-view go test ./internal/viewlisting -count=1 -timeout 600s -run TestUnitViewListingMatchesTheIR
	@mkdir -p build/elixir-view/negative
	@cp build/elixir-view/unflattened.listing build/elixir-view/negative/tabledemo.listing
	@if SCHEMA_VIEW_LISTING_DIR=$(CURDIR)/build/elixir-view/negative SCHEMA_VIEW_LISTING_UNITS=tabledemo go test ./internal/viewlisting -count=1 -timeout 600s -run TestUnitViewListingMatchesTheIR > build/elixir-view/negative.log 2>&1; then echo "Elixir UnitView negative control escaped"; exit 1; fi
	@grep -q "listing is not the compiler's" build/elixir-view/negative.log
	@echo "Elixir UnitView: $(words $(VIEW_CORPUS)) generated registries match the IR; damaged documentation is detected"

test-elixir: tables-elixir-view

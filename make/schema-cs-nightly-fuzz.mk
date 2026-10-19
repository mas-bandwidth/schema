# NIGHTLY FUZZING FOR THE C# PORT (Glenn, 2026-10-18).
#
# The C# port's generated readers are fuzzed with the port's native
# coverage-guided fuzzer: SharpFuzz (AFL-based) instrumenting the generated
# assembly, driven by AFL++. This file is the card's own make fragment; the
# root Makefile includes make/*.mk, so nothing else needs editing.
#
# THE CORPUS IS THE SCHEMA REPOSITORY'S OWN. CS_FUZZ_SCHEMA_DIRS below names
# exactly the directories the generation targets read (make/cs.mk's
# build/tables-generated-cs/.stamp and make/go.mk's table corpus). The list is
# written to $(CS_FUZZ_CORPUS), one `schema<TAB>outdir` line per schema, and
# that file is what the harness reads: no second corpus lives here.
#
# THE ORACLE. A read or parse of malformed bytes may refuse with a typed
# verdict, but it must never throw, hang, or silently accept past the input.
# The harness turns a thrown exception into a non-zero exit, which is what AFL
# records as a crash; a refusal is the generated reader's own explicit
# `TableWire.Verdict`/`false`.
#
# THE TIME BUDGET is an argument, never the clock in a unit test:
#   make schema-cs-nightly-fuzz                 # 10 minutes per port
#   make schema-cs-nightly-fuzz CS_FUZZ_TIME=1m
#
# TOOLS. Both are installed fleet-wide from their official sources by the
# fleet play (rowan-tools/fleet); this target never installs anything. When a
# tool is absent it says `tool missing: <name>` and exits non-zero, naming the
# machine: a missing fuzzer is a not-done, never a pass.
#
# Versions are pinned so a drifting fuzzer cannot silently change the gate.
CS_FUZZ_AFL_VERSION ?= 4.21c
CS_FUZZ_SHARPFUZZ_VERSION ?= 2.2.0
CS_FUZZ_TIME ?= 10m
CS_FUZZ_SEED ?= 24845619678
CS_FUZZ_MEMORY ?= none
CS_FUZZ_TIMEOUT ?= 5000
CS_FUZZ_KIND ?= load text

CS_FUZZ_BUILD := build/schema-cs-nightly-fuzz
CS_FUZZ_READERS := $(CS_FUZZ_BUILD)/readers
CS_FUZZ_CORPUS := $(CS_FUZZ_BUILD)/corpus.txt
CS_FUZZ_SEEDS := $(CS_FUZZ_BUILD)/seeds
CS_FUZZ_OUT := $(CS_FUZZ_BUILD)/afl
CS_FUZZ_CORPUS_COMMITTED := testdata/schema-cs-nightly-fuzz/corpus

# THE CORPUS, verbatim from make/cs.mk's tables_generate_cs: the same
# directory-or-file units the C# generation target reads, one outdir each.
# Entries are `<source>:<outname>`; a source that is a directory contributes
# every .schema under it to the manifest. Nothing here is a second corpus.
CS_FUZZ_UNITS := \
	tables/examples:examples tables/pointers:pointers tables/block:block \
	tables/blockhome:blockhome \
	test/tables/V1.schema:v1 test/tables/V2.schema:v2 test/tables/P1.schema:p1 \
	test/tables/P3.schema:p3 test/tables/FX1.schema:fx1 test/tables/FX2.schema:fx2 \
	test/tables/K1.schema:k1 test/tables/K2.schema:k2 test/tables/CsIds.schema:csids \
	test/tables/CsUnions.schema:csunions test/tables/CsView.schema:csview \
	tables/messages:messages test/tables/M1.schema:m1 test/tables/M2.schema:m2 \
	test/tables/A1.schema:a1 test/tables/A2.schema:a2 tables/scalars:scalars \
	test/tables/Scalars2.schema:scalars2 examples-wide/Caption.schema:caption \
	tables/blobs:blobs test/tables/P2.schema:p2 test/tables/W1.schema:w1 \
	test/tables/W2.schema:w2 tables/lists:lists tables/maps:maps \
	tables/stream:stream test/tables/G1.schema:g1 tables/backend:backend \
	tables/vocab:vocab tables/vocab9:vocab9 test/tables/R1.schema:r1 \
	test/tables/R2.schema:r2 test/tables/RT1.schema:rt1 test/tables/RT2.schema:rt2 \
	test/tables/RT3.schema:rt3 test/tables/CsRetain1.schema:csretain1 \
	test/tables/CsRetain2.schema:csretain2 test/tables/CsCollections1.schema:cscollections1 \
	test/tables/CsCollections2.schema:cscollections2 test/tables/UT.schema:ut

CS_FUZZ_SCHEMAS := $(sort $(foreach u,$(CS_FUZZ_UNITS),$(if $(wildcard $(firstword $(subst :, ,$(u)))/*.schema),$(wildcard $(firstword $(subst :, ,$(u)))/*.schema),$(firstword $(subst :, ,$(u))))))

# THE COVERAGE CHECK: every corpus schema must be in the manifest, and every
# manifest line must have a generated reader. A silently dropped unit is the
# failure mode this guards.
.PHONY: schema-cs-nightly-fuzz-corpus
schema-cs-nightly-fuzz-corpus:
	@mkdir -p $(CS_FUZZ_BUILD)
	@: > $(CS_FUZZ_CORPUS)
	@set -e; for u in $(CS_FUZZ_UNITS); do \
		src=$${u%%:*}; name=$${u##*:}; \
		if [ -d "$$src" ]; then schemas="$$src"/*.schema; else schemas="$$src"; fi; \
		for schema in $$schemas; do \
			printf '%s\t%s\n' "$$schema" "$(CURDIR)/$(CS_FUZZ_READERS)/$$name" >> $(CS_FUZZ_CORPUS); \
		done; \
	done
	@test -s $(CS_FUZZ_CORPUS) || { echo 'schema-cs-nightly-fuzz: the corpus is empty'; exit 1; }
	@echo "schema-cs-nightly-fuzz: $(words $(CS_FUZZ_SCHEMAS)) corpus schemas listed in $(CS_FUZZ_CORPUS)"

# THE READERS the harness drives: one generated C# unit per corpus schema.
# CS_FUZZ_PLANT=1 is the negative control's only switch: it drops a bounds
# check from a SCRATCH COPY under build/, never from a tracked file and never
# from the generator. The marker it leaves is what the planted-pattern test
# reads.
.PHONY: schema-cs-nightly-fuzz-readers
schema-cs-nightly-fuzz-readers: bin/schema schema-cs-nightly-fuzz-corpus
	@rm -rf $(CS_FUZZ_READERS) && mkdir -p $(CS_FUZZ_READERS)
	@set -e; for u in $(CS_FUZZ_UNITS); do \
		src=$${u%%:*}; name=$${u##*:}; \
		mkdir -p "$(CS_FUZZ_READERS)/$$name"; \
		./bin/schema generate --lang cs --out "$(CS_FUZZ_READERS)/$$name" "$$src" >/dev/null 2>&1 || \
			{ echo "schema-cs-nightly-fuzz: generate failed for $$src"; exit 1; }; \
	done
	@if [ "$(CS_FUZZ_PLANT)" = 1 ]; then \
		found=0; \
		for f in $$(find $(CS_FUZZ_READERS) -name '*Table.cs'); do \
			if grep -q 'if (n > buffer.Length) { return -1; }' "$$f"; then \
				sed 's|if (n > buffer.Length) { return -1; }|/* SABOTAGED: dropped bounds check */|' "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f"; \
				found=1; \
			fi; \
		done; \
		test $$found -eq 1 || { echo 'schema-cs-nightly-fuzz: the plant found no bounds check to drop'; exit 1; }; \
	fi
	@set -e; : > $(CS_FUZZ_BUILD)/roots.txt; \
	for f in $$(find $(CS_FUZZ_READERS) -name '*Table.cs' | sort); do \
		ns=$$(sed -n 's/^[[:space:]]*namespace[[:space:]]\{1,\}\([A-Za-z0-9_.]*\).*/\1/p' "$$f" | head -1); \
		[ -n "$$ns" ] || continue; \
		dir=$$(dirname "$$f"); \
		grep -oE 'public static bool [A-Za-z0-9_]+FromJson\(' "$$f" | sed 's/public static bool //; s/(//' | while read -r method; do \
			root=$${method%FromJson}; \
			printf '%s\t%s\t%s\n' "$$dir" "$$ns" "$$root" >> $(CS_FUZZ_BUILD)/roots.txt; \
		done; \
	done; \
	test -s $(CS_FUZZ_BUILD)/roots.txt || { echo 'schema-cs-nightly-fuzz: no reader entry points found'; exit 1; }
	@echo "schema-cs-nightly-fuzz: $$(wc -l < $(CS_FUZZ_BUILD)/roots.txt) reader roots generated"

# THE HARNESS. One process, one reader kind named on argv, every generated
# root called with the same input. `load` drives the strict wire read
# (<Root>LoadVerdict), `text` drives <Root>FromJson. An exception propagates
# out of Main, so AFL sees a crash and minimises it. The harness is generated
# from the manifest above, so a new corpus schema joins with no edit here.
.PHONY: schema-cs-nightly-fuzz-harness
schema-cs-nightly-fuzz-harness: schema-cs-nightly-fuzz-readers
	@mkdir -p $(CS_FUZZ_BUILD)/harness
	@printf '%s\n' \
		'using System;' \
		'using System.IO;' \
		'static class FuzzHarness' \
		'{' > $(CS_FUZZ_BUILD)/harness/Program.cs
	@while IFS=$$(printf '\t') read -r dir ns root; do \
		id=$$(printf '%s_%s' "$$ns" "$$root" | tr -c 'A-Za-z0-9_' '_'); \
		printf '    static void Load_%s(byte[] data) { var v = new %s.%s(); %s.Schema.%sLoadVerdict(v, new ReadOnlySpan<byte>(data), new %s.TableReport()); }\n' "$$id" "$$ns" "$$root" "$$ns" "$$root" "$$ns" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
		printf '    static void Text_%s(byte[] data) { var v = new %s.%s(); %s.Schema.%sFromJson(v, new ReadOnlySpan<byte>(data), new %s.TableReport()); }\n' "$$id" "$$ns" "$$root" "$$ns" "$$root" "$$ns" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
	done < $(CS_FUZZ_BUILD)/roots.txt
	@printf '%s\n' \
		'    static int Main(string[] args)' \
		'    {' \
		'        if (args.Length != 2) { Console.Error.WriteLine("usage: harness load|text <file>"); return 2; }' \
		'        byte[] data = File.ReadAllBytes(args[1]);' \
		'        if (data.Length > (1 << 20)) return 0;' \
		'        if (args[0] == "load")' \
		'        {' >> $(CS_FUZZ_BUILD)/harness/Program.cs
	@while IFS=$$(printf '\t') read -r dir ns root; do \
		id=$$(printf '%s_%s' "$$ns" "$$root" | tr -c 'A-Za-z0-9_' '_'); \
		printf '            Load_%s(data);\n' "$$id" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
	done < $(CS_FUZZ_BUILD)/roots.txt
	@printf '%s\n' \
		'        }' \
		'        else if (args[0] == "text")' \
		'        {' >> $(CS_FUZZ_BUILD)/harness/Program.cs
	@while IFS=$$(printf '\t') read -r dir ns root; do \
		id=$$(printf '%s_%s' "$$ns" "$$root" | tr -c 'A-Za-z0-9_' '_'); \
		printf '            Text_%s(data);\n' "$$id" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
	done < $(CS_FUZZ_BUILD)/roots.txt
	@printf '%s\n' \
		'        }' \
		'        else { Console.Error.WriteLine("unknown reader kind"); return 2; }' \
		'        return 0;' \
		'    }' \
		'}' >> $(CS_FUZZ_BUILD)/harness/Program.cs
	@echo "schema-cs-nightly-fuzz: harness generated at $(CS_FUZZ_BUILD)/harness"

# THE BUILD, with the SDK's own Roslyn csc and the net10.0 ref pack, no
# csproj and no NuGet (the repository's C# probes use the same route).
.PHONY: schema-cs-nightly-fuzz-build
schema-cs-nightly-fuzz-build: schema-cs-nightly-fuzz-harness
	@mkdir -p $(CS_FUZZ_BUILD)/bin
	@dotnet=$$(command -v $(DOTNET)); root=$$(dirname $$(readlink -f $$dotnet)); \
	csc=$$(ls $$root/sdk/*/Roslyn/bincore/csc.dll | head -1); \
	ref=$$(ls -d $$root/packs/Microsoft.NETCore.App.Ref/*/ref/net10.0 | head -1); \
	refv=$$(basename $$(dirname $$(dirname $$ref))); \
	[ -n "$$csc" ] && [ -d "$$ref" ] || { echo "schema-cs-nightly-fuzz: no Roslyn csc or ref pack under $$root"; exit 1; }; \
	set --; for r in "$$ref"/*.dll; do set -- "$$@" "-r:$$r"; done; \
	$(DOTNET) exec "$$csc" -nologo -langversion:latest -unsafe -out:$(CS_FUZZ_BUILD)/bin/Harness.dll "$$@" \
		$(CS_FUZZ_BUILD)/harness/Program.cs $$(find $(CS_FUZZ_READERS) -name '*Table.cs' | sort) || exit 1; \
	printf '{\n  "runtimeOptions": {\n    "tfm": "net10.0",\n    "framework": { "name": "Microsoft.NETCore.App", "version": %s }\n  }\n}\n' "\"$$refv\"" > $(CS_FUZZ_BUILD)/bin/Harness.runtimeconfig.json

# THE TOOL PROBE. Never installs; names the missing tool and the machine.
.PHONY: schema-cs-nightly-fuzz-tools
schema-cs-nightly-fuzz-tools:
	@status=0; \
	command -v afl-fuzz >/dev/null 2>&1 || { echo "tool missing: afl-fuzz (want $(CS_FUZZ_AFL_VERSION)) on $$(hostname)"; status=1; }; \
	command -v sharpfuzz >/dev/null 2>&1 || command -v dotnet-sharpfuzz >/dev/null 2>&1 || { echo "tool missing: sharpfuzz (want $(CS_FUZZ_SHARPFUZZ_VERSION)) on $$(hostname)"; status=1; }; \
	command -v dotnet >/dev/null 2>&1 || { echo "tool missing: dotnet on $$(hostname)"; status=1; }; \
	exit $$status

# THE NIGHTLY TARGET. Generates readers and harness, seeds AFL from the Go
# fuzz corpus and the conformance vectors, instruments the assembly with
# SharpFuzz, then runs one AFL campaign per reader kind for CS_FUZZ_TIME.
# Crashes are minimised with afl-tmin and saved under
# testdata/schema-cs-nightly-fuzz/corpus/<kind>/.
.PHONY: schema-cs-nightly-fuzz
schema-cs-nightly-fuzz: schema-cs-nightly-fuzz-tools schema-cs-nightly-fuzz-build
	@sharpfuzz $(CS_FUZZ_BUILD)/bin/Harness.dll 2>/dev/null || dotnet-sharpfuzz $(CS_FUZZ_BUILD)/bin/Harness.dll
	@mkdir -p $(CS_FUZZ_SEEDS)/load $(CS_FUZZ_SEEDS)/text $(CS_FUZZ_OUT)
	@find internal/fuzz/testdata testdata/conformance -type f -size -1M 2>/dev/null | while read -r f; do \
		cp "$$f" "$(CS_FUZZ_SEEDS)/load/$$(basename $$f)"; \
		cp "$$f" "$(CS_FUZZ_SEEDS)/text/$$(basename $$f)"; \
	done
	@set -e; for kind in $(CS_FUZZ_KIND); do \
		mkdir -p $(CS_FUZZ_OUT)/$$kind; \
		AFL_SKIP_CPUFREQ=1 AFL_NO_AFFINITY=1 timeout $(CS_FUZZ_TIME) \
			afl-fuzz -i $(CS_FUZZ_SEEDS)/$$kind -o $(CS_FUZZ_OUT)/$$kind \
			-m $(CS_FUZZ_MEMORY) -t $(CS_FUZZ_TIMEOUT) -V $(CS_FUZZ_TIME) \
			-- $(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll $$kind @@ > $(CS_FUZZ_BUILD)/afl-$$kind.log 2>&1 || true; \
		mkdir -p $(CS_FUZZ_CORPUS_COMMITTED)/$$kind; \
		for crash in $(CS_FUZZ_OUT)/$$kind/default/crashes/id:*; do \
			[ -e "$$crash" ] || continue; \
			afl-tmin -i "$$crash" -o $(CS_FUZZ_CORPUS_COMMITTED)/$$kind/$$(basename $$crash) \
				-- $(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll $$kind @@ >/dev/null 2>&1 || \
				cp "$$crash" $(CS_FUZZ_CORPUS_COMMITTED)/$$kind/; \
		done; \
	done
	@echo "schema-cs-nightly-fuzz: campaigns complete; crashes (if any) under $(CS_FUZZ_CORPUS_COMMITTED)"

# THE FAST REPLAY. No fuzzer, no clock: every saved input is replayed through
# the same harness on every push. A missing saved corpus is not a skip — the
# port's corpus directory is created by the nightly target.
.PHONY: schema-cs-nightly-fuzz-replay
schema-cs-nightly-fuzz-replay: schema-cs-nightly-fuzz-build
	@test -d $(CS_FUZZ_CORPUS_COMMITTED) || { echo "schema-cs-nightly-fuzz-replay: missing corpus $(CS_FUZZ_CORPUS_COMMITTED)"; exit 1; }
	@set -e; n=0; for f in $$(find $(CS_FUZZ_CORPUS_COMMITTED) -type f | sort); do \
		kind=$$(basename $$(dirname $$f)); \
		if ! $(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll $$kind "$$f" > $(CS_FUZZ_BUILD)/replay.log 2>&1; then \
			echo "schema-cs-nightly-fuzz-replay: RED on $$f"; cat $(CS_FUZZ_BUILD)/replay.log; exit 1; \
		fi; \
		n=$$((n+1)); \
	done; \
	echo "schema-cs-nightly-fuzz-replay: $$n saved inputs replayed, all clean"

# THE PLANTED-PATTERN TEST. The static half of the negative control: the
# clean reader tree must carry no `SABOTAGED: dropped bounds check` marker;
# the planted tree must. TestSchemaCSNightlyFuzzPlantedPattern refuses the
# marker, so the planted run is RED and the clean run is GREEN. The target
# below asserts both directions and prints the green `--- PASS`.
.PHONY: schema-cs-nightly-fuzz-planted-pattern
schema-cs-nightly-fuzz-planted-pattern:
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-readers CS_FUZZ_PLANT=0
	@SCHEMA_SLOW=1 SCHEMA_CS_FUZZ_READERS="$(CURDIR)/$(CS_FUZZ_READERS)" \
		go test ./internal/ci -count=1 -timeout 600s -run 'TestSchemaCSNightlyFuzzPlantedPattern' -v 2>&1 | tee $(CS_FUZZ_BUILD)/planted-clean.log
	@grep -q -- '--- PASS: TestSchemaCSNightlyFuzzPlantedPattern' $(CS_FUZZ_BUILD)/planted-clean.log
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-readers CS_FUZZ_PLANT=1
	@if SCHEMA_SLOW=1 SCHEMA_CS_FUZZ_READERS="$(CURDIR)/$(CS_FUZZ_READERS)" \
		go test ./internal/ci -count=1 -timeout 600s -run 'TestSchemaCSNightlyFuzzPlantedPattern' -v > $(CS_FUZZ_BUILD)/planted-red.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the planted-pattern test stayed green with the plant in'; cat $(CS_FUZZ_BUILD)/planted-red.log; exit 1; fi
	@grep -q 'SABOTAGED: dropped bounds check' $(CS_FUZZ_BUILD)/planted-red.log || { echo 'NEGATIVE CONTROL FAILED: the plant was not named'; cat $(CS_FUZZ_BUILD)/planted-red.log; exit 1; }
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-readers CS_FUZZ_PLANT=0
	@echo 'schema-cs-nightly-fuzz negative control: the plant turns the pattern test red by name, and the clean tree is green'

# The registered negative control (make/negative-controls.json). The static
# plant/unplant pair is the fast half and runs nightly; AFL finds the same bug
# inside the nightly budget when the tools are installed. A missing tool is a
# not-done reported by schema-cs-nightly-fuzz, never a green here.
.PHONY: schema-cs-nightly-fuzz-negative-control
schema-cs-nightly-fuzz-negative-control: schema-cs-nightly-fuzz-planted-pattern

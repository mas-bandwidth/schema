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
CS_FUZZ_PLANT_TIME ?= 2m
# SharpFuzz.CommandLine ships SharpFuzz.dll and its Common runtime beside the
# tool assembly; the harness needs them at run time (the instrumented assembly
# references SharpFuzz.Common). A host install leaves them in the dotnet-tool
# store; an explicit path wins so a bench can point at its fleet copy.
# The fleet play (rowan-tools fleet/fuzz.yml) puts the tool under ~/sdk/sharpfuzz-<v> beside the ~/sdk/bin/sharpfuzz wrapper on
# PATH, so the pinned version's store is found from the command itself, whatever HOME a job runs with.
CS_FUZZ_SHARPFUZZ_BIN := $(shell command -v sharpfuzz 2>/dev/null)
CS_FUZZ_SHARPFUZZ_LIB ?= $(firstword $(wildcard $(dir $(CS_FUZZ_SHARPFUZZ_BIN))../sharpfuzz-$(CS_FUZZ_SHARPFUZZ_VERSION)/.store/sharpfuzz.commandline/$(CS_FUZZ_SHARPFUZZ_VERSION)/sharpfuzz.commandline/$(CS_FUZZ_SHARPFUZZ_VERSION)/tools/net*/any) $(wildcard $(HOME)/.dotnet/tools/.store/sharpfuzz.commandline/$(CS_FUZZ_SHARPFUZZ_VERSION)/sharpfuzz.commandline/$(CS_FUZZ_SHARPFUZZ_VERSION)/tools/net*/any))

# afl-fuzz -V takes whole SECONDS (atoi: "-V 10m" is ten seconds, which is what attempt 3's campaign ran), so a budget
# written 10m, 2m, 1h or 90s is converted here; the outer timeout leaves AFL's start-up (fork server, the dry run and
# calibration of every seed: ~30 s on amd-2) a further 10 minutes before it kills a wedged run. Converted by make, so
# `make -n` prints the -V AFL gets (TestSchemaCSNightlyFuzzBudgetSeconds reads it).
cs_fuzz_seconds = $(strip $(if $(filter %h,$(1)),$(shell expr $(1:%h=%) \* 3600),$(if $(filter %m,$(1)),$(shell expr $(1:%m=%) \* 60),$(1:%s=%))))
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
			if grep -q 'public bool Has(int n) { return n >= 0 && n <= Buffer.Length - Offset; }' "$$f"; then \
				sed 's|public bool Has(int n) { return n >= 0 && n <= Buffer.Length - Offset; }|public bool Has(int n) { /* SABOTAGED: dropped bounds check */ return n >= 0; }|' "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f"; \
				found=1; \
			fi; \
		done; \
		test $$found -eq 1 || { echo 'schema-cs-nightly-fuzz: the plant found no reader bounds check to drop'; exit 1; }; \
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
# (<Root>LoadVerdict), `text` drives <Root>FromJson. In fuzz mode the harness
# hands stdin to SharpFuzz's AFL fork server; an exception escaping a reader
# is reported to AFL as a crash. In replay mode (`harness <kind> <file>`) the
# same dispatch runs on a saved input, so the fast test needs no fuzzer and no
# clock. The harness is generated from the manifest above, so a new corpus
# schema joins with no edit here.
.PHONY: schema-cs-nightly-fuzz-harness
schema-cs-nightly-fuzz-harness: schema-cs-nightly-fuzz-readers
	@mkdir -p $(CS_FUZZ_BUILD)/harness
	@printf '%s\n' \
		'using System;' \
		'using System.IO;' \
		'using System.Reflection;' \
		'static class FuzzHarness' \
		'{' > $(CS_FUZZ_BUILD)/harness/Program.cs
	@while IFS=$$(printf '\t') read -r dir ns root; do \
		id=$$(printf '%s_%s' "$$ns" "$$root" | tr -c 'A-Za-z0-9_' '_'); \
		printf '    static void Load_%s(byte[] data) { var v = new %s.%s(); %s.Schema.%sLoadVerdict(v, new ReadOnlySpan<byte>(data), new %s.TableReport()); }\n' "$$id" "$$ns" "$$root" "$$ns" "$$root" "$$ns" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
		printf '    static void Text_%s(byte[] data) { var v = new %s.%s(); %s.Schema.%sFromJson(v, new ReadOnlySpan<byte>(data), new %s.TableReport()); }\n' "$$id" "$$ns" "$$root" "$$ns" "$$root" "$$ns" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
	done < $(CS_FUZZ_BUILD)/roots.txt
	@printf '%s\n' \
		'    static void Dispatch(string kind, byte[] data)' \
		'    {' \
		'        if (kind == "load")' \
		'        {' >> $(CS_FUZZ_BUILD)/harness/Program.cs
	@while IFS=$$(printf '\t') read -r dir ns root; do \
		id=$$(printf '%s_%s' "$$ns" "$$root" | tr -c 'A-Za-z0-9_' '_'); \
		printf '            Load_%s(data);\n' "$$id" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
	done < $(CS_FUZZ_BUILD)/roots.txt
	@printf '%s\n' \
		'        }' \
		'        else if (kind == "text")' \
		'        {' >> $(CS_FUZZ_BUILD)/harness/Program.cs
	@while IFS=$$(printf '\t') read -r dir ns root; do \
		id=$$(printf '%s_%s' "$$ns" "$$root" | tr -c 'A-Za-z0-9_' '_'); \
		printf '            Text_%s(data);\n' "$$id" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
	done < $(CS_FUZZ_BUILD)/roots.txt
	@printf '%s\n' \
		'        }' \
		'        else { throw new ArgumentException("unknown reader kind: " + kind); }' \
		'    }' \
		'    static int Main(string[] args)' \
		'    {' \
		'        if (args.Length == 2) { byte[] saved = File.ReadAllBytes(args[1]); if (saved.Length <= (1 << 20)) Dispatch(args[0], saved); return 0; }' \
		'        if (args.Length != 1) { Console.Error.WriteLine("usage: harness load|text [file]"); return 2; }' \
		'        string kind = args[0];' \
		'        var asm = Assembly.LoadFrom(Path.Combine(AppContext.BaseDirectory, "SharpFuzz.dll"));' \
		'        var type = asm.GetType("SharpFuzz.Fuzzer+OutOfProcess", true);' \
		'        var run = type.GetMethod("Run", new Type[] { typeof(Action<Stream>) });' \
		'        run.Invoke(null, new object[] { new Action<Stream>(stream => { using (var memory = new MemoryStream()) { stream.CopyTo(memory); Dispatch(kind, memory.ToArray()); } }) });' \
		'        return 0;' \
		'    }' \
		'}' >> $(CS_FUZZ_BUILD)/harness/Program.cs
	@echo "schema-cs-nightly-fuzz: harness generated at $(CS_FUZZ_BUILD)/harness"

# THE BUILD, with the SDK's own Roslyn csc and the net10.0 ref pack, no
# csproj and no NuGet (the repository's C# probes use the same route).
#
# The generated unit carries fixed-form serializers next to the table reader;
# those compile against the serialize.cs runtime, which lives in the sibling
# checkout when one is present (the route make/cs.mk and the conformance leg
# use). On a bench without that sibling this target compiles a signature-only
# Serialize shim instead: it exists only so the generated unit compiles, and
# the load/text paths the harness drives never call it.
.PHONY: schema-cs-nightly-fuzz-build
schema-cs-nightly-fuzz-build: schema-cs-nightly-fuzz-harness
	@mkdir -p $(CS_FUZZ_BUILD)/bin
	@if [ -n "$(CS_FUZZ_SHARPFUZZ_LIB)" ] && [ -f "$(CS_FUZZ_SHARPFUZZ_LIB)/SharpFuzz.dll" ]; then \
		cp "$(CS_FUZZ_SHARPFUZZ_LIB)/SharpFuzz.dll" "$(CS_FUZZ_SHARPFUZZ_LIB)/SharpFuzz.Common.dll" $(CS_FUZZ_BUILD)/bin/; \
	fi
	@dotnet=$$(command -v $(DOTNET)); root=$$(dirname $$(readlink -f $$dotnet)); \
	csc=$$(ls $$root/sdk/*/Roslyn/bincore/csc.dll | head -1); \
	ref=$$(ls -d $$root/packs/Microsoft.NETCore.App.Ref/*/ref/net10.0 | head -1); \
	refv=$$(basename $$(dirname $$(dirname $$ref))); \
	[ -n "$$csc" ] && [ -d "$$ref" ] || { echo "schema-cs-nightly-fuzz: no Roslyn csc or ref pack under $$root"; exit 1; }; \
	set --; for r in "$$ref"/*.dll; do set -- "$$@" "-r:$$r"; done; \
	if [ -f ../serialize.cs/src/Serialize.cs ]; then \
		runtime="$(CURDIR)/../serialize.cs/src/Serialize.cs $(CURDIR)/../serialize.cs/src/Int128Pair.cs"; \
	else \
		printf '%s\n' \
			'using System;' \
			'namespace Serialize' \
			'{' \
			'    public class RuntimeBatch' \
			'    {' \
			'        public bool Ok = true;' \
			'        public void End() { }' \
			'        public bool SerializeBits(ref uint value, int bits) { return true; }' \
			'        public bool SerializeBits(ref ulong value, int bits) { return true; }' \
			'        public bool SerializeBits64(ref ulong value, int bits) { return true; }' \
			'        public bool SerializeInt(ref int value, int min, int max) { return true; }' \
			'        public bool SerializeInt(ref uint value, int min, int max) { return true; }' \
			'        public bool SerializeInt(ref long value, int min, int max) { return true; }' \
			'        public bool SerializeInt(ref ulong value, int min, int max) { return true; }' \
			'        public bool SerializeFloat(ref float value) { return true; }' \
			'        public bool SerializeDouble(ref double value) { return true; }' \
			'        public bool SerializeFixed(ref int value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeFixed(ref uint value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeFixed(ref long value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeFixed(ref ulong value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeBytes(ReadOnlySpan<byte> value) { return true; }' \
			'    }' \
			'    public sealed class ReadBatch : RuntimeBatch { }' \
			'    public sealed class WriteBatch : RuntimeBatch { }' \
			'    public class RuntimeStream' \
			'    {' \
			'        public bool Ok = true;' \
			'        public bool Error = false;' \
			'        public bool SerializeBits(ref uint value, int bits) { return true; }' \
			'        public bool SerializeBits(ref ulong value, int bits) { return true; }' \
			'        public bool SerializeBits64(ref ulong value, int bits) { return true; }' \
			'        public bool SerializeInt(ref int value, int min, int max) { return true; }' \
			'        public bool SerializeInt(ref uint value, int min, int max) { return true; }' \
			'        public bool SerializeInt(ref long value, int min, int max) { return true; }' \
			'        public bool SerializeInt(ref ulong value, int min, int max) { return true; }' \
			'        public bool SerializeFloat(ref float value) { return true; }' \
			'        public bool SerializeDouble(ref double value) { return true; }' \
			'        public bool SerializeFixed(ref int value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeFixed(ref uint value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeFixed(ref long value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeFixed(ref ulong value, int bits, int frac, long min, long max) { return true; }' \
			'        public bool SerializeBytes(ReadOnlySpan<byte> value) { return true; }' \
			'    }' \
			'    public sealed class ReadStream : RuntimeStream { public ReadBatch BeginBatch() { return new ReadBatch(); } }' \
			'    public sealed class WriteStream : RuntimeStream { public WriteBatch BeginBatch() { return new WriteBatch(); } }' \
			'}' \
			> $(CS_FUZZ_BUILD)/harness/Runtime.cs; \
		runtime=$(CS_FUZZ_BUILD)/harness/Runtime.cs; \
	fi; \
	$(DOTNET) exec "$$csc" -nologo -langversion:latest -unsafe -target:library -out:$(CS_FUZZ_BUILD)/bin/Readers.dll "$$@" \
		$$runtime $$(find $(CS_FUZZ_READERS) -name '*.cs' | sort) || exit 1; \
	$(DOTNET) exec "$$csc" -nologo -langversion:latest -unsafe -out:$(CS_FUZZ_BUILD)/bin/Harness.dll "$$@" \
		-r:$(CS_FUZZ_BUILD)/bin/Readers.dll $(CS_FUZZ_BUILD)/harness/Program.cs || exit 1; \
	printf '{\n  "runtimeOptions": {\n    "tfm": "net10.0",\n    "framework": { "name": "Microsoft.NETCore.App", "version": %s }\n  }\n}\n' "\"$$refv\"" > $(CS_FUZZ_BUILD)/bin/Harness.runtimeconfig.json

# THE TOOL PROBE. Never installs; names the missing tool and the machine.
.PHONY: schema-cs-nightly-fuzz-tools
schema-cs-nightly-fuzz-tools:
	@status=0; \
	if command -v afl-fuzz >/dev/null 2>&1; then \
		{ afl-fuzz -h 2>&1 || true; } | grep -q 'afl-fuzz++$(CS_FUZZ_AFL_VERSION)' || { echo "tool wrong version: afl-fuzz is not $(CS_FUZZ_AFL_VERSION) on $$(hostname)"; status=1; }; \
	else echo "tool missing: afl-fuzz (want $(CS_FUZZ_AFL_VERSION)) on $$(hostname)"; status=1; fi; \
	command -v sharpfuzz >/dev/null 2>&1 || command -v dotnet-sharpfuzz >/dev/null 2>&1 || { echo "tool missing: sharpfuzz (want $(CS_FUZZ_SHARPFUZZ_VERSION)) on $$(hostname)"; status=1; }; \
	[ -f "$(CS_FUZZ_SHARPFUZZ_LIB)/SharpFuzz.Common.dll" ] || { echo "tool missing: SharpFuzz $(CS_FUZZ_SHARPFUZZ_VERSION) runtime (SharpFuzz.dll, SharpFuzz.Common.dll) on $$(hostname)"; status=1; }; \
	command -v dotnet >/dev/null 2>&1 || { echo "tool missing: dotnet on $$(hostname)"; status=1; }; \
	exit $$status

# THE NIGHTLY TARGET. Generates readers and harness, seeds AFL from the Go
# fuzz corpus and the conformance vectors, instruments the assembly with
# SharpFuzz, then runs one AFL campaign per reader kind for CS_FUZZ_TIME
# (schema-cs-nightly-fuzz-campaign below). Crashes are minimised with afl-tmin
# and saved under testdata/schema-cs-nightly-fuzz/corpus/<kind>/.
.PHONY: schema-cs-nightly-fuzz
schema-cs-nightly-fuzz: schema-cs-nightly-fuzz-tools schema-cs-nightly-fuzz-build
	@sharpfuzz $(CS_FUZZ_BUILD)/bin/Readers.dll 2>/dev/null || dotnet-sharpfuzz $(CS_FUZZ_BUILD)/bin/Readers.dll
	@mkdir -p $(CS_FUZZ_SEEDS)/load $(CS_FUZZ_SEEDS)/text $(CS_FUZZ_OUT)
	@find internal/fuzz/testdata testdata/conformance -type f -size -1048576c 2>/dev/null | while read -r f; do \
		cp "$$f" "$(CS_FUZZ_SEEDS)/load/$$(basename $$f)"; \
		cp "$$f" "$(CS_FUZZ_SEEDS)/text/$$(basename $$f)"; \
	done
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-campaign

# THE CAMPAIGN, one AFL run per reader kind, and its verdict told by EXIT CODE.
# afl-fuzz 4.21c exits 0 when -V runs out, whether or not it saved crashes, and
# 1 on any FATAL (fork server failure, no usable seed, bad instrumentation);
# timeout(1) gives 124 for a wedged run (measured on amd-1, 2026-10-19). So a
# crash is read off AFL's crashes directory, never off its exit code, and the
# exit code is kept for the tool:
#   exit 0  CLEAN         AFL exited 0, its fuzzer_stats show run_time >= the
#                         budget and execs_done > 0, and it saved no crash
#   exit 1  FINDING       as CLEAN, but AFL saved crashes: each is minimised
#                         and saved under CS_FUZZ_SAVE for the replay test
#   exit 3  TOOL FAILURE  AFL exited non-zero or timed out, left no
#                         fuzzer_stats receipt, stopped short of the budget, or
#                         ran nothing: no campaign happened, so never a green
# make names the recipe's code in its `Error <n>` line (make itself exits 2 for
# any failed recipe), and each kind's verdict is written, one line, to
# $(CS_FUZZ_OUT)/status-<kind>. Every kind runs; a tool failure outranks a
# finding. CS_FUZZ_AFL, CS_FUZZ_TMIN and CS_FUZZ_TARGET are variables so
# TestSchemaCSNightlyFuzzCampaignVerdict drives this verdict with a stand-in
# fuzzer (no dotnet, no clock).
CS_FUZZ_AFL ?= afl-fuzz
CS_FUZZ_TMIN ?= afl-tmin
CS_FUZZ_TARGET ?= $(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll
CS_FUZZ_SAVE ?= $(CS_FUZZ_CORPUS_COMMITTED)
.PHONY: schema-cs-nightly-fuzz-campaign
schema-cs-nightly-fuzz-campaign:
	@secs=$(call cs_fuzz_seconds,$(CS_FUZZ_TIME)); \
	case "$$secs" in ''|*[!0-9]*) echo "schema-cs-nightly-fuzz: CS_FUZZ_TIME=$(CS_FUZZ_TIME) is not a whole number of s, m or h"; exit 2;; esac; \
	worst=0; \
	for kind in $(CS_FUZZ_KIND); do \
		out=$(CS_FUZZ_OUT)/$$kind; log=$(CS_FUZZ_OUT)/afl-$$kind.log; \
		mkdir -p $$out; rm -f $(CS_FUZZ_OUT)/status-$$kind; \
		rc=0; AFL_SKIP_CPUFREQ=1 AFL_NO_AFFINITY=1 AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES=1 AFL_SKIP_BIN_CHECK=1 AFL_NO_UI=1 timeout $$((secs + 600)) \
			$(CS_FUZZ_AFL) -i $(CS_FUZZ_SEEDS)/$$kind -o $$out \
			-m $(CS_FUZZ_MEMORY) -t $(CS_FUZZ_TIMEOUT) -V $$secs \
			-- $(CS_FUZZ_TARGET) $$kind > $$log 2>&1 || rc=$$?; \
		stats=$$out/default/fuzzer_stats; \
		ran=$$(sed -n 's/^run_time *: *\([0-9][0-9]*\).*/\1/p' $$stats 2>/dev/null | head -1); \
		execs=$$(sed -n 's/^execs_done *: *\([0-9][0-9]*\).*/\1/p' $$stats 2>/dev/null | head -1); \
		why=""; \
		if [ $$rc -eq 124 ]; then why="afl-fuzz wedged: killed by timeout after $$((secs + 600)) s"; \
		elif [ $$rc -ne 0 ]; then why="afl-fuzz exited $$rc"; \
		elif [ -z "$$ran" ] || [ -z "$$execs" ]; then why="afl-fuzz left no fuzzer_stats receipt at $$stats"; \
		elif [ "$$execs" -eq 0 ]; then why="afl-fuzz ran no executions"; \
		elif [ "$$ran" -lt "$$secs" ]; then why="afl-fuzz stopped after $$ran s of a $$secs s budget"; fi; \
		if [ -n "$$why" ]; then \
			echo "tool-failure rc=$$rc" > $(CS_FUZZ_OUT)/status-$$kind; \
			echo "schema-cs-nightly-fuzz: TOOL FAILURE ($$kind): $$why; no campaign ran. Last lines of $$log:"; \
			tail -n 20 $$log 2>/dev/null | sed 's/^/  /'; \
			worst=3; continue; \
		fi; \
		crashes=0; mkdir -p $(CS_FUZZ_SAVE)/$$kind; \
		for crash in $$out/default/crashes/id:*; do \
			[ -e "$$crash" ] || continue; \
			crashes=$$((crashes + 1)); \
			$(CS_FUZZ_TMIN) -i "$$crash" -o $(CS_FUZZ_SAVE)/$$kind/$$(basename $$crash) \
				-- $(CS_FUZZ_TARGET) $$kind >/dev/null 2>&1 || \
				cp "$$crash" $(CS_FUZZ_SAVE)/$$kind/; \
		done; \
		if [ $$crashes -gt 0 ]; then \
			echo "finding crashes=$$crashes" > $(CS_FUZZ_OUT)/status-$$kind; \
			echo "schema-cs-nightly-fuzz: FINDING ($$kind): $$crashes crash(es) in $$ran s / $$execs execs, saved under $(CS_FUZZ_SAVE)/$$kind"; \
			[ $$worst -eq 3 ] || worst=1; \
		else \
			echo "clean" > $(CS_FUZZ_OUT)/status-$$kind; \
			echo "schema-cs-nightly-fuzz: CLEAN ($$kind): $$ran s, $$execs execs, no crash"; \
		fi; \
	done; \
	exit $$worst

# THE DYNAMIC NEGATIVE CONTROL. Build a SCRATCH PLANTED copy of the readers
# (CS_FUZZ_PLANT=1 drops the reader's central Has() bounds check), instrument
# it, and let AFL find the crash inside CS_FUZZ_PLANT_TIME. The saved input is
# then replayed against the planted build (must fail) and against a clean
# rebuild (must be a typed refusal, exit 0). This is the proof the nightly
# campaign's oracle can see a dropped reader check, not just the static marker.
.PHONY: schema-cs-nightly-fuzz-planted-campaign
schema-cs-nightly-fuzz-planted-campaign: schema-cs-nightly-fuzz-tools
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-build CS_FUZZ_PLANT=1
	@mkdir -p $(CS_FUZZ_SEEDS)/load $(CS_FUZZ_SEEDS)/text $(CS_FUZZ_BUILD)/afl-planted
	@find internal/fuzz/testdata testdata/conformance -type f -size -1048576c 2>/dev/null | while read -r f; do \
		cp "$$f" "$(CS_FUZZ_SEEDS)/load/$$(basename $$f)"; \
		cp "$$f" "$(CS_FUZZ_SEEDS)/text/$$(basename $$f)"; \
	done
	@sharpfuzz $(CS_FUZZ_BUILD)/bin/Readers.dll 2>/dev/null || dotnet-sharpfuzz $(CS_FUZZ_BUILD)/bin/Readers.dll
	@rm -rf $(CS_FUZZ_BUILD)/afl-planted $(CS_FUZZ_BUILD)/planted-crash*; \
	found=0; \
	for kind in $(CS_FUZZ_KIND); do \
		mkdir -p $(CS_FUZZ_BUILD)/afl-planted/$$kind; \
		secs=$(call cs_fuzz_seconds,$(CS_FUZZ_PLANT_TIME)); \
		case "$$secs" in ''|*[!0-9]*) echo "schema-cs-nightly-fuzz: CS_FUZZ_PLANT_TIME=$(CS_FUZZ_PLANT_TIME) is not a whole number of s, m or h"; exit 2;; esac; \
		rc=0; AFL_SKIP_CPUFREQ=1 AFL_NO_AFFINITY=1 AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES=1 AFL_SKIP_BIN_CHECK=1 AFL_NO_UI=1 timeout $$((secs + 600)) \
			afl-fuzz -i $(CS_FUZZ_SEEDS)/$$kind -o $(CS_FUZZ_BUILD)/afl-planted/$$kind \
			-m $(CS_FUZZ_MEMORY) -t $(CS_FUZZ_TIMEOUT) -V $$secs \
			-- $(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll $$kind > $(CS_FUZZ_BUILD)/afl-planted-$$kind.log 2>&1 || rc=$$?; \
		if [ $$rc -ne 0 ]; then echo "NEGATIVE CONTROL FAILED: TOOL FAILURE ($$kind): afl-fuzz exited $$rc (124: killed by timeout), so no campaign ran; last lines of $(CS_FUZZ_BUILD)/afl-planted-$$kind.log:"; tail -n 20 $(CS_FUZZ_BUILD)/afl-planted-$$kind.log | sed 's/^/  /'; exit 3; fi; \
		stats=$(CS_FUZZ_BUILD)/afl-planted/$$kind/default/fuzzer_stats; \
		ran=$$(sed -n 's/^run_time *: *\([0-9][0-9]*\).*/\1/p' $$stats 2>/dev/null | head -1); \
		execs=$$(sed -n 's/^execs_done *: *\([0-9][0-9]*\).*/\1/p' $$stats 2>/dev/null | head -1); \
		if [ -z "$$ran" ] || [ -z "$$execs" ]; then echo "NEGATIVE CONTROL FAILED: TOOL FAILURE ($$kind): afl-fuzz left no fuzzer_stats receipt at $$stats"; exit 3; fi; \
		if [ "$$execs" -eq 0 ]; then echo "NEGATIVE CONTROL FAILED: TOOL FAILURE ($$kind): afl-fuzz ran no executions"; exit 3; fi; \
		crash=$$(ls $(CS_FUZZ_BUILD)/afl-planted/$$kind/default/crashes/id:* 2>/dev/null | head -1); \
		if [ -n "$$crash" ]; then cp "$$crash" $(CS_FUZZ_BUILD)/planted-crash-$$kind; echo $$kind > $(CS_FUZZ_BUILD)/planted-crash-kind; found=1; break; fi; \
	done; \
	test $$found -eq 1 || { echo 'NEGATIVE CONTROL FAILED: AFL found no crash from the planted reader inside $(CS_FUZZ_PLANT_TIME)'; exit 1; }
	@# The replay runs on an UNINSTRUMENTED planted rebuild: a SharpFuzz-instrumented Readers.dll throws
	@# NullReferenceException in every reader constructor outside AFL's fork server (no shared coverage map),
	@# so replaying against it would "crash" on any input and prove nothing. The empty seed must replay clean
	@# on the same planted build, so the crash is the saved input's, not the build's.
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-build CS_FUZZ_PLANT=1
	@kind=$$(cat $(CS_FUZZ_BUILD)/planted-crash-kind); \
	$(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll $$kind $(CS_FUZZ_CORPUS_COMMITTED)/$$kind/seed-empty >/dev/null 2>&1 || { echo 'NEGATIVE CONTROL FAILED: the planted build crashes on the empty seed too, so its crash proves nothing'; exit 1; }; \
	crashed=0; $(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll $$kind $(CS_FUZZ_BUILD)/planted-crash-$$kind >/dev/null 2>&1 || crashed=1; \
	test $$crashed -eq 1 || { echo 'NEGATIVE CONTROL FAILED: the planted build accepted the crash input'; exit 1; }
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-build CS_FUZZ_PLANT=0
	@kind=$$(cat $(CS_FUZZ_BUILD)/planted-crash-kind); \
	$(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll $$kind $(CS_FUZZ_BUILD)/planted-crash-$$kind >/dev/null 2>&1 || { echo "NEGATIVE CONTROL FAILED: the clean reader refused (non-zero) the saved input"; exit 1; }
	@echo "schema-cs-nightly-fuzz dynamic control: AFL found the planted reader bug inside $(CS_FUZZ_PLANT_TIME); the planted build crashes on the saved input and the clean build refuses it cleanly"

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
# below asserts both directions and prints the green `--- PASS`. It runs under
# SCHEMA_REQUIRE_CORPUS=1 (the repo's promise that the oracle was built), so a
# missing readers tree is a hard failure here, never a skip.
.PHONY: schema-cs-nightly-fuzz-planted-pattern
schema-cs-nightly-fuzz-planted-pattern:
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-readers CS_FUZZ_PLANT=0
	@SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 SCHEMA_CS_FUZZ_READERS="$(CURDIR)/$(CS_FUZZ_READERS)" \
		go test ./internal/ci -count=1 -timeout 600s -run 'TestSchemaCSNightlyFuzzPlantedPattern' -v 2>&1 | tee $(CS_FUZZ_BUILD)/planted-clean.log
	@grep -q -- '--- PASS: TestSchemaCSNightlyFuzzPlantedPattern' $(CS_FUZZ_BUILD)/planted-clean.log
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-readers CS_FUZZ_PLANT=1
	@if SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 SCHEMA_CS_FUZZ_READERS="$(CURDIR)/$(CS_FUZZ_READERS)" \
		go test ./internal/ci -count=1 -timeout 600s -run 'TestSchemaCSNightlyFuzzPlantedPattern' -v > $(CS_FUZZ_BUILD)/planted-red.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the planted-pattern test stayed green with the plant in'; cat $(CS_FUZZ_BUILD)/planted-red.log; exit 1; fi
	@grep -q 'SABOTAGED: dropped bounds check' $(CS_FUZZ_BUILD)/planted-red.log || { echo 'NEGATIVE CONTROL FAILED: the plant was not named'; cat $(CS_FUZZ_BUILD)/planted-red.log; exit 1; }
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-readers CS_FUZZ_PLANT=0
	@echo 'schema-cs-nightly-fuzz negative control: the plant turns the pattern test red by name, and the clean tree is green'

# The registered negative control (make/negative-controls.json, the nightly
# group certify.yml runs with dotnet only): the static planted-bad-pattern proof
# above. The dynamic half needs AFL++ and SharpFuzz, which that matrix does not
# install, so schema-cs-nightly-fuzz-dynamic-negative-control (both halves:
# SharpFuzz instruments a scratch planted build, AFL finds the dropped bounds
# check inside the nightly budget, the saved input replays red on the planted
# build and clean after a rebuild) is in the manifest's exclusions and runs in
# .github/workflows/schema-cs-nightly-fuzz.yml on the fleet runners the fleet
# play equips. A missing tool there is named by schema-cs-nightly-fuzz-tools,
# never a green.
.PHONY: schema-cs-nightly-fuzz-negative-control schema-cs-nightly-fuzz-dynamic-negative-control
schema-cs-nightly-fuzz-negative-control: schema-cs-nightly-fuzz-planted-pattern
schema-cs-nightly-fuzz-dynamic-negative-control: schema-cs-nightly-fuzz-planted-pattern schema-cs-nightly-fuzz-planted-campaign

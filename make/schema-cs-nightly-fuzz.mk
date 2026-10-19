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
CS_FUZZ_PLANTED ?= 1
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
# CS_FUZZ_PLANT is the negative control's only switch, and it edits a SCRATCH
# COPY under build/, never a tracked file and never the generator:
#   CS_FUZZ_PLANT=1     drops the reader's central Has() bounds check
#   CS_FUZZ_PLANT=list  drops the wire reader's list clamp (keep = count, not
#                       min(count, bound)), so a list longer than its declared
#                       bound writes past the storage. The input has to carry
#                       a list field, so it tests the seeds' list coverage;
#                       CS_FUZZ_SEED_SET=legacy reruns it on the seeds from
#                       before the seeder to compare.
# The marker either leaves is what the planted-pattern test reads.
.PHONY: schema-cs-nightly-fuzz-readers
schema-cs-nightly-fuzz-readers: bin/schema schema-cs-nightly-fuzz-corpus
	@rm -rf $(CS_FUZZ_READERS) && mkdir -p $(CS_FUZZ_READERS)
	@set -e; for u in $(CS_FUZZ_UNITS); do \
		src=$${u%%:*}; name=$${u##*:}; \
		mkdir -p "$(CS_FUZZ_READERS)/$$name"; \
		./bin/schema generate --lang cs --out "$(CS_FUZZ_READERS)/$$name" "$$src" >/dev/null 2>&1 || \
			{ echo "schema-cs-nightly-fuzz: generate failed for $$src"; exit 1; }; \
	done
	@if [ "$(CS_FUZZ_PLANT)" = list ]; then \
		found=0; \
		for f in $$(find $(CS_FUZZ_READERS) -name '*Table.cs'); do \
			if grep -q 'int keep = (int)Math.Min(count, (ulong)f.ArrayBound);' "$$f"; then \
				sed 's|int keep = (int)Math.Min(count, (ulong)f.ArrayBound);|int keep = (int)count; /* SABOTAGED: dropped bounds check (list) */|' "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f"; \
				found=1; \
			fi; \
		done; \
		test $$found -eq 1 || { echo 'schema-cs-nightly-fuzz: the list plant found no reader list clamp to drop'; exit 1; }; \
	elif [ "$(CS_FUZZ_PLANT)" = 1 ]; then \
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

# THE SEEDER: every corpus root's own populated instance, written by the
# generated WRITER, is a seed. The Go fuzz corpus and the conformance vectors
# reach only the roots and shapes they happen to name, and the sibling elixir
# fuzz's seeds never filled a pointer field, so its planted reader bugs there
# survived. The harness's `seed <dir>` mode fills each root by reflection
# (every scalar 1, every enum and union tag its first variant, every nested or
# pointer table built, every list and map given two entries, every string two
# bytes), saves it with <Root>Save and <Root>ToJson, reloads both, and writes
# the reloaded value's census to <dir>/coverage.txt: how many table-reference
# (pointer or nested), list and map fields the saved seed really carries, of
# how many it tried. A root whose full fill is refused falls back to its
# structure (scalars left default), then to empty, and the line says which.
# schema-cs-nightly-fuzz-seed-coverage refuses a seed set that misses a root
# or drops a shape the root has.
define CS_FUZZ_SEEDER_CS
using System;
using System.IO;
using System.Reflection;

static class Seeder
{
    const int MaxDepth = 4;
    public sealed class Census { public int Pointer, List, Map; }

    static bool IsTable(Type t) { return t.IsClass && t != typeof(string) && !t.IsArray && t.GetConstructor(Type.EmptyTypes) != null; }
    static bool IsMapEntry(Type t) { return IsTable(t) && t.GetField("Key") != null; }
    static FieldInfo[] Fields(Type t) { return t.GetFields(BindingFlags.Public | BindingFlags.Instance); }
    static FieldInfo CountOf(Type owner, string name) { return owner.GetField(name + "Count") ?? owner.GetField(name + "Length"); }
    static bool IsCompanion(Type owner, FieldInfo f)
    {
        string n = f.Name;
        if (f.FieldType == typeof(int) && n.EndsWith("Count") && owner.GetField(n.Substring(0, n.Length - 5)) != null) return true;
        if (f.FieldType == typeof(int) && n.EndsWith("Length") && owner.GetField(n.Substring(0, n.Length - 6)) != null) return true;
        if (f.FieldType == typeof(bool) && n.EndsWith("Present") && owner.GetField(n.Substring(0, n.Length - 7)) != null) return true;
        return false;
    }

    static object Scalar(Type t)
    {
        if (t.IsEnum) return Enum.ToObject(t, 1);
        if (t == typeof(bool)) return true;
        if (t == typeof(float)) return 1.5f;
        if (t == typeof(double)) return 1.5;
        if (t == typeof(char)) return 'a';
        if (t == typeof(string)) return "ab";
        if (t == typeof(UInt128)) return (UInt128)1;
        if (t == typeof(Int128)) return (Int128)1;
        if (t.IsPrimitive && t != typeof(IntPtr) && t != typeof(UIntPtr)) return Convert.ChangeType(1, t);
        return null;
    }

    public static void Fill(object v, int depth, bool scalars)
    {
        Type type = v.GetType();
        foreach (FieldInfo f in Fields(type))
        {
            if (f.IsInitOnly || IsCompanion(type, f)) continue;
            Type ft = f.FieldType;
            if (ft.IsArray)
            {
                if (!FillArray(v, f, depth, scalars)) continue;
            }
            else if (IsTable(ft))
            {
                if (depth >= MaxDepth) continue;
                object child = f.GetValue(v) ?? Activator.CreateInstance(ft);
                Fill(child, depth + 1, scalars);
                f.SetValue(v, child);
            }
            else if (scalars || ft.IsEnum)
            {
                object value = Scalar(ft);
                if (value == null) continue;
                f.SetValue(v, value);
            }
            else continue;
            FieldInfo present = type.GetField(f.Name + "Present");
            if (present != null && present.FieldType == typeof(bool)) present.SetValue(v, true);
        }
    }

    static bool FillArray(object v, FieldInfo f, int depth, bool scalars)
    {
        Type et = f.FieldType.GetElementType();
        FieldInfo count = CountOf(v.GetType(), f.Name);
        Array a = (Array)f.GetValue(v);
        if (a == null || a.Length == 0)
        {
            if (count == null) return false;
            a = Array.CreateInstance(et, 2);
            f.SetValue(v, a);
        }
        int n = Math.Min(2, a.Length);
        for (int i = 0; i < n; i++)
        {
            if (et == typeof(byte)) a.SetValue((byte)('a' + i), i);
            else if (et == typeof(char)) a.SetValue((char)('a' + i), i);
            else if (IsTable(et))
            {
                if (depth >= MaxDepth) return false;
                object element = a.GetValue(i) ?? Activator.CreateInstance(et);
                Fill(element, depth + 1, scalars);
                FieldInfo key = et.GetField("Key");
                if (key != null) DistinctKey(element, key, i);
                a.SetValue(element, i);
            }
            else
            {
                object value = scalars || et.IsEnum ? Scalar(et) : null;
                if (value != null) a.SetValue(value, i);
            }
        }
        if (count != null) count.SetValue(v, n);
        return true;
    }

    // A map's entries must carry DISTINCT keys or the writer refuses the map, and
    // the fill gives every entry the same values: key i is set apart here.
    static void DistinctKey(object entry, FieldInfo key, int i)
    {
        Type kt = key.FieldType;
        if (kt == typeof(byte[])) { byte[] b = (byte[])key.GetValue(entry); if (b != null && b.Length > 0) b[0] = (byte)('k' + i); }
        else if (kt == typeof(char[])) { char[] c = (char[])key.GetValue(entry); if (c != null && c.Length > 0) c[0] = (char)('k' + i); }
        else if (kt == typeof(string)) key.SetValue(entry, "k" + i);
        else if (kt == typeof(bool)) key.SetValue(entry, i != 0);
        else if (kt.IsEnum) key.SetValue(entry, Enum.ToObject(kt, i + 1));
        else if (kt == typeof(UInt128)) key.SetValue(entry, (UInt128)(i + 1));
        else if (kt == typeof(Int128)) key.SetValue(entry, (Int128)(i + 1));
        else if (kt.IsPrimitive) key.SetValue(entry, Convert.ChangeType(i + 1, kt));
    }

    static bool IsDefault(object o)
    {
        if (o == null) return true;
        Type t = o.GetType();
        return t.IsValueType && o.Equals(Activator.CreateInstance(t));
    }

    static bool NonEmpty(object v, int depth)
    {
        if (v == null || depth > MaxDepth + 1) return false;
        Type type = v.GetType();
        foreach (FieldInfo f in Fields(type))
        {
            object value = f.GetValue(v);
            if (f.FieldType.IsArray)
            {
                Array a = (Array)value;
                if (a == null) continue;
                FieldInfo count = CountOf(type, f.Name);
                if (count != null) { if ((int)count.GetValue(v) > 0) return true; continue; }
                for (int i = 0; i < a.Length; i++)
                {
                    object e = a.GetValue(i);
                    if (IsTable(f.FieldType.GetElementType()) ? NonEmpty(e, depth + 1) : !IsDefault(e)) return true;
                }
            }
            else if (IsTable(f.FieldType)) { if (NonEmpty(value, depth + 1)) return true; }
            else if (f.FieldType == typeof(string)) { if (!string.IsNullOrEmpty((string)value)) return true; }
            else if (!IsDefault(value)) return true;
        }
        return false;
    }

    public static void Count(object v, int depth, Census c)
    {
        if (v == null || depth > MaxDepth + 1) return;
        Type type = v.GetType();
        foreach (FieldInfo f in Fields(type))
        {
            Type ft = f.FieldType;
            if (ft.IsArray)
            {
                Type et = ft.GetElementType();
                if (et == typeof(byte) || et == typeof(char)) continue;
                Array a = (Array)f.GetValue(v);
                if (a == null) continue;
                FieldInfo count = CountOf(type, f.Name);
                int n = count != null ? Math.Min((int)count.GetValue(v), a.Length) : a.Length;
                bool any = count != null && n > 0;
                for (int i = 0; i < n; i++)
                {
                    object e = a.GetValue(i);
                    if (IsTable(et)) { if (NonEmpty(e, depth + 1)) any = true; Count(e, depth + 1, c); }
                    else if (!IsDefault(e)) any = true;
                }
                if (any) { if (IsMapEntry(et)) c.Map++; else c.List++; }
            }
            else if (IsTable(ft))
            {
                object child = f.GetValue(v);
                if (NonEmpty(child, depth + 1)) { c.Pointer++; Count(child, depth + 1, c); }
            }
        }
    }

    static byte[] Bytes(object v, Func<object, long> measure, Func<object, byte[], long> save)
    {
        long n = measure(v);
        if (n <= 0 || n > (1 << 20)) return null;
        byte[] b = new byte[n];
        return save(v, b) == n ? b : null;
    }

    static readonly string[] Levels = { "full", "structure", "empty" };

    // One form (wire or text): the richest fill that saves AND reloads clean.
    static string Form(Func<int, object> make, Func<object, long> measure, Func<object, byte[], long> save,
        Func<byte[], Tuple<object, bool>> load, string path)
    {
        // tried is the FULL fill's census whichever level saved, so a root that
        // falls back to a poorer fill shows the shapes it lost and the gate refuses it.
        string why = "";
        Census tried = new Census();
        try { Count(make(1), 0, tried); } catch (Exception e) { why += "census:" + e.GetType().Name + " "; }
        for (int k = 0; k < Levels.Length; k++)
        {
            try
            {
                object v = make(1 - k);
                byte[] bytes = Bytes(v, measure, save);
                if (bytes == null) { why += Levels[k] + ":save-refused "; continue; }
                Tuple<object, bool> back = load(bytes);
                if (!back.Item2) { why += Levels[k] + ":reload-refused "; continue; }
                Census kept = new Census(); Count(back.Item1, 0, kept);
                File.WriteAllBytes(path, bytes);
                return Levels[k] + " pointer=" + kept.Pointer + "/" + tried.Pointer + " list=" + kept.List + "/" + tried.List + " map=" + kept.Map + "/" + tried.Map + (why == "" ? "" : " fallback=" + why.Trim().Replace(' ', ','));
            }
            catch (Exception e) { why += Levels[k] + ":" + e.GetType().Name + ":" + e.Message.Replace(' ', '_').Replace('\t', '_') + " "; }
        }
        return "none " + why.Trim().Replace(' ', ',');
    }

    public static void Root(string dir, string id, string name, Func<int, object> make,
        Func<object, long> measure, Func<object, byte[], long> save, Func<byte[], Tuple<object, bool>> load,
        Func<object, long> jmeasure, Func<object, byte[], long> jsave, Func<byte[], Tuple<object, bool>> jload)
    {
        string wire = Form(make, measure, save, load, Path.Combine(dir, "load", "seed-" + id + ".bin"));
        string text = Form(make, jmeasure, jsave, jload, Path.Combine(dir, "text", "seed-" + id + ".json"));
        File.AppendAllText(Path.Combine(dir, "coverage.txt"), name + "\twire " + wire + "\ttext " + text + "\n");
    }
}
endef

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
		printf '%s\n' '    static void Seed_@ID@(string dir) { Seeder.Root(dir, "@ID@", "@NS@.@ROOT@", fill => { var v = new @NS@.@ROOT@(); if (fill >= 0) Seeder.Fill(v, 0, fill > 0); return v; }, o => @NS@.Schema.@ROOT@Measure((@NS@.@ROOT@)o), (o, b) => @NS@.Schema.@ROOT@Save((@NS@.@ROOT@)o, b), b => { var back = new @NS@.@ROOT@(); var r = new @NS@.TableReport(); bool ok = @NS@.Schema.@ROOT@LoadVerdict(back, new ReadOnlySpan<byte>(b), r) == @NS@.Schema.TableWire.Verdict.Ok && !r.Refused && !r.Malformed; return Tuple.Create((object)back, ok); }, o => @NS@.Schema.@ROOT@ToJsonMeasure((@NS@.@ROOT@)o), (o, b) => @NS@.Schema.@ROOT@ToJson((@NS@.@ROOT@)o, b), b => { var back = new @NS@.@ROOT@(); var r = new @NS@.TableReport(); bool ok = @NS@.Schema.@ROOT@FromJson(back, new ReadOnlySpan<byte>(b), r) && !r.Refused && !r.Malformed; return Tuple.Create((object)back, ok); }); }' | \
			sed "s/@ID@/$$id/g; s/@NS@/$$ns/g; s/@ROOT@/$$root/g" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
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
		'    static void Seed(string dir)' \
		'    {' >> $(CS_FUZZ_BUILD)/harness/Program.cs
	@while IFS=$$(printf '\t') read -r dir ns root; do \
		id=$$(printf '%s_%s' "$$ns" "$$root" | tr -c 'A-Za-z0-9_' '_'); \
		printf '        Seed_%s(dir);\n' "$$id" >> $(CS_FUZZ_BUILD)/harness/Program.cs; \
	done < $(CS_FUZZ_BUILD)/roots.txt
	@printf '%s\n' \
		'    }' \
		'    static int Main(string[] args)' \
		'    {' \
		'        if (args.Length == 2 && args[0] == "seed") { Seed(args[1]); return 0; }' \
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
# checkout when one is present (make/cs.mk's SERIALIZE_CS, the route the
# conformance leg uses). On a bench without that sibling this target compiles
# a signature-only Serialize shim instead: it exists only so the generated
# unit compiles, and the load/text paths the harness drives never call it.
#
# The runtime is TWO files, so it goes to csc as two arguments: quoted as one
# word, csc looked for a single file named by both paths and refused the build
# on every bench with the sibling (CS2001). schema-cs-nightly-fuzz-compile has
# no prerequisites, so TestSchemaCSNightlyFuzzCompileRuntime drives it with a
# stand-in dotnet that refuses a source path that does not exist, as csc does.
CS_FUZZ_RUNTIME_DIR ?= $(SERIALIZE_CS)/src
.PHONY: schema-cs-nightly-fuzz-build schema-cs-nightly-fuzz-compile
schema-cs-nightly-fuzz-build: schema-cs-nightly-fuzz-harness
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-compile

schema-cs-nightly-fuzz-compile:
	@mkdir -p $(CS_FUZZ_BUILD)/bin $(CS_FUZZ_BUILD)/harness
	$(shell mkdir -p $(CS_FUZZ_BUILD)/harness)$(file >$(CS_FUZZ_BUILD)/harness/Seeder.cs,$(CS_FUZZ_SEEDER_CS))
	@if [ -n "$(CS_FUZZ_SHARPFUZZ_LIB)" ] && [ -f "$(CS_FUZZ_SHARPFUZZ_LIB)/SharpFuzz.dll" ]; then \
		cp "$(CS_FUZZ_SHARPFUZZ_LIB)/SharpFuzz.dll" "$(CS_FUZZ_SHARPFUZZ_LIB)/SharpFuzz.Common.dll" $(CS_FUZZ_BUILD)/bin/; \
	fi
	@dotnet=$$(command -v $(DOTNET)); root=$$(dirname $$(readlink -f $$dotnet)); \
	csc=$$(ls $$root/sdk/*/Roslyn/bincore/csc.dll | head -1); \
	ref=$$(ls -d $$root/packs/Microsoft.NETCore.App.Ref/*/ref/net10.0 | head -1); \
	refv=$$(basename $$(dirname $$(dirname $$ref))); \
	[ -n "$$csc" ] && [ -d "$$ref" ] || { echo "schema-cs-nightly-fuzz: no Roslyn csc or ref pack under $$root"; exit 1; }; \
	set --; for r in "$$ref"/*.dll; do set -- "$$@" "-r:$$r"; done; \
	if [ -f $(CS_FUZZ_RUNTIME_DIR)/Serialize.cs ]; then \
		runtime="$(abspath $(CS_FUZZ_RUNTIME_DIR))/Serialize.cs $(abspath $(CS_FUZZ_RUNTIME_DIR))/Int128Pair.cs"; \
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
		-r:$(CS_FUZZ_BUILD)/bin/Readers.dll $(CS_FUZZ_BUILD)/harness/Program.cs $(CS_FUZZ_BUILD)/harness/Seeder.cs || exit 1; \
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

# THE SEEDS, built from the CLEAN, UNINSTRUMENTED build (the instrumented
# assembly cannot run outside AFL's fork server): every corpus root's own
# populated instance from the harness's seed mode (the seeder above), the wire
# goldens the conformance instances name (testdata/wire/tables), the Go fuzz
# corpus and the conformance vectors. The seeder's census must pass
# schema-cs-nightly-fuzz-seed-coverage before any campaign runs.
# CS_FUZZ_SEED_SET=legacy is the seed-coverage control only: the Go fuzz
# corpus and conformance vectors alone, the set before the seeder, which
# carries no populated list on the wire.
CS_FUZZ_SEED_SET ?= full
CS_FUZZ_COVERAGE = $(CS_FUZZ_SEEDS)/coverage.txt
.PHONY: schema-cs-nightly-fuzz-seeds
schema-cs-nightly-fuzz-seeds: schema-cs-nightly-fuzz-build
	@rm -rf $(CS_FUZZ_SEEDS) && mkdir -p $(CS_FUZZ_SEEDS)/load $(CS_FUZZ_SEEDS)/text
	@find internal/fuzz/testdata testdata/conformance -type f -size -1048576c 2>/dev/null | while read -r f; do \
		cp "$$f" "$(CS_FUZZ_SEEDS)/load/$$(basename $$f)"; \
		cp "$$f" "$(CS_FUZZ_SEEDS)/text/$$(basename $$f)"; \
	done
	@if [ "$(CS_FUZZ_SEED_SET)" = legacy ]; then echo "schema-cs-nightly-fuzz: LEGACY seed set (control only): no seeder, no wire goldens"; else \
		for f in testdata/wire/tables/*.bin; do cp "$$f" "$(CS_FUZZ_SEEDS)/load/wire-$$(basename $$f)"; done; \
		$(DOTNET) $(CS_FUZZ_BUILD)/bin/Harness.dll seed $(CS_FUZZ_SEEDS) || { echo 'schema-cs-nightly-fuzz: the seeder failed'; exit 1; }; \
		$(MAKE) --no-print-directory schema-cs-nightly-fuzz-seed-coverage || exit 1; \
	fi
	@echo "schema-cs-nightly-fuzz: seeds: $$(ls $(CS_FUZZ_SEEDS)/load | wc -l) load, $$(ls $(CS_FUZZ_SEEDS)/text | wc -l) text"

# THE SEED COVERAGE GATE. Every root in roots.txt has a coverage line, both of
# its forms saved and reloaded clean, and for each shape (table reference,
# list, map) the root's fill reached, the saved WIRE seed still carries at
# least one: a seed set that leaves a shape empty leaves the reader code behind
# it unfuzzed. It reads two files and needs no dotnet, so
# TestSchemaCSNightlyFuzzSeedCoverage drives it on stand-in files.
.PHONY: schema-cs-nightly-fuzz-seed-coverage
schema-cs-nightly-fuzz-seed-coverage:
	@test -s $(CS_FUZZ_COVERAGE) || { echo "schema-cs-nightly-fuzz: SEED COVERAGE: no census at $(CS_FUZZ_COVERAGE)"; exit 1; }
	@awk -F'\t' 'FNR == NR { want[$$2 "." $$3] = 1; next } \
		{ if (!($$1 in seen)) seeded++; seen[$$1] = 1; \
		  if (index($$0, "census:")) { print "SEED COVERAGE: " $$1 ": the full fill's census failed, so its shapes are unknown: " $$0; bad++; next } \
		  if ($$2 ~ /^wire none/ || $$3 ~ /^text none/) { print "SEED COVERAGE: " $$1 ": a form saved no seed: " $$2 " / " $$3; bad++; next } \
		  n = split($$2, w, " "); \
		  for (i = 3; i <= n; i++) { split(w[i], kv, "="); split(kv[2], ab, "/"); \
		    if (ab[2] + 0 > 0 && ab[1] + 0 == 0) { print "SEED COVERAGE: " $$1 ": the wire seed carries no " kv[1] " field of the " ab[2] " its fill set (" $$2 ")"; bad++ } } } \
		END { for (r in want) if (!(r in seen)) { print "SEED COVERAGE: " r ": no seed"; bad++ } \
		      if (bad) { print bad " seed coverage failure(s)"; exit 1 } \
		      print "schema-cs-nightly-fuzz: seed coverage: " seeded " roots seeded, every shape a root has is on the wire" }' \
		$(CS_FUZZ_BUILD)/roots.txt $(CS_FUZZ_COVERAGE)

# THE NIGHTLY TARGET. Generates readers and harness, seeds AFL (above),
# instruments the assembly with SharpFuzz, then runs one AFL campaign per
# reader kind for CS_FUZZ_TIME (schema-cs-nightly-fuzz-campaign below).
# Crashes are minimised with afl-tmin and saved under
# testdata/schema-cs-nightly-fuzz/corpus/<kind>/.
.PHONY: schema-cs-nightly-fuzz
schema-cs-nightly-fuzz: schema-cs-nightly-fuzz-tools schema-cs-nightly-fuzz-seeds
	@sharpfuzz $(CS_FUZZ_BUILD)/bin/Readers.dll 2>/dev/null || dotnet-sharpfuzz $(CS_FUZZ_BUILD)/bin/Readers.dll
	@mkdir -p $(CS_FUZZ_OUT)
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
# (CS_FUZZ_PLANTED names the plant: 1, the default, drops the reader's central
# Has() bounds check; list drops the wire reader's list clamp, which an input
# reaches through a list field), instrument it, and let AFL find the crash inside
# CS_FUZZ_PLANT_TIME, seeded by the same seed set the nightly uses. The saved input is
# then replayed against the planted build (must fail) and against a clean
# rebuild (must be a typed refusal, exit 0). This is the proof the nightly
# campaign's oracle can see a dropped reader check, not just the static marker.
.PHONY: schema-cs-nightly-fuzz-planted-campaign
schema-cs-nightly-fuzz-planted-campaign: schema-cs-nightly-fuzz-tools
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-seeds CS_FUZZ_PLANT=0
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-build CS_FUZZ_PLANT=$(CS_FUZZ_PLANTED)
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
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-build CS_FUZZ_PLANT=$(CS_FUZZ_PLANTED)
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
	@$(MAKE) --no-print-directory schema-cs-nightly-fuzz-planted-campaign CS_FUZZ_PLANTED=list CS_FUZZ_KIND=load

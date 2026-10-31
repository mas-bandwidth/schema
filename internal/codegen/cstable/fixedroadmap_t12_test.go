package cstable

// fixedroadmap_t12 — the cs leg's plan-selection, closure, retirement and
// retired-runtime rows of docs/roadmap.sexp's node `fixed-tables`: the leaves
// cs/R7, cs/R14, cs/R15, cs/R32 and cs/R29 that were :unknown at BASE, plus
// cs/E3/other-required-widens' write half. Law: docs/FIXED-FORM-ALGORITHM.md
// §5.2 (the floor and retirement), §5.3 (the identity lane), §4.6 (the bounds
// pass), §5.4 (the counters). A clause already held by an existing test is
// named by that test; this file carries the direct assertion of each clause
// that had none, one subtest per leaf id, red first, then its fix, then its
// flip.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
)

func TestFixedRoadmapCSharpT12PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/R32", t12R32},
		{"cs/R7", t12R7},
		{"cs/R14", t12R14},
		{"cs/R15", t12R15},
		{"cs/R29", t12R29},
		{"cs/E3/other-required-widens", t12E3},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t12Norm collapses whitespace so an assertion reads an emitted statement and
// not the indentation the string builder chose.
func t12Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t12Has is a required substring, named by the rule it holds.
func t12Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t12Retire builds THREE generations of one table — x, x+y, x+y+z — marks the
// first `retired` of the first two retired in the lineage, and answers the
// emitted source together with the floor it computes (§5.2).
func t12Retire(t *testing.T, retired int) string {
	t.Helper()
	older := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	middle := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n")
	own := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    z int32\n}\n")
	e1, ok := FixedLineageOf(older, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the first generation")
	}
	e2, ok := FixedLineageOf(middle, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the second generation")
	}
	if retired > 0 {
		e1.Retired = true
		e1.Reason = "the operator retired this layout"
	}
	if retired > 1 {
		e2.Retired = true
		e2.Reason = "the operator retired this layout"
	}
	files, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {e1, e2}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

// t12R32 holds cs/R32 to §5.2's retirement, read off the emitted load: the
// operator's RETIRED mark and reason are laid down, the floor check sits ONCE,
// after the hash select and BEFORE the byte comparison or the record loop, and
// its whole body is the pure named refusal layout_unsupported — a second load
// of the same file answers the same name (idempotent), and retiring the same
// entry twice yields the same floor.
func t12R32(t *testing.T) {
	text := t12Retire(t, 1)
	t12Has(t, text, "// RETIRED: the operator retired this layout", "R32 the retired entry's mark and reason are laid down")
	load := t12Load(t, text, "Lineage")
	// ONCE, after the select, before the byte comparison and the record loop.
	if n := strings.Count(load, "pick < LineageFixedFloor"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "pick < LineageFixedFloor")
	known := strings.Index(load, "SequenceEqual(known.Layout)")
	records := strings.Index(load, "for (int k = 0; k < n; ++k)")
	if at < 0 || known < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d)", at, known, records)
	}
	if at >= known || known >= records {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d)", at, known, records)
	}
	// THE PURE NAMED REFUSAL: the guarded body reads `pick` and returns
	// layout_unsupported; nothing before it mutates the report or destination.
	body := load[at:]
	if end := strings.Index(body, "return -1;"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "Unknown") || strings.Contains(body, "values[") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	t12Has(t, body, `Reason = "layout_unsupported"`, "R32 below the floor is layout_unsupported")
	// IDEMPOTENT: retiring the same entry twice yields the same floor, and a
	// second generation retires to a higher floor.
	again := t12Retire(t, 1)
	t12Has(t, again, "public const int LineageFixedFloor = 1;", "R32 retiring the same entry twice gives the same floor (idempotent)")
	both := t12Retire(t, 2)
	t12Has(t, both, "public const int LineageFixedFloor = 2;", "R32 the floor is 1 + the highest retired index")
}

// t12Load returns one emitted load function's body, from its `name(` to the
// function's closing brace.
func t12Load(t *testing.T, text, name string) string {
	t.Helper()
	sig := "public static long " + name + "FixedLoad("
	at := strings.Index(text, sig)
	if at < 0 {
		t.Fatalf("%sFixedLoad is not emitted", name)
	}
	rest := text[at:]
	// cut at the next top-level method: the [] overload follows.
	if end := strings.Index(rest, "public static long "+name+"FixedLoad("+name+"[]"); end > 0 {
		rest = rest[:end]
	}
	return rest
}

// t12R7 holds cs/R7 to §5.3: the identity lane is an INDEX COMPARISON — the
// resolved lineage index against the reader's own index (the last entry) —
// never a recomputed or re-compared hash, and TableFixedWire.HashOf is gone.
func t12R7(t *testing.T) {
	text := t12Retire(t, 0)
	n := t12Norm(text)
	// The reader's own entry is the LAST lineage entry (§5.2 appends it last),
	// so the lane is chosen by comparing the resolved index with len-1.
	t12Has(t, n, "if (pick != 2)", "R7 the identity lane compares the resolved lineage index with the reader's own index")
	if strings.Contains(n, "if (hash != LineageFixedHash)") {
		t.Error("R7: the lane is still chosen by comparing the header hash with the compiler-handed constant")
	}
	if strings.Contains(text, "HashOf(") {
		t.Error("R7: the emitted runtime still defines TableFixedWire.HashOf, a layout hasher nothing calls")
	}
}

// t12R14 holds cs/R14 to §5.2's PLAN: the runtime names layout_record_too_large
// for an entry whose source extent reaches past the writer's OWN declared
// record size, refusing the plan WHOLE and never landing a short read. The
// check is the reference's TableFixedSrcEnd (docs/FIXED-FORM-ALGORITHM.md §5.2,
// docs/FIXED-FORM-BILL-READS-BACKWARD.md §12.5) carried into the compiled plan:
// the record bound is the writer's root size, the over-reach is recorded on the
// compiler, the compile answers -3, and the lineage lane carries the name.
func t12R14(t *testing.T) {
	runtime := t12Norm(tableFixedWireSource)
	t12Has(t, runtime, "public static uint SrcEnd(in TableFixedEntry e)", "R14 the entry bound is carried")
	t12Has(t, runtime, "public bool TooLarge;", "R14 the over-reach is recorded")
	t12Has(t, runtime, "public uint Record;", "R14 the writer's declared record is carried")
	t12Has(t, runtime, "if (Record != 0 && SrcEnd(stamped) > Record) { TooLarge = true; return; }", "R14 an entry reaching past the writer's declared record")
	t12Has(t, runtime, "if (c.TooLarge) { return -3; }", "R14 the plan is refused WHOLE")
	t12Has(t, runtime, `lane.Why = "layout_record_too_large";`, "R14 the lane carries the name")
	t12Has(t, runtime, "Record = EntryAt(theirs, 0).Size", "R14 the record bound is the writer's root size")
	// The generated build carries the runtime, so the ban is on the emitted
	// build and not on this package's constant alone.
	text := t12Retire(t, 0)
	t12Has(t, text, "SrcEnd", "R14 the emitted build carries the entry bound")
}

// t12R15 holds cs/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are retired, each layout_newer now. A hash the lineage
// does not hold is refused BEFORE any record and before any plan selection, on
// the file's hash; the count and text clamps that REMAIN are the writer's
// bounds carried by the plan (a backward read over a locked entry), and the
// unknown-field count lands from the COMPILE census after a returning read,
// never per record.
func t12R15(t *testing.T) {
	text := t12Retire(t, 0)
	load := t12Load(t, text, "Lineage")
	miss := strings.Index(load, `Reason = "layout_newer"`)
	records := strings.Index(load, "for (int k = 0; k < n; ++k)")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	t12Has(t, load, "census_unknown = lane.Unknown;", "R15 unknown is the compile census")
	runtime := t12Norm(text)
	t12Has(t, runtime, "else if ((uint)v > p.Size)", "R15 the count clamp is the writer's bound")
	if n := strings.Count(runtime, "else if ((uint)v > p.Size)"); n < 2 {
		t.Errorf("R15 the text clamp is the writer's span: the count and the text op clamp to p.Size at %d site(s), want both", n)
	}
	// The §4.6 bounds pass (emitFixedClamp) is not a forward-read clamp and stays;
	// the clamps that REMAIN in the read are the writer's bounds above, never the
	// reader's own. The behavioural half — each of the four retired forward-read
	// clamps comes back layout_newer with the destination untouched — runs through
	// one dotnet build and run, under the make target that sets SCHEMA_REQUIRE_CORPUS.
	t.Run("forward-reads", func(t *testing.T) {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the four forward-read clamps run under make tables-cs-fixed-roadmap")
		}
		t12R15Forward(t)
	})
}

// t12ForwardClamp is one of the four forward-read clamps §5.6 retires: a count
// clamp across bounds, a range clamp across versions, a remap of an unknown
// variant to None, and a drop-and-count of an unknown field. Each is a NEWER
// writer whose hash is in no lineage entry of the reader, so each is refused
// layout_newer before any record — the same name the versioning harness's
// old_refuses_new column asserts, named here per clamp on this leg.
type t12ForwardClamp struct {
	id     string
	reader string // the reader's schema, the older generation
	writer string // the writer's schema, the newer generation
}

var t12ForwardClamps = []t12ForwardClamp{
	{id: "cs/R15/count-clamp-across-bounds",
		reader: "package r1\n\nfixed table T\n{\n    tail int32 = 7\n    a [..4]int32\n}\n",
		writer: "package w1\n\nfixed table T\n{\n    tail int32 = 7\n    a [..8]int32\n}\n"},
	{id: "cs/R15/range-clamp-across-versions",
		reader: "package r2\n\nfixed table T\n{\n    tail int32 = 7\n    n int32 = 0 | min = 0, max = 100\n}\n",
		writer: "package w2\n\nfixed table T\n{\n    tail int32 = 7\n    n int32 = 0 | min = 0, max = 200\n}\n"},
	{id: "cs/R15/remap-unknown-variant-to-none",
		reader: "package r3\n\nenum E { A, B }\n\nfixed table T\n{\n    tail int32 = 7\n    e E\n}\n",
		writer: "package w3\n\nenum E { A, B, C }\n\nfixed table T\n{\n    tail int32 = 7\n    e E\n}\n"},
	{id: "cs/R15/drop-and-count-unknown-field",
		reader: "package r4\n\nfixed table T\n{\n    tail int32 = 7\n    x int32\n}\n",
		writer: "package w4\n\nfixed table T\n{\n    tail int32 = 7\n    x int32\n    y int32\n}\n"},
}

// t12R15ForwardBody writes the newer writer's own record, then reads it with the
// older reader: layout_newer, the file's hash on the report, nothing marked
// malformed, no counter moved, and the destination still the sentinel — REFUSE
// is total.
const t12R15ForwardBody = `
            var w = new @W@.T();
            w.Tail = 5;
            byte[] buf = new byte[@W@.Schema.TFixedMeasure(1)];
            if (@W@.Schema.TFixedSave(w, buf) < 0) { bad += ProbeLog.Fail(@NAME@, "the newer writer did not write its own file"); }
            var back = new T();
            back.Tail = 99;
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.TFixedLoad(back, buf, plan, r);
            if (n != -1) { bad += ProbeLog.Fail(@NAME@, "the forward read was not refused: n=" + n); }
            if (r.Reason != "layout_newer") { bad += ProbeLog.Fail(@NAME@, "a hash in no lineage entry owes layout_newer, not " + r.Reason); }
            ulong want = BinaryPrimitives.ReadUInt64LittleEndian(buf.AsSpan(8));
            if (r.LayoutHash != want) { bad += ProbeLog.Fail(@NAME@, "layout_newer reports the file's hash and nothing else"); }
            if (r.Malformed) { bad += ProbeLog.Fail(@NAME@, "a refusal by name never sets malformed too"); }
            if (r.Clamped != 0 || r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0) { bad += ProbeLog.Fail(@NAME@, "REFUSE is total: no counter moves"); }
            if (back.Tail != 99) { bad += ProbeLog.Fail(@NAME@, "REFUSE wrote the destination: tail=" + back.Tail); }
`

// t12R15Forward builds the four clamp probes into one assembly and runs it with
// the SDK's Roslyn compiler and dotnet exec — the t10 harness, no serialize.cs
// and no corpus — then reads each clamp's verdict out of the one run.
func t12R15Forward(t *testing.T) {
	t.Helper()
	t10Discover(t)
	dir := t.TempDir()
	write := func(root string, files map[string][]byte) {
		for name, data := range files {
			p := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var calls []string
	for i, c := range t12ForwardClamps {
		ru := unitFrom(t, c.reader)
		wu := unitFrom(t, c.writer)
		rns := capitalize(ru.Package)
		wns := capitalize(wu.Package)
		rgen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i), "r")
		wgen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i), "w")
		pk, err := csharp.Generate(ru)
		if err != nil {
			t.Fatal(err)
		}
		tb, err := Generate(ru)
		if err != nil {
			t.Fatal(err)
		}
		write(rgen, pk)
		write(rgen, tb)
		wpk, err := csharp.Generate(wu)
		if err != nil {
			t.Fatal(err)
		}
		wtb, err := Generate(wu)
		if err != nil {
			t.Fatal(err)
		}
		write(wgen, wpk)
		write(wgen, wtb)
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.NewReplacer("@NAME@", fmt.Sprintf("%q", c.id), "@W@", wns).Replace(t12R15ForwardBody)
		src := fmt.Sprintf(`// generated by internal/codegen/cstable's t12 harness: the four retired forward-read clamps
using System;
using System.Buffers.Binary;

namespace %s
{
    static class %s
    {
        public static int Run()
        {
            int bad = 0;
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, rns, probeClass, body, fmt.Sprintf("%q", c.id))
		if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("            bad += %s.%s.Run();", rns, probeClass))
	}
	main := fmt.Sprintf(`using System;

static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
        try
        {
%s
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs t12 forward reads: every row green" : "cs t12 forward reads: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, strings.Join(calls, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	topFiles, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	genFiles, _ := filepath.Glob(filepath.Join(dir, "gen", "*", "*", "*.cs"))
	refs, _ := filepath.Glob(filepath.Join(t10refDir, "*.dll"))
	args := []string{t10csc, "-nologo", "-langversion:latest", "-unsafe", "-out:" + filepath.Join(dir, "app.dll")}
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	args = append(args, topFiles...)
	args = append(args, genFiles...)
	cmd := exec.Command(t10dotnet, append([]string{"exec"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		for _, c := range t12ForwardClamps {
			t10Assert(t, out, err, c.id)
		}
		t.Fatalf("the t12 forward-read assembly did not compile: %s", t10Tail(string(out), err))
	}

	rtc := fmt.Sprintf(`{
  "runtimeOptions": {
    "tfm": "net10.0",
    "framework": { "name": "Microsoft.NETCore.App", "version": %q }
  }
}
`, t10refVersion)
	if err := os.WriteFile(filepath.Join(dir, "app.runtimeconfig.json"), []byte(rtc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(t10dotnet, "exec", filepath.Join(dir, "app.dll"))
	run.Dir = dir
	run.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	runOut, runErr := run.CombinedOutput()
	for _, c := range t12ForwardClamps {
		t10Assert(t, runOut, runErr, c.id)
	}
}

// t12R29 holds cs/R29's second half: the bounds pass clamping to the WRITER's
// bounds. The writer carries its ranged leaves in the lineage entry; the
// compiled lane's bounds pass holds an endpoint to what IT declared and never to
// this reader's wider own (§4.6, bill §12.5, TableFixedClampKnownRanges).
func t12R29(t *testing.T) {
	writer := unitFrom(t, "package wband\n\nfixed table Band\n{\n    n int32 = 0 | min = 0, max = 100\n}\n")
	reader := unitFrom(t, "package rband\n\nfixed table Band\n{\n    n int32 = 0 | min = 0, max = 200\n}\n")
	we, ok := FixedLineageOf(writer, "Band")
	if !ok {
		t.Fatal("no lineage entry for the writer")
	}
	we.Ranges = []FixedRangeSpec{{Name: "n", Lo: 0, Hi: 100}}
	files, err := GenerateLineage(reader, map[string][]FixedLineageEntry{"Band": {we}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	text := t12Norm(b.String())
	// The reader's own pass still holds the reader's wider own; the writer's
	// carried pass holds the writer's narrower 0..100.
	t12Has(t, text, "else if (value.N > 200) { value.N = 200; clamped++; }", "R29 the reader's own pass holds the reader's 0..200")
	t12Has(t, text, "else if (value.N > 100) { value.N = 100; clamped++; }", "R29 the writer's carried pass holds the writer's 0..100")
	t12Has(t, text, "BandFixedClampKnown0(Band value, TableReport report)", "R29 the writer's bounds are one clamp per lineage entry")
	t12Has(t, text, "System.Action<Band, TableReport>[] BandFixedClampKnown", "R29 the writer's bounds dispatch by pick")
	t12Has(t, text, "writerClamp = BandFixedClampKnown[pick];", "R29 the load selects the writer's bounds by the resolved lineage index")
	t12Has(t, text, "writerClamp?.Invoke(values[k], report);", "R29 the compiled lane holds the writer's bounds after the run")

	t.Run("forged", func(t *testing.T) {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the writer-bound clamp runs under make tables-cs-fixed-roadmap")
		}
		t12R29Forged(t)
	})
}

// t12R29ForgedBody writes the older writer's own file, forges the bounded field
// to a value inside the reader's range but past the writer's bound, and reads
// it back: the writer's 100 and one clamp, never the reader's 200. The negative
// control is the reader's own wider range leaving the same value whole.
const t12R29ForgedBody = `
            var w = new @W@.Band();
            w.N = 50;
            byte[] wbuf = new byte[@W@.Schema.BandFixedMeasure(1)];
            if (@W@.Schema.BandFixedSave(w, wbuf) < 0) { bad += ProbeLog.Fail(@NAME@, "the writer did not write its own file"); }
            int wAt = 20 + (int)@W@.Schema.BandFixedLayoutBytes;
            BinaryPrimitives.WriteInt32LittleEndian(wbuf.AsSpan(wAt + 8), 150);
            var back = new Band();
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.BandFixedLoad(back, wbuf, plan, r);
            if (n != 1) { bad += ProbeLog.Fail(@NAME@, "the forged record did not read back: n=" + n + " " + r.Reason); }
            if (back.N != 100) { bad += ProbeLog.Fail(@NAME@, "a value inside the reader's range but past the writer's bound did not land the writer's 100: " + back.N); }
            if (r.Clamped != 1) { bad += ProbeLog.Fail(@NAME@, "the writer-bound clamp did not count once: " + r.Clamped); }
            if (r.Refused || r.Malformed) { bad += ProbeLog.Fail(@NAME@, "a clean forged read refused or malformed: " + r.Reason); }
            var own = new Band();
            own.N = 150;
            byte[] rbuf = new byte[Schema.BandFixedMeasure(1)];
            if (Schema.BandFixedSave(own, rbuf) < 0) { bad += ProbeLog.Fail(@NAME@, "the reader did not write its own file"); }
            var back2 = new Band();
            TableReport r2 = new TableReport();
            long n2 = Schema.BandFixedLoad(back2, rbuf, plan, r2);
            if (n2 != 1) { bad += ProbeLog.Fail(@NAME@, "the reader's own file did not read back: n=" + n2 + " " + r2.Reason); }
            if (back2.N != 150) { bad += ProbeLog.Fail(@NAME@, "the reader's own 0..200 did not leave 150 whole: " + back2.N); }
            if (r2.Clamped != 0) { bad += ProbeLog.Fail(@NAME@, "a value inside the reader's range clamped on the identity read: " + r2.Clamped); }
`

// t12R29Forged builds the writer and reader into one assembly and runs the
// forge-and-read probe through the SDK's Roslyn compiler and dotnet exec — the
// t10 harness, no corpus — then reads the one verdict out of the run.
func t12R29Forged(t *testing.T) {
	t.Helper()
	t10Discover(t)
	writer := unitFrom(t, "package wband\n\nfixed table Band\n{\n    n int32 = 0 | min = 0, max = 100\n}\n")
	reader := unitFrom(t, "package rband\n\nfixed table Band\n{\n    n int32 = 0 | min = 0, max = 200\n}\n")
	we, ok := FixedLineageOf(writer, "Band")
	if !ok {
		t.Fatal("no lineage entry for the writer")
	}
	we.Ranges = []FixedRangeSpec{{Name: "n", Lo: 0, Hi: 100}}
	dir := t.TempDir()
	write := func(root string, files map[string][]byte) {
		for name, data := range files {
			p := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	wpk, err := csharp.Generate(writer)
	if err != nil {
		t.Fatal(err)
	}
	wtb, err := Generate(writer)
	if err != nil {
		t.Fatal(err)
	}
	wgen := filepath.Join(dir, "gen", "w")
	write(wgen, wpk)
	write(wgen, wtb)
	rpk, err := csharp.Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	rtb, err := GenerateLineage(reader, map[string][]FixedLineageEntry{"Band": {we}})
	if err != nil {
		t.Fatal(err)
	}
	rgen := filepath.Join(dir, "gen", "r")
	write(rgen, rpk)
	write(rgen, rtb)
	rns := capitalize(reader.Package)
	const id = "cs/R29"
	body := strings.NewReplacer("@NAME@", fmt.Sprintf("%q", id), "@W@", capitalize(writer.Package)).Replace(t12R29ForgedBody)
	src := fmt.Sprintf(`// generated by internal/codegen/cstable's t12 harness: the writer-bound clamp
using System;
using System.Buffers.Binary;

namespace %s
{
    static class ProbeR29
    {
        public static int Run()
        {
            int bad = 0;
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, rns, body, fmt.Sprintf("%q", id))
	if err := os.WriteFile(filepath.Join(dir, "ProbeR29.cs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	main := `using System;

static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
        try
        {
            bad += ` + rns + `.ProbeR29.Run();
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs t12 r29: green" : "cs t12 r29: FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	topFiles, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	genFiles, _ := filepath.Glob(filepath.Join(dir, "gen", "*", "*.cs"))
	refs, _ := filepath.Glob(filepath.Join(t10refDir, "*.dll"))
	args := []string{t10csc, "-nologo", "-langversion:latest", "-unsafe", "-out:" + filepath.Join(dir, "app.dll")}
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	args = append(args, topFiles...)
	args = append(args, genFiles...)
	cmd := exec.Command(t10dotnet, append([]string{"exec"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the t12 r29 assembly did not compile: %s", t10Tail(string(out), err))
	}

	rtc := fmt.Sprintf(`{
  "runtimeOptions": {
    "tfm": "net10.0",
    "framework": { "name": "Microsoft.NETCore.App", "version": %q }
  }
}
`, t10refVersion)
	if err := os.WriteFile(filepath.Join(dir, "app.runtimeconfig.json"), []byte(rtc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(t10dotnet, "exec", filepath.Join(dir, "app.dll"))
	run.Dir = dir
	run.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	runOut, runErr := run.CombinedOutput()
	t10Assert(t, runOut, runErr, id)
}

// t12WidenCase is one widen-ladder fixture E3's write half writes and reads:
// the older (narrower) writer and the newer (wider) reader, both under one
// table name in two packages, so the two layouts are ONE LINEAGE.
type t12WidenCase struct {
	id     string
	writer string // the older, narrower generation
	reader string // the newer, wider generation
	body   string // C# asserting the boundary values land widened, over `back`
}

var t12WidenCases = []t12WidenCase{
	{id: "cs/E3/int_widen",
		writer: "package w1\n\nfixed table T\n{\n    v int8\n}\n",
		reader: "package r1\n\nfixed table T\n{\n    v int16\n}\n",
		body: `
            var w = new @W@.T[2];
            w[0] = new @W@.T(); w[0].V = -128;
            w[1] = new @W@.T(); w[1].V = 127;
            byte[] buf = new byte[@W@.Schema.TFixedMeasure(2)];
            if (@W@.Schema.TFixedSave(w, buf) < 0) { bad += ProbeLog.Fail(@NAME@, "int_widen writer did not write"); }
            var back = new T[2];
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.TFixedLoad(back, buf, plan, r);
            if (n != 2) { bad += ProbeLog.Fail(@NAME@, "int_widen did not read 2: n=" + n + " " + r.Reason); }
            if (back[0].V != -128 || back[1].V != 127) { bad += ProbeLog.Fail(@NAME@, "int8 did not sign-extend to int16: " + back[0].V + " " + back[1].V); }
            if (r.Widened != 2) { bad += ProbeLog.Fail(@NAME@, "int_widen widened=" + r.Widened + ", want 2"); }
`},
	{id: "cs/E3/uint_widen",
		writer: "package w2\n\nfixed table T\n{\n    v uint16\n}\n",
		reader: "package r2\n\nfixed table T\n{\n    v uint32\n}\n",
		body: `
            var w = new @W@.T();
            w.V = 65535;
            byte[] buf = new byte[@W@.Schema.TFixedMeasure(1)];
            if (@W@.Schema.TFixedSave(w, buf) < 0) { bad += ProbeLog.Fail(@NAME@, "uint_widen writer did not write"); }
            var back = new T();
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.TFixedLoad(back, buf, plan, r);
            if (n != 1) { bad += ProbeLog.Fail(@NAME@, "uint_widen did not read: n=" + n + " " + r.Reason); }
            if (back.V != 65535) { bad += ProbeLog.Fail(@NAME@, "uint16 65535 did not zero-extend across the band: " + back.V); }
            if (r.Widened != 1) { bad += ProbeLog.Fail(@NAME@, "uint_widen widened=" + r.Widened + ", want 1"); }
`},
	{id: "cs/E3/float_widen",
		writer: "package w3\n\nfixed table T\n{\n    v float32\n}\n",
		reader: "package r3\n\nfixed table T\n{\n    v float64\n}\n",
		body: `
            var w = new @W@.T();
            w.V = 3.5f;
            byte[] buf = new byte[@W@.Schema.TFixedMeasure(1)];
            if (@W@.Schema.TFixedSave(w, buf) < 0) { bad += ProbeLog.Fail(@NAME@, "float_widen writer did not write"); }
            var back = new T();
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.TFixedLoad(back, buf, plan, r);
            if (n != 1) { bad += ProbeLog.Fail(@NAME@, "float_widen did not read: n=" + n + " " + r.Reason); }
            if (back.V != 3.5) { bad += ProbeLog.Fail(@NAME@, "f32 3.5 did not widen to f64 3.5: " + back.V); }
            if (r.Widened != 1) { bad += ProbeLog.Fail(@NAME@, "float_widen widened=" + r.Widened + ", want 1"); }
`},
	{id: "cs/E3/fixed_I_grow",
		writer: "package w4\n\nfixed table T\n{\n    v fixed(4, 4) | min = -8, max = 7\n}\n",
		reader: "package r4\n\nfixed table T\n{\n    v fixed(12, 4) | min = -8, max = 7\n}\n",
		body: `
            var w = new @W@.T[2];
            w[0] = new @W@.T(); w[0].V = -128;
            w[1] = new @W@.T(); w[1].V = 112;
            byte[] buf = new byte[@W@.Schema.TFixedMeasure(2)];
            if (@W@.Schema.TFixedSave(w, buf) < 0) { bad += ProbeLog.Fail(@NAME@, "fixed_I_grow writer did not write"); }
            var back = new T[2];
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.TFixedLoad(back, buf, plan, r);
            if (n != 2) { bad += ProbeLog.Fail(@NAME@, "fixed_I_grow did not read 2: n=" + n + " " + r.Reason); }
            if (back[0].V != -128 || back[1].V != 112) { bad += ProbeLog.Fail(@NAME@, "fixed(4,4) raw did not sign-extend to fixed(12,4): " + back[0].V + " " + back[1].V); }
            if (r.Widened != 2) { bad += ProbeLog.Fail(@NAME@, "fixed_I_grow widened=" + r.Widened + ", want 2"); }
`},
}

// t12E3 holds cs/E3/other-required-widens' write half: this leg's own writer
// writes each widen-ladder fixture with non-default and boundary values, and
// this leg's own reader reads them back widened. The ladder and the sign rule
// are asserted in source at TestFixedRoadmapCSharpT04Versions/cs/E3/other-required-widens
// and the read landings against the C++ bytes at TestFixedVersioning; what that
// pair does not show is the writer producing the bytes, which is this.
func t12E3(t *testing.T) {
	t.Run("write-half", func(t *testing.T) {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the widen-ladder write half runs under make tables-cs-fixed-roadmap")
		}
		t12E3Write(t)
	})
}

// t12E3Write builds each widen pair into one assembly and runs it with the
// SDK's Roslyn compiler and dotnet exec — the t10 harness, no corpus — then
// reads each fixture's verdict out of the one run.
func t12E3Write(t *testing.T) {
	t.Helper()
	t10Discover(t)
	dir := t.TempDir()
	write := func(root string, files map[string][]byte) {
		for name, data := range files {
			p := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var calls []string
	for i, c := range t12WidenCases {
		wu := unitFrom(t, c.writer)
		ru := unitFrom(t, c.reader)
		we, ok := FixedLineageOf(wu, "T")
		if !ok {
			t.Fatalf("%s: no lineage entry for the writer", c.id)
		}
		rns := capitalize(ru.Package)
		wns := capitalize(wu.Package)
		wgen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i), "w")
		rgen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i), "r")
		wpk, err := csharp.Generate(wu)
		if err != nil {
			t.Fatal(err)
		}
		wtb, err := Generate(wu)
		if err != nil {
			t.Fatal(err)
		}
		write(wgen, wpk)
		write(wgen, wtb)
		rpk, err := csharp.Generate(ru)
		if err != nil {
			t.Fatal(err)
		}
		rtb, err := GenerateLineage(ru, map[string][]FixedLineageEntry{"T": {we}})
		if err != nil {
			t.Fatal(err)
		}
		write(rgen, rpk)
		write(rgen, rtb)
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.NewReplacer("@NAME@", fmt.Sprintf("%q", c.id), "@W@", wns).Replace(c.body)
		src := fmt.Sprintf(`// generated by internal/codegen/cstable's t12 harness: the widen-ladder write half
using System;
using System.Buffers.Binary;

namespace %s
{
    static class %s
    {
        public static int Run()
        {
            int bad = 0;
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, rns, probeClass, body, fmt.Sprintf("%q", c.id))
		if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("            bad += %s.%s.Run();", rns, probeClass))
	}
	main := fmt.Sprintf(`using System;

static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
        try
        {
%s
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs t12 e3: every row green" : "cs t12 e3: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, strings.Join(calls, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	topFiles, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	genFiles, _ := filepath.Glob(filepath.Join(dir, "gen", "*", "*", "*.cs"))
	refs, _ := filepath.Glob(filepath.Join(t10refDir, "*.dll"))
	args := []string{t10csc, "-nologo", "-langversion:latest", "-unsafe", "-out:" + filepath.Join(dir, "app.dll")}
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	args = append(args, topFiles...)
	args = append(args, genFiles...)
	cmd := exec.Command(t10dotnet, append([]string{"exec"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		for _, c := range t12WidenCases {
			t10Assert(t, out, err, c.id)
		}
		t.Fatalf("the t12 widen assembly did not compile: %s", t10Tail(string(out), err))
	}

	rtc := fmt.Sprintf(`{
  "runtimeOptions": {
    "tfm": "net10.0",
    "framework": { "name": "Microsoft.NETCore.App", "version": %q }
  }
}
`, t10refVersion)
	if err := os.WriteFile(filepath.Join(dir, "app.runtimeconfig.json"), []byte(rtc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(t10dotnet, "exec", filepath.Join(dir, "app.dll"))
	run.Dir = dir
	run.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	runOut, runErr := run.CombinedOutput()
	for _, c := range t12WidenCases {
		t10Assert(t, runOut, runErr, c.id)
	}
}

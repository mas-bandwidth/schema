package cstable

// fixedroadmap_t08 — the cs leg's plan-execution, interoperability,
// hostile-input and value-capability rows of docs/roadmap.sexp's node
// `fixed-tables` (ROADMAP.md "NEW Fixed Tables"). One subtest per task id this
// card proves.
//
// A VALUE-CAPABILITY CLAUSE IS A RUN, NOT A SUBSTRING. Each driven subtest
// generates the fixture's C#, compiles it with the SDK's own csc against the
// net10.0 reference pack and runs it with `dotnet exec`, then asserts the run's
// verdict. That is the leg's reader executed on this seat, with the SDK the
// gate builds against (`~/sdk/dotnet`), and no sibling `serialize.cs` checkout:
// the emitted fixed form references `UInt128`/`Int128`, which `using System`
// resolves to the BCL's `System.UInt128`/`System.Int128` (net7+) — the same
// shapes the emulated pair mirrors, so the codec compiles and runs standalone.
// The three value rows write non-default and boundary values and read them
// back; the hostile and interoperability rows drive the emitted reader with a
// wrong plan and a whole-file byte-flip sweep.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §3 (the record: every field at its declared
// width, little-endian, min and max of the storage type included, never
// clamped, never sign-extended to a neighbour's byte), §4.2 (ONE plan, the
// identity plan), §5.2 (COMPILE, the compiled plan), §5.9 #42 (the split and
// the unguarded-before-guarded partition), §7 items 1, 4 and 5
// (write-read-write byte-identical, the wrong plan must go red, the byte-flip
// fuzz under a sanitizer with three answers and never a fourth);
// docs/SPEC-TABLES.md §3.4 (the fixed record and its kinds). A clause already
// held by an existing test of this leg is named by that test and is not
// repeated here.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t08Flat is the smallest declared fixed table: two adjacent int32 scalars.
const t08Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t08UnionMid carries a union in the MIDDLE of the table, the fixture cs/W6's
// partition clause reads: two scalars around a two-arm union, so the arms'
// entries are guarded and the tail scalar PAST the union is not.
const t08UnionMid = `package probe

union Pick
{
    a int32
    b int32
}

fixed table T
{
    head int32
    pick Pick
    tail int32
}
`

// t08Emit generates the fixture and answers its one root and the emitted
// source of the shared file that carries the table's fixed form and the
// runtime.
func t08Emit(t *testing.T, src string) (*ir.Unit, *ir.Struct, string) {
	t.Helper()
	u := unitFrom(t, src)
	st := t02Root(t, u)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, ok := files["ProbeTable.cs"]
	if !ok {
		t.Fatalf("Generate emitted no ProbeTable.cs; files are: %v", t02Names(files))
	}
	return u, st, string(body)
}

func TestFixedRoadmapCSharpT08ConformanceIntegers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/W6", t08W6},
		{"cs/P1", t08P1},
		{"cs/W13", t08W13},
		{"cs/R27", t08R27},
		{"cs/P2", t08P2},
		{"cs/P3", t08P3},
		{"cs/P4", t08P4},
		{"signed-integers/cs/valid-data", t08Signed},
		{"unsigned-integers/cs/valid-data", t08Unsigned},
		{"bits-values/cs/valid-data", t08Bits},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t08WireEntry is ONE identity-plan entry, the fields the partition clause
// reads: the source byte offset, the destination slot, the size, and the guard
// and op as the emitted source spells them.
type t08WireEntry struct {
	src, dst, size int64
	guard          string
	op             string
	arg            string
}

// t08Plan reads the emitted identity plan back off the C#: the split constant
// and every entry, in emission order. It is the same shape the runtime carries,
// so the partition is asserted against what the reader actually runs.
func t08Plan(t *testing.T, src, table string) (int, []t08WireEntry) {
	t.Helper()
	prefix := fmt.Sprintf("public const int %sFixedPlanSplit = ", table)
	i := strings.Index(src, prefix)
	if i < 0 {
		t.Fatalf("W6: the emitted source carries no %sFixedPlanSplit constant", table)
	}
	rest := src[i+len(prefix):]
	j := strings.Index(rest, ";")
	if j < 0 {
		t.Fatalf("W6: %sFixedPlanSplit is not terminated", table)
	}
	split, err := strconv.Atoi(strings.TrimSpace(rest[:j]))
	if err != nil {
		t.Fatalf("W6: %sFixedPlanSplit is not a number: %q", table, rest[:j])
	}
	block := t02Block(t, src, fmt.Sprintf("%sFixedPlan = new TableFixedPlan(new TableFixedEntry[] {", table), "});")
	var entries []t08WireEntry
	for {
		k := strings.Index(block, "new TableFixedEntry(")
		if k < 0 {
			break
		}
		block = block[k+len("new TableFixedEntry("):]
		end := strings.Index(block, "),")
		if end < 0 {
			t.Fatalf("W6: an identity-plan entry is not terminated")
		}
		args := block[:end]
		block = block[end+2:]
		f := strings.Split(args, ",")
		if len(f) < 7 {
			t.Fatalf("W6: an identity-plan entry has %d fields: %q", len(f), args)
		}
		atoi := func(s string) int64 {
			s = strings.TrimSpace(strings.TrimSuffix(s, "u"))
			v, _ := strconv.ParseInt(s, 10, 64)
			return v
		}
		entries = append(entries, t08WireEntry{
			src:   atoi(f[0]),
			dst:   atoi(f[1]),
			size:  atoi(f[2]),
			guard: strings.TrimSpace(f[4]),
			op:    strings.TrimSpace(f[5]),
			arg:   strings.TrimSpace(f[6]),
		})
	}
	return split, entries
}

// t08W6: W6 [weak] "plan partition / split" — FIXED-FORM-ALGORITHM §5.9 #42:
// every leg carries the split — the number of UNGUARDED entries, the index the
// guarded half starts at — and partitions the plan unguarded-first, though its
// run loop may still test the guard per entry. A field past a union would
// otherwise land its unguarded entry behind the union's guarded arms. The
// identity plan is read back off the emitted C# and held to all three: the
// split constant, the order, and the boundary pair the coalescer may not cross.
func t08W6(t *testing.T) {
	_, _, src := t08Emit(t, t08UnionMid)
	split, entries := t08Plan(t, src, "T")
	if split <= 0 || split >= len(entries) {
		t.Fatalf("W6: the identity plan really has a guarded half (split=%d of %d), so the case is not vacuous", split, len(entries))
	}
	tailBeforeSplit := false
	for i, e := range entries {
		guarded := !strings.Contains(e.guard, "NoGuard")
		if i < split && guarded {
			t.Errorf("W6: entry %d (src %d) is below the split and carries a GUARD", i, e.src)
		}
		if i >= split && !guarded {
			t.Errorf("W6: entry %d (src %d) is above the split and carries NO guard", i, e.src)
		}
		if i < split && e.src == 9 {
			tailBeforeSplit = true
		}
	}
	if !tailBeforeSplit {
		t.Errorf("W6: the tail scalar past the union (src 9) did not move into the unguarded half")
	}
	a, b := entries[split-1], entries[split]
	if a.op == "TableFixedWire.Copy" && b.op == "TableFixedWire.Copy" &&
		a.guard == b.guard && a.arg == b.arg && a.src+a.size == b.src && a.dst == b.dst {
		t.Errorf("W6: the pair at the split was coalesced across it")
	}
}

// t08P1: P1 [verify] "identity == compiled" — SPEC-TABLES §3.4: "THE SAME LOOP
// RUNS OVER THE SAME PLAN WHETHER THE WRITER IS THIS BUILD OR ANOTHER, and the
// only thing that differs is which plan it was handed. There is no strict flag,
// no fast path, no second reader to keep honest against the first." The emitted
// load defaults to the identity plan and swaps in the compiled lane's entries
// only under `hash != own`; the record loop's single Run call runs both, so a
// record this build wrote and a record a peer wrote are read by one path.
func t08P1(t *testing.T) {
	_, _, src := t08Emit(t, t08Flat)
	n := t02Norm(src)

	t02Has(t, src, "ReadOnlySpan<TableFixedEntry> entries = TFixedPlan;",
		"P1 the identity plan is the load's default entries")
	t02Has(t, src, "if (hash != TFixedHash)",
		"P1 the compiled lane is selected only when the header's hash is not this build's")
	t02Has(t, n, "entries = new ReadOnlySpan<TableFixedEntry>(lane.Entries, 0, lane.Count);",
		"P1 the compiled lane swaps in the selected lane's entries and nothing else")

	// ONE record loop, one Run call, shared by both lanes: the only thing that
	// differs is which `entries` span was handed to it.
	if got := strings.Count(src, "TableFixedWire.Run(entries, "); got != 1 {
		t.Errorf("P1: TableFixedWire.Run(entries, occurs %d times in the load, want its one record loop", got)
	}
	for _, forbidden := range []string{"strict", "identity_plan", "compiled_plan"} {
		if strings.Contains(strings.ToLower(n), forbidden) {
			t.Errorf("P1: the emitted load carries a %q, which is a second reader", forbidden)
		}
	}
}

// t08W13: W13 [weak] "layout+hash == C++ reference" — FIXED-FORM-ALGORITHM §7
// item 1 ("the block-equals-the-reference test", §8 "dump identity of bytes"):
// this leg's emitted layout block and the hash over it equal the compiler's own
// walk, ir.TableFixedLayoutBytes and ir.TableFixedLayoutHash, byte for byte and
// number for number. ir is the one law every port renders (C++ included) and it
// produces BYTES, so it is the oracle and never a second backend.
func t08W13(t *testing.T) {
	src := `package probe
fixed table Point {
    x int32
    y int32
}
fixed table Vector {
    a int64
    b float32
    name string(8)
    tags [4]int16
}
`
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("W13: Generate: %v", err)
	}
	text := string(files["ProbeTable.cs"])

	compared := 0
	for _, st := range ir.TableFixedRoots(u) {
		if !ir.TableFixedEmitted(u, st) {
			continue
		}
		want := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		wantHash := ir.TableFixedLayoutHash(want, st)
		got := fixedLayoutOf(&tableGen{unit: u}, st)
		if string(got) != string(want) {
			t.Errorf("W13: %s's emitted layout differs from the compiler's own walk (docs/FIXED-FORM-ALGORITHM.md §7 item 1):\n  ir % x\n  cs % x", st.Name, want, got)
		}
		if !strings.Contains(text, fmt.Sprintf("public const ulong %sFixedHash = 0x%016xul;", st.Name, wantHash)) {
			t.Errorf("W13: %s's emitted hash constant is not ir's %#x — a record written against the law is `no_layout` to this port", st.Name, wantHash)
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("W13: no root was compared against ir at all")
	}
}

// t08R27: R27 [weak] "the manifest is the wire: build/fixedform-corpus/
// manifest.txt is the single value oracle every leg reads to" —
// docs/FIXED-FORM-ALGORITHM.md §5.9 #32, #37: "ASSERT THE MANIFEST, NEVER READ
// THE DUMP", and §5.9 #28 names the manifest as the one place that states what
// each corpus file holds. The cs leg's versioning harness reads the manifest
// per file and in decimal and takes a float's bits from it — never from a
// literal — so the harness's expected values are the manifest's own.
func t08R27(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("fixedversioning_test.go"))
	if err != nil {
		t.Fatalf("R27: read the cs leg's versioning harness: %v", err)
	}
	harness := string(data)
	t02Has(t, harness, `os.Open(filepath.Join(corpus, "manifest.txt"))`,
		"R27 the harness reads the corpus manifest")
	t02Has(t, harness, "THE ORACLE IS THE CORPUS MANIFEST",
		"R27 the harness names the corpus manifest the single value oracle")
	t02Has(t, harness, `csManifestBrackets(t, corpus, "old_"+row+".bin")`,
		"R27 the harness reads one file's values from the manifest, not from the bytes under test")
}

// ---- the driven reader ------------------------------------------------------

// t08Main is the C# driver every driven subtest runs. The body accumulates a
// failure count through ProbeLog.Fail and the process prints one verdict line
// the Go side asserts on: "PROBE OK" when nothing failed, "FAILED n" otherwise.
const t08Main = `using System;

static class ProbeLog
{
    public static int Fail(string what) { Console.WriteLine("FAIL " + what); return 1; }
}

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
%s
        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`

// t08RunCS generates the fixture's C#, compiles it with the SDK's own csc
// (found beside the resolved `dotnet` on PATH) against the net10.0 reference
// pack, and runs it with `dotnet exec`, returning the combined output. It
// fails, never skips: a driven subtest that cannot reach a reader is a red
// clause, not a pass.
func t08RunCS(t *testing.T, schemaSrc, body string) string {
	t.Helper()
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(fmt.Sprintf(t08Main, body)), 0o600); err != nil {
		t.Fatal(err)
	}

	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	dp, err := exec.LookPath(dotnet)
	if err != nil {
		t.Fatalf("dotnet is not on PATH: %v", err)
	}
	rp, err := filepath.EvalSymlinks(dp)
	if err != nil {
		t.Fatalf("resolve dotnet %s: %v", dp, err)
	}
	root := filepath.Dir(rp)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	refs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net10.0"))
	if len(refs) == 0 {
		t.Fatalf("no net10.0 reference pack under %s", root)
	}
	frameworks, _ := filepath.Glob(filepath.Join(root, "shared", "Microsoft.NETCore.App", "*"))
	if len(frameworks) == 0 {
		t.Fatalf("no shared framework under %s", root)
	}
	version := filepath.Base(frameworks[0])

	sources, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	args := []string{"exec", cscs[0], "-nologo", "-unsafe", "-out:" + filepath.Join(dir, "probe.dll")}
	args = append(args, sources...)
	for _, r := range refs {
		dlls, _ := filepath.Glob(filepath.Join(r, "*.dll"))
		for _, dll := range dlls {
			args = append(args, "-r:"+dll)
		}
	}
	if out, err := exec.Command(dotnet, args...).CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", out)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, version)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command(dotnet, "exec", filepath.Join(dir, "probe.dll")).CombinedOutput()
	return string(out)
}

// t08CSOK runs the body and asserts the driver printed PROBE OK, failing with
// the driver's own words otherwise.
func t08CSOK(t *testing.T, schemaSrc, body string) {
	t.Helper()
	out := t08RunCS(t, schemaSrc, body)
	if strings.Contains(out, "FAIL ") || !strings.Contains(out, "PROBE OK") {
		t.Fatalf("the driven reader failed:\n%s", out)
	}
}

// ---- the value-capability rows ---------------------------------------------

// t08Signed: signed-integers/cs/valid-data [capability] "Signed integers: 8,
// 16, 32 and 64 bits: valid-data write/read acceptance" (SPEC-TABLES §3.4
// "record, int8..int64"; ALGORITHM §3). Every width is written at its declared
// storage width, little-endian, at both ends of its storage range, and read
// back exactly — never clamped, never sign-extended to a neighbour's byte.
func t08Signed(t *testing.T) {
	t08CSOK(t, `package probe

fixed table T
{
    i8  int8
    i16 int16
    i32 int32
    i64 int64
}
`, `int RT(Probe.T v, string what)
{
    byte[] buf = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, buf) != buf.Length) { return ProbeLog.Fail(what + ": save did not fill the measure"); }
    Probe.T back = new Probe.T();
    Probe.Schema.TableReset(back);
    Probe.TableReport r = new Probe.TableReport();
    Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
    long n = Probe.Schema.TFixedLoad(back, buf, plan, r);
    if (n != 1 || r.Refused || r.Malformed || r.Clamped != 0) { return ProbeLog.Fail(what + ": clean read n=" + n + " refused=" + r.Refused + " malformed=" + r.Malformed + " clamped=" + r.Clamped); }
    if (back.I8 != v.I8 || back.I16 != v.I16 || back.I32 != v.I32 || back.I64 != v.I64)
    {
        return ProbeLog.Fail(what + ": " + back.I8 + " " + back.I16 + " " + back.I32 + " " + back.I64);
    }
    return 0;
}
Probe.T min = new Probe.T();
Probe.Schema.TableReset(min);
min.I8 = sbyte.MinValue; min.I16 = short.MinValue; min.I32 = int.MinValue; min.I64 = long.MinValue;
bad += RT(min, "signed min");
Probe.T max = new Probe.T();
Probe.Schema.TableReset(max);
max.I8 = sbyte.MaxValue; max.I16 = short.MaxValue; max.I32 = int.MaxValue; max.I64 = long.MaxValue;
bad += RT(max, "signed max");`)
}

// t08Unsigned: unsigned-integers/cs/valid-data [capability] "Unsigned
// integers: 8, 16, 32 and 64 bits: valid-data write/read acceptance"
// (SPEC-TABLES §3.4 "record, uint8..uint64"). Every width round-trips 0 and its
// storage maximum, exactly, with no clamp and no counter moved.
func t08Unsigned(t *testing.T) {
	t08CSOK(t, `package probe

fixed table T
{
    u8  uint8
    u16 uint16
    u32 uint32
    u64 uint64
}
`, `int RT(Probe.T v, string what)
{
    byte[] buf = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, buf) != buf.Length) { return ProbeLog.Fail(what + ": save did not fill the measure"); }
    Probe.T back = new Probe.T();
    Probe.Schema.TableReset(back);
    Probe.TableReport r = new Probe.TableReport();
    Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
    long n = Probe.Schema.TFixedLoad(back, buf, plan, r);
    if (n != 1 || r.Refused || r.Malformed || r.Clamped != 0) { return ProbeLog.Fail(what + ": clean read n=" + n + " refused=" + r.Refused + " malformed=" + r.Malformed + " clamped=" + r.Clamped); }
    if (back.U8 != v.U8 || back.U16 != v.U16 || back.U32 != v.U32 || back.U64 != v.U64)
    {
        return ProbeLog.Fail(what + ": " + back.U8 + " " + back.U16 + " " + back.U32 + " " + back.U64);
    }
    return 0;
}
Probe.T zero = new Probe.T();
Probe.Schema.TableReset(zero);
zero.U8 = 0; zero.U16 = 0; zero.U32 = 0u; zero.U64 = 0ul;
bad += RT(zero, "unsigned zero");
Probe.T max = new Probe.T();
Probe.Schema.TableReset(max);
max.U8 = byte.MaxValue; max.U16 = ushort.MaxValue; max.U32 = uint.MaxValue; max.U64 = ulong.MaxValue;
bad += RT(max, "unsigned max");`)
}

// t08Bits: bits-values/cs/valid-data [capability] "bits(N) fields: valid-data
// write/read acceptance" (SPEC-TABLES §3.4 "record, bits(N)"). Each declared
// width round-trips 0 and its own 2^N-1 boundary exactly, at its storage width
// (uint for N<=32, ulong for N>32).
func t08Bits(t *testing.T) {
	t08CSOK(t, `package probe

fixed table T
{
    b1  bits(1)
    b7  bits(7)
    b12 bits(12)
    b40 bits(40)
}
`, `int RT(Probe.T v, string what)
{
    byte[] buf = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, buf) != buf.Length) { return ProbeLog.Fail(what + ": save did not fill the measure"); }
    Probe.T back = new Probe.T();
    Probe.Schema.TableReset(back);
    Probe.TableReport r = new Probe.TableReport();
    Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
    long n = Probe.Schema.TFixedLoad(back, buf, plan, r);
    if (n != 1 || r.Refused || r.Malformed || r.Clamped != 0) { return ProbeLog.Fail(what + ": clean read n=" + n + " refused=" + r.Refused + " malformed=" + r.Malformed + " clamped=" + r.Clamped); }
    if (back.B1 != v.B1 || back.B7 != v.B7 || back.B12 != v.B12 || back.B40 != v.B40)
    {
        return ProbeLog.Fail(what + ": " + back.B1 + " " + back.B7 + " " + back.B12 + " " + back.B40);
    }
    return 0;
}
Probe.T zero = new Probe.T();
Probe.Schema.TableReset(zero);
bad += RT(zero, "bits zero");
Probe.T max = new Probe.T();
Probe.Schema.TableReset(max);
max.B1 = 1; max.B7 = 127; max.B12 = 4095; max.B40 = (1ul << 40) - 1;
bad += RT(max, "bits max");`)
}

// ---- the interoperability and hostile rows ----------------------------------

// t08P2: P2 [weak] "write-read-write byte-identical" — FIXED-FORM-ALGORITHM §7
// item 1: "Read a file and save it back; the bytes must be identical". A
// bounded string leaves slack, and §3.4 says the writer leaves every slack byte
// the template's zero, so the write after a read is byte-identical to the first
// write — a writer that leaked its storage into the slack comes back different.
func t08P2(t *testing.T) {
	t08CSOK(t, `package probe

fixed table T
{
    a     int32
    b     int64
    label string(8)
}
`, `Probe.T v = new Probe.T();
Probe.Schema.TableReset(v);
v.A = 12345;
v.B = -9876543210L;
v.Label[0] = (byte)'h'; v.Label[1] = (byte)'i'; v.LabelLength = 2;
byte[] w1 = new byte[Probe.Schema.TFixedMeasure(1)];
if (Probe.Schema.TFixedSave(v, w1) != w1.Length) { bad += ProbeLog.Fail("P2: save did not fill the measure"); }
Probe.T back = new Probe.T();
Probe.Schema.TableReset(back);
Probe.TableReport r = new Probe.TableReport();
Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
long n = Probe.Schema.TFixedLoad(back, w1, plan, r);
if (n != 1 || r.Refused || r.Malformed) { bad += ProbeLog.Fail("P2: clean read n=" + n + " refused=" + r.Refused + " malformed=" + r.Malformed); }
if (back.A != 12345 || back.B != -9876543210L || back.LabelLength != 2 || back.Label[0] != (byte)'h' || back.Label[1] != (byte)'i')
{
    bad += ProbeLog.Fail("P2: the read did not reproduce the record");
}
byte[] w2 = new byte[Probe.Schema.TFixedMeasure(1)];
Probe.Schema.TFixedSave(back, w2);
if (!w1.AsSpan().SequenceEqual(w2.AsSpan())) { bad += ProbeLog.Fail("P2: the write after the read is not byte-identical"); }`)
}

// t08P3: P3 [owed] "hostile bytes, sweep + sanitizer" — FIXED-FORM-ALGORITHM §7
// item 5: "a byte-flip fuzz over the whole file ... every byte, one bit at a
// time, is answered one of three ways and never a fourth: a refusal by name, a
// malformed read, or a read that lands values". The managed runtime is the
// sanitizer: the sweep is wrapped so a load that THROWS — the fourth answer —
// fails by name. All three answers must occur, so the sweep is proven to have
// reached the refusal, framing and value paths.
func t08P3(t *testing.T) {
	t08CSOK(t, `package probe

fixed table T
{
    a int32
    b int32
}
`, `Probe.T v = new Probe.T();
Probe.Schema.TableReset(v);
v.A = 12345; v.B = -67890;
byte[] good = new byte[Probe.Schema.TFixedMeasure(1)];
if (Probe.Schema.TFixedSave(v, good) != good.Length) { bad += ProbeLog.Fail("P3: save did not fill the measure"); }
int refusals = 0, malformed = 0, landed = 0;
for (int off = 0; off < good.Length; ++off)
{
    for (int bit = 0; bit < 8; ++bit)
    {
        byte[] f = (byte[])good.Clone();
        f[off] ^= (byte)(1 << bit);
        try
        {
            Probe.T back = new Probe.T();
            Probe.Schema.TableReset(back);
            Probe.TableReport r = new Probe.TableReport();
            Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
            long n = Probe.Schema.TFixedLoad(back, f, plan, r);
            if (r.Refused) { refusals++; }
            else if (r.Malformed) { malformed++; }
            else if (n >= 1) { landed++; }
            else { bad += ProbeLog.Fail("P3: flip at " + off + "/" + bit + " is a fourth answer: n=" + n + " reason=" + r.Reason); }
        }
        catch (System.Exception e)
        {
            bad += ProbeLog.Fail("P3: flip at " + off + "/" + bit + " threw: " + e.Message);
        }
    }
}
if (refusals == 0 || malformed == 0 || landed == 0)
{
    bad += ProbeLog.Fail("P3: sweep saw refusals=" + refusals + " malformed=" + malformed + " landed=" + landed + ", want all three answers and never a fourth");
}`)
}

// t08P4: P4 [owed] "wrong plan goes red" — FIXED-FORM-ALGORITHM §7 item 4: "the
// wrong plan must go red". This build's identity plan for A (one int64) run
// over B's record body (two int32) splices the two fields into one value, so it
// must NOT reproduce B's record; the run is the emitted reader's own loop, fed
// the wrong plan directly.
func t08P4(t *testing.T) {
	t08CSOK(t, `package probe

fixed table A
{
    p int64
}

fixed table B
{
    x int32
    y int32
}
`, `Probe.B b = new Probe.B();
Probe.Schema.TableReset(b);
b.X = 111; b.Y = 222;
byte[] wb = new byte[Probe.Schema.BFixedMeasure(1)];
if (Probe.Schema.BFixedSave(b, wb) != wb.Length) { bad += ProbeLog.Fail("P4: save did not fill the measure"); }
uint lb = System.Buffers.Binary.BinaryPrimitives.ReadUInt32LittleEndian(wb.AsSpan(Probe.Schema.TableFixedWire.HeaderBytes));
System.ReadOnlySpan<byte> body = wb.AsSpan(Probe.Schema.TableFixedWire.HeaderBytes + 4 + (int)lb + 8);
Probe.A a = new Probe.A();
Probe.Schema.TableReset(a);
Probe.TableReport r = new Probe.TableReport();
byte[] scratch = System.Array.Empty<byte>();
Probe.Schema.TableFixedWire.Run(Probe.Schema.AFixedPlan, Probe.Schema.AFixedSlots, body, a, r, System.ReadOnlySpan<byte>.Empty, ref scratch);
if (a.P == b.X) { bad += ProbeLog.Fail("P4: this build's identity plan reproduced another schema's record"); }`)
}

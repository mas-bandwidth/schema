package cstable

// fixedroadmap_t18 — the cs leg's union-field-arms and payload-free-arms
// valid-data cells of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW
// Fixed Tables"). The two cells share one emitter path, the union arm switch,
// so one card proves both (docs/SPEC-TABLES.md §2.6, docs/FIXED-FORM-ALGORITHM.md
// §3.1).
//
// A VALUE-CAPABILITY CLAUSE IS A RUN, NOT A SUBSTRING (fixedroadmap_t08_test.go
// lines 8-29). The functional tier below generates the fixture's C#, compiles it
// with the SDK's own csc against the net10.0 reference pack and runs it with
// `dotnet exec`, then asserts the run's verdict. The unit tier asserts the
// emitted text alone: no dotnet, no clock, no socket, no network.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const t18DirectSchema = `package probe

union Body
{
    s string(8)
    b bytes(4)
    a [3]int32
    c [..3]int32
}

fixed table T
{
    lead  int32
    body  Body
    trail int32
}
`

const t18PayloadFreeSchema = `package probe

union Pick
{
    alpha int32
    quiet
    signal
}

fixed table T
{
    lead  int32
    pick  Pick
    trail int32
}
`

// TestFixedCSharpT18ArmLines pins the C# the union arm switch emits for one
// direct text arm, one direct bytes arm, one direct fixed array arm, one
// direct counted array arm, and two payload-free arms
// (docs/SPEC-TABLES.md §2.6: "the arm's tag is its whole storage"; an arm is a
// field line). It emits the two fixtures in memory and asserts the text alone,
// in the shape of TestFixedArmWritesAreFieldLines
// (fixedform_csunions_test.go line 146): no dotnet, no clock, no socket.
func TestFixedCSharpT18ArmLines(t *testing.T) {
	t.Parallel()

	direct := csFn(generateCS(t, t18DirectSchema), "public static void TFixedWriteBody(")
	if direct == "" {
		t.Fatal("DIRECT: no TFixedWriteBody")
	}
	for _, want := range []string{
		"int len_Body_s = ",                  // the direct text arm's used length
		"int len_Body_b = ",                  // the direct bytes arm's used length
		"int count_Body_c = ",                // the direct counted array arm's live count
		"MemoryMarshal.AsBytes(value.Body.A", // the direct fixed array arm's elements
	} {
		if !strings.Contains(direct, want) {
			t.Errorf("DIRECT TFixedWriteBody: missing %q", want)
		}
	}
	if strings.Contains(direct, "/* ? */") {
		t.Errorf("DIRECT: an arm spells the fallback type /* ? */")
	}

	free := csFn(generateCS(t, t18PayloadFreeSchema), "public static void TFixedWriteBody(")
	if free == "" {
		t.Fatal("PAYLOADFREE: no TFixedWriteBody")
	}
	for _, arm := range []string{"Quiet", "Signal"} {
		sig := "case PickType." + arm + ":"
		_, rest, found := strings.Cut(free, sig)
		if !found {
			t.Errorf("PAYLOADFREE TFixedWriteBody: missing %q", sig)
			continue
		}
		stored, _, found := strings.Cut(rest, "break;")
		if !found {
			t.Errorf("PAYLOADFREE TFixedWriteBody: %s case has no break", arm)
			continue
		}
		// THE TAG ALONE IS ITS WHOLE STORAGE (§2.6): a payload-free arm's case
		// carries no store, only the break.
		if body := strings.TrimSpace(stored); body != "" {
			t.Errorf("PAYLOADFREE TFixedWriteBody: %s case stores %q", arm, body)
		}
	}
}

// t18DirectBody drives the DIRECT fixture's direct arms s, b, a and c: a
// boundary record (full-length text and bytes, every array slot at int32 min or
// max, a full counted array) and an edge (empty text, a zero count) through the
// emitted writer and reader, and asserts the tag, the value, its used length or
// live count, lead and trail, a clean report, and zero bytes past the used length
// in the written record (docs/SPEC-TABLES.md §3.4).
const t18DirectBody = `
int a0 = 0, a2 = 0, a3 = 0, a4 = 0;
int d = (int)Probe.Schema.TableFixedWire.HeaderBytes + 4 + (int)Probe.Schema.TFixedLayoutBytes;
Probe.T back = new Probe.T();
Probe.TableReport rep = new Probe.TableReport();
Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
int Fail(string arm, string why) { Console.WriteLine("T18 union-field-arms/cs/valid-data/" + arm + " BAD " + why); return 1; }
void Ok(string arm) { Console.WriteLine("T18 union-field-arms/cs/valid-data/" + arm + " ok"); }
int Read(Probe.T v, byte[] buf, string arm)
{
    back = new Probe.T();
    Probe.Schema.TableReset(back);
    rep = new Probe.TableReport();
    plan = new Probe.TableFixedEntry[64];
    long n = Probe.Schema.TFixedLoad(back, buf, plan, rep);
    if (n != 1 || rep.Refused || rep.Malformed || rep.Clamped != 0) { return Fail(arm, "clean read n=" + n + " refused=" + rep.Refused + " malformed=" + rep.Malformed + " clamped=" + rep.Clamped); }
    if (back.Lead != v.Lead || back.Trail != v.Trail) { return Fail(arm, "lead/trail " + back.Lead + "/" + back.Trail); }
    return 0;
}
int Zero(byte[] b, int from, int to, string arm)
{
    for (int k = from; k < to; k++) { if (b[k] != 0) { return Fail(arm, "slack not zero at " + k); } }
    return 0;
}
// s: direct text arm. File: header 8, lead [8,12), union tag 12, count [13,17), data [17,29), trail [29,33).
{
    Probe.T v = new Probe.T();
    Probe.Schema.TableReset(v);
    v.Lead = 123; v.Trail = -456; v.Body.Type = Probe.BodyType.S;
    for (int k = 0; k < 8; k++) { v.Body.S[k] = (byte)('a' + k); }
    v.Body.SLength = 8;
    byte[] w1 = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, w1) != w1.Length) { a0 += Fail("s", "save"); }
    a0 += Read(v, w1, "s");
    if (back.Body.Type != Probe.BodyType.S || back.Body.SLength != 8) { a0 += Fail("s", "tag/len " + back.Body.Type + "/" + back.Body.SLength); }
    for (int k = 0; k < 8; k++) { if (back.Body.S[k] != (byte)('a' + k)) { a0 += Fail("s", "data " + k); } }
    a0 += Zero(w1, 25 + d, 29 + d, "s");
    Probe.T e = new Probe.T();
    Probe.Schema.TableReset(e);
    e.Lead = -7; e.Trail = 9; e.Body.Type = Probe.BodyType.S; e.Body.SLength = 0;
    byte[] w2 = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(e, w2) != w2.Length) { a0 += Fail("s", "edge save"); }
    a0 += Read(e, w2, "s");
    if (back.Body.Type != Probe.BodyType.S || back.Body.SLength != 0) { a0 += Fail("s", "edge tag/len"); }
    a0 += Zero(w2, 17 + d, 29 + d, "s");
}
if (a0 == 0) { Ok("s"); }
// b: direct bytes arm. data [17,21), slack [21,29).
{
    Probe.T v = new Probe.T();
    Probe.Schema.TableReset(v);
    v.Lead = 7; v.Trail = 8; v.Body.Type = Probe.BodyType.B;
    for (int k = 0; k < 4; k++) { v.Body.B[k] = (byte)(0x10 + k); }
    v.Body.BLength = 4;
    byte[] w1 = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, w1) != w1.Length) { a2 += Fail("b", "save"); }
    a2 += Read(v, w1, "b");
    if (back.Body.Type != Probe.BodyType.B || back.Body.BLength != 4) { a2 += Fail("b", "tag/len"); }
    for (int k = 0; k < 4; k++) { if (back.Body.B[k] != (byte)(0x10 + k)) { a2 += Fail("b", "data " + k); } }
    a2 += Zero(w1, 21 + d, 29 + d, "b");
    Probe.T e = new Probe.T();
    Probe.Schema.TableReset(e);
    e.Lead = 3; e.Trail = 4; e.Body.Type = Probe.BodyType.B; e.Body.BLength = 0;
    byte[] w2 = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(e, w2) != w2.Length) { a2 += Fail("b", "edge save"); }
    a2 += Read(e, w2, "b");
    if (back.Body.Type != Probe.BodyType.B || back.Body.BLength != 0) { a2 += Fail("b", "edge tag/len"); }
    a2 += Zero(w2, 17 + d, 29 + d, "b");
}
if (a2 == 0) { Ok("b"); }
// a: direct fixed array arm, every slot at int32 min or max. data [17,29).
{
    Probe.T v = new Probe.T();
    Probe.Schema.TableReset(v);
    v.Lead = 41; v.Trail = 42; v.Body.Type = Probe.BodyType.A;
    v.Body.A[0] = int.MinValue; v.Body.A[1] = int.MaxValue; v.Body.A[2] = 0;
    byte[] w1 = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, w1) != w1.Length) { a3 += Fail("a", "save"); }
    a3 += Read(v, w1, "a");
    if (back.Body.Type != Probe.BodyType.A) { a3 += Fail("a", "tag " + back.Body.Type); }
    if (back.Body.A[0] != int.MinValue || back.Body.A[1] != int.MaxValue || back.Body.A[2] != 0) { a3 += Fail("a", "slots"); }
}
if (a3 == 0) { Ok("a"); }
// c: direct counted array arm: a full array, then a zero count. count [13,17), data [17,29).
{
    Probe.T v = new Probe.T();
    Probe.Schema.TableReset(v);
    v.Lead = 51; v.Trail = 52; v.Body.Type = Probe.BodyType.C;
    v.Body.C[0] = int.MinValue; v.Body.C[1] = int.MaxValue; v.Body.C[2] = -1; v.Body.CCount = 3;
    byte[] w1 = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, w1) != w1.Length) { a4 += Fail("c", "save"); }
    a4 += Read(v, w1, "c");
    if (back.Body.Type != Probe.BodyType.C || back.Body.CCount != 3) { a4 += Fail("c", "tag/count " + back.Body.Type + "/" + back.Body.CCount); }
    if (back.Body.C[0] != int.MinValue || back.Body.C[1] != int.MaxValue || back.Body.C[2] != -1) { a4 += Fail("c", "slots"); }
    Probe.T e = new Probe.T();
    Probe.Schema.TableReset(e);
    e.Lead = 61; e.Trail = 62; e.Body.Type = Probe.BodyType.C; e.Body.CCount = 0;
    byte[] w2 = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(e, w2) != w2.Length) { a4 += Fail("c", "edge save"); }
    a4 += Read(e, w2, "c");
    if (back.Body.Type != Probe.BodyType.C || back.Body.CCount != 0) { a4 += Fail("c", "edge tag/count"); }
    a4 += Zero(w2, 17 + d, 29 + d, "c");
}
if (a4 == 0) { Ok("c"); }
`

// t18PayloadFreeBody drives the PAYLOADFREE fixture (a payload arm alpha and the
// payload-free arms quiet and signal): three records (alpha at int32 min, quiet,
// signal), asserting the tag, lead and trail on both sides, a clean report, and
// all-zero arm storage under a payload-free tag (docs/FIXED-FORM-ALGORITHM.md
// §3.1, "a union writes the tag and the taken arm only").
const t18PayloadFreeBody = `
int p0 = 0, p1 = 0, p2 = 0;
int d = (int)Probe.Schema.TableFixedWire.HeaderBytes + 4 + (int)Probe.Schema.TFixedLayoutBytes;
Probe.T back = new Probe.T();
Probe.TableReport rep = new Probe.TableReport();
Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
int Fail(string arm, string why) { Console.WriteLine("T18 payload-free-arms/cs/valid-data/" + arm + " BAD " + why); return 1; }
void Ok(string arm) { Console.WriteLine("T18 payload-free-arms/cs/valid-data/" + arm + " ok"); }
int Read(Probe.T v, byte[] buf, string arm)
{
    back = new Probe.T();
    Probe.Schema.TableReset(back);
    rep = new Probe.TableReport();
    plan = new Probe.TableFixedEntry[64];
    long n = Probe.Schema.TFixedLoad(back, buf, plan, rep);
    if (n != 1 || rep.Refused || rep.Malformed || rep.Clamped != 0) { return Fail(arm, "clean read n=" + n + " refused=" + rep.Refused + " malformed=" + rep.Malformed + " clamped=" + rep.Clamped); }
    if (back.Lead != v.Lead || back.Trail != v.Trail) { return Fail(arm, "lead/trail " + back.Lead + "/" + back.Trail); }
    return 0;
}
int Zero(byte[] b, int from, int to, string arm)
{
    for (int k = from; k < to; k++) { if (b[k] != 0) { return Fail(arm, "arm storage not zero at " + k); } }
    return 0;
}
// File: header 8, lead [8,12), union tag 12, arm storage [13,17), trail [17,21).
{
    Probe.T v = new Probe.T();
    Probe.Schema.TableReset(v);
    v.Lead = 11; v.Trail = 22; v.Pick.Type = Probe.PickType.Alpha; v.Pick.Alpha = int.MinValue;
    byte[] w = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, w) != w.Length) { p0 += Fail("alpha", "save"); }
    p0 += Read(v, w, "alpha");
    if (back.Pick.Type != Probe.PickType.Alpha || back.Pick.Alpha != int.MinValue) { p0 += Fail("alpha", "tag/value " + back.Pick.Type + "/" + back.Pick.Alpha); }
}
if (p0 == 0) { Ok("alpha"); }
{
    Probe.T v = new Probe.T();
    Probe.Schema.TableReset(v);
    v.Lead = 3; v.Trail = -4; v.Pick.Type = Probe.PickType.Quiet;
    byte[] w = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, w) != w.Length) { p1 += Fail("quiet", "save"); }
    p1 += Read(v, w, "quiet");
    if (back.Pick.Type != Probe.PickType.Quiet) { p1 += Fail("quiet", "tag " + back.Pick.Type); }
    p1 += Zero(w, 13 + d, 17 + d, "quiet");
}
if (p1 == 0) { Ok("quiet"); }
{
    Probe.T v = new Probe.T();
    Probe.Schema.TableReset(v);
    v.Lead = 5; v.Trail = -6; v.Pick.Type = Probe.PickType.Signal;
    byte[] w = new byte[Probe.Schema.TFixedMeasure(1)];
    if (Probe.Schema.TFixedSave(v, w) != w.Length) { p2 += Fail("signal", "save"); }
    p2 += Read(v, w, "signal");
    if (back.Pick.Type != Probe.PickType.Signal) { p2 += Fail("signal", "tag " + back.Pick.Type); }
    p2 += Zero(w, 13 + d, 17 + d, "signal");
}
if (p2 == 0) { Ok("signal"); }
`

// t18TagPastBody forges the tag byte past Pick's arm set: the union slot's
// setRawReport clamp (fixedform.go) reads the tag as None and counts Clamped
// once, and no arm payload is read.
const t18TagPastBody = `
int t0 = 0;
int d = (int)Probe.Schema.TableFixedWire.HeaderBytes + 4 + (int)Probe.Schema.TFixedLayoutBytes;
int Fail(string why) { Console.WriteLine("T18 tag-past-the-arm-set BAD " + why); return 1; }
Probe.T v = new Probe.T();
Probe.Schema.TableReset(v);
v.Lead = 1; v.Trail = 2; v.Pick.Type = Probe.PickType.Quiet;
byte[] w = new byte[Probe.Schema.TFixedMeasure(1)];
Probe.Schema.TFixedSave(v, w);
w[12 + d] = 200;
Probe.T back = new Probe.T();
Probe.Schema.TableReset(back);
Probe.TableReport rep = new Probe.TableReport();
Probe.TableFixedEntry[] plan = new Probe.TableFixedEntry[64];
long n = Probe.Schema.TFixedLoad(back, w, plan, rep);
if (back.Pick.Type != Probe.PickType.None) { t0 += Fail("tag " + back.Pick.Type); }
if (rep.Clamped != 1) { t0 += Fail("clamped " + rep.Clamped); }
if (back.Pick.Alpha != 0) { t0 += Fail("a payload was read: " + back.Pick.Alpha); }
if (n != 1) { t0 += Fail("n " + n); }
if (t0 == 0) { Console.WriteLine("T18 tag-past-the-arm-set ok"); }
`

// t18Discover performs the skip-by-name / fail-by-name-under-require logic that
// t13Discover does, but without mutating any package-level state. t13Discover
// wrote the shared t13* globals; TestFixedRoadmapCSharpT13* tests are also
// t.Parallel and call it, so a second caller from this file raced on the writes
// (repro: go test -race -run 'T13|T18'). t08RunCS does its own local discovery
// and never skips (a driven clause that cannot run is red, not skipped), so this
// local discover only decides early skip/fatal then hands off to t08RunCS.
func t18Discover(t *testing.T) {
	t.Helper()
	require := os.Getenv("SCHEMA_REQUIRE_CORPUS") != ""
	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	resolved, err := exec.LookPath(dotnet)
	if err != nil {
		if require {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no dotnet on PATH: %v", err)
		}
		t.Skipf("no dotnet on PATH (set DOTNET): %v", err)
	}
	real, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		real = resolved
	}
	root := filepath.Dir(real)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		if require {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no Roslyn csc.dll under %s", root)
		}
		t.Skipf("no Roslyn csc.dll under %s", root)
	}
	refs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net10.0"))
	if len(refs) == 0 {
		if require {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no .NET ref pack under %s", root)
		}
		t.Skipf("no .NET ref pack under %s", root)
	}
}

// TestFixedRoadmapCSharpT18UnionArms drives the generated C# for the two union
// fixtures. It skips by name without dotnet unless SCHEMA_REQUIRE_CORPUS=1 is
// set, and fails by name when it is (t18Discover does the skip/fail logic locally
// to avoid racing t13Discover's package globals with other t.Parallel t13 tests;
// see t13Discover at fixedroadmap_t13_test.go:127). The driver prints one line per
// arm, "T18 <leaf>/<arm> ok" or "T18 <leaf>/<arm> BAD <why>"; the Go side logs
// every line and fails on any BAD or a missing arm, because t08CSOK alone sees
// only PROBE OK (fixedroadmap_t08_test.go line 506).
func TestFixedRoadmapCSharpT18UnionArms(t *testing.T) {
	t.Parallel()
	t18Discover(t)

	cases := []struct {
		name   string
		schema string
		body   string
		ok     []string
	}{
		{"union-field-arms/cs/valid-data", t18DirectSchema, t18DirectBody, []string{
			"T18 union-field-arms/cs/valid-data/s ok",
			"T18 union-field-arms/cs/valid-data/b ok",
			"T18 union-field-arms/cs/valid-data/a ok",
			"T18 union-field-arms/cs/valid-data/c ok",
		}},
		{"payload-free-arms/cs/valid-data", t18PayloadFreeSchema, t18PayloadFreeBody, []string{
			"T18 payload-free-arms/cs/valid-data/alpha ok",
			"T18 payload-free-arms/cs/valid-data/quiet ok",
			"T18 payload-free-arms/cs/valid-data/signal ok",
		}},
		{"tag-past-the-arm-set", t18PayloadFreeSchema, t18TagPastBody, []string{
			"T18 tag-past-the-arm-set ok",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out := t08RunCS(t, c.schema, c.body)
			for line := range strings.SplitSeq(out, "\n") {
				if line = strings.TrimSpace(line); strings.HasPrefix(line, "T18 ") {
					t.Log(line)
				}
			}
			if strings.Contains(out, " BAD ") {
				t.Fatalf("a round-trip arm went red:\n%s", out)
			}
			for _, want := range c.ok {
				if !strings.Contains(out, want) {
					t.Fatalf("missing %q:\n%s", want, out)
				}
			}
		})
	}
}

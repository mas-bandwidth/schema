package cstable

// The C# (cs) leg's share of the cs/unity verdict fixes: each row below is red
// on the code as it stood before the fix and green after it. The C# legs write
// no release-build write check (docs/SPEC.md §5.1), so a write refusal is a
// debug assert, run in its own debug build; every read verdict is a plain run.
//
//	dep_*         a `| deprecated` variant reads as the field's declared default
//	              (docs/SPEC-TABLES.md §2.15), on the wire and in the text
//	dep_write_*   and its write asserts (debug build)
//	elide_*       a counted array of tables at its born count is elided
//	mapdup_*      a repeated binary map key is damage, not a named refusal
//	mapkey_*      a map key is bounded by its own string(N), not the 255-byte name cap
//	b64_*         a non-canonical base64 value is a named refusal
//	reuse_*       a read into a reused report answers for that read alone

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const csVerdictsSchema = `package probe

enum Kind
{
    Alpha
    Old   | deprecated
    Beta
}

table Root
{
    k    Kind = Beta
    ks   [..3]Kind
}

table Tab
{
    x int32
}

table Born
{
    items [2..4]Tab
}

table Maps
{
    entries map[uint32]int32
    names   map[string(300)]int32
}

table Blob
{
    data bytes(8)
}

fixed table Fx
{
    k Kind = Beta
}
`

// csVerdictsBuild generates the unit, compiles it with the probe's own main
// and returns the directory holding probe.dll. debug turns the write asserts on.
func csVerdictsBuild(t *testing.T, mainSrc string, debug bool) string {
	t.Helper()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")
	runtimeDir, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "src", "Serialize.cs")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the C# runtime checkout is not at %s", runtimeDir)
		}
		t.Skipf("the C# runtime checkout is not at %s", runtimeDir)
	}
	u := unitFrom(t, csVerdictsSchema)
	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatalf("csharp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	main := filepath.Join(dir, "ProbeMain.cs")
	if err := os.WriteFile(main, []byte(mainSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	for _, rt := range []string{"Serialize.cs", "Int128Pair.cs"} {
		if p := filepath.Join(runtimeDir, "src", rt); fileExists(p) {
			sources = append(sources, p)
		}
	}
	csc, refs, runtime := csDotnetToolchain(t)
	args := []string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-out:" + filepath.Join(dir, "probe.dll")}
	if debug {
		args = append(args, "-define:DEBUG")
	}
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	cmd := testproc.Command(t, csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", out)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func csVerdictsRun(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	run := testproc.Command(t, csDotnet(), append([]string{"exec", filepath.Join(dir, "probe.dll")}, args...)...)
	run.Env = csDotnetEnv(dir, dir)
	out, err := run.CombinedOutput()
	return string(out), err
}

func csvLE64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// csvWire frames one form-1 table wire: the body (its terminator included),
// then the id table and its count.
func csvWire(body []byte, ids ...uint64) []byte {
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = append(wire, csvLE64(id)...)
	}
	return append(wire, csvLE64(uint64(len(ids)))...)
}

func TestCSVerdictsReads(t *testing.T) {
	t.Parallel()
	m := tabletext.NewModel(unitFrom(t, csVerdictsSchema))
	rootK := ir.TableFieldWireId(m.Lookup("Root").Fields[0])
	rootKs := ir.TableFieldWireId(m.Lookup("Root").Fields[1])
	entries := ir.TableFieldWireId(m.Lookup("Maps").Fields[0])

	// a deprecated variant on the wire: the id table names it
	oldWire := csvWire([]byte{1, 30, 2, 0}, rootK, ir.TableWireId("Old"))
	betaWire := csvWire([]byte{1, 30, 2, 0}, rootK, ir.TableWireId("Beta"))
	// an array whose one element is the deprecated variant
	oldElemWire := csvWire([]byte{1, 14, 3, 30, 1, 2, 0}, rootKs, ir.TableWireId("Old"))
	// a field this build does not know, and the empty root
	unknownWire := csvWire([]byte{1, 6, 5, 0}, 0xdeadbeef12345678)
	emptyWire := csvWire([]byte{0})
	// two map entries under ONE binary key
	entry := []byte{2, 8, 7, 0, 0, 0, 0}
	payload := append([]byte{13, 2, byte(len(entry))}, entry...)
	payload = append(payload, byte(len(entry)))
	payload = append(payload, entry...)
	dupBody := append([]byte{1, 14, byte(len(payload))}, payload...)
	dupBody = append(dupBody, 0)
	dupWire := csvWire(dupBody, entries, ir.MapKeyWireId)

	// the reference reads them as the spec says
	for name, wire := range map[string][]byte{"old": oldWire, "oldElem": oldElemWire, "beta": betaWire, "empty": emptyWire} {
		var rep tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), wire, &rep); !ok || err != nil || rep.Unknown != 0 {
			t.Fatalf("reference read of %s: ok=%v err=%v %+v", name, ok, err, rep)
		}
	}
	var rep tabletext.Report
	if ok, _ := tablewire.Decode(m, m.New(m.Lookup("Root")), unknownWire, &rep); !ok || rep.Unknown != 1 {
		t.Fatalf("reference read of the unknown field: %+v", rep)
	}
	rep = tabletext.Report{}
	if ok, _ := tablewire.Decode(m, m.New(m.Lookup("Maps")), dupWire, &rep); ok || !strings.HasPrefix(rep.Reason, "wire_damaged") {
		t.Fatalf("reference read of a repeated map key must be damage (wire_damaged), not a named refusal: ok=%v %+v", ok, rep)
	}
	// (the 300-byte map key of mapkey_300_bytes below is the generated Go reader's verdict,
	// which holds a key to its own string(N); the tabletext reference caps it at 255)

	body := fmt.Sprintf(`
byte[] J(string s) { return System.Text.Encoding.UTF8.GetBytes(s); }
int Row(string name, bool good, string why) { if (good) { return 0; } return ProbeLog.Fail(name + ": " + why); }
Probe.TableReport R() { return new Probe.TableReport(); }
byte[] H(string h) { return Convert.FromHexString(h); }
byte[] oldWire = H(%q), betaWire = H(%q), unknownWire = H(%q), emptyWire = H(%q), dupWire = H(%q), oldElemWire = H(%q);

{
    var v = new Probe.Root(); var r = R();
    bool ok = Probe.Schema.RootLoad(v, oldElemWire, r);
    bad += Row("dep_wire_element", ok && v.KsCount == 1 && v.Ks[0] == Probe.Kind.None && r.Unknown == 0, "ok=" + ok + " count=" + v.KsCount + " k0=" + v.Ks[0] + " unknown=" + r.Unknown);
}
{
    var v = new Probe.Root(); var r = R();
    bool ok = Probe.Schema.RootLoad(v, oldWire, r);
    bad += Row("dep_wire_scalar", ok && v.K == Probe.Kind.Beta && r.Unknown == 0, "ok=" + ok + " k=" + v.K + " unknown=" + r.Unknown);
    v = new Probe.Root(); r = R();
    ok = Probe.Schema.RootLoad(v, betaWire, r);
    bad += Row("dep_wire_control", ok && v.K == Probe.Kind.Beta && r.Unknown == 0, "ok=" + ok + " k=" + v.K);
}
{
    var f = new Probe.Fx(); f.K = Probe.Kind.Old; // no write assert in this build: the read is the subject
    byte[] fbuf = new byte[Probe.Schema.FxFixedMeasure(1)];
    long fn = Probe.Schema.FxFixedSave(f, fbuf);
    var fback = new Probe.Fx(); Probe.Schema.TableReset(fback);
    var fr = R(); var plan = new Probe.TableFixedEntry[64];
    long fl = Probe.Schema.FxFixedLoad(fback, fbuf, plan, fr);
    bad += Row("dep_fixed_scalar", fn == fbuf.Length && fl == 1 && fback.K == Probe.Kind.Beta, "save=" + fn + " load=" + fl + " k=" + fback.K);
}
{
    var v = new Probe.Root(); var r = R();
    bool ok = Probe.Schema.RootFromJson(v, J("{\"k\":\"Old\"}"), r);
    bad += Row("dep_json_scalar", ok && v.K == Probe.Kind.Beta && r.Unknown == 0, "ok=" + ok + " k=" + v.K + " unknown=" + r.Unknown);
    v = new Probe.Root(); r = R();
    ok = Probe.Schema.RootFromJson(v, J("{\"k\":\"Alpha\",\"ks\":[\"Alpha\",\"Old\"]}"), r);
    bad += Row("dep_json_element", ok && v.K == Probe.Kind.Alpha && v.KsCount == 2 && v.Ks[0] == Probe.Kind.Alpha && v.Ks[1] == Probe.Kind.None && r.Unknown == 0,
        "ok=" + ok + " count=" + v.KsCount + " ks=" + v.Ks[0] + "," + v.Ks[1] + " unknown=" + r.Unknown);
}
{
    var b = new Probe.Born();
    byte[] buf = new byte[256];
    long n = Probe.Schema.BornSave(b, buf);
    bad += Row("elide_born_default_tables", n == 10 && Probe.Schema.BornMeasure(b) == n, "a born-count array of default tables wrote " + n + " bytes, want 10");
    var back = new Probe.Born(); var r = R();
    bool ok = n > 0 && Probe.Schema.BornLoad(back, buf.AsSpan(0, (int)n), r);
    bad += Row("elide_born_reads_back", ok && back.ItemsCount == 2, "ok=" + ok + " count=" + back.ItemsCount);
    b = new Probe.Born(); b.Items[1].X = 5;
    long m2 = Probe.Schema.BornSave(b, buf);
    var again = new Probe.Born();
    ok = m2 > 10 && Probe.Schema.BornLoad(again, buf.AsSpan(0, (int)m2), r) && again.ItemsCount == 2 && again.Items[1].X == 5;
    bad += Row("elide_born_control_rides", ok, "a born-count array holding a value wrote " + m2 + " bytes, ok=" + ok);
}
{
    var v = new Probe.Maps(); var r = R();
    var verdict = Probe.Schema.MapsLoadVerdict(v, dupWire, r);
    bad += Row("mapdup_wire_is_damage", verdict == Probe.Schema.TableWire.Verdict.Damaged && r.Reason != "duplicate", "verdict=" + verdict + " refused=" + r.Refused + " reason=" + r.Reason);
}
{
    var v = new Probe.Maps(); var r = R();
    bool ok = Probe.Schema.MapsFromJson(v, J("{\"names\":{\"" + new string('a', 300) + "\":1}}"), r);
    bad += Row("mapkey_300_bytes", ok && v.NamesCount == 1 && v.Names[0].KeyLength == 300 && v.Names[0].Value == 1, "ok=" + ok + " refused=" + r.Refused + " reason=" + r.Reason);
}
{
    var v = new Probe.Blob(); var r = R();
    bool ok = Probe.Schema.BlobFromJson(v, J("{\"data\":\"QUI=\"}"), r);
    bad += Row("b64_control_canonical", ok && v.DataLength == 2, "ok=" + ok + " reason=" + r.Reason);
    v = new Probe.Blob(); r = R();
    ok = Probe.Schema.BlobFromJson(v, J("{\"data\":\"QUJ=\"}"), r);
    bad += Row("b64_pad_bit_is_refused", !ok && r.Refused && r.Reason != null && r.Reason.StartsWith("base64_not_canonical"), "ok=" + ok + " refused=" + r.Refused + " reason=" + r.Reason);
    v = new Probe.Blob(); r = R();
    ok = Probe.Schema.BlobFromJson(v, J("{\"data\":\"QUI\"}"), r);
    bad += Row("b64_unpadded_is_refused", !ok && r.Refused && r.Reason != null && r.Reason.StartsWith("base64_not_canonical"), "ok=" + ok + " refused=" + r.Refused + " reason=" + r.Reason);
}
{
    var r = R();
    bool ok = Probe.Schema.RootFromJson(new Probe.Root(), J("{\"zzz\":1}"), r);
    int first = r.Unknown;
    ok = Probe.Schema.RootFromJson(new Probe.Root(), J("{}"), r);
    bad += Row("reuse_json_report", first == 1 && ok && r.Unknown == 0, "first=" + first + " ok=" + ok + " unknown after a clean read=" + r.Unknown);
    r = R();
    ok = Probe.Schema.RootFromJson(new Probe.Root(), J("{\"k\":7}"), r);
    bool refusedFirst = r.Refused;
    ok = Probe.Schema.RootFromJson(new Probe.Root(), J("{}"), r);
    bad += Row("reuse_json_refusal", refusedFirst && ok && !r.Refused && r.Reason == null, "refusedFirst=" + refusedFirst + " ok=" + ok + " refused=" + r.Refused + " reason=" + r.Reason);
}
{
    var r = R();
    bool ok = Probe.Schema.RootLoad(new Probe.Root(), unknownWire, r);
    int first = r.Unknown;
    ok = Probe.Schema.RootLoad(new Probe.Root(), emptyWire, r);
    bad += Row("reuse_load_report", first == 1 && ok && r.Unknown == 0, "first=" + first + " ok=" + ok + " unknown after a clean load=" + r.Unknown);
}
`, hex.EncodeToString(oldWire), hex.EncodeToString(betaWire), hex.EncodeToString(unknownWire), hex.EncodeToString(emptyWire), hex.EncodeToString(dupWire), hex.EncodeToString(oldElemWire))

	dir := csVerdictsBuild(t, fmt.Sprintf(t08Main, body), false)
	out, _ := csVerdictsRun(t, dir)
	if !strings.Contains(out, "PROBE OK") || strings.Contains(out, "FAIL ") {
		t.Fatalf("the driven reader failed:\n%s", out)
	}
}

// TestCSVerdictsDeprecatedWriteAsserts holds the write side: a deprecated
// variant is caught by the leg's debug write assert, the same value without it
// writes.
func TestCSVerdictsDeprecatedWriteAsserts(t *testing.T) {
	t.Parallel()
	const probe = `using System;

static class ProbeMain
{
    static int Main(string[] args)
    {
        var v = new Probe.Root();
        byte[] buf = new byte[1024];
        switch (args[0])
        {
            case "ok": v.K = Probe.Kind.Alpha; break;
            case "scalar_json": case "scalar_wire": v.K = Probe.Kind.Old; break;
            case "fixed": { var f = new Probe.Fx(); f.K = Probe.Kind.Old; Console.WriteLine("CALLING fixed"); long m = Probe.Schema.FxFixedSave(f, new byte[Probe.Schema.FxFixedMeasure(1)]); Console.WriteLine("RETURNED " + m); return 0; }
            case "element_wire": v.Ks[0] = Probe.Kind.Old; v.KsCount = 1; break;
        }
        Console.WriteLine("CALLING " + args[0]);
        long n = args[0].EndsWith("json") ? Probe.Schema.RootToJson(v, buf) : Probe.Schema.RootSave(v, buf);
        Console.WriteLine("RETURNED " + n);
        return 0;
    }
}
`
	dir := csVerdictsBuild(t, probe, true)
	out, err := csVerdictsRun(t, dir, "ok")
	if err != nil || !strings.Contains(out, "RETURNED") {
		t.Fatalf("the control write did not return: %v\n%s", err, out)
	}
	for _, mode := range []string{"scalar_json", "scalar_wire", "element_wire", "fixed"} {
		t.Run("dep_write_"+mode, func(t *testing.T) {
			out, err := csVerdictsRun(t, dir, mode)
			if err == nil || strings.Contains(out, "RETURNED") || !strings.Contains(out, "Assertion failed.") {
				t.Fatalf("a deprecated variant did not hit the debug write assert (%s): err=%v\n%s", mode, err, out)
			}
		})
	}
}

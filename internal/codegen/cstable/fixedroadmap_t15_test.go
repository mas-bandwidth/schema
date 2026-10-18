package cstable

// TestFixedRoadmapCSharpT15GuardChain is the cs leg's row of the card
// the-guard-chain-answers-the-outer-tag: docs/roadmap.sexp cs/W16 ("nested
// union answers outer tag") and cs/R28 ("the guard chain: the plan entry
// carries offset+count into a pool of (guard, arg, argw) links, outermost
// first, the only bound the layout's 64; an arm inside an arm answers to the
// OUTER tag"). Law: docs/FIXED-FORM-ALGORITHM.md §4.1.
//
// THE FIXTURE IS THE FIVE LEGS' ONE (t15GuardChainSchema, the same text in
// gotable, javatable, jstable and darttable): a union whose arm is a union
// whose arm is a union whose arm is a table, three tags deep on one path. The
// generated C# is compiled with the SDK's csc and run; the deepest leaf must
// land as written and must NOT land while any one of the three tags names
// another arm, on the IDENTITY plan and on a plan COMPILED from the layout.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// t15RunCS generates the fixture's C# — the packet form, which declares the arm
// types (LeafA, LeafB) and the unions' classes the table form reads through,
// and the table form over it — compiles it with the SDK's own csc
// (csDotnetToolchain) and runs the driven reader, failing with its words on
// anything but PROBE OK. It fails, never skips.
func t15RunCS(t *testing.T, schemaSrc, body string) {
	t.Helper()
	// THE PACKET FORM READS THROUGH THE SIBLING C# RUNTIME (serialize.cs), the
	// checkout t06WideRun compiles against: missing, it is a skip by name, and a
	// failure under SCHEMA_REQUIRE_CORPUS — never faked.
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
	u := unitFrom(t, schemaSrc)
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
	if err := os.WriteFile(main, []byte(fmt.Sprintf(t08Main, body)), 0o600); err != nil {
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
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", out)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(csDotnet(), "exec", filepath.Join(dir, "probe.dll"))
	run.Env = csDotnetEnv(dir, dir)
	out, _ := run.CombinedOutput()
	if strings.Contains(string(out), "FAIL ") || !strings.Contains(string(out), "PROBE OK") {
		t.Fatalf("the driven reader failed:\n%s", out)
	}
}

// t15GuardChainSchema is THE fixture of the five legs' T15GuardChain tests.
// The record body is [0] Outer's tag, [1] Middle's, [2] Inner's, [3..7) x.
const t15GuardChainSchema = `package probe

type LeafA { x int32 = 0 }
type LeafB { y int32 = 0 }
union Inner
{
    a LeafA
    b LeafB
}
union Middle
{
    i Inner
    n LeafB
}
union Outer
{
    m Middle
    p LeafB
}
fixed table Root
{
    o Outer
    mid Middle
    inn Inner
}
`

func TestFixedRoadmapCSharpT15GuardChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/W16", t15CSW16},
		{"cs/R28", t15CSR28},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// t15CSProbe is the driven reader: write the three-deep value, then read it on
// the identity plan and on the compiled plan, clean and with each tag poisoned
// in turn. chain asserts the deepest entry's offset+count into the pool and its
// links OUTERMOST FIRST, on both plans.
func t15CSProbe(chain bool) string {
	check := ""
	if chain {
		check = `
int CheckChain(Probe.TableFixedEntry[] entries, int count, byte[] pool, string what)
{
    for (int i = 0; i < count; ++i)
    {
        Probe.TableFixedEntry e = entries[i];
        if (e.GuardCount != 3) { continue; }
        for (int j = 0; j < 3; ++j)
        {
            int at = (int)e.GuardIndex + j * Probe.Schema.TableFixedWire.GuardLinkBytes;
            if (at + Probe.Schema.TableFixedWire.GuardLinkBytes > pool.Length) { return ProbeLog.Fail(what + ": the chain's offset+count point outside the pool"); }
            uint g = BitConverter.ToUInt32(pool, at);
            ulong a = BitConverter.ToUInt64(pool, at + 4);
            if (g != (uint)j || a != 1 || pool[at + 12] != 1) { return ProbeLog.Fail(what + ": link " + j + " is (" + g + ", " + a + ", " + pool[at + 12] + "), so the chain is not OUTERMOST FIRST"); }
        }
        if (e.Guard != 2u || e.Guard2 != 1u) { return ProbeLog.Fail(what + ": the two lanes are not the chain's innermost two"); }
        return 0;
    }
    return ProbeLog.Fail(what + ": no plan entry carries a three-link chain (offset+count into the pool)");
}
`
	}
	return check + `
Probe.Root v = new Probe.Root();
Probe.Schema.TableReset(v);
v.O.Type = Probe.OuterType.M;
v.O.M.Type = Probe.MiddleType.I;
v.O.M.I.Type = Probe.InnerType.A;
v.O.M.I.A.X = 42;
byte[] buf = new byte[Probe.Schema.RootFixedMeasure(1)];
if (Probe.Schema.RootFixedSave(v, buf) != buf.Length) { bad += ProbeLog.Fail("save did not fill the measure"); }
int rec = 16 + 4 + Probe.Schema.RootFixedLayout.Length + 8;
if (buf[rec] != 1 || buf[rec + 1] != 1 || buf[rec + 2] != 1) { bad += ProbeLog.Fail("the writer did not put the three tags where the chain says"); }
` + map[bool]string{true: `bad += CheckChain(Probe.Schema.RootFixedPlan.Entries, Probe.Schema.RootFixedPlan.Count, Probe.Schema.RootFixedGuardPool, "identity");
`, false: ""}[chain] + `
for (int poison = 0; poison <= 3; ++poison)
{
    byte[] file = (byte[])buf.Clone();
    if (poison > 0) { file[rec + poison - 1] = 2; }
    Probe.Root back = new Probe.Root();
    Probe.Schema.TableReset(back);
    Probe.TableReport r = new Probe.TableReport();
    long n = Probe.Schema.RootFixedLoad(back, file, new Probe.TableFixedEntry[1 << 12], r);
    if (n != 1 || r.Refused) { bad += ProbeLog.Fail("identity poison " + poison + ": n=" + n + " reason=" + r.Reason); continue; }
    int x = back.O.M.I.A.X;
    if (poison == 0 && x != 42) { bad += ProbeLog.Fail("identity: the record as written did not land its deepest leaf"); }
    if (poison > 0 && x == 42) { bad += ProbeLog.Fail("identity: the deepest leaf landed while tag " + (poison - 1) + " names another arm"); }
}

Probe.TableFixedEntry[] cplan = new Probe.TableFixedEntry[1 << 12];
if (!Probe.Schema.TableFixedWire.ParseLayout(Probe.Schema.RootFixedLayout, out Probe.TableFixedLayoutView parsed)) { bad += ProbeLog.Fail("the layout does not parse"); }
int made = Probe.Schema.TableFixedWire.Compile(parsed, Probe.Schema.RootFixedLayout, Probe.Schema.RootFixedDst, cplan, new Probe.TableReport(), out int split);
if (made <= 0) { bad += ProbeLog.Fail("the compiled plan refused a three-link chain: " + made); }
byte[] cbytes = System.Runtime.InteropServices.MemoryMarshal.AsBytes<Probe.TableFixedEntry>(cplan).ToArray();
` + map[bool]string{true: `if (made > 0) { bad += CheckChain(cplan, made, cbytes, "compiled"); }
`, false: ""}[chain] + `
for (int poison = 0; made > 0 && poison <= 3; ++poison)
{
    byte[] body = new byte[buf.Length - rec];
    Array.Copy(buf, rec, body, 0, body.Length);
    if (poison > 0) { body[poison - 1] = 2; }
    Probe.Root back = new Probe.Root();
    Probe.Schema.TableReset(back);
    Probe.TableReport r = new Probe.TableReport();
    Probe.Schema.TableFixedRun(new ReadOnlySpan<Probe.TableFixedEntry>(cplan, 0, made), body, back, r, cbytes);
    if (r.Refused) { bad += ProbeLog.Fail("compiled poison " + poison + ": " + r.Reason); continue; }
    int x = back.O.M.I.A.X;
    if (poison == 0 && x != 42) { bad += ProbeLog.Fail("compiled: the record as written did not land its deepest leaf"); }
    if (poison > 0 && x == 42) { bad += ProbeLog.Fail("compiled: the deepest leaf landed while tag " + (poison - 1) + " names another arm"); }
}
`
}

// cs/W16 — an arm inside an arm answers to the OUTER tag: the leaf lands only
// when every enclosing tag holds, on both plans.
func t15CSW16(t *testing.T) {
	t15RunCS(t, t15GuardChainSchema, t15CSProbe(false))
}

// cs/R28 — the guard chain: the deepest entry carries offset+count into the
// pool, its links OUTERMOST FIRST, on both plans, and the leaf answers to all
// three tags. The emitted header publishes the pool.
func t15CSR28(t *testing.T) {
	_, _, src := t08Emit(t, t15GuardChainSchema)
	if !strings.Contains(src, "public static readonly byte[] RootFixedGuardPool = new byte[] {") {
		t.Fatal("R28: the emitted C# publishes no guard pool")
	}
	if !strings.Contains(src, fmt.Sprintf(", %du, %du),", 0, 3)) {
		t.Fatal("R28: no published identity-plan entry carries offset+count (0, 3) into the pool")
	}
	t15RunCS(t, t15GuardChainSchema, t15CSProbe(true))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

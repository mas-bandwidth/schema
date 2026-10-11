package cstable

// fixedroadmap_t06 — the cs leg's bounds/ordinals/normalization/array-bounds
// rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed
// Tables"): one subtest per task this card proves, each asserting the clause
// the task's own title states, read off the C# this leg emits or off the ir
// walk it renders. Law: docs/FIXED-FORM-ALGORITHM.md §4.6 (the read-side
// bounds pass), §5.2 (the static plan's Arg/ArgW lanes), §5.8 rows 4, 7, 12,
// and docs/SPEC-TABLES.md §3.4 ("it rides as the float, not as a quantized
// index"). The versioning tests that already hold C1/C2/R11/R30 end to end are
// named in the card's verdicts; this file carries the direct assertion each of
// the weak and owed clauses lacked.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCSharpT06Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/C7", t06C7},
		{"cs/R30", t06R30},
		{"cs/C9", t06C9},
		{"cs/C10", t06C10},
		{"cs/R31", t06R31},
		{"cs/C12", t06C12},
		{"cs/C13", t06C13},
		{"cs/R17", t06R17},
		{"cs/C1", t06C1},
		{"cs/C2", t06C2},
		{"cs/R11", t06R11},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06Has is a required substring, named by rule.
func t06Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted C# does not carry %q", what, want)
	}
}

// t06C7: cs/C7 [verify] "ranged scalar clamp" — a ranged scalar's declared min
// and max are held by the read-side bounds pass and every clamp counts
// (docs/FIXED-FORM-ALGORITHM.md §4.6).
func t06C7(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    x int32 | min = -5, max = 5
}
`)
	body := csFn(src, "public static void TFixedClampBody(")
	if body == "" {
		t.Fatal("C7: the ranged scalar carries no bounds pass")
	}
	t06Has(t, body, "if (value.X < -5) { value.X = -5; clamped++; }", "C7 the low end clamps and counts")
	t06Has(t, body, "else if (value.X > 5) { value.X = 5; clamped++; }", "C7 the high end clamps and counts")
}

// t06R30: cs/R30 [owed] "compressed float rides as the float" — in the FIXED
// form min, max and resolution are DEFINITIONS in the digest (SPEC-TABLES
// §3.4), the step is the digest's 'Q' row, and the value on the wire is the
// float32 itself, not a quantized index.
func t06R30(t *testing.T) {
	u := unitFrom(t, `package probe
fixed table T {
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	roots := ir.TableFixedRoots(u)
	if len(roots) != 1 {
		t.Fatalf("R30: the fixture declares %d fixed roots, want one", len(roots))
	}
	d := ir.TableFixedDefinitionsDigest(roots[0])
	if len(d) != 26 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R30: a compressed float's digest = % x, want 'R' min max 'Q' resolution", d)
	}
	src := generateCS(t, `package probe
fixed table T {
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.Q = BitConverter.UInt32BitsToSingle((uint)v)",
		"R30 the compressed field rides as the float32 bits, never a quantized index")
	t06Has(t, csFn(src, "public static void TFixedClampBody("), "value.Q",
		"R30 the reader's own min/max hold the landed float")
}

// t06C9: cs/C9 [verify] "union tag past arms → None" — a tag past the writer's
// arm count names no arm: it lands None (the zero tag) and counts, on both
// plans (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.8 row 12).
func t06C9(t *testing.T) {
	src := generateCS(t, `package probe
union Pick {
    a int32
    b int32
}
fixed table T {
    p Pick
}
`)
	body := csFn(src, "public static void TFixedClampBody(")
	if body == "" {
		t.Fatal("C9: the union carries no bounds pass")
	}
	t06Has(t, body, "if ((ulong)value.P.Type > 2) { value.P.Type = (PickType)0; clamped++; }",
		"C9 a union tag past the arm count lands None and counts")

	// A tag past the last arm of a union whose tag is two bytes wide: 300 names
	// no arm of the 257, and lands None on both plans, never an arm.
	for _, plan := range []string{"identity", "compiled"} {
		t.Run("forged-tag-roundtrip-"+plan, func(t *testing.T) {
			out, err := t06WideRun(t)
			csVersionAssert(t, out, err, "forged/"+plan)
		})
	}
}

// t06C10: cs/C10 [verify] "enum ordinal past top → None" — an ordinal past the
// enum's top value lands None and counts (docs/FIXED-FORM-ALGORITHM.md §4.6).
func t06C10(t *testing.T) {
	src := generateCS(t, `package probe
enum Tier { bronze, silver, gold }
fixed table T {
    e Tier
}
`)
	body := csFn(src, "public static void TFixedClampBody(")
	if body == "" {
		t.Fatal("C10: the enum carries no bounds pass")
	}
	t06Has(t, body, "if ((ulong)value.E > 3) { value.E = (Tier)0; clamped++; }",
		"C10 an enum ordinal past the top lands None and counts")
}

// t06R31: cs/R31 [weak] "full-width lanes: arg/arg2 full width in the emitted
// static plan with the compare at the tag's own width; a 64-bit ordinal
// temporary; the remap table as long as the WRITER's variant count". The guard
// carries ArgW and the run loop compares at that width (§5.2 EMIT kind 15,
// §5.8 row 7); the ordinal op reads through a 64-bit temporary with an 8-byte
// case; the remap table's length word is the writer's variant count and the
// op bounds the ordinal against it.
func t06R31(t *testing.T) {
	if !strings.Contains(tableFixedRuntimeTypes, "public byte ArgW;") {
		t.Error("R31: TableFixedEntry has no full-width guard lane ArgW")
	}
	t06Has(t, tableFixedWireSource, "TagAt(src, p.Guard, p.ArgW) != p.Arg",
		"R31 the guard compares at the tag's own width, never as a prefix")
	if !strings.Contains(tableFixedWireSource, "public static ulong TagAt(") {
		t.Error("R31: the runtime has no full-width tag read")
	}
	at := strings.Index(tableFixedWireSource, "case Ordinal:")
	if at < 0 {
		t.Fatal("R31: the run loop has no ordinal op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Widen:"); end > 0 {
		op = op[:end]
	}
	if !strings.Contains(op, "ulong raw = 0;") {
		t.Error("R31: the ordinal op reads through a 32-bit temporary, not a 64-bit one")
	}
	if !strings.Contains(op, "p.Size == 8") {
		t.Error("R31: the ordinal op has no eight-byte case")
	}
	if !strings.Contains(op, "ushort count = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes);") {
		t.Error("R31: the remap table's length word is not read as the writer's variant count")
	}
	if !strings.Contains(op, "raw <= count") {
		t.Error("R31: the ordinal is not bounded by the remap table's length word")
	}

	var b strings.Builder
	b.WriteString("package probe\n\nunion Wide {\n")
	for i := range 256 {
		fmt.Fprintf(&b, "    a%d int32\n", i)
	}
	b.WriteString("}\n\nfixed table Root { pick Wide }\n")
	wide := generateCS(t, b.String())
	if !guardedPlanHasArgW(wide, 2) {
		t.Error("R31: a 256-arm union's emitted static plan did not carry the full-width tag lane ArgW=2")
	}

	// THE VALUE, NOT ONLY THE WIDTH: ArgW=2 says the compare reads two bytes, and
	// it is the ordinal beside it (arg) that the compare is made against. The
	// 256th arm's ordinal truncated to a byte (arg 0 beside ArgW 2) while the
	// width stayed right, so a width-only assertion stayed green. A 257-arm
	// union is written and read back on the emitted static plan and on a compiled
	// lineage plan, and ordinals 255, 256 and 257 must land as themselves.
	for _, plan := range []string{"identity", "compiled"} {
		t.Run("ordinal-roundtrip-"+plan, func(t *testing.T) {
			out, err := t06WideRun(t)
			csVersionAssert(t, out, err, "ordinals/"+plan)
		})
	}
}

// t06C12: cs/C12 [weak] "bool byte != 0" — the wire byte is normalised to a
// bool by `!= 0`, so every nonzero byte is true (docs/FIXED-FORM-ALGORITHM.md
// §4.5).
func t06C12(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    ok bool
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.Ok = v != 0",
		"C12 a bool byte is true for every nonzero value")
}

// t06C13: cs/C13 [weak] "present flag byte != 0" — an optional's present byte is
// normalised the same way (docs/FIXED-FORM-ALGORITHM.md §4.5).
func t06C13(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    maybe ?int32
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.MaybePresent = v != 0",
		"C13 a present flag byte is true for every nonzero value")
}

// t06R17: cs/R17 [verify] "a bool or present byte that is not 0/1 normalises
// and counts nothing" — the normalisation is `!= 0` and the slot is a plain
// setter with no report, so no counter moves (docs/FIXED-FORM-ALGORITHM.md
// §4.5, §5.4).
func t06R17(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    ok bool
    maybe ?int32
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.Ok = v != 0", "R17 the bool byte normalises")
	t06Has(t, src, "setRaw: (t, v) => t.MaybePresent = v != 0", "R17 the present byte normalises")
	for _, bad := range []string{"t.Ok = (v == 1)", "t.MaybePresent = (v == 1)", "t.Ok ? 1 : 0"} {
		if strings.Contains(src, bad) {
			t.Errorf("R17: the byte is compared to 1 rather than normalised from any nonzero value: %q", bad)
		}
	}
	if strings.Contains(src, "= v != 0, rep") {
		t.Error("R17: the bool/present normalisation carries a report and can count")
	}
}

// t06C1: cs/C1 [weak] "count clamp v<0" — the count op clamps a negative live
// count to zero and counts (docs/FIXED-FORM-ALGORITHM.md §5.3 step 11).
func t06C1(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case Count:")
	if at < 0 {
		t.Fatal("C1: the run loop has no count op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Text:"); end > 0 {
		op = op[:end]
	}
	t06Has(t, op, "if (v < 0) { v = 0; if (report != null) report.Clamped++; }",
		"C1 a negative count clamps to zero and counts")
}

// t06C2: cs/C2 [weak] "count clamp v>Max" — a count past the plan entry's bound
// clamps to that bound and counts (docs/FIXED-FORM-ALGORITHM.md §5.3 step 11).
func t06C2(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case Count:")
	if at < 0 {
		t.Fatal("C2: the run loop has no count op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Text:"); end > 0 {
		op = op[:end]
	}
	t06Has(t, op, "else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }",
		"C2 a count past the bound clamps to it and counts")
}

// t06R11: cs/R11 [owed] "the plan carries the WRITER's bounds, per plan; the
// hostile pass runs against the plan's bounds" — the bound is the plan entry's
// size, and a compiled plan is one entry per lineage peer, so the bound travels
// with the plan and never with the reader (docs/FIXED-FORM-ALGORITHM.md §5.2,
// §5.8 row 4).
func t06R11(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case Count:")
	if at < 0 {
		t.Fatal("R11: the run loop has no count op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Text:"); end > 0 {
		op = op[:end]
	}
	t06Has(t, op, "else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }",
		"R11 the count clamps against the plan entry's own size, the writer's bound")

	u := unitFrom(t, `package probe
fixed table T {
    vals [..4]int32
}
`)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R11: FixedLineageOf has no entry for T")
	}
	older := FixedLineageEntry{Wire: own.Wire ^ 0x5a5a5a5a, Layout: own.Layout, Record: own.Record}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R11: GenerateLineage: %v", err)
	}
	src := string(files[t02File])
	t06Has(t, strings.Join(strings.Fields(src), " "),
		"TableFixedWire.LineagePlans(TFixedKnown, TFixedLayout, TFixedDst, TFixedHash)",
		"R11 one plan per lineage peer, so the writer's bound travels with the peer's plan")
}

// ---- the 257-arm round trip (§5.2 EMIT kind 15, §5.8 row 7) -----------------

const t06WideArms = 257

var (
	t06WideOnce sync.Once
	t06WideOut  []byte
	t06WideErr  error
)

// t06WideRun generates the 257-arm unit, writes records through the generated
// writer and reads them back on both plans, in one dotnet run shared by the
// subtests that read its rows. A missing runtime checkout or SDK is a skip and
// is a failure under SCHEMA_REQUIRE_CORPUS, which `make tables-cs-versioning`
// sets: a target that needs a sibling runtime is named as not run, never faked.
func t06WideRun(t *testing.T) ([]byte, error) {
	t.Helper()
	runtime, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	missing := ""
	if _, err := os.Stat(filepath.Join(runtime, "src", "Serialize.cs")); err != nil {
		missing = "the C# runtime checkout is not at " + runtime
	} else if _, err := exec.LookPath(dotnet); err != nil {
		missing = "no dotnet SDK: " + err.Error()
	}
	if missing != "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s", missing)
		}
		t.Skip(missing)
	}
	t06WideOnce.Do(func() { t06WideOut, t06WideErr = t06WideBuildAndRun(runtime, dotnet) })
	return t06WideOut, t06WideErr
}

func t06WideBuildAndRun(runtime, dotnet string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "t06wide")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var schema strings.Builder
	schema.WriteString("package probe\n\nunion Wide {\n")
	for i := range t06WideArms {
		fmt.Fprintf(&schema, "    a%d int32\n", i)
	}
	schema.WriteString("}\n\nfixed table Root {\n    pick Wide\n    seq int32\n}\n")
	u, err := t06UnitOf(schema.String())
	if err != nil {
		return nil, err
	}
	own, ok := FixedLineageOf(u, "Root")
	if !ok {
		return nil, fmt.Errorf("FixedLineageOf has no entry for Root")
	}
	// ONE OLDER PEER with the same layout and another hash: the reader selects it
	// by the file's hash and reads under the COMPILED plan, not the static one.
	older := FixedLineageEntry{Wire: own.Wire ^ 0x5a5a5a5a, Layout: own.Layout, Record: own.Record}
	files, err := csharp.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Root": {older}})
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	for name, data := range files {
		path := filepath.Join(dir, "gen", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
	}

	var set, get strings.Builder
	for i := range t06WideArms {
		fmt.Fprintf(&set, "                case %d: p.A%d = v; break;\n", i+1, i)
		if i > 0 {
			get.WriteString(", ")
		}
		fmt.Fprintf(&get, "p.A%d", i)
	}
	probe := fmt.Sprintf(`using System;
using System.Buffers.Binary;

namespace Probe
{
    static class WideProbe
    {
        const int Arms = %d;

        static void SetArm(Wide p, int ord, int v)
        {
            switch (ord)
            {
%s                default: throw new Exception("no arm " + ord);
            }
        }

        static int[] ArmValues(Wide p) { return new int[] { %s }; }

        // One record written through the generated writer and read back. A tag
        // that names an arm carries 1000+ordinal in that arm; a forged tag
        // carries nothing. The peer read patches the file's hash and the record's
        // hash to the older peer's, so the reader selects the compiled plan.
        static string Once(ulong tag, bool arm, bool peer)
        {
            Root w = new Root();
            w.Seq = 41;
            w.Pick.Type = (WideType)tag;
            if (arm) { SetArm(w.Pick, (int)tag, 1000 + (int)tag); }
            byte[] buf = new byte[Schema.RootFixedMeasure(1)];
            long wrote = Schema.RootFixedSave(w, buf);
            if (wrote != buf.Length) { return "tag " + tag + ": wrote " + wrote + " of " + buf.Length; }
            if (peer)
            {
                ulong oldHash = Schema.RootFixedHash ^ 0x5a5a5a5aUL;
                BinaryPrimitives.WriteUInt64LittleEndian(buf.AsSpan(Schema.TableFixedWire.HashAt), oldHash);
                uint layoutBytes = BinaryPrimitives.ReadUInt32LittleEndian(buf.AsSpan(Schema.TableFixedWire.HeaderBytes));
                BinaryPrimitives.WriteUInt64LittleEndian(buf.AsSpan(Schema.TableFixedWire.HeaderBytes + 4 + (int)layoutBytes), oldHash);
            }
            Root[] back = new Root[] { new Root() };
            TableReport rep = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[65536];
            long n = Schema.RootFixedLoad(back, buf, plan, rep);
            if (n != 1 || rep.Refused || rep.Malformed)
            {
                return "tag " + tag + ": read n=" + n + " reason=" + rep.Reason + " malformed=" + rep.Malformed;
            }
            if (back[0].Seq != 41) { return "tag " + tag + ": the scalar after the union is " + back[0].Seq; }
            int[] vals = ArmValues(back[0].Pick);
            if (!arm)
            {
                if ((ulong)back[0].Pick.Type != 0) { return "tag " + tag + " names no arm and must land None, not " + (ulong)back[0].Pick.Type; }
                for (int i = 0; i < Arms; ++i) { if (vals[i] != 0) { return "tag " + tag + " names no arm but arm " + (i + 1) + " landed " + vals[i]; } }
                return null;
            }
            if ((ulong)back[0].Pick.Type != tag) { return "tag " + tag + " landed as ordinal " + (ulong)back[0].Pick.Type; }
            for (int i = 0; i < Arms; ++i)
            {
                int want = (i + 1) == (int)tag ? 1000 + (int)tag : 0;
                if (vals[i] != want) { return "tag " + tag + ": arm " + (i + 1) + " holds " + vals[i] + ", want " + want; }
            }
            return null;
        }

        static int Group(string row, ulong[] tags, bool arm, bool peer)
        {
            int bad = 0;
            foreach (ulong tag in tags)
            {
                string why = Once(tag, arm, peer);
                if (why != null) { bad += ProbeLog.Fail(row, why); }
            }
            if (bad == 0) { Console.WriteLine("ROW " + row + ": ok"); }
            return bad;
        }

        public static int Run()
        {
            ulong[] ordinals = new ulong[] { 1, 2, 128, 255, 256, 257 };
            ulong[] forged = new ulong[] { 258, 300, 65535 };
            int bad = 0;
            bad += Group("ordinals/identity", ordinals, true, false);
            bad += Group("ordinals/compiled", ordinals, true, true);
            bad += Group("forged/identity", forged, false, false);
            bad += Group("forged/compiled", forged, false, true);
            return bad;
        }
    }
}

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
        try { return Probe.WideProbe.Run() == 0 ? 0 : 1; }
        catch (Exception e) { Console.WriteLine("ROW harness: FAILED " + e.ToString()); return 1; }
    }
}
`, t06WideArms, set.String(), get.String())
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(probe), 0o600); err != nil {
		return nil, err
	}
	proj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <AssemblyName>cswide</AssemblyName>
    <NoWarn>CS0162;CS0168;CS0219;CS8600;CS8602;CS8604;CS8625</NoWarn>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
    <Compile Include="gen/**/*.cs" />
    <Compile Include="%s/src/Serialize.cs" />
    <Compile Include="%s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, runtime, runtime)
	if err := os.WriteFile(filepath.Join(dir, "cswide.csproj"), []byte(proj), 0o600); err != nil {
		return nil, err
	}
	cmd := exec.Command(dotnet, "run", "--project", dir, "-v", "q", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	return cmd.CombinedOutput()
}

func t06UnitOf(src string) (*ir.Unit, error) {
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, fmt.Errorf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		return nil, fmt.Errorf("check: %v", cerrs[0])
	}
	return u, nil
}

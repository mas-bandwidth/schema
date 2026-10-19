package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func csCompileFiles(t *testing.T, files map[string][]byte, mainBody string) (string, error) {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(fmt.Sprintf(t08Main, mainBody)), 0o600); err != nil {
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
	sources, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	args := []string{"exec", cscs[0], "-nologo", "-unsafe", "-out:" + filepath.Join(dir, "probe.dll")}
	args = append(args, sources...)
	for _, r := range refs {
		dlls, _ := filepath.Glob(filepath.Join(r, "*.dll"))
		for _, dll := range dlls {
			args = append(args, "-r:"+dll)
		}
	}
	out, err := exec.Command(dotnet, args...).CombinedOutput()
	return string(out), err
}

func TestCsFixedTablesPrivate(t *testing.T) {
	t.Parallel()
	_, _, cs := t08Emit(t, t08Flat)
	norm := t02Norm(cs)

	// 1. None of the six tables may be exposed as a public static readonly array field.
	forbiddenArrays := []string{
		"public static readonly byte[] TFixedLayout",
		"public static readonly TableFixedDst[] TFixedDst",
		"public static readonly TableFixedFill[] TFixedCover",
		"public static readonly byte[] TFixedGuardPool",
		"public static readonly TableFixedKnownLayout[] TFixedKnown",
		"public static readonly TableFixedLineagePlan[] TFixedLineagePlans",
	}
	for _, f := range forbiddenArrays {
		if strings.Contains(norm, f) {
			t.Errorf("emitted source exposes mutable array field %q", f)
		}
	}

	// 2. Each of the six tables has a private static readonly array backing field
	// and a read-only public property.
	requiredDecls := []string{
		"private static readonly byte[] _TFixedLayout = new byte[]",
		"public static ReadOnlySpan<byte> TFixedLayout => _TFixedLayout;",
		"private static readonly TableFixedDst[] _TFixedDst = new TableFixedDst[]",
		"public static System.Collections.Generic.IReadOnlyList<TableFixedDst> TFixedDst => System.Array.AsReadOnly(_TFixedDst);",
		"private static readonly TableFixedFill[] _TFixedCover = new TableFixedFill[]",
		"public static System.Collections.Generic.IReadOnlyList<TableFixedFill> TFixedCover => System.Array.AsReadOnly(_TFixedCover);",
		"private static readonly byte[] _TFixedGuardPool = new byte[]",
		"public static ReadOnlySpan<byte> TFixedGuardPool => _TFixedGuardPool;",
		"private static readonly TableFixedKnownLayout[] _TFixedKnown = new TableFixedKnownLayout[]",
		"public static System.Collections.Generic.IReadOnlyList<TableFixedKnownLayout> TFixedKnown => System.Array.AsReadOnly(_TFixedKnown);",
		"private static readonly TableFixedLineagePlan[] _TFixedLineagePlans = TableFixedWire.LineagePlans(TFixedKnown, TFixedLayout, TFixedDst, TFixedHash);",
		"public static System.Collections.Generic.IReadOnlyList<TableFixedLineagePlan> TFixedLineagePlans => System.Array.AsReadOnly(_TFixedLineagePlans);",
	}
	for _, req := range requiredDecls {
		if !strings.Contains(norm, req) {
			t.Errorf("emitted source missing required declaration %q", req)
		}
	}

	// 3. Element types TableFixedDst and TableFixedFill are readonly structs,
	// and TableFixedKnownLayout and TableFixedLineagePlan do not expose mutable arrays.
	requiredRuntime := []string{
		"public readonly struct TableFixedDst",
		"public struct TableFixedFill",
		"public byte[] Layout => _layout != null ? (byte[])_layout.Clone() : Array.Empty<byte>();",
		"public TableFixedEntry[] Entries => _entries != null ? (TableFixedEntry[])_entries.Clone() : Array.Empty<TableFixedEntry>();",
	}
	for _, req := range requiredRuntime {
		if !strings.Contains(norm, req) {
			t.Errorf("emitted runtime missing required immutable surface %q", req)
		}
	}
	forbiddenRuntime := []string{
		"public readonly byte[] Layout;",
		"public TableFixedEntry[] Entries { get; set; }",
	}
	for _, f := range forbiddenRuntime {
		if strings.Contains(norm, f) {
			t.Errorf("emitted runtime still exposes mutable array field %q", f)
		}
	}

	if os.Getenv("SCHEMA_SLOW") == "" {
		return
	}

	// 4. Positive C# runtime check: read properties and run Save/Load round-trip.
	t08CSOK(t, t08Flat, `
if (Probe.Schema.TFixedLayout.Length != Probe.Schema.TFixedLayoutBytes) { bad += ProbeLog.Fail("TFixedLayout.Length mismatch"); }
if (Probe.Schema.TFixedDst.Count == 0) { bad += ProbeLog.Fail("TFixedDst.Count == 0"); }
if (Probe.Schema.TFixedCover.Count == 0) { bad += ProbeLog.Fail("TFixedCover.Count == 0"); }
if (Probe.Schema.TFixedGuardPool.Length < 0) { bad += ProbeLog.Fail("TFixedGuardPool.Length < 0"); }
if (Probe.Schema.TFixedKnown.Count == 0) { bad += ProbeLog.Fail("TFixedKnown.Count == 0"); }
if (Probe.Schema.TFixedLineagePlans.Count == 0) { bad += ProbeLog.Fail("TFixedLineagePlans.Count == 0"); }

byte[] fakeLayout = new byte[] { 1, 2, 3 };
var kl = new Probe.TableFixedKnownLayout(0x1234UL, fakeLayout, 16);
fakeLayout[0] = 99;
if (kl.Layout[0] != 1) { bad += ProbeLog.Fail("TableFixedKnownLayout did not defensively clone layout bytes"); }
byte[] gotLayout = kl.Layout;
gotLayout[0] = 77;
if (kl.Layout[0] != 1) { bad += ProbeLog.Fail("TableFixedKnownLayout.Layout getter did not defensively clone"); }

var t = new Probe.T { X = 42, Y = -7 };
byte[] wire = new byte[Probe.Schema.TFixedMeasure(1)];
long wrote = Probe.Schema.TFixedSave(t, wire);
if (wrote != wire.Length) { bad += ProbeLog.Fail("TFixedSave failed"); }
var dst = new Probe.T();
var plan = new Probe.TableFixedEntry[16];
var rep = new Probe.TableReport();
long read = Probe.Schema.TFixedLoad(dst, wire, plan, rep);
if (read != 1 || dst.X != 42 || dst.Y != -7) { bad += ProbeLog.Fail("TFixedLoad round-trip mismatch"); }
`)

	// 5. Negative C# compilation checks: mutating any of the 6 surfaces must be a compile error.
	u := unitFrom(t, t08Flat)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	mutations := []struct {
		name string
		stmt string
	}{
		{"TFixedLayout", "Probe.Schema.TFixedLayout[0] = 0;"},
		{"TFixedDst", "Probe.Schema.TFixedDst[0] = default;"},
		{"TFixedCover", "Probe.Schema.TFixedCover[0] = default;"},
		{"TFixedGuardPool", "Probe.Schema.TFixedGuardPool[0] = 0;"},
		{"TFixedKnown", "Probe.Schema.TFixedKnown[0] = default;"},
		{"TFixedLineagePlans", "Probe.Schema.TFixedLineagePlans[0] = null;"},
		{"_TFixedLayout", "Probe.Schema._TFixedLayout[0] = 0;"},
		{"_TFixedDst", "Probe.Schema._TFixedDst[0] = default;"},
	}
	for _, m := range mutations {
		out, err := csCompileFiles(t, files, m.stmt)
		if err == nil {
			t.Errorf("mutation %s (%s) unexpectedly compiled; output:\n%s", m.name, m.stmt, out)
		}
	}
}

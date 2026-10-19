package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// csPrivateToolchain locates dotnet and the Roslyn toolchain. It skips by name
// when the toolchain is absent, and fails by name under SCHEMA_REQUIRE_CORPUS=1,
// the same discipline the corpus harnesses use: a promised toolchain that is
// not here is a failure, never a silent pass.
func csPrivateToolchain(t *testing.T) {
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

// The six tables' names are the rule's names. A public static array member of
// one of them is the table leaked, whatever the member is called: the scan
// matches the array declaration first and then the table stem in the member
// name or a reference to its private backing field. That catches both
// `public static byte[] TFixedLayout` and an alias such as
// `public static byte[] TFixedLayoutArray => _TFixedLayout`.
var (
	csPublicArrayMemberRe = regexp.MustCompile(`(?m)^[ \t]*public[ \t]+static[ \t]+(?:readonly[ \t]+)?[^\n;{}=]*\[\s*\][ \t]*([A-Za-z_]\w*)`)
	csFixedTableStemRe    = regexp.MustCompile(`Fixed(?:Layout|Dst|Cover|GuardPool|Known|LineagePlans)\b`)
	csFixedBackingRefRe   = regexp.MustCompile(`_[A-Za-z_]\w*Fixed(?:Layout|Dst|Cover|GuardPool|Known|LineagePlans)\b`)
)

func TestCsFixedTablesPrivate(t *testing.T) {
	t.Parallel()
	_, _, cs := t08Emit(t, t08Flat)
	norm := t02Norm(cs)

	// 1. No public static member of array type may expose one of the six
	// tables, either by its own name or through an alias of its backing field.
	for _, line := range strings.Split(cs, "\n") {
		m := csPublicArrayMemberRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if csFixedTableStemRe.MatchString(m[1]) || csFixedBackingRefRe.MatchString(line) {
			t.Errorf("emitted source exposes a public static array member of a fixed table: %q", strings.TrimSpace(line))
		}
	}

	// 2. Each of the six tables has a private static readonly array backing field
	// and a read-only public accessor. The hot path reads the backing fields.
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
		"private static readonly TableFixedLineagePlan[] _TFixedLineagePlans = TableFixedWire.LineagePlans(_TFixedKnown, _TFixedLayout, _TFixedDst, TFixedHash);",
		"public static System.Collections.Generic.IReadOnlyList<TableFixedLineagePlan> TFixedLineagePlans => System.Array.AsReadOnly(_TFixedLineagePlans);",
		"int pick = TableFixedWire.Select(_TFixedKnown, hash);",
		"TableFixedKnownLayout known = _TFixedKnown[pick];",
		"TableFixedLineagePlan lane = _TFixedLineagePlans[pick];",
		"fillCount = TableFixedWire.Fills(entries, _TFixedCover, landed, fillBuf);",
	}
	for _, req := range requiredDecls {
		if !strings.Contains(norm, req) {
			t.Errorf("emitted source missing required declaration %q", req)
		}
	}

	// 3. The hot path reads the private arrays, not the allocating accessors.
	forbiddenHot := []string{
		"TableFixedWire.Select(TFixedKnown, hash)",
		"= TFixedKnown[pick]",
		"= TFixedLineagePlans[pick]",
		"TableFixedWire.Fills(entries, TFixedCover,",
		"known.Layout.Length",
		"new ReadOnlySpan<TableFixedEntry>(lane.Entries",
		"MemoryMarshal.AsBytes<TableFixedEntry>(lane.Entries)",
	}
	for _, f := range forbiddenHot {
		if strings.Contains(norm, f) {
			t.Errorf("emitted hot path still reads the accessor %q", f)
		}
	}

	// 4. Element types are read-only to callers; the runtime carries internal
	// no-copy accessors for its own hot path.
	requiredRuntime := []string{
		"public readonly struct TableFixedDst",
		"public struct TableFixedFill",
		"public byte[] Layout => _layout != null ? (byte[])_layout.Clone() : Array.Empty<byte>();",
		"internal ReadOnlySpan<byte> LayoutSpan => _layout != null ? _layout : ReadOnlySpan<byte>.Empty;",
		"public TableFixedEntry[] Entries => _entries != null ? (TableFixedEntry[])_entries.Clone() : Array.Empty<TableFixedEntry>();",
		"internal ReadOnlySpan<TableFixedEntry> EntriesSpan => _entries != null ? _entries : ReadOnlySpan<TableFixedEntry>.Empty;",
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

	// 5. With dotnet present: a decode of the same bytes is unchanged after the
	// probe mutates everything each accessor hands out.
	csPrivateToolchain(t)
	t08CSOK(t, t08Flat, `
var original = new Probe.T { X = 42, Y = -7 };
byte[] wire = new byte[Probe.Schema.TFixedMeasure(1)];
long wrote = Probe.Schema.TFixedSave(original, wire);
if (wrote != wire.Length) { bad += ProbeLog.Fail("TFixedSave failed"); }

if (Probe.Schema.TFixedLayout.Length != Probe.Schema.TFixedLayoutBytes) { bad += ProbeLog.Fail("TFixedLayout.Length mismatch"); }
if (Probe.Schema.TFixedDst.Count == 0) { bad += ProbeLog.Fail("TFixedDst.Count == 0"); }
if (Probe.Schema.TFixedCover.Count == 0) { bad += ProbeLog.Fail("TFixedCover.Count == 0"); }
if (Probe.Schema.TFixedGuardPool.Length < 0) { bad += ProbeLog.Fail("TFixedGuardPool.Length < 0"); }
if (Probe.Schema.TFixedKnown.Count == 0) { bad += ProbeLog.Fail("TFixedKnown.Count == 0"); }
if (Probe.Schema.TFixedLineagePlans.Count == 0) { bad += ProbeLog.Fail("TFixedLineagePlans.Count == 0"); }

var before = new Probe.T();
var beforeReport = new Probe.TableReport();
long readBefore = Probe.Schema.TFixedLoad(before, wire, new Probe.TableFixedEntry[64], beforeReport);
if (readBefore != 1 || before.X != 42 || before.Y != -7) { bad += ProbeLog.Fail("decode before mutation mismatch"); }

var dstView = Probe.Schema.TFixedDst;
if (dstView is Probe.TableFixedDst[]) { ((Probe.TableFixedDst[])dstView)[0] = default; }
var coverView = Probe.Schema.TFixedCover;
if (coverView is Probe.TableFixedFill[]) { ((Probe.TableFixedFill[])coverView)[0] = default; }
var knownView = Probe.Schema.TFixedKnown;
if (knownView is Probe.TableFixedKnownLayout[]) { ((Probe.TableFixedKnownLayout[])knownView)[0] = default; }
var planView = Probe.Schema.TFixedLineagePlans;
if (planView is Probe.TableFixedLineagePlan[]) { ((Probe.TableFixedLineagePlan[])planView)[0] = null; }
if (knownView.Count > 0) {
    byte[] layoutCopy = knownView[0].Layout;
    if (layoutCopy.Length > 0) { layoutCopy[0] ^= 0xff; }
}
if (planView.Count > 0) {
    var entriesCopy = planView[0].Entries;
    if (entriesCopy.Length > 0) { entriesCopy[0] = default; }
}

var after = new Probe.T();
var afterReport = new Probe.TableReport();
long readAfter = Probe.Schema.TFixedLoad(after, wire, new Probe.TableFixedEntry[64], afterReport);
if (readAfter != 1 || after.X != 42 || after.Y != -7) { bad += ProbeLog.Fail("decode after mutation attempts mismatch"); }
`)

	// 6. From another class, writing an element of any of the six must be a
	// compile error.
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

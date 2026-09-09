// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCsEmitsTableSources: the cs target adds <Base>Table.cs beside the packet
// sources for a unit with tables, and adds NOTHING for one without — the same
// contract the cpp target holds, under both spellings of the target name.
func TestCsEmitsTableSources(t *testing.T) {
	c := New()
	for _, target := range []string{"cs", "csharp"} {
		with, err := c.Generate(unitFromSource(t, tableSrc), target, Options{})
		if err != nil {
			t.Fatalf("--lang %s: %v", target, err)
		}
		if _, ok := with["ProbeTable.cs"]; !ok {
			t.Fatalf("--lang %s emitted no ProbeTable.cs for a unit with tables; got %d files", target, len(with))
		}
		without, err := c.Generate(unitFromSource(t, packetSrc), target, Options{})
		if err != nil {
			t.Fatalf("--lang %s: %v", target, err)
		}
		for name := range without {
			if strings.HasSuffix(name, "Table.cs") {
				t.Errorf("--lang %s emitted %s for a table-free unit", target, name)
			}
		}
	}
}

// Recursive pointers use nullable storage and the flat node-table wire.
func TestCsEmitsPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "cs", Options{})
	if err != nil {
		t.Fatalf("--lang cs refused a pointered unit outright — the accelerators need no codec: %v", err)
	}
	wire := string(files["ProbeTable.cs"])
	for _, want := range []string{"public Node Next;", "NodeLoadMeasure", "NodeSave", "NodeLoad"} {
		if !strings.Contains(wire, want) {
			t.Errorf("variable source lacks %q", want)
		}
	}
	var cooks int
	for name, data := range files {
		if !strings.HasSuffix(name, "Cook.cs") && !strings.HasSuffix(name, "Block.cs") {
			continue
		}
		if strings.HasSuffix(name, "Cook.cs") {
			cooks++
		}
		text := string(data)
		if strings.Contains(text, "WIRE SURFACE OF THIS UNIT IS REFUSED") {
			t.Errorf("%s refuses implemented variable wire", name)
		}
	}
	if cooks == 0 {
		t.Error("--lang cs emitted no cook reader for a pointered unit — a root is any table (docs/SPEC-TABLES.md §7)")
	}
	// and the cook's own surface is there: <Root>Cook with Open and At on it
	for name, data := range files {
		if !strings.HasSuffix(name, "Cook.cs") {
			continue
		}
		text := string(data)
		if strings.Contains(text, "struct NodeCook") {
			if !strings.Contains(text, "public static bool Open(out NodeCook cook, IntPtr pointer, long length)") {
				t.Errorf("%s declares NodeCook without the pointer-and-length Open", name)
			}
			if !strings.Contains(text, "public static NodeRow* At(long* slot)") {
				t.Errorf("%s declares NodeCook without At, which is how a reference is dereferenced (§6.3)", name)
			}
		}
	}
	// cpp still carries both classes
	if _, err := c.Generate(u, "cpp", Options{}); err != nil {
		t.Errorf("--lang cpp refused a pointered unit: %v", err)
	}
}

// TestCsKeyedAccessorCoversBothEnds: C# used to refuse only key 0 and let
// Slots[key-1] throw IndexOutOfRangeException past Max, naming an index.
// M5 is one unsigned compare covering None and past Max, naming the key.
func TestCsKeyedAccessorCoversBothEnds(t *testing.T) {
	u := unitFromSource(t, `package probe
enum Slot { Alpha, Beta, Gamma }
table Root { tokens [Slot]int32 }
`)
	files, err := New().Generate(u, "cs", Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["ProbeTable.cs"])
	if strings.Contains(src, "RefuseNone") {
		t.Error("TableKeyed still has RefuseNone — past Max would be a CLR index exception")
	}
	for _, want := range []string{
		"static void RefuseKey(int key)",
		"(uint)(key - 1) >= (uint)SlotCount",
		"neither does a key past Max",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("TableKeyed lacks %q", want)
		}
	}
}

// TestCsCarriesBlobs: #723 emits *bytes/*string; registerBuiltin must name
// C# as a carrier so a non-carrier's refusal lists --lang cs beside c/cpp/go.
func TestCsCarriesBlobs(t *testing.T) {
	u := unitFromSource(t, "package probe\ntable Root { data *bytes\n text *string }\n")
	files, err := New().Generate(u, "cs", Options{})
	if err != nil {
		t.Fatalf("--lang cs refused blobs: %v", err)
	}
	src := string(files["ProbeTable.cs"])
	for _, want := range []string{"data", "text"} {
		if !strings.Contains(src, want) {
			t.Errorf("blob field %q missing from generated C#", want)
		}
	}
	err = refuseBlobs(u, "rust")
	if err == nil {
		t.Fatal("refuseBlobs accepted a blob-bearing unit for rust")
	}
	for _, want := range []string{"*bytes and *string are c, cpp, cs and go only today", "--lang cs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("blob-carrier refusal does not name %q: %v", want, err)
		}
	}
}

// TestCsCountedArrayTailReset: a shorter counted-array replacement must restore
// slots above the new count (#725). C++/C/Go already walk previous..decoded.
func TestCsCountedArrayTailReset(t *testing.T) {
	u := unitFromSource(t, `package probe
table Child { n int32 = 7 }
table Root { values [..8]Child }
`)
	files, err := New().Generate(u, "cs", Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["ProbeTable.cs"])
	for _, want := range []string{
		"static void ResetCountedTail(object value, TableFieldInfo f, int previous, int decoded)",
		"int previous = f.Counted && f.GetCount != null ? f.GetCount(value) : 0",
		"ResetCountedTail(value, f, previous, decoded)",
		"static void ResetUnion(object union, TableUnionInfo arms)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("C# counted-array load lacks %q", want)
		}
	}
}

// TestCsFloatBitCastIsAtTheAssignment: M3. A float must not cross a helper
// call the JIT might leave as a call. Field GetRaw/SetRaw bit-cast inline.
func TestCsFloatBitCastIsAtTheAssignment(t *testing.T) {
	u := unitFromSource(t, "package probe\ntable Root { mass float32\n span float64 }\n")
	files, err := New().Generate(u, "cs", Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["ProbeTable.cs"])
	for _, want := range []string{
		"BitConverter.Int32BitsToSingle",
		"BitConverter.SingleToInt32Bits",
		"BitConverter.Int64BitsToDouble",
		"BitConverter.DoubleToInt64Bits",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("C# float path lacks %q", want)
		}
	}
	if strings.Contains(src, "GetRaw = delegate") && strings.Contains(src, "TableFloatToBits(value.Mass)") {
		t.Error("f32 GetRaw still calls TableFloatToBits instead of bit-casting at the assignment")
	}
}

// TestCsUnionElementReset: TableReset of a union clears every arm in place,
// not the tag alone (#734). Tag-only left a list arm standing under None.
func TestCsUnionElementReset(t *testing.T) {
	u := unitFromSource(t, `package probe
type Cell { x int32 }
union Choice
{
    signal
    many [..4]Cell
}
table Root { history [..8]Choice }
`)
	files, err := New().Generate(u, "cs", Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["ProbeTable.cs"])
	for _, want := range []string{
		"public static void TableReset(Choice value)",
		"value.Type = ChoiceType.None;",
		"TableReset(value.History[i]);",
		"else if (f.Kind == 15) { ResetUnion(f.GetChild(value, i), f.Arms); }",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("C# union reset lacks %q", want)
		}
	}
	if strings.Contains(src, "value.History[i].Type = ChoiceType.None;") {
		t.Error("C# union array reset is still tag-only")
	}
}

// Packet unions used as table fields are not File.TableUnions (no table arm)
// but TableReset(value.effect) still needs the overload (#734).
func TestCsPacketUnionTableReset(t *testing.T) {
	u := unitFromSource(t, `package probe
type Buff { n int32 }
union Effect { buff Buff }
table Root { effect Effect }
`)
	files, err := New().Generate(u, "cs", Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["ProbeTable.cs"])
	for _, want := range []string{
		"public static void TableReset(Effect value)",
		"TableReset(value.Effect);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("C# packet-union table reset lacks %q", want)
		}
	}
}

// csFiles generates the cs target's whole output for one source.
func csFiles(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, src), "cs", Options{})
	if err != nil {
		t.Fatalf("--lang cs: %v", err)
	}
	return files
}

// TestCsDescriptorsAreSafelyPublished asserts the structural shape of safe
// descriptor publication (schema#411).
//
// Under the CLI memory model on weakly ordered architectures (ARM64), a plain
// cache write allows reordering where a non-null reference can be read before
// the descriptor fields are fully written.
//
// Every generated descriptor accessor must be paired with its own nested static
// holder's static readonly Instance field (<Name>TableInfo.Instance), holders
// must be private static class, and the plain `if (info != null)` cache idiom
// must be completely absent.
func TestCsDescriptorsAreSafelyPublished(t *testing.T) {
	for _, src := range []string{runtimeSrc, pointerSrc} {
		files := csFiles(t, src)
		accessors := regexp.MustCompile(`public static TableTypeInfo ([A-Za-z0-9_]+)TableType\(\)\s*\{([^}]*)\}`)
		holders := 0
		for name, data := range files {
			text := string(data)
			for _, m := range accessors.FindAllStringSubmatch(text, -1) {
				holders++
				typeName := m[1]
				body := strings.TrimSpace(m[2])
				expectedReturn := fmt.Sprintf("return %sTableInfo.Instance;", typeName)
				if !strings.Contains(body, expectedReturn) {
					t.Errorf("%s: %sTableType() does not read its matching holder's Instance field — its body is %q, expected %q; "+
						"a plain cache publishes a mutable descriptor unsafely", name, typeName, body, expectedReturn)
				}
				expectedHolder := fmt.Sprintf("private static class %sTableInfo", typeName)
				if !strings.Contains(text, expectedHolder) {
					t.Errorf("%s: missing matching holder class for %sTableType(): expected %q",
						name, typeName, expectedHolder)
				}
			}
			for line := range strings.SplitSeq(text, "\n") {
				if strings.Contains(line, "if (info != null)") {
					t.Errorf("%s carries the plain-cache idiom: %q", name, strings.TrimSpace(line))
				}
			}
		}
		if holders == 0 {
			t.Fatal("the scan found no descriptor accessor at all — the scan, not the emitter, is what broke")
		}
		for name, data := range files {
			text := string(data)
			for line := range strings.SplitSeq(text, "\n") {
				if strings.Contains(line, "TableInfo") && strings.Contains(line, "class") && !strings.Contains(line, "private static class") {
					t.Errorf("%s: a descriptor holder is not a private static class: %q", name, strings.TrimSpace(line))
				}
				if strings.Contains(line, "Instance =") && !strings.Contains(line, "internal static readonly TableTypeInfo Instance = Build();") {
					t.Errorf("%s: a holder's Instance is not internal static readonly: %q", name, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestCsDescriptorUserVocabularyScope verifies that user types and enums named
// `Instance` or `Build` do not collide with the holder class members (schema#411).
func TestCsDescriptorUserVocabularyScope(t *testing.T) {
	const src = `package vocab

enum Instance {
    Alpha
    Beta
}

enum Build {
    First
    Second
}

table Subject {
    keyed [Instance]int32
    active Instance
    state Build = Second
}
`
	files := csFiles(t, src)
	subjectCs, ok := files["ProbeTable.cs"]
	if !ok {
		t.Fatalf("ProbeTable.cs not found in generated output; got keys: %v", files)
	}
	text := string(subjectCs)
	if !strings.Contains(text, "private static class SubjectTableInfo") {
		t.Errorf("expected SubjectTableInfo holder class in generated output")
	}
	if !strings.Contains(text, "(int)global::Vocab.Instance.Max") {
		t.Errorf("expected qualified (int)global::Vocab.Instance.Max, got:\n%s", text)
	}
	if !strings.Contains(text, "global::Vocab.Build.Second") {
		t.Errorf("expected qualified global::Vocab.Build.Second in default, got:\n%s", text)
	}
	if !strings.Contains(text, "global::Vocab.Instance.None") {
		t.Errorf("expected qualified global::Vocab.Instance.None in default, got:\n%s", text)
	}

	holderStart := strings.Index(text, "private static class SubjectTableInfo")
	if holderStart == -1 {
		t.Fatalf("SubjectTableInfo not found in generated output")
	}
	holderEnd := strings.Index(text[holderStart:], "public static TableTypeInfo SubjectTableType()")
	if holderEnd == -1 {
		t.Fatalf("SubjectTableType accessor not found after SubjectTableInfo")
	}
	holderBody := text[holderStart : holderStart+holderEnd]

	// Strip every properly qualified reference `global::Vocab.Build.` and `global::Vocab.Instance.`,
	// then assert no bare `Build.` or `Instance.` member access remains inside the holder class body.
	// Bare member access would bind to the holder's `private static TableTypeInfo Build()` method or
	// `internal static readonly TableTypeInfo Instance` field.
	stripped := strings.ReplaceAll(holderBody, "global::Vocab.Build.", "")
	stripped = strings.ReplaceAll(stripped, "global::Vocab.Instance.", "")
	if strings.Contains(stripped, "Build.") {
		t.Errorf("found bare Build. in SubjectTableInfo holder body:\n%s", holderBody)
	}
	if strings.Contains(stripped, "Instance.") {
		t.Errorf("found bare Instance. in SubjectTableInfo holder body:\n%s", holderBody)
	}
}

// TestCsTypedVsDynamicWireEquivalence verifies that typed fast paths and dynamic
// descriptor walks produce identical wire bytes across normal, guarded, and optional
// array fields.
func TestCsTypedVsDynamicWireEquivalence(t *testing.T) {
	const src = `package probe

type Leaf {
    value int32 = 0
}

table Subject {
    normal [2]Leaf
    counted [..2]Leaf
    on bool
    if on {
        guarded [2]Leaf
    }
    opt ?[2]Leaf
    opt_counted ?[..2]Leaf
}
`
	files := csFiles(t, src)
	subjectCs, ok := files["ProbeTable.cs"]
	if !ok {
		t.Fatalf("ProbeTable.cs not found in generated output; got keys: %v", files)
	}
	text := string(subjectCs)

	// Structural assertions:
	// 1. Leaf typed helpers must be generated.
	for _, want := range []string{
		"public static bool LeafCollectTyped(Leaf v, ref TableWire.Ids ids)",
		"public static long LeafBodySizeTyped(Leaf v, ref TableWire.Ids ids",
		"public static void LeafWriteBodyTyped(ref TableWire.Writer w, Leaf v, ref TableWire.Ids ids",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in generated output", want)
		}
	}

	// 2. Normal and counted arrays must use the typed helper calls.
	if !strings.Contains(text, "LeafBodySizeTyped(v.Normal[i_0], ref ids)") {
		t.Errorf("expected LeafBodySizeTyped call for normal array")
	}
	if !strings.Contains(text, "LeafBodySizeTyped(v.Counted[i_1], ref ids)") {
		t.Errorf("expected LeafBodySizeTyped call for counted array")
	}
	if !strings.Contains(text, "LeafWriteBodyTyped(ref w, v.Normal[i_0], ref ids)") {
		t.Errorf("expected LeafWriteBodyTyped call for normal array")
	}
	if !strings.Contains(text, "LeafWriteBodyTyped(ref w, v.Counted[i_1], ref ids)") {
		t.Errorf("expected LeafWriteBodyTyped call for counted array")
	}

	// 3. Guarded and optional arrays must NOT use typed child calls directly;
	// they must fall back to dynamic descriptor dispatch (BodySizeField / WriteBodyField).
	if strings.Contains(text, "LeafBodySizeTyped(v.Guarded") {
		t.Errorf("guarded array must not use typed child delegation")
	}
	if strings.Contains(text, "LeafBodySizeTyped(v.Opt[") {
		t.Errorf("optional uncounted array must not use typed child delegation")
	}
	if strings.Contains(text, "LeafBodySizeTyped(v.OptCounted[") {
		t.Errorf("optional counted array must not use typed child delegation")
	}

	// 4. Unqualified alias overloads must not be emitted.
	for _, unwanted := range []string{
		"public static bool CollectTyped(Subject ",
		"public static long BodySizeTyped(Subject ",
		"public static void WriteBodyTyped(ref TableWire.Writer w, Subject ",
		"public static long SaveTyped(Subject ",
	} {
		if strings.Contains(text, unwanted) {
			t.Errorf("found unwanted unqualified alias overload: %q", unwanted)
		}
	}

	// 5. If dotnet is available, compile and run end-to-end wire equivalence verification.
	dotnet := findDotnet()
	if dotnet == "" {
		return
	}

	runtime, err := filepath.Abs("../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "src", "Serialize.cs")); err != nil {
		t.Skip("serialize.cs unavailable")
	}

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	csproj := `<Project Sdk="Microsoft.NET.Sdk">
<PropertyGroup><TargetFramework>net10.0</TargetFramework><OutputType>Exe</OutputType><AllowUnsafeBlocks>true</AllowUnsafeBlocks></PropertyGroup>
<ItemGroup><Compile Include="` + filepath.ToSlash(runtime) + `/src/Serialize.cs" /><Compile Include="` + filepath.ToSlash(runtime) + `/src/Int128Pair.cs" /></ItemGroup>
</Project>`
	if err := os.WriteFile(filepath.Join(dir, "probe.csproj"), []byte(csproj), 0o600); err != nil {
		t.Fatal(err)
	}

	const programCs = `using System;
using Probe;

static class Program
{
    static void AssertEqual(string name, Subject s)
    {
        Span<ulong> ids1 = stackalloc ulong[64];
        Span<ulong> ids2 = stackalloc ulong[64];
        Span<byte> buf1 = stackalloc byte[1024];
        Span<byte> buf2 = stackalloc byte[1024];

        long mTyped = Schema.SubjectMeasure(s);
        long mDyn = Schema.TableWire.Save(s, Schema.SubjectTableType(), Span<byte>.Empty, ids2, true);
        if (mTyped != mDyn)
        {
            Console.Error.WriteLine($"{name}: measure divergence: typed {mTyped} vs dynamic {mDyn}");
            Environment.Exit(1);
        }

        long nTyped = Schema.SubjectSave(s, buf1);
        long nDyn = Schema.TableWire.Save(s, Schema.SubjectTableType(), buf2, ids1, false);
        if (nTyped != nDyn)
        {
            Console.Error.WriteLine($"{name}: size divergence: typed {nTyped} vs dynamic {nDyn}");
            Environment.Exit(1);
        }
        if (nTyped != mTyped)
        {
            Console.Error.WriteLine($"{name}: save len {nTyped} != measure {mTyped}");
            Environment.Exit(1);
        }
        if (!buf1.Slice(0, (int)nTyped).SequenceEqual(buf2.Slice(0, (int)nDyn)))
        {
            Console.Error.WriteLine($"{name}: byte divergence between typed and dynamic save!");
            Environment.Exit(1);
        }
    }

    static void Main()
    {
        // 1. All defaults
        AssertEqual("defaults", new Subject());

        // 2. Normal populated
        Subject s2 = new Subject();
        s2.Normal[0].Value = 10; s2.Normal[1].Value = 20;
        s2.CountedCount = 2; s2.Counted[0].Value = 30; s2.Counted[1].Value = 40;
        AssertEqual("normal populated", s2);

        // 3. Guarded array: on = false, elements nonzero
        Subject s3 = new Subject();
        s3.On = false;
        s3.Guarded[0].Value = 50; s3.Guarded[1].Value = 60;
        AssertEqual("guarded on=false", s3);

        // 4. Guarded array: on = true, elements nonzero
        Subject s4 = new Subject();
        s4.On = true;
        s4.Guarded[0].Value = 50; s4.Guarded[1].Value = 60;
        AssertEqual("guarded on=true", s4);

        // 5. Optional uncounted array: opt_present = false, elements nonzero
        Subject s5 = new Subject();
        s5.OptPresent = false;
        s5.Opt[0].Value = 70; s5.Opt[1].Value = 80;
        AssertEqual("opt uncounted absent", s5);

        // 6. Optional uncounted array: opt_present = true
        Subject s6 = new Subject();
        s6.OptPresent = true;
        s6.Opt[0].Value = 70; s6.Opt[1].Value = 80;
        AssertEqual("opt uncounted present", s6);

        // 7. Optional counted array: opt_counted_present = false, opt_counted_count = 2 (Rowan case c1)
        Subject s7 = new Subject();
        s7.OptCountedPresent = false;
        s7.OptCountedCount = 2;
        s7.OptCounted[0].Value = 90; s7.OptCounted[1].Value = 100;
        AssertEqual("opt counted absent with count=2", s7);

        // 8. Optional counted array: opt_counted_present = true, opt_counted_count = 0 (Rowan case c2)
        Subject s8 = new Subject();
        s8.OptCountedPresent = true;
        s8.OptCountedCount = 0;
        AssertEqual("opt counted present with count=0", s8);

        // 9. Optional counted array: opt_counted_present = true, opt_counted_count = 2
        Subject s9 = new Subject();
        s9.OptCountedPresent = true;
        s9.OptCountedCount = 2;
        s9.OptCounted[0].Value = 90; s9.OptCounted[1].Value = 100;
        AssertEqual("opt counted present with count=2", s9);
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(programCs), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(dotnet, "run", "--configuration", "Release", "-property:UseSharedCompilation=false")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet run failed: %v\n%s", err, out)
	}
}


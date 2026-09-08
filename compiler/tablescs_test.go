// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"
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

// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
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

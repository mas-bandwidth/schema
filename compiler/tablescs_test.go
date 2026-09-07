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

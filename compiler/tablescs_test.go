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

// TestCsRefusesPointeredTables: the C# variable-class refusal is a refusal of
// the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11). The wire codec is
// the half the variable class is missing — the arena, the builder, the region,
// the node table — and the two ACCELERATORS need none of it: a block and a cook
// are POINTED AT, not parsed, so both are emitted and the cook's <Root>Open
// opens this unit's cooked assets in full.
//
// NAMED, NEVER SILENT is what this holds: no Table source at all, and every
// source the unit does emit opening with a banner that names each refused table
// and the follow-on. A consumer reaching for Save or Load gets a missing name
// from its own compiler, beside a file that says why.
func TestCsRefusesPointeredTables(t *testing.T) {
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
	var cooks int
	for name, data := range files {
		if strings.HasSuffix(name, "Table.cs") {
			t.Errorf("--lang cs emitted the WIRE surface %s for a pointered unit", name)
		}
		if !strings.HasSuffix(name, "Cook.cs") && !strings.HasSuffix(name, "Block.cs") {
			continue
		}
		if strings.HasSuffix(name, "Cook.cs") {
			cooks++
		}
		text := string(data)
		if !strings.Contains(text, "THE C# WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME") {
			t.Errorf("%s carries no refusal banner", name)
		}
		if !strings.Contains(text, "Node") || !strings.Contains(text, "is a named follow-on") {
			t.Errorf("%s does not name the table and the follow-on", name)
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

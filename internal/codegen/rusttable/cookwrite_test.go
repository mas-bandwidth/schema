package rusttable

import (
	"strings"
	"testing"
)

// TestCookWriteStandsDownOnTheExtent pins the writer's boundary
// (docs/SPEC-TABLES.md §2.9, §7.6): a root whose graph reaches an unbounded
// list rides the node EXTENT this writer does not lay down, so it gets NO
// <t>_cook_write — a missing function, never a wrong byte — while a root of
// the same unit that reaches no list gets one. The control is the same unit
// with the list taken out: the root it held off gains its writer.
func TestCookWriteStandsDownOnTheExtent(t *testing.T) {
	const withList = `package probe

table Leaf
{
    v int32
}

table Holder
{
    name  string(8)
    leafs []Leaf
}

table Plain
{
    head *Leaf
    tag  string(4)
}

table Outer
{
    held *Holder
}
`
	body := func(src string) string {
		var all strings.Builder
		for name, data := range generate(t, src) {
			if strings.HasSuffix(name, "_table.rs") {
				all.Write(data)
			}
		}
		return all.String()
	}
	got := body(withList)
	for _, root := range []string{"holder", "outer"} {
		if strings.Contains(got, "pub fn "+root+"_cook_write(") {
			t.Errorf("%s reaches an unbounded list and must get no cook writer", root)
		}
	}
	for _, root := range []string{"plain", "leaf"} {
		if !strings.Contains(got, "pub fn "+root+"_cook_write(") {
			t.Errorf("%s reaches no list and must get its cook writer", root)
		}
	}
	// THE CONTROL: the same unit with the list taken out cooks every root
	control := body(strings.Replace(withList, "    leafs []Leaf\n", "", 1))
	for _, root := range []string{"holder", "outer", "plain", "leaf"} {
		if !strings.Contains(control, "pub fn "+root+"_cook_write(") {
			t.Errorf("control: %s must get its cook writer once the list is gone", root)
		}
	}
	// the writer is the cooked form's, so it is behind the `cook` feature
	if !strings.Contains(got, "#[cfg(feature = \"cook\")]\npub fn plain_cook_write(") {
		t.Error("the cook writer must be behind the `cook` feature, as every piece of the cooked form is")
	}
}

// TestCookDescriptorCompanionIsNeverTheFieldItself pins the cook descriptor's
// count and presence columns to the field's COMPANIONS (docs/SPEC-TABLES.md
// §7.5, §7.6): a scalar whose own name ends in _count, _length or _present is
// not a companion, so its count_offset and present_offset are -1. Found by the
// Rust allocation gate (`tables-rust-alloc`): Keyed.schema's spawn_count read
// itself as its count, and every non-zero value cooked to a refusal against a
// bound of zero. The control is the field's real companion: a counted array
// named the same way keeps its count column.
func TestCookDescriptorCompanionIsNeverTheFieldItself(t *testing.T) {
	const src = `package probe

fixed table Team
{
    spawn_count int32 = 4 | min = 0, max = 64
    last_length uint16
    was_present bool
    hits_count  [..4]int32
}
`
	var all strings.Builder
	for name, data := range generate(t, src) {
		if strings.HasSuffix(name, "_cook.rs") {
			all.Write(data)
		}
	}
	got := all.String()
	field := func(name string) string {
		at := strings.Index(got, "name: \""+name+"\",")
		if at < 0 {
			t.Fatalf("no cook descriptor row for %s", name)
		}
		end := strings.Index(got[at:], "},")
		return got[at : at+end]
	}
	for _, name := range []string{"spawn_count", "last_length", "was_present"} {
		row := field(name)
		if !strings.Contains(row, "count_offset: -1,") || !strings.Contains(row, "present_offset: -1,") {
			t.Errorf("%s names itself as its own companion:\n%s", name, row)
		}
	}
	// THE CONTROL: a counted array's real companion, hits_count_count, is kept
	if row := field("hits_count"); strings.Contains(row, "count_offset: -1,") {
		t.Errorf("the counted array lost its count companion:\n%s", row)
	}
}

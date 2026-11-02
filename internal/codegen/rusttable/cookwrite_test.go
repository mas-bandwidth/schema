package rusttable

import (
	"strings"
	"testing"
)

// TestCookWriteCarriesTheExtent pins the writer's boundary (docs/SPEC-TABLES.md
// §2.8, §2.9, §7.6): a root whose graph reaches an unbounded list or a
// map gets its <t>_cook_write, the arrays riding the node EXTENT, while a root
// that reaches a LIST OF TEXT, which this writer does not lay down, gets none —
// a missing function, never a wrong byte. The control is the same unit with
// the text list taken out: the root it held off gains its writer.
func TestCookWriteCarriesTheExtent(t *testing.T) {
	const src = `package probe

table Leaf
{
    v int32
}

table Holder
{
    name  string(8)
    leafs []Leaf
    index map[uint8]Leaf
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

table Noted
{
    notes []string
    tag   int32
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
	got := body(src)
	for _, root := range []string{"holder", "outer", "plain", "leaf"} {
		if !strings.Contains(got, "pub fn "+root+"_cook_write(") {
			t.Errorf("%s must get its cook writer: a list and a map ride the node extent", root)
		}
	}
	if strings.Contains(got, "pub fn noted_cook_write(") {
		t.Error("noted reaches a list of text and must get no cook writer")
	}
	if !strings.Contains(got, "pub fn holder_cook_carve(") {
		t.Error("holder holds a list and a map and must get its extent carve")
	}
	// THE CONTROL: the same unit with the text list taken out cooks every root
	control := body(strings.Replace(src, "    notes []string\n", "", 1))
	if !strings.Contains(control, "pub fn noted_cook_write(") {
		t.Error("control: noted must get its cook writer once its text list is gone")
	}
	// the writer is the cooked form's, so it is behind the `cook` feature
	if !strings.Contains(got, "#[cfg(feature = \"cook\")]\npub fn plain_cook_write(") {
		t.Error("the cook writer must be behind the `cook` feature, as every piece of the cooked form is")
	}
}

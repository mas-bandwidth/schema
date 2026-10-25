package cpptable

// TestCppTableResetArrayInPlace pins the C++ table Reset's array form on the
// emitted text alone (no toolchain): every element of a self-initializing
// array is reset in place in one loop, and no loop copies element 0 across
// the rest. The copy form (`x[i] = x[0]`), once inlined into an enclosing
// Reset of a keyed or array slot, made gcc's -Wstringop-overflow misread the
// copy's destination and warn in every translation unit including the header.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const resetInPlaceSchema = `package resetprobe

enum Slot { Head, Body, Tail }

table Leaf
{
    n   int32 = 3
    tag string(8)
}

table Cell
{
    leaves [4]Leaf
    one    [1]Leaf
}

table Root
{
    cells [2]Cell
    keyed [Slot]Leaf
    some  [..3]Leaf
}
`

// resetCopyLoop is the element-0 copy form the generator no longer emits.
var resetCopyLoop = regexp.MustCompile(`\[(\w+)\] = [^;]*\[0\];`)

func TestCppTableResetArrayInPlace(t *testing.T) {
	t.Parallel()

	u := resetProbeUnit(t, resetInPlaceSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header, ok := files["probeTable.h"]
	if !ok {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		t.Fatalf("Generate emits no probeTable.h; it emits %v", names)
	}

	resets := resetBodies(string(header))
	for _, name := range []string{"LeafReset", "CellReset", "RootReset"} {
		if _, ok := resets[name]; !ok {
			t.Fatalf("the header defines no %s", name)
		}
	}
	for name, body := range resets {
		if m := resetCopyLoop.FindString(body); m != "" {
			t.Errorf("%s copies element 0 across an array (%q); every element is reset in place", name, m)
		}
	}

	cell := resets["CellReset"]
	for _, want := range []string{
		"for ( int32_t i = 0; i < 4; i++ )",
		"LeafReset( value.leaves[i] );",
		"LeafReset( value.one[0] );",
	} {
		if !strings.Contains(cell, want) {
			t.Errorf("CellReset does not reset each element in place: missing %q in\n%s", want, cell)
		}
	}
	root := resets["RootReset"]
	for _, want := range []string{
		"CellReset( value.cells[i] );",
		"LeafReset( value.keyed.slots[i] );",
		"LeafReset( value.some[i] );",
	} {
		if !strings.Contains(root, want) {
			t.Errorf("RootReset does not reset each element in place: missing %q in\n%s", want, root)
		}
	}
}

// resetProbeUnit parses and checks one probe schema source.
func resetProbeUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	const name = "probe.schema"
	f, perrs := parser.Parse(name, []byte(src))
	if len(perrs) > 0 || f == nil {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  name,
		Name:  name,
		Base:  strings.TrimSuffix(name, ".schema"),
		Bytes: []byte(src),
		AST:   f,
	}})
	if len(cerrs) > 0 || u == nil {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

// resetBodies maps each `inline void <T>Reset( ... )` definition in the header
// to its body: the lines from the signature to the closing brace in column 0.
func resetBodies(header string) map[string]string {
	bodies := map[string]string{}
	lines := strings.Split(header, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "inline void ") || !strings.Contains(line, "Reset( ") || strings.HasSuffix(line, ";") {
			continue
		}
		name := strings.TrimPrefix(line, "inline void ")
		name = name[:strings.Index(name, "(")]
		name = strings.TrimSpace(name)
		var b strings.Builder
		for i++; i < len(lines) && lines[i] != "}"; i++ {
			b.WriteString(lines[i])
			b.WriteString("\n")
		}
		bodies[name] = b.String()
	}
	return bodies
}

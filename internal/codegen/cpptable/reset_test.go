package cpptable

// TestCppTableResetArrayInPlace pins the C++ table Reset's array form on the
// emitted text alone (no toolchain): every element of a self-initializing
// array is reset in place in one loop, and no loop copies element 0 across
// the rest. The copy form (`x[i] = x[0]`), once inlined into an enclosing
// Reset of a keyed or array slot, made gcc's -Wstringop-overflow misread the
// copy's destination and warn in every translation unit including the header.
//
// It also holds the loop index names apart: an array of unions whose arms are
// themselves arrays emits a loop inside a loop in one Reset, and an inner
// `int32_t i` would shadow the outer one (-Wshadow). Every loop in every
// Reset declares an index no enclosing loop of that Reset declares.

import (
	"regexp"
	"strings"
	"testing"
)

const resetInPlaceSchema = `package resetprobe

enum Slot { Head, Body, Tail }

type Leaf
{
    n int32 = 3
    tag string(8)
}

fixed table Only { w int32 }

union Pick
{
    leaves [3]Leaf
    some   [..2]Leaf
    refs   [2]*Only
    plain  int32
}

fixed table Cell
{
    leaves [4]Leaf
    one    [1]Leaf
}

table Root
{
    cells  [2]Cell
    keyed  [Slot]Leaf
    picks  [3]Pick
    counted [..3]Pick
}
`

// resetCopyLoop is the element-0 copy form the generator no longer emits.
var resetCopyLoop = regexp.MustCompile(`\[(\w+)\] = [^;]*\[0\];`)

// resetLoopIndex is a reset loop's declared index.
var resetLoopIndex = regexp.MustCompile(`for \( int32_t (\w+) = `)

func TestCppTableResetArrayInPlace(t *testing.T) {
	t.Parallel()

	u := loadUnitFromSource(t, resetInPlaceSchema)
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

	nested := false
	for name, body := range resets {
		if m := resetCopyLoop.FindString(body); m != "" {
			t.Errorf("%s copies element 0 across an array (%q); every element is reset in place", name, m)
		}
		if resetLoopsNest(t, name, body) {
			nested = true
		}
	}
	if !nested {
		t.Fatal("no Reset in the probe nests one loop inside another, so the shadowing half of this test proves nothing")
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
		"LeafReset( value.picks[i].leaves[j] );",
		"value.picks[i].refs[j].value = 0;",
	} {
		if !strings.Contains(root, want) {
			t.Errorf("RootReset does not reset each element in place with its own index: missing %q in\n%s", want, root)
		}
	}
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

// resetLoopsNest walks one Reset body by brace depth and fails the test when
// a loop declares an index an enclosing loop already declared. It reports
// whether any loop sat inside another.
func resetLoopsNest(t *testing.T, name, body string) bool {
	t.Helper()
	type open struct {
		index string
		depth int
	}
	var active []open
	depth := 0
	nested := false
	for line := range strings.SplitSeq(body, "\n") {
		if m := resetLoopIndex.FindStringSubmatch(line); m != nil {
			for _, a := range active {
				if a.index == m[1] {
					t.Errorf("%s declares loop index %q inside a loop that already declares it:\n%s", name, m[1], body)
				}
			}
			if len(active) > 0 {
				nested = true
			}
			active = append(active, open{m[1], depth})
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if strings.Contains(line, "}") {
			for len(active) > 0 && depth <= active[len(active)-1].depth {
				active = active[:len(active)-1]
			}
		}
	}
	return nested
}

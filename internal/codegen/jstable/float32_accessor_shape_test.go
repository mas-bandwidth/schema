package jstable

import (
	"sort"
	"strings"
	"testing"
)

// TestBlockFloat32AccessorCallsNoClosure pins the SHAPE of every float32 read
// the block form and the record objects emit: the accessor's body is a call to
// ONE named module-level conversion, never a closure built on the read path
// (an arrow or a function expression there is a heap allocation per read,
// which breaks docs/PORTING.md M1; `make tables-js-alloc` is the functional
// proof). The conversion is emitted ONCE per unit, in the block home, and not
// once per field or per module.
func TestBlockFloat32AccessorCallsNoClosure(t *testing.T) {
	t.Parallel()
	const schemaSrc = `package probe

type Lens
{
    fov float32
    aspect float32
}

fixed table Shot
{
    exposure float32
    gain float32
    lenses [2]Lens
}
`
	const reader = "TableBlockReadF32"
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	// every float32 accessor the unit's block-side modules emit: the record
	// object's (view, at, i) form and the block handle's (block, index) form
	accessors := map[string]bool{"Fov": false, "Aspect": false, "Exposure": false, "Gain": false}
	definitions := 0
	for _, name := range names {
		if !strings.HasSuffix(name, "Block.js") && !strings.HasSuffix(name, "Cook.js") {
			continue
		}
		src := string(files[name])
		definitions += strings.Count(src, "function "+reader+"(")
		for line := range strings.SplitSeq(src, "\n") {
			for member := range accessors {
				if !strings.HasPrefix(line, "  function "+member+"(") {
					continue
				}
				accessors[member] = true
				open := strings.Index(line, "{")
				if open < 0 {
					t.Errorf("%s: accessor %s has no body: %s", name, member, line)
					continue
				}
				body := line[open:]
				if strings.Contains(body, "=>") || strings.Contains(body, "function") {
					t.Errorf("%s: float32 accessor %s builds a closure on the read path (docs/PORTING.md M1): %s", name, member, line)
				}
				if !strings.Contains(body, reader+"(") {
					t.Errorf("%s: float32 accessor %s does not call the module-level %s: %s", name, member, reader, line)
				}
			}
		}
	}
	for member, seen := range accessors {
		if !seen {
			t.Errorf("no float32 accessor %s was emitted; the probe no longer reaches the path it pins", member)
		}
	}
	if definitions != 1 {
		t.Errorf("%s is defined %d times across the unit, want exactly once (the block home)", reader, definitions)
	}
	home := string(files["ProbeBlock.js"])
	if !strings.Contains(home, "export function "+reader+"(") {
		t.Errorf("the block home ProbeBlock.js does not export %s", reader)
	}
	// the non-finite latch (docs/SPEC-TABLES.md §3.3) is built once, at module
	// scope, and a finite read builds nothing
	if n := strings.Count(home, "new WeakMap()"); n != 1 {
		t.Errorf("the non-finite latch is built %d times in ProbeBlock.js, want once at module scope", n)
	}
	if strings.Contains(home, "new ArrayBuffer(8)") {
		t.Errorf("ProbeBlock.js still builds the retired NaN scratch")
	}
}

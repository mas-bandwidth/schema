package compiler

import (
	"strings"
	"testing"
)

// Issue #751: C# table wire-fuzz report differs on streamdemo.Feed
// (KindMismatch 2 vs oracle 1) because pointer union arms resolved the node
// index before validating that the arm's framed payload had no trailing bytes.
//
// Under SPEC-TABLES §3: "an L that is not the byte count of the reference it
// frames ... is that arm's own framing damage — the union reads None,
// malformed counts, and the parent reads on past L".
//
// Validating r.Offset == r.Buffer.Length before Graph.Resolve/NativePointer ensures
// that framing damage prevents resolving the pointer, so no pointee kind
// mismatch is erroneously counted.
func TestIssue751PointerArmTrailingFraming(t *testing.T) {
	src := `package p
table Chunk {
    data bytes(8)
    next *Chunk
}
union Frame {
    chunk Chunk
    link *Chunk
}
table Feed {
    id uint32
    frame Frame
    parts [..4]*Chunk
}
`
	u := unitFromSource(t, src)
	c := New()

	files, err := c.Generate(u, "cs", Options{})
	if err != nil {
		t.Fatalf("generate cs: %v", err)
	}

	var foundWireCheck, foundRegionCheck bool
	for name, content := range files {
		str := string(content)
		if strings.HasSuffix(name, "Table.cs") {
			if strings.Contains(str, "!r.Var(out ulong indexValue) || r.Offset != r.Buffer.Length") {
				foundWireCheck = true
			}
		}
		if strings.HasSuffix(name, "Region.cs") {
			if strings.Contains(str, "!r.Var(out ulong indexValue) || r.Offset != r.Buffer.Length") {
				foundRegionCheck = true
			}
		}
	}

	if !foundWireCheck {
		t.Errorf("C# generated Table.cs missing exact framing check for pointer arm")
	}
	if !foundRegionCheck {
		t.Errorf("C# generated Region.cs missing exact framing check for pointer arm")
	}
}

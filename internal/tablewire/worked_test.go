package tablewire_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE PAGE'S WORKED SAVE, byte for byte (docs/SPEC-TABLES.md §3.1). A second
// implementation reproduces these 120 bytes from the page alone, so this test
// is the page holding the engine rather than the engine holding itself: the
// form byte, the 1-based references, the node table under the reserved id, the
// eight entries in first-use order, and the fixed u64 count last.
const workedSave = `
	01 01 11 02 02 11 04 03
	0c 25 03 04 0d 05 04 01
	00 00 00 06 11 03 02 11
	04 00 04 0a 05 04 02 00
	00 00 02 11 04 00 07 07
	08 04 07 00 00 00 00 00
	03 0c 9a 5f cc 12 8f 0a
	80 b6 52 83 08 91 d6 9d
	ff ff ff ff ff ff ff ff
	8d b6 f6 d2 c6 1c bd 66
	ea 0c e8 30 94 fd e4 7c
	28 f0 25 a0 ba 6c 31 e5
	e0 ad 84 20 6e 53 af c8
	c0 3a 5c b5 07 2e b7 08
	08 00 00 00 00 00 00 00
`

func TestWorkedSaveIsTheBytesOnThePage(t *testing.T) {
	dir := t.TempDir()
	src := "package demo\n\n" +
		"table Palette\n{\n    id int32\n}\n\n" +
		"table Node\n{\n    value   int32\n    next    *Node\n    palette *Palette\n}\n\n" +
		"table Scene\n{\n    head    *Node\n    palette *Palette\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "Demo.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)

	// scene.head = A, A.next = B, and A.palette, B.palette and scene.palette
	// all naming one Palette P
	p := m.New(m.Lookup("Palette"))
	setInt(p, 0, 7)
	b := m.New(m.Lookup("Node"))
	setInt(b, 0, 2)
	b.Fields[2].Cell.Node = p
	a := m.New(m.Lookup("Node"))
	setInt(a, 0, 1)
	a.Fields[1].Cell.Node = b
	a.Fields[2].Cell.Node = p
	scene := m.New(m.Lookup("Scene"))
	scene.Fields[0].Cell.Node = a
	scene.Fields[1].Cell.Node = p

	wire, err := tablewire.Encode(m, scene)
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString(strings.NewReplacer(" ", "", "\n", "", "\t", "").Replace(workedSave))
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 120 {
		t.Fatalf("the worked save is 120 bytes on the page, and this expectation is %d", len(want))
	}
	if hex.EncodeToString(wire) != hex.EncodeToString(want) {
		t.Fatalf("the worked save moved:\n got %s\nwant %s", hexDump(wire), hexDump(want))
	}

	// and it reads back: P is one node named three times, B.next is null
	back := m.New(m.Lookup("Scene"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, back, wire, &r)
	if err != nil || !ok || !r.Silent() {
		t.Fatalf("the worked save did not read back clean: %v %v %+v", err, ok, r)
	}
	head := back.Fields[0].Cell.Node
	if head == nil || head.Fields[1].Cell.Node == nil {
		t.Fatal("the chain off head did not survive")
	}
	shared := back.Fields[1].Cell.Node
	if shared == nil || head.Fields[2].Cell.Node != shared || head.Fields[1].Cell.Node.Fields[2].Cell.Node != shared {
		t.Fatal("P is one node on the wire and must be one node in a reader")
	}
	if shared.Fields[0].Cell.I != 7 {
		t.Fatalf("P.id read %d", shared.Fields[0].Cell.I)
	}
	if head.Fields[1].Cell.Node.Fields[1].Cell.Node != nil {
		t.Fatal("B.next is null and must read null")
	}
}

func setInt(inst *tabletext.Instance, field int, v int64) {
	inst.Fields[field].Cell.I = v
	inst.Fields[field].Cell.U = uint64(v)
}

func hexDump(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); i += 8 {
		end := i + 8
		if end > len(b) {
			end = len(b)
		}
		out.WriteString("\n  ")
		out.WriteString(hex.EncodeToString(b[i:end]))
	}
	return out.String()
}

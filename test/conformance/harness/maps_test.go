package main

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE TOOL'S MAP HALF AGAINST THE REFERENCE (docs/SPEC-TABLES.md §2.8): the
// compiler's own engine reads the C++ reference's pinned map wires, writes them
// back byte for byte, renders the text and reads that text back to the same
// bytes. A map rides as kind 14 over element kind 13 in ASCENDING KEY ORDER
// with no key twice, and this is what says both implementations agree on the
// order as well as the framing — at three depths, a map of maps included.
func TestTheToolWritesTheReferencesMapBytes(t *testing.T) {
	const root = "../../../"
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{root + "tables/maps"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	for _, tc := range []struct{ name, root string }{{"map_full", "Fleet"}, {"map_empty", "Fleet"}, {"map_depth", "Depth"}} {
		name, rootName := tc.name, tc.root
		data, err := os.ReadFile(root + "testdata/wire/tables/" + name + ".bin")
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(rootName))
		var rep tabletext.Report
		ok, derr := tablewire.Decode(m, inst, data, &rep)
		if derr != nil || !ok {
			t.Errorf("%s: %v ok=%v", name, derr, ok)
			continue
		}
		if !rep.Silent() {
			t.Errorf("%s: the reference's own bytes did not read clean: %+v", name, rep)
		}
		again, eerr := tablewire.Encode(m, inst)
		if eerr != nil {
			t.Errorf("%s: %v", name, eerr)
			continue
		}
		if hex.EncodeToString(again) != hex.EncodeToString(data) {
			t.Errorf("%s: the engine re-encoded %d bytes, the reference wrote %d\n  got  %s\n  want %s",
				name, len(again), len(data), hex.EncodeToString(again), hex.EncodeToString(data))
			continue
		}
		text, terr := m.Write(inst)
		if terr != nil {
			t.Errorf("%s: text: %v", name, terr)
			continue
		}
		back := m.New(m.Lookup(rootName))
		var r2 tabletext.Report
		if !m.Read(back, text, &r2) || !r2.Silent() {
			t.Errorf("%s: the text did not read back clean: %+v\n%s", name, r2, text)
			continue
		}
		round, _ := tablewire.Encode(m, back)
		if hex.EncodeToString(round) != hex.EncodeToString(data) {
			t.Errorf("%s: text round trip moved bytes", name)
		}
	}
}

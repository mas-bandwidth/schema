package main

import (
	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"testing"
)

func TestWideRegionFramingAlignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Wide.schema")
	if err := os.WriteFile(path, []byte("package wideprobe\ntable Node { n uint64 }\ntable Root { wide uint128\nvertex *Node }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	roster := &units{c: c, loaded: map[string]*ir.Unit{"wideprobe": u}}
	root, err := newWireRoot(roster, "wideprobe", "Root", false, false)
	if err != nil {
		t.Fatal(err)
	}
	value := root.model.New(root.def)
	var report tabletext.Report
	if !root.model.Read(value, []byte(`{"wide":1,"vertex":{"n":2}}`), &report) {
		t.Fatalf("json %+v", report)
	}
	wire, err := tablewire.Encode(root.model, value)
	if err != nil {
		t.Fatal(err)
	}
	align := ir.TableRegionAlign(u)
	rootSize := ir.RecordLayout(u, root.def).Size
	nodeSize := (ir.RecordLayout(u, root.model.Lookup("Node")).Size + align - 1) & -align
	measured, whole, refused := tablewire.FileRegionMeasure(u, root.def, wire)
	if align != 16 || rootSize != 32 || nodeSize != 16 || refused || !whole || measured != 80 {
		t.Fatalf("wide framing: align=%d root=%d node=%d measured=%d whole=%v refused=%v", align, rootSize, nodeSize, measured, whole, refused)
	}
}

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
	if err := os.WriteFile(path, []byte("package wideprobe\ntable Node { n uint64 }\ntable Root { wide uint128\nnode *Node }\n"), 0600); err != nil {
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
	if !root.model.Read(value, []byte(`{"wide":1,"node":{"n":2}}`), &report) {
		t.Fatalf("json %+v", report)
	}
	wire, err := tablewire.Encode(root.model, value)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if root.align != 16 || root.rootStorage != 32 || root.maxStorage != 16 || !answer.exact || answer.bytes != 80 {
		t.Fatalf("wide framing: align=%d root=%d node=%d answer=%+v", root.align, root.rootStorage, root.maxStorage, answer)
	}
	message, err := newWireRoot(roster, "wideprobe", "Root", true, false)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := tablewire.EncodeMessage(root.model, value)
	if err != nil {
		t.Fatal(err)
	}
	answer, err = message.oracle(batch)
	if err != nil || !answer.exact || answer.bytes != 80 {
		t.Fatalf("message framing: %+v %v", answer, err)
	}

}

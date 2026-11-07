package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestEngineWireBoolPayloadNot0Or1(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../tables/examples/Guarded.schema")
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse("Guarded.schema", b)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Guarded.schema", Name: "Guarded.schema", Base: "Guarded", Bytes: b, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Patrol"))
	wire, _ := hex.DecodeString("010101fe020a0000c03f0301010404e8030000006f0c6f030b798065400e20a08a498122bdcfaa557f0e7f245050a215c09abcb70400000000000000")
	var report tabletext.Report
	// a bool byte other than 0 or 1 is damage, and DAMAGE DISCARDS THE WHOLE
	// LOAD (docs/SPEC-TABLES.md §4): the read refuses and hands back the root
	// at its declared defaults
	ok, derr := tablewire.Decode(m, inst, wire, &report)
	if ok || derr != nil {
		t.Fatalf("decode: ok=%v err=%v", ok, derr)
	}
	if !report.Malformed {
		t.Fatal("decode did not report malformed")
	}
	got, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	want, err := tablewire.Encode(m, m.New(m.Lookup("Patrol")))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("a damaged load handed back a value: %x", got)
	}
}

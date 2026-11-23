// Package idcapwire builds the one file the retain-writer id cap tests load
// (docs/SPEC-TABLES.md §3 and §6.6, R8 follow-up).
//
// A NEWER build of tablewire.IdCapSchema's unit declares one more field, `z`.
// Its file has every slot of `keys` set to 1 and z = 1: 4095 ids the older
// build knows plus the one it does not, 4096 in all, so the file is lawful.
// The older build loads it WITH RETENTION (z is retained, one foreign id),
// sets x = 1 (a known id the file did not carry) and saves: 4096 known ids
// plus 1 retained foreign id would be 4097. A retaining writer must not write
// that table: it refuses the save, or drops the retained id and counts it
// lost, and either way the file it writes is one every reader accepts.
package idcapwire

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// NewerFile is the lawful 4096-id file of the newer build.
func NewerFile(t *testing.T) []byte {
	t.Helper()
	src := strings.Replace(tablewire.IdCapSchema(), "    y uint8\n", "    y uint8\n    z uint8\n", 1)
	f, perrs := parser.Parse("Newer.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{Path: "Newer.schema", Name: "Newer.schema", Base: "Newer", Bytes: []byte(src), AST: f}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	for i := range inst.Fields {
		switch inst.Fields[i].Def.Name {
		case "keys":
			for e := range inst.Fields[i].Elems {
				inst.Fields[i].Elems[e].U = 1
			}
		case "z":
			inst.Fields[i].Cell.U = 1
		}
	}
	out, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("encode the newer build's file: %v", err)
	}
	return out
}

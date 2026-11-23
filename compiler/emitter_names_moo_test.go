//go:build schema_leg_moo

package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestMooModuleNamesAtCheck: a schema file whose moo module is the runtime's
// (json.moo and table_message.moo, runtime of a unit with tables) is refused by
// schema check, naming the MOO target, as moo's generate refuses it
// (SPEC §4.6). A table-free unit generates no table runtime, so
// json and table_message are its own.
func TestMooModuleNamesAtCheck(t *testing.T) {
	t.Parallel()
	const inner = "package probe\n\ntype Inner\n{\n    a uint8\n}\n"
	const tab = "package probe\n\ntable Rec\n{\n    value uint32\n}\n"
	for _, c := range []struct {
		base, src string
		refused   bool
	}{
		{"json", tab, true},
		{"table_message", tab, true},
		{"table_message", inner, false},
		{"json", inner, false},
		{"notruntime", tab, false},
	} {
		var files []check.SourceFile
		for _, f := range []struct{ name, src string }{{c.base, c.src}, {"Other", "package probe\n\ntype Holder\n{\n    x uint8\n}\n"}} {
			pf, perrs := parser.Parse(f.name+".schema", []byte(f.src))
			if len(perrs) > 0 {
				t.Fatal(perrs[0])
			}
			files = append(files, check.SourceFile{Path: f.name + ".schema", Name: f.name + ".schema", Base: f.name, Bytes: []byte(f.src), AST: pf})
		}
		_, errs := check.Unit(files)
		got := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "the MOO target cannot express a name") {
				got = true
			}
		}
		if got != c.refused {
			t.Errorf("%s.schema: refused for MOO = %v, want %v (errors %v)", c.base, got, c.refused, errs)
		}
	}
}

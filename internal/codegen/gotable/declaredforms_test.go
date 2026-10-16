// The Go target carries exactly the forms a table declares
// (docs/SPEC-TABLES.md §2.11): `table T | forms = "wire,json"`. A table that
// leaves `json` out has no <T>FromJson/ToJson/ToJsonMeasure, the wire codecs
// stay, a table that declares nothing carries everything, and an undeclared
// table generates byte-identically to the same table with every form written
// out.
package gotable

import (
	"bytes"
	"strings"
	"testing"
)

const declaredFormsSrc = `package probe

fixed table Both | forms = "wire,json"
{
    a int32
}

fixed table WireOnly | forms = "wire"
{
    b int32
}

fixed table Plain
{
    c int32
}
`

func TestGoTableEmitsOnlyItsDeclaredForms(t *testing.T) {
	t.Parallel()
	files := generate(t, declaredFormsSrc)
	var table, text string
	for name, data := range files {
		switch {
		case strings.HasSuffix(name, "TableJson.go"):
			text = string(data)
		case strings.HasSuffix(name, "Table.go") && bytes.Contains(data, []byte("func BothMeasure(")):
			table = string(data)
		}
	}
	if table == "" || text == "" {
		t.Fatalf("no table or text file among %d generated files", len(files))
	}
	for _, name := range []string{"Both", "WireOnly", "Plain"} {
		for _, fn := range []string{"func " + name + "Measure(", "func " + name + "Save(", "func " + name + "Load("} {
			if !strings.Contains(table, fn) {
				t.Errorf("the wire form is always carried: %s is missing", fn)
			}
		}
	}
	for _, c := range []struct {
		table string
		json  bool
	}{{"Both", true}, {"Plain", true}, {"WireOnly", false}} {
		for _, fn := range []string{"FromJson(", "ToJson(", "ToJsonMeasure("} {
			has := strings.Contains(text, "func "+c.table+fn)
			if has != c.json {
				t.Errorf("%s%s present=%v, want %v (forms declared: json=%v)", c.table, fn, has, c.json, c.json)
			}
		}
	}
}

// What a table leaves out compiles away with it, and what it keeps still runs.
func TestGoTableDeclaredFormsCompileAndRun(t *testing.T) {
	t.Parallel()
	runGenerated(t, declaredFormsSrc, `package probe
import "testing"
func TestCarried(t *testing.T) {
 var b Both; BothReset(&b); b.A = 7
 buf := make([]byte, BothToJsonMeasure(&b)); if BothToJson(&b, buf) != int64(len(buf)) { t.Fatal("json") }
 var got Both; if !BothFromJson(&got, buf, nil) || got.A != 7 { t.Fatalf("round trip: %+v", got) }
 var w WireOnly; WireOnlyReset(&w); w.B = 9
 wb := make([]byte, WireOnlyMeasure(&w)); if WireOnlySave(&w, wb) != int64(len(wb)) { t.Fatal("wire save") }
 var back WireOnly; var r TableReport
 if !WireOnlyLoad(&back, wb, &r) || back.B != 9 { t.Fatalf("wire load: %+v", back) }
}
`)
}

// A table with no declaration generates the bytes it generates with every form
// written out: the default is every form, and the declaration costs nothing
// when it says so.
func TestUndeclaredTableIsByteIdenticalToAllFormsDeclared(t *testing.T) {
	t.Parallel()
	bare := generate(t, "package probe\nfixed table P\n{\n    a int32\n}\n")
	full := generate(t, "package probe\nfixed table P | forms = \"wire,json\"\n{\n    a int32\n}\n")
	if len(bare) != len(full) {
		t.Fatalf("file sets differ: %d vs %d", len(bare), len(full))
	}
	for name, data := range bare {
		if !bytes.Equal(data, full[name]) {
			t.Errorf("%s differs between an undeclared table and forms = \"wire,json\"", name)
		}
	}
}

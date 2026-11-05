package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// utcSchema holds unbounded text in every place a cook carves it
// (docs/SPEC-TABLES.md §2.13.1): a list element, an optional list element, a
// counted array's optional element and a map key.
const utcSchema = `package textcook

table Root
{
    words []string
    maybe []?string
    first [..4]?string
    by_name map[string]int32
    tag int32
}
`

// utcTexts are the rows: the empty value, and one with empty, short and long
// texts, absent elements and an empty key beside a long one.
var utcTexts = []string{
	`{}`,
	`{"words":["unbounded text is populated","","x"],"maybe":["a",null,"","longer than eight bytes"],"first":["q",null,""],"by_name":{"a key longer than eight bytes":23,"":1,"k":2},"tag":7}`,
}

// utcCarveAnchor is the cook writer's unbounded-text branch; the negative
// control turns it off.
const utcCarveAnchor = `  if (f.shape == "str" && f.unbounded) {
    // UNBOUNDED TEXT`

// utcRun cooks every row through the Squirrel cook writer in both byte orders
// and answers each row's verdict line, keyed "<row> <order>".
func utcRun(t *testing.T, patch func(map[string][]byte)) (map[string]string, int) {
	t.Helper()
	sqrun.Require(t)
	u, model, _ := nfLoadNamed(t, utcSchema)
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if patch != nil {
		patch(tables)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "utc")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	inputs := map[string][]byte{}
	var p strings.Builder
	p.WriteString(strings.Replace(cookWritePrelude, `dofile(unit + "/schema_module.nut", true)`, `dofile("utc/schema_module.nut", true)`, 1))
	for i, text := range utcTexts {
		inst := model.New(model.Lookup("Root"))
		var report tabletext.Report
		if !model.Read(inst, []byte(text), &report) {
			t.Fatalf("row %d: the Go text engine refused the row: %+v", i, report)
		}
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("row %d: the Go wire: %v", i, err)
		}
		little, err := tablecook.Cook(model, inst, tablecook.Options{})
		if err != nil {
			t.Fatalf("row %d: the tool's cook: %v", i, err)
		}
		big, err := tablecook.Cook(model, inst, tablecook.Options{Big: true})
		if err != nil {
			t.Fatalf("row %d: the tool's big-endian cook: %v", i, err)
		}
		key := fmt.Sprintf("row%d", i)
		inputs[key+".wire"], inputs[key+".little"], inputs[key+".big"] = wire, little, big
		fmt.Fprintf(&p, "run(%q, \"Root\", %q, \"utc\");\n", squirreltable.ModuleName(u), key)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Fatalf("probe line %q is not a row, an order and a verdict", line)
		}
		got[fields[0]+" "+fields[1]] = strings.Join(fields[2:], " ")
	}
	return got, len(utcTexts)
}

// TestSquirrelCookWritesUnboundedText holds the Squirrel cook writer to the
// tool's bytes for unbounded text (docs/SPEC-TABLES.md §2.13.1, §7.6): each
// non-empty value is a blob record in its holder's extent, padded to the
// region's alignment, its slot the self-relative delta, and the empty string's
// slot null, in both byte orders, with CookMeasure the file's length.
func TestSquirrelCookWritesUnboundedText(t *testing.T) {
	t.Parallel()
	got, rows := utcRun(t, nil)
	for i := range rows {
		for _, order := range []string{"1", "2"} {
			key := fmt.Sprintf("row%d %s", i, order)
			if got[key] != "ok" {
				t.Errorf("%s: %q, want ok", key, got[key])
			}
		}
	}
}

// TestSquirrelCookWritesUnboundedTextNegativeControl writes the text inline,
// as the writer did before it carved a blob: the row holding text must go red
// in both orders and the empty row must stay green.
func TestSquirrelCookWritesUnboundedTextNegativeControl(t *testing.T) {
	t.Parallel()
	got, _ := utcRun(t, fpPatch(t, "schema_table_cookwrite.nut", [][2]string{
		{utcCarveAnchor, "  if (false) {\n    // UNBOUNDED TEXT"},
		{`(f.shape == "arr" && f.list) || (f.shape == "str" && f.unbounded)) return true;`, `(f.shape == "arr" && f.list)) return true;`},
	}))
	for _, order := range []string{"1", "2"} {
		if v := got["row0 "+order]; v != "ok" {
			t.Errorf("row0 %s went red under the control: %q", order, v)
		}
		if v := got["row1 "+order]; v == "ok" {
			t.Errorf("row1 %s stayed green under the control", order)
		}
	}
}

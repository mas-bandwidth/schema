package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestLobsterJsonTextListTotalClampsPrefix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "TextTotal.schema")
	const schema = `package texttotal

table Root
{
    names [..4]string(8) | total = 5
}
`
	if err := os.WriteFile(source, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{source})
	if err != nil {
		t.Fatalf("generate text-list unit: %v", err)
	}
	var b strings.Builder
	for _, mod := range g.mods {
		b.WriteString("import ." + mod + "\n")
	}
	b.WriteString(`import .load
import .json

let report = schema_rt.LoadReport {}
let text = read_file("input")
assert text
let rec = schema_rt.json_read("Root", text, report)
assert rec
let names = rec.fields[0]
let first = names.elems[0].tab
assert first
print("{report.clamped}|{report.reason}|{names.count}|" + first.fields[0].cell.s)
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{"names":["abc","def"]}`
	got := string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"input": []byte(input)}))
	if got != "1|clamped: Root.names|1|abc\n" {
		t.Fatalf("Lobster JSON total result %q, want one total clamp and the fitting prefix", got)
	}

	model := tabletext.NewModel(g.u)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(input), &report) {
		t.Fatal("the reference refused ordinary text-list JSON")
	}
	if report.Clamped != 1 || report.Reason != "clamped: Root.names" {
		t.Fatalf("reference total report = %d, %q", report.Clamped, report.Reason)
	}
	var names int = -1
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "names" {
			names = inst.Fields[i].Count
		}
	}
	if names != 1 {
		t.Fatalf("reference kept %d text-list elements, want 1", names)
	}
}

package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// closedSchema is the small unit the read-closed vectors read: a flags field,
// an enum, a union, a map keyed by string and a string field, plus a pointered
// root for the `&node` name's site.
const closedSchema = `package closed

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

func closedModel(t *testing.T) *tabletext.Model {
	t.Helper()
	ast, errs := parser.Parse("Closed.schema", []byte(closedSchema))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	unit, cerrs := check.Unit([]check.SourceFile{{
		Path: "Closed.schema", Name: "Closed.schema", Base: "Closed", Bytes: []byte(closedSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	return tabletext.NewModel(unit)
}

// TestTextReadClosedRows holds every manifest row of
// testdata/wire/json-read-closed/ through the engine: a `read` row places the
// value with nothing counted, and a `refused` row refuses the whole load.
func TestTextReadClosedRows(t *testing.T) {
	t.Parallel()
	m := closedModel(t)
	dir := filepath.Join("..", "..", "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
		if verdict != "read" && verdict != "refused" {
			t.Errorf("%s: verdict %q, want read or refused", name, verdict)
			continue
		}
		st := m.Lookup(root)
		if st == nil {
			t.Errorf("%s: no root %q in the unit", name, root)
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, jsonFile))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		inst := m.New(st)
		var report tabletext.Report
		ok := m.Read(inst, body, &report)
		switch verdict {
		case "read":
			if !ok || report.Refused {
				t.Errorf("%s: read refused %q: %+v", name, body, report)
			}
		case "refused":
			if ok || !report.Refused {
				t.Errorf("%s: read accepted %q", name, body)
			}
		}
		rows++
	}
	if rows == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}
}

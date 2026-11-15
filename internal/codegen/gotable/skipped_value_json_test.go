package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoSkippedValueIsValidated holds the generated Go reader to the standing
// rule of docs/SPEC-TABLES.md §4 and §16.3 (docs/rulings.json
// skipped-value-is-validated): a value the reader SKIPS is held to what a read
// of it would hold, and damage inside it refuses the whole read. The cases are
// the shared vectors' JSON rows, each with its verdict in the manifest; the
// message rows are the message skip's own card and are not run here.
func TestGoSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	rows := skippedValueJSONRows(t, filepath.Join(root, "testdata", "wire", "skipped-value"))
	if len(rows) == 0 {
		t.Fatal("the manifest carries no JSON rows")
	}
	u := loadUnitPaths(t, root, []string{"test/skipped-value/reader"})
	var source strings.Builder
	source.WriteString("package ")
	source.WriteString(u.Package)
	source.WriteString("\n\nimport \"testing\"\n\nfunc TestSkippedValueJSON(t *testing.T) {\n\tt.Parallel()\n\trows := []struct {\n\t\tname string\n\t\ttext string\n\t\tread bool\n\t}{\n")
	for _, row := range rows {
		fmt.Fprintf(&source, "\t\t{%q, %q, %t},\n", row.file, string(row.text), row.read)
	}
	source.WriteString("\t}\n\tfor _, row := range rows {\n\t\trow := row\n\t\tt.Run(row.name, func(t *testing.T) {\n\t\t\tt.Parallel()\n\t\t\tvar value Root\n\t\t\tvar report TableReport\n\t\t\tok := RootFromJSON(&value, []byte(row.text), &report)\n\t\t\tif ok != row.read {\n\t\t\t\tt.Fatalf(\"%s: read=%v, want %v (report %+v)\", row.name, ok, row.read, report)\n\t\t\t}\n\t\t})\n\t}\n}\n")
	if out, err := runGeneratedUnit(t, u, source.String(), nil); err != nil {
		t.Fatalf("generated skipped-value probe: %v\n%s", err, out)
	}
}

type skippedValueRow struct {
	file string
	text []byte
	read bool
}

// skippedValueJSONRows reads the manifest itself, never a copied list, and the
// JSON vector each of its `json` rows names.
func skippedValueJSONRows(t *testing.T, dir string) []skippedValueRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []skippedValueRow
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) != 3 {
			t.Fatalf("%s: manifest row %q is not file, form, verdict", dir, line)
		}
		if fields[1] != "json" {
			continue
		}
		if fields[2] != "read" && fields[2] != "refused" {
			t.Fatalf("%s: manifest verdict %q is neither read nor refused", fields[0], fields[2])
		}
		text, err := os.ReadFile(filepath.Join(dir, fields[0]))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, skippedValueRow{file: fields[0], text: text, read: fields[2] == "read"})
	}
	return rows
}

package mootable

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// compared on the verdict alone with the Go engine's (internal/tabletext), and
// the engine is first held to the verdict the vectors state. Running the
// generated moo makes this a functional test, gated by slowtest.

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const mooJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type mooJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func mooJsonFlagsNameOnceRows(t *testing.T) []mooJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(mooJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []mooJsonFlagsNameOnceRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 6 {
			t.Fatalf("MANIFEST.tsv: %d columns, want 6: %q", len(c), line)
		}
		rows = append(rows, mooJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

func TestMooJsonFlagsNameOnce(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()

	rows := mooJsonFlagsNameOnceRows(t)
	unit, root := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != root {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, root)
		}
	}
	u := loadUnit(t, []string{unit})

	model := tabletext.NewModel(u)
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("%s: the unit declares no %s", unit, root)
	}

	inputs := map[string][]byte{}
	var cases []textCase
	for _, row := range rows {
		src, err := os.ReadFile(filepath.Join(mooJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(model.New(def), src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		inputs[row.name] = src
		cases = append(cases, textCase{root, row.name})
	}

	res := textRun(t, u, cases, inputs, nil)
	for i, row := range rows {
		cols := strings.SplitN(res[i][0], ",", 8)
		if len(cols) != 8 {
			t.Fatalf("%s: the probe printed %q", row.name, res[i][0])
		}
		if cols[6] != row.verdict {
			t.Errorf("%s: the strict read %s, want %s", row.name, cols[6], row.verdict)
		}
	}
}

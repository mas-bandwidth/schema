package jstable

// TestJsSkippedValueIsValidated holds the JavaScript leg to
// docs/rulings.json's skipped-value-is-validated rule (docs/SPEC-TABLES.md §4,
// §3.3, §16.3): a value the reader SKIPS is held to what a read of it would
// hold, and damage inside it refuses the whole read. The shared vectors under
// testdata/wire/skipped-value/ are the cases, each with its verdict in the
// manifest; this test runs every JSON row through the leg's JSON read and
// every message row through the leg's message load under the writer's
// announcement.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type skippedValueRow struct {
	file, form, verdict string
}

// readSkippedValueManifest reads testdata/wire/skipped-value/MANIFEST.tsv:
// <file> TAB <form> TAB <verdict>, the file named relative to the manifest.
func readSkippedValueManifest(t *testing.T) []skippedValueRow {
	t.Helper()
	const manifestPath = "../../../testdata/wire/skipped-value/MANIFEST.tsv"
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var rows []skippedValueRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifestPath, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifestPath, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifestPath, i+1, f[2])
		}
		rows = append(rows, skippedValueRow{file: f[0], form: f[1], verdict: f[2]})
	}
	if len(rows) == 0 {
		t.Fatal("the skipped-value manifest names no rows")
	}
	return rows
}

func skippedValueVectors(t *testing.T, form string) map[string][]byte {
	t.Helper()
	const dir = "../../../testdata/wire/skipped-value"
	out := map[string][]byte{}
	for _, row := range readSkippedValueManifest(t) {
		if row.form != form {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		out[row.file] = data
	}
	if len(out) == 0 {
		t.Fatalf("the skipped-value manifest names no %s rows", form)
	}
	return out
}

func TestJsSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated modules)")

	reader := loadUnitPaths(t, []string{repoPath("test/skipped-value/reader")})

	t.Run("json", func(t *testing.T) {
		t.Parallel()
		files := skippedValueVectors(t, "json")
		var rows strings.Builder
		for _, row := range readSkippedValueManifest(t) {
			if row.form == "json" {
				rows.WriteString(`["` + row.file + `","` + row.verdict + `"],`)
			}
		}
		jsDiscardRun(t, reader, files, `
const rows = [`+rows.String()+`];
for (const [file, verdict] of rows) {
  const r = new m.TableVariableReport();
  const value = m.RootFromJson(readFileSync(file, "utf8"), r);
  const got = value === null ? "refused" : "read";
  if (got !== verdict) {
    fail(file + ": verdict " + got + ", want " + verdict + " " + JSON.stringify(r));
  }
}
`)
	})

	t.Run("message", func(t *testing.T) {
		t.Parallel()
		node := jsNode(t)
		files := skippedValueVectors(t, "message")
		var rows strings.Builder
		for _, row := range readSkippedValueManifest(t) {
			if row.form == "message" {
				rows.WriteString(`["` + row.file + `","` + row.verdict + `"],`)
			}
		}
		st := findTable(t, reader, "Root")
		probe := `import { readFileSync } from "node:fs";
import { TableVocabulary, AnnounceRead, ` + st.Name + ` } from "./` + messageHomeBase(reader) + `.js";
import { ` + st.Name + `LoadMessages } from "./` + messageRootBase(reader, st) + `.js";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
const announcement = new Uint8Array(readFileSync("announcement.bin"));
const rows = [` + rows.String() + `];
for (const [file, verdict] of rows) {
  const v = new TableVocabulary();
  v.Init(new Array(4096));
  const ar = {};
  const announced = AnnounceRead(v, announcement, ar);
  let got;
  if (file === "announcement.bin") {
    got = announced ? "read" : "refused";
  } else {
    const storage = [new ` + st.Name + `()];
    const rep = {};
    const n = announced ? ` + st.Name + `LoadMessages(storage, v, new Uint8Array(readFileSync(file)), rep) : -1;
    got = (n === 1 && !rep.Damaged && rep.Verdict === 0) ? "read" : "refused";
  }
  if (got !== verdict) {
    fail(file + ": verdict " + got + ", want " + verdict);
  }
}
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
		out, err := jsRunMessageProbe(t, node, reader, files, probe)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	})
}

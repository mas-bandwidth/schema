package jstable

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A JSON number's LENGTH is never why a read refuses: a valid number is read
// by its value however many bytes it spans, placed or under an unknown key
// (docs/SPEC-TABLES.md §16.2). The rows past 512 bytes are the ones a length
// cap would refuse; the Go engine reads the same text and is the reference.
func TestJavaScriptJsonNumberAnyLength(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const root = "ProfileConfig"
	u := loadUnitForMeasure(t, repo, "tables/examples")
	m := tabletext.NewModel(u)
	def := m.Lookup(root)
	if def == nil {
		t.Fatalf("no table %s in tables/examples", root)
	}

	zeros := func(n int) string { return strings.Repeat("0", n) }
	cases := []struct {
		name string
		text string
		ok   bool
	}{
		{"experience-509", `{"experience":1.` + zeros(509) + `}`, true},
		{"experience-510", `{"experience":1.` + zeros(510) + `}`, true},
		{"experience-511", `{"experience":1.` + zeros(511) + `}`, true},
		{"experience-600", `{"experience":1.` + zeros(600) + `}`, true},
		{"precision-600", `{"precision":1.` + zeros(600) + `}`, true},
		{"unknown-key-5000", `{"x":1` + zeros(4999) + `}`, true},
		{"experience-600-integer", `{"experience":1` + zeros(599) + `}`, false},
	}

	type result struct {
		OK      bool   `json:"ok"`
		Canon   string `json:"canon"`
		Unknown int    `json:"unknown"`
	}
	expected := make([]result, len(cases))
	type input struct {
		Name string `json:"name"`
		Text string `json:"text"`
	}
	inputs := make([]input, len(cases))
	for i, c := range cases {
		inputs[i] = input{c.name, c.text}
		inst := m.New(def)
		report := &tabletext.Report{}
		ok := m.Read(inst, []byte(c.text), report)
		if ok != c.ok {
			t.Fatalf("%s: Go read = %v, want %v (%+v)", c.name, ok, c.ok, report)
		}
		want := result{OK: ok, Unknown: report.Unknown}
		if ok {
			text, err := m.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			want.Canon = string(text)
		}
		expected[i] = want
	}

	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	spec, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), spec, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`import { readFileSync } from "node:fs";
import { %sFromJson, %sToJson, TableVariableReport } from "./%sVariable.js";
const rows = JSON.parse(readFileSync("cases.json", "utf8"));
for (const row of rows) {
  const report = new TableVariableReport();
  const value = %sFromJson(row.text, report);
  const ok = value !== null;
  const canon = ok ? %sToJson(value).trimEnd() : "";
  console.log(JSON.stringify({ ok: ok, canon: canon, unknown: report.unknown }));
}
`, root, root, runtimeHome(u), root, root)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "probe.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native number probe: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d native rows, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got result
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if got != expected[i] {
				t.Errorf("native: %+v\nGo:     %+v", got, expected[i])
			}
		})
	}
}

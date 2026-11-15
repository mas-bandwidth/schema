package jstable

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A repeated key refuses the read in every object the text walks, whether the
// key names a field, names no field, or sits inside a value the reader only
// skips (docs/SPEC-TABLES.md §16.2, §16.12). The verdict is the contract; the
// JavaScript leg and the engine must agree on it row by row.
func TestJavaScriptTextRepeatedKeyEveryObject(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	node := jsNode(t)
	u := unitFrom(t, `package probe
table Child {
 value int32
}
table Root {
 id int32
 child Child
}
`)
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
	cases := []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	}{
		{"unknown-key-at-root", `{"extra":1,"extra":2}`},
		{"unknown-key-in-nested-table", `{"child":{"extra":1,"extra":2}}`},
		{"repeat-in-skipped-object", `{"extra":{"a":1,"a":2}}`},
		{"repeat-in-skipped-object-two-levels", `{"extra":{"inner":{"a":1,"a":2}}}`},
		{"repeat-in-skipped-array-object", `{"extra":[{"a":1,"a":2}]}`},
		{"control-distinct-unknown-keys", `{"extra":1,"other":2}`},
		{"control-same-key-in-two-objects", `{"child":{"value":1},"extra":{"value":2}}`},
	}
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	expected := make([]bool, len(cases))
	for i, c := range cases {
		inst := model.New(def)
		expected[i] = model.Read(inst, []byte(c.Input), &tabletext.Report{})
	}
	spec, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), spec, 0o600); err != nil {
		t.Fatal(err)
	}
	const probe = `import { readFileSync } from "node:fs";
import { RootFromJson, TableVariableReport } from "./ProbeVariable.js";
const rows = JSON.parse(readFileSync("cases.json", "utf8"));
for (const row of rows) {
  const report = new TableVariableReport();
  const value = RootFromJson(row.input, report);
  console.log(JSON.stringify({ OK: value !== null }));
}
`
	if err := os.WriteFile(filepath.Join(dir, "repeat.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "repeat.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native repeated-key probe: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d native rows, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var got struct {
				OK bool
			}
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if got.OK != expected[i] {
				t.Errorf("input %s\nnative: %v\nGo:     %v", c.Input, got.OK, expected[i])
			}
		})
	}
}

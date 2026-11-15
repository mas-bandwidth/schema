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

// A flags array that names the same known variant twice is a repeated key and
// refuses the read (§16.2), wherever the array is read: at a field, inside a
// nested table, or as a map value. An unknown name is skipped and counted,
// repeated or not.
func TestJavaScriptTextFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	node := jsNode(t)
	u := unitFrom(t, `package probe
flags Caps { Jump, Crouch, Fly }
table Child {
 caps Caps
}
table Root {
 caps Caps
 child Child
 by_name map[string(8)]Caps
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
		name, input string
	}{
		{"field-repeated", `{"caps":["Jump","Jump"]}`},
		{"nested-repeated", `{"child":{"caps":["Jump","Jump"]}}`},
		{"map-value-repeated", `{"by_name":{"a":["Jump","Jump"]}}`},
		{"distinct", `{"caps":["Jump","Fly"]}`},
		{"unknown-twice", `{"caps":["Zed","Zed"]}`},
		{"known-beside-unknown", `{"caps":["Jump","Zed"]}`},
	}
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	expected := make([]bool, len(cases))
	rows := make([]struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	}, len(cases))
	for i, c := range cases {
		rows[i].Name = c.name
		rows[i].Input = c.input
		expected[i] = model.Read(model.New(def), []byte(c.input), &tabletext.Report{})
	}
	spec, err := json.Marshal(rows)
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
  const value = RootFromJson(row.input, new TableVariableReport());
  console.log(JSON.stringify({ Name: row.name, OK: value !== null }));
}
`
	if err := os.WriteFile(filepath.Join(dir, "flags.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "flags.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native flags probe: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d native rows, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got struct {
				Name string
				OK   bool
			}
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if got.OK != expected[i] {
				t.Errorf("%s: input %s\nnative: OK=%v\nGo:     OK=%v", c.name, c.input, got.OK, expected[i])
			}
		})
	}
}

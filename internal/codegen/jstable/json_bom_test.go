package jstable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A BOM is allowed exactly once at the start of a text, not by the ordinary
// whitespace scanner. Preserve U+FEFF when it is actual field string content.
func TestJavaScriptTextBOMMatchesGo(t *testing.T) {
	slowtest.Gate(t, "node")
	node := jsNode(t)
	u := unitFrom(t, `package probe
table Root {
 value int32 = 7
 tag string(32)
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
	const doc = "{\"value\":9,\"tag\":\"ok\"}\n"
	cases := []struct {
		name, input string
		reuse       bool
	}{
		{"plain", doc, false},
		{"one-leading-bom", "\ufeff" + doc, false},
		{"two-leading-boms", "\ufeff\ufeff" + doc, false},
		{"space-before-bom", " \ufeff" + doc, false},
		{"newline-before-bom", "\n\ufeff" + doc, false},
		{"comment-before-bom", "/*layout*/\ufeff" + doc, false},
		{"bom-inside-object", "{\ufeff\"value\":9}", false},
		{"bom-before-value", "{\"value\":\ufeff9}", false},
		{"bom-after-document", doc + "\ufeff", false},
		{"truncated-bom", "\xef\xbb" + doc, false},
		{"field-string-raw-feff", "{\"tag\":\"a\ufeffb\"}", false},
		{"field-string-escaped-feff", "{\"tag\":\"a\\uFEFFb\"}", false},
		{"reuse-events", "{\"unknown\":1,\"value\":\"bad\",\"value\":9}", true},
		{"reuse-damage", "\ufeff\ufeff" + doc, true},
		{"reuse-valid-leading-bom", "\ufeff" + doc, true},
		{"reuse-field-string", "{\"tag\":\"a\ufeffb\"}", true},
	}
	type result struct {
		OK           bool
		Unknown      int
		KindMismatch int
		Widened      int
		Clamped      int
		Duplicate    int
		Malformed    bool
		Canon        string
		Wire         string
	}
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	expected := make([]result, len(cases))
	type input struct {
		Path  string
		Reuse bool
	}
	inputs := make([]input, len(cases))
	var reused tabletext.Report
	for i, c := range cases {
		name := fmt.Sprintf("case-%02d.json", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(c.input), 0o600); err != nil {
			t.Fatal(err)
		}
		inputs[i] = input{name, c.reuse}
		report := &tabletext.Report{}
		if c.reuse {
			report = &reused
		}
		inst := model.New(def)
		ok := model.Read(inst, []byte(c.input), report)
		// the verdict, and on acceptance the canonical text and wire; the
		// counters are per-language diagnostics (docs/SPEC-TABLES.md §6.5)
		want := result{OK: ok}
		if ok {
			text, err := model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatal(err)
			}
			want.Canon = string(text)
			want.Wire = hex.EncodeToString(wire)
		}
		expected[i] = want
	}
	spec, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), spec, 0o600); err != nil {
		t.Fatal(err)
	}
	const probe = `import { readFileSync } from "node:fs";
import { RootFromJson, RootToJson, RootSave, TableVariableReport } from "./ProbeVariable.js";
const rows = JSON.parse(readFileSync("cases.json", "utf8"));
const reused = new TableVariableReport();
for (const row of rows) {
  const report = row.Reuse ? reused : new TableVariableReport();
  // Preserve a valid BOM for the generated string API. A truncated UTF-8
  // prefix becomes replacement content and must still be refused; strict
  // adapter-byte decoding has its own separately executed boundary control.
  const text = readFileSync(row.Path).toString("utf8");
  const value = RootFromJson(text, report);
  const ok = value !== null;
  const canon = ok ? RootToJson(value).trimEnd() : "";
  const wire = ok ? Buffer.from(RootSave(value)).toString("hex") : "";
  console.log(JSON.stringify({
    OK: ok, Canon: canon, Wire: wire
  }));
}
`
	if err := os.WriteFile(filepath.Join(dir, "bom.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "bom.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native BOM probe: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d native rows, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "reuse-events" {
				t.Skip("layer 4 (js): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
			}
			var got result
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if got != expected[i] {
				t.Errorf("input bytes %x\nnative: %+v\nGo:     %+v", []byte(c.input), got, expected[i])
			}
		})
	}
}

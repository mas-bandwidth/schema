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
)

// TestJavaScriptTextBase64MatchesEngine pins docs/SPEC-TABLES.md §16.2's
// bytes(N) rule: the JavaScript text reader and the compiler's engine give the
// same verdict on every base64 body, and on a read the same bytes. The JSON
// layer decides first, so a body's escapes are undone before base64 sees it;
// standard-alphabet symbols, then zero to two trailing `=` of the right count,
// and a body that does not decode refuses.
func TestJavaScriptTextBase64MatchesEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	node := jsNode(t)
	u := unitFrom(t, `package probe
table Root {
 v bytes(4)
 p *bytes
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

	bodies := []struct{ name, body string }{
		{"empty", ""},
		{"canonical-padded", "AQI="},
		{"canonical-unpadded", "AQI"},
		{"one-pad-where-two-belong", "AQ="},
		{"single-symbol", "A"},
		{"pad-in-the-middle", "AQ=I"},
		{"pad-at-the-front", "=AQI"},
		{"four-pads-after-a-full-group", "AQID===="},
		{"pads-alone", "===="},
		{"escaped-symbol", `\u0041QI=`},
		{"escaped-padding", `\u0041QI\u003D`},
		{"escaped-one-pad-where-two-belong", `AQ\u003D`},
		{"outside-the-alphabet", "AQ!D"},
		{"one-byte-past-the-bound", "AQIDBAU="},
	}
	fields := []struct{ name, key string }{
		{"bytes4", "v"},
		{"pointer-bytes", "p"},
	}
	type row struct {
		name   string
		field  string
		text   string
		path   string
		wantOK bool
		want   string
	}
	var rows []row
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	for _, f := range fields {
		for _, b := range bodies {
			text := `{"` + f.key + `":"` + b.body + `"}`
			inst := model.New(def)
			ok := model.Read(inst, []byte(text), &tabletext.Report{})
			want := ""
			if ok {
				fv, found := inst.FieldByKey(f.key)
				if !found {
					t.Fatalf("%s: no %s field", b.name, f.key)
				}
				data := fv.Cell.Str
				if fv.Cell.Blob != nil {
					data = fv.Cell.Blob.Data
				}
				want = hex.EncodeToString(data)
			}
			path := fmt.Sprintf("case-%02d.json", len(rows))
			if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row{
				name:   f.name + "/" + b.name,
				field:  f.key,
				text:   text,
				path:   path,
				wantOK: ok,
				want:   want,
			})
		}
	}

	type input struct {
		Path  string
		Field string
	}
	inputs := make([]input, len(rows))
	for i, r := range rows {
		inputs[i] = input{r.path, r.field}
	}
	spec, err := json.Marshal(inputs)
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
  const text = readFileSync(row.Path, "utf8");
  const value = RootFromJson(text, new TableVariableReport());
  const ok = value !== null;
  let bytes = "";
  if (ok) {
    const b = value[row.Field];
    if (b !== null) { bytes = Buffer.from(b).toString("hex"); }
  }
  console.log(JSON.stringify({ OK: ok, Bytes: bytes }));
}
`
	if err := os.WriteFile(filepath.Join(dir, "base64.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "base64.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native base64 probe: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d native rows, want %d:\n%s", len(lines), len(rows), out)
	}
	for i, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			var got struct {
				OK    bool
				Bytes string
			}
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if got.OK != r.wantOK {
				t.Errorf("%s: native verdict %t, engine %t", r.text, got.OK, r.wantOK)
			} else if got.OK && got.Bytes != r.want {
				t.Errorf("%s: native bytes %s, engine %s", r.text, got.Bytes, r.want)
			}
		})
	}
}

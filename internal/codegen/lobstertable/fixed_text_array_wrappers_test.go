package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Struct elements keep their scalar strings' explicit lengths in fixed,
// counted and enum-keyed arrays. Check complete fixed bytes and canonical JSON
// through the public Record bridge, including an unused counted tail and the
// second keyed slot without the first.
func TestLobsterFixedTextArrayWrappers(t *testing.T) {
	g := auditUnit(t, `package wrapperaudit
enum Key { First, Second }
type Leaf {
    text string(8)
    data bytes(4)
    wide wstring(3)
}
fixed table Root {
    fixed_values [2]Leaf
    counted_values [..2]Leaf
    keyed_values [Key]Leaf
}
`)
	input := []byte(`{"fixed_values":[{"text":"ab","data":"AQI=","wide":"λ"},{"text":"cd","data":"/wA=","wide":"🙂"}],"counted_values":[{"text":"ef","data":"AAM=","wide":"Z"}],"keyed_values":{"Second":{"text":"λb","data":"BAX/","wide":"Qλ"}}}`)
	m := tabletext.NewModel(g.u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, input, &report) || report.Refused {
		t.Fatalf("wrapper input refused: %+v", report)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := m.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	auditRun(t, g, `import .AuditTable
import .schema_save
import .json
let input = read_file("input")
let expected = read_file("wire")
let canonical = read_file("canonical")
assert input and expected and canonical
let rec = schema_rt.json_read("Root", input, schema_rt.LoadReport {})
assert rec
let wire, reason = schema_rt.save_reasoned(rec)
assert reason == "" and wire == expected and wire[0] == 3
let loaded = schema_rt.load("Root", expected, schema_rt.LoadReport {})
assert loaded
assert length(loaded.fields[0].elems) == 2 and loaded.fields[1].count == 1
let back, back_reason = schema_rt.save_reasoned(loaded)
assert back_reason == "" and back == expected
let text, text_reason = schema_rt.json_write(loaded)
assert text_reason == "" and text == canonical
print("pass")
`, map[string][]byte{"input": input, "wire": wire, "canonical": append(canonical, '\n')})
}

func TestLobsterDirectTextScalarArraysRemainRefused(t *testing.T) {
	for _, scalar := range []string{"string(8)", "bytes(4)", "wstring(3)"} {
		for _, shape := range []string{"[2]", "[..2]", "[Key]"} {
			t.Run(shape+scalar, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "Direct.schema")
				source := fmt.Sprintf("package directaudit\nenum Key { First, Second }\nfixed table Root { values %s%s }\n", shape, scalar)
				if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
					t.Fatal(err)
				}
				c := compiler.New()
				u, err := c.Load([]string{path})
				if err == nil {
					_, err = c.Generate(u, "lobster", nil)
				}
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "array") {
					t.Fatalf("direct scalar array did not retain its named refusal: %v", err)
				}
			})
		}
	}
}

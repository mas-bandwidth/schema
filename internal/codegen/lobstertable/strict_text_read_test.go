package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const strictTextSchema = `package stricttextprobe
table Node { value int32 | required
 next *Node }
table Root {
 n int32 | min = 0, max = 10
 s string(4)
 wide wstring(2)
 a [..2]int32
 words [..2]string(4) | total = 5
 keys map[string(4)]int32
 f float32
 link *Node
 other *Node
 blob bytes(3)
 optional ?int32
 big int64
 x fixed(16,16) | min = -32768, max = 32767
 y ufixed(16,16) | min = 0, max = 65535
 need int32 | required
}
`

type strictTextCase struct{ name, input, want string }

// Accepted reads are compared with independently specified values through the
// reference writer. Refused reads must preserve a caller-owned variable root,
// including its shared pointees; no report-counter equality is involved.
func runStrictTextCases(t *testing.T, form string, cases []strictTextCase) {
	t.Helper()
	schema := filepath.Join(t.TempDir(), "StrictText.schema")
	if err := os.WriteFile(schema, []byte(strictTextSchema), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{schema})
	if err != nil {
		t.Fatal(err)
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
import .text_form
import .schema_save
let seed = read_file("seed.json")
assert seed
let rec = schema_rt.json_read("Root", seed, schema_rt.LoadReport {})
assert rec
let before = schema_rt.save(rec)
def probe(i:int, accept:bool):
    let input = read_file("input{i}")
    assert input
`)
	if form == "json" {
		program.WriteString(`    let report = schema_rt.LoadReport {}
    let ok = schema_rt.json_read_into(rec, input, report)
    assert ok == accept
    if ok:
        let out, reason = schema_rt.json_write(rec)
        assert reason == ""
        assert write_file("out{i}", out)
        assert schema_rt.json_read_into(rec, seed, schema_rt.LoadReport {})
    else:
        assert report.refused
    assert schema_rt.save(rec) == before
`)
	} else {
		program.WriteString(`    let doc = schema_rt.text_load("Root", input)
    assert (doc != nil) == accept
    if doc:
        assert schema_rt.text_save(doc) == input
        let out, reason = schema_rt.json_write(doc.rec)
        assert reason == ""
        assert write_file("out{i}", out)
        schema_rt.release(doc.rec)
`)
	}
	inputs := map[string][]byte{"seed.json": []byte(`{"need":7,"n":5,"link":{"&node":9,"value":9},"other":{"&node":9}}`)}
	for i, tc := range cases {
		inputs[fmt.Sprintf("input%d", i)] = []byte(tc.input)
		fmt.Fprintf(&program, "probe(%d, %t)\n", i, tc.want != "")
	}
	program.WriteString("schema_rt.release(rec)\nprint(\"PASS\")\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "PASS\n" {
		t.Fatalf("native output: %q", out)
	}
	model := tabletext.NewModel(g.u)
	for i, tc := range cases {
		if tc.want == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			expected := model.New(model.Lookup("Root"))
			var report tabletext.Report
			if !model.Read(expected, []byte(tc.want), &report) {
				t.Fatal("reference refuses expected value")
			}
			want, err := model.Write(expected)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(g.dir, fmt.Sprintf("out%d", i)))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want)+"\n" {
				t.Fatalf("read changed the value:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestLobsterStrictJSONRead(t *testing.T) {
	t.Parallel()
	runStrictTextCases(t, "json", []strictTextCase{
		{"defaults", `{"need":1}`, `{"need":1}`},
		{"bounds", `{"need":1,"n":10,"s":"αβ","wide":"😀","a":[1,2],"words":["abc","de"],"keys":{"abcd":2},"f":1.5}`, `{"need":1,"n":10,"s":"αβ","wide":"😀","a":[1,2],"words":["abc","de"],"keys":{"abcd":2},"f":1.5}`},
		{"unknown", `{"need":1,"future":{"x":[1,true,null,"ok"]}}`, `{"need":1}`},
		{"base64", `{"need":1,"blob":"YQ"}`, `{"need":1,"blob":"YQ=="}`},
		{"optional", `{"need":1,"optional":null}`, `{"need":1}`},
		{"sharing", `{"need":1,"link":{"&node":1,"value":3},"other":{"&node":1}}`, `{"need":1,"link":{"&node":1,"value":3},"other":{"&node":1}}`},
		{"duplicate", `{"need":1,"need":2}`, ""},
		{"escaped duplicate", `{"need":1,"\u006eeed":2}`, ""},
		{"unknown duplicate", `{"need":1,"future":1,"future":2}`, ""},
		{"nested unknown duplicate", `{"need":1,"future":{"x":1,"x":2}}`, ""},
		{"required", `{}`, ""},
		{"range", `{"need":1,"n":11}`, ""},
		{"fraction", `{"need":1.25}`, ""},
		{"kind", `{"need":true}`, ""},
		{"float overflow", `{"need":1,"f":1e39}`, ""},
		{"string bound", `{"need":1,"s":"abcde"}`, ""},
		{"wide bound", `{"need":1,"wide":"😀a"}`, ""},
		{"array bound", `{"need":1,"a":[1,2,3]}`, ""},
		{"array kind", `{"need":1,"a":[true]}`, ""},
		{"text total", `{"need":1,"words":["abc","def"]}`, ""},
		{"map key bound", `{"need":1,"keys":{"abcde":2}}`, ""},
		{"map value", `{"need":1,"keys":{"a":true}}`, ""},
		{"map duplicate", `{"need":1,"keys":{"a":1,"\u0061":2}}`, ""},
		{"high surrogate", `{"need":1,"s":"\ud800"}`, ""},
		{"low surrogate", `{"need":1,"s":"\udc00"}`, ""},
		{"unknown surrogate", `{"need":1,"future":"\ud800"}`, ""},
		{"malformed unknown", `{"need":1,"future":[1,,2]}`, ""},
		{"invalid base64", `{"need":1,"blob":"Y=Q="}`, ""},
		{"blob bound", `{"need":1,"blob":"YWJjZA=="}`, ""},
		{"required pointee", `{"need":1,"link":{}}`, ""},
		{"fixed boundary", `{"need":1,"x":-32768,"y":65535}`, `{"need":1,"x":-32768,"y":65535}`},
		{"fixed signed excess", `{"need":1,"x":32767.0000152587890625}`, ""},
		{"fixed unsigned excess", `{"need":1,"y":65536}`, ""},
		{"fixed finer fraction", `{"need":1,"x":0.1}`, ""},
		{"stored zero", `{"need":1,"s":"\u0000"}`, ""},
		{"long field key", `{"need":1,"` + strings.Repeat("x", 256) + `":0}`, ""},
		{"partial graph", `{"need":1,"link":{"&node":1,"value":3},"other":{"&node":1},"s":"abcde"}`, ""},
		{"cycle", `{"need":1,"link":{"&node":1,"next":{"&node":1}}}`, ""},
		{"unknown cycle", `{"need":1,"future":{"&node":1,"next":{"&node":1}}}`, ""},
	})
}

func TestLobsterStrictTextRead(t *testing.T) {
	t.Parallel()
	runStrictTextCases(t, "text", []strictTextCase{
		{"defaults", `(:need 1)`, `{"need":1}`},
		{"bounds", `(:need 1 :n 10 :s "αβ" :wide "😀" :a (1 2) :words ("abc" "de") :keys (("abcd" 2)) :f 1.5)`, `{"need":1,"n":10,"s":"αβ","wide":"😀","a":[1,2],"words":["abc","de"],"keys":{"abcd":2},"f":1.5}`},
		{"escapes", `(:need 1 :s "\u03b1")`, `{"need":1,"s":"α"}`},
		{"exact exponent", `(:need 1e0 :n 10.0)`, `{"need":1,"n":10}`},
		{"sharing", `(:need 1 :link ("&node" 1 :value 3) :other ("&node" 1))`, `{"need":1,"link":{"&node":1,"value":3},"other":{"&node":1}}`},
		{"exact int64 exponent", `(:need 1 :big 9007199254740993e0)`, `{"need":1,"big":9007199254740993}`},
		{"base64", `(:need 1 :blob "YQ==")`, `{"need":1,"blob":"YQ=="}`},
		{"optional", `(:need 1 :optional null)`, `{"need":1}`},
		{"duplicate", `(:need 1 "need" 2)`, ""},
		{"map duplicate", `(:need 1 :keys (("a" 1) ("\u0061" 2)))`, ""},
		{"required", `()`, ""},
		{"range", `(:need 1 :n 11)`, ""},
		{"fraction", `(:need 1.25)`, ""},
		{"overflow", `(:need 4294967296)`, ""},
		{"huge exponent", `(:need 1e999999999)`, ""},
		{"float overflow", `(:need 1 :f 1e39)`, ""},
		{"string bound", `(:need 1 :s "abcde")`, ""},
		{"wide bound", `(:need 1 :wide "😀a")`, ""},
		{"array bound", `(:need 1 :a (1 2 3))`, ""},
		{"text total", `(:need 1 :words ("abc" "def"))`, ""},
		{"map bound", `(:need 1 :keys (("abcde" 1)))`, ""},
		{"lone surrogate", `(:need 1 :s "\ud800")`, ""},
		{"invalid number", `(:need 1e)`, ""},
		{"leading plus", `(:need +1)`, ""},
		{"unknown", `(:need 1 :future 2)`, ""},
		{"base64 pad bits", `(:need 1 :blob "YR==")`, ""},
		{"base64 padding", `(:need 1 :blob "YQ")`, ""},
		{"required pointee", `(:need 1 :link ())`, ""},
		{"fixed boundary", `(:need 1 :x -32768 :y 65535)`, `{"need":1,"x":-32768,"y":65535}`},
		{"fixed signed excess", `(:need 1 :x 32767.0000152587890625)`, ""},
		{"fixed unsigned excess", `(:need 1 :y 65536)`, ""},
		{"fixed finer fraction", `(:need 1 :x 0.1)`, ""},
		{"stored zero", `(:need 1 :s "\u0000")`, ""},
		{"bare CR", "(:need 1)\r", ""},
		{"cycle", `(:need 1 :link ("&node" 1 :next ("&node" 1)))`, ""},
	})
}

// Removing duplicate detection must turn a refused unknown subtree into a
// successful read. This witnesses the layer below descriptor field lookup.
func TestLobsterStrictJSONDuplicateNegativeControl(t *testing.T) {
	t.Parallel()
	schema := filepath.Join(t.TempDir(), "StrictText.schema")
	if err := os.WriteFile(schema, []byte(strictTextSchema), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{schema})
	if err != nil {
		t.Fatal(err)
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
let input = read_file("input.json")
assert input
let value = schema_rt.json_read("Root", input, schema_rt.LoadReport {})
print(if value: "read" else: "refused")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"input.json": []byte(`{"need":1,"future":{"x":1,"x":2}}`)}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "refused\n" {
		t.Fatalf("duplicate read: %q", out)
	}
	patchJson(t, g, "        if prior == key:\n            return j_refuse(r, \"duplicate_key\")", "        if prior == key and length(keys) < 0:\n            return j_refuse(r, \"duplicate_key\")")
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "read\n" {
		t.Fatalf("negative control did not expose dropped duplicate detection: %q", out)
	}
}

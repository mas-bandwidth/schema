package jstable

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJSVariableHistoricalFieldCompatibility(t *testing.T) {
	t.Parallel()
	prefix := `package probe
enum Mode { Red, Blue }
type Child { n int32 }
union Pick { n int32 }
`
	cases := []struct{ name, oldField, newField, input string }{
		{"required_scalar", "x int32 | required", "x int32", ""},
		{"required_string", "x string(4) | required", "x string(4)", ""},
		{"required_bytes", "x bytes(4) | required", "x bytes(4)", ""},
		{"required_array", "x [..2]int32 | required", "x [..2]int32", ""},
		{"required_fixed_array", "x [2]int32 | required", "x [2]int32", ""},
		{"required_map", "x map[string(4)]int32 | required", "x map[string(4)]int32", ""},
		{"required_table", "x Child | required", "x Child", ""},
		{"required_pointer", "x *Child | required", "x *Child", ""},
		{"required_union", "x Pick | required", "x Pick", ""},
		{"required_enum", "x Mode | required", "x Mode", ""},
		{"optional_scalar", "x ?int32", "x int32", `{"x":0}`},
		{"optional_string", "x ?string(4)", "x string(4)", `{"x":""}`},
		{"optional_fixed_array", "x ?[2]int32", "x [2]int32", `{"x":[0,0]}`},
		{"optional_array", "x ?[..2]int32", "x [..2]int32", `{"x":[]}`},
		{"optional_map", "x ?map[string(4)]int32", "x map[string(4)]int32", `{"x":{}}`},
		{"reordered", "a int32\n b int32", "b int32\n a int32", `{"a":7,"b":9}`},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			old := unitFrom(t, prefix+"table Root {\n"+c.oldField+"\n}\n")
			current := unitFrom(t, prefix+"table Root {\n"+c.newField+"\n}\n")
			encode := func(uText string, useOld bool) []byte {
				u := current
				if useOld {
					u = old
				}
				model := tabletext.NewModel(u)
				inst := model.New(model.Lookup("Root"))
				if uText != "" {
					var r tabletext.Report
					if !model.Read(inst, []byte(uText), &r) || !r.Silent() {
						t.Fatalf("%s text refused: %+v", c.name, r)
					}
				}
				wire, err := tablewire.EncodeVariable(model, inst)
				if err != nil {
					t.Fatalf("%s writer: %v", c.name, err)
				}
				return wire
			}
			oldWire := encode(c.input, true)
			newText := c.input
			if c.name != "reordered" {
				newText = ""
			}
			wantWire := encode(newText, false)
			tree := generatedTree(current)
			module := moduleExporting(t, tree, "export function RootLoad(")
			script := fmt.Sprintf(`import * as m from %q;
const wire=Uint8Array.from(Buffer.from(%q,"hex")); const expected=%q;
const report=new m.TableVariableReport(); const value=m.RootLoad(wire,report);
if(value===null||report.err()!==null) {console.error("load",report);process.exit(1)}
const save=m.RootSave(value);if(save===null||Buffer.from(save).toString("hex")!==expected){console.error("canonical save",save&&Buffer.from(save).toString("hex"),expected);process.exit(1)}
const retain=new m.TableVariableRetain(new Uint8Array(65536),new Array(256));const rr=new m.TableVariableReport();const v=m.RootLoadRetain(wire,retain,rr);
if(v===null||rr.err()!==null){console.error("retain",rr);process.exit(1)}
`, "./"+module+".js", hex.EncodeToString(oldWire), hex.EncodeToString(wantWire))
			out, err := jsRunMessageProbe(t, jsNode(t), current, nil, script)
			if err != nil {
				t.Fatalf("%s: %v\n%s", c.name, err, out)
			}
		})
	}
}

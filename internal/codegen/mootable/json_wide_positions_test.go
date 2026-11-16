package mootable_test

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"strings"
	"testing"
)

const positionSchema = `package probe
enum Key { A, B }
union Choice { arm int64 }
table Root {
 plain int64
 values [..2]int64
 opt ?int64
 choice Choice
 lookup map[string]int64
 keyed [Key]int64
 narrow int32
 i128 int128 | min = -100, max = 100
 u128 uint128
 bits40 bits(40)
 fixed64 fixed(48,16) | min = -10, max = 10
 fixed128 fixed(96,32) | min = -10, max = 10
 fixed32 fixed(16,16) | min = -10, max = 10
}
`

var positionCases = []struct{ name, number, quoted string }{{"plain", `{"plain":7}`, `{"plain":"7"}`},
	{"array", `{"values":[7]}`, `{"values":["7"]}`},
	{"optional", `{"opt":7}`, `{"opt":"7"}`},
	{"union", `{"choice":{"arm":7}}`, `{"choice":{"arm":"7"}}`},
	{"map", `{"lookup":{"x":7}}`, `{"lookup":{"x":"7"}}`},
	{"keyed", `{"keyed":{"A":7}}`, `{"keyed":{"A":"7"}}`},
	{"i128", `{"i128":7}`, `{"i128":"7"}`},
	{"u128", `{"u128":7}`, `{"u128":"7"}`},
	{"bits40", `{"bits40":7}`, `{"bits40":"7"}`},
	{"fixed64", `{"fixed64":1.5}`, `{"fixed64":"1.5"}`},
	{"fixed128", `{"fixed128":1.5}`, `{"fixed128":"1.5"}`}}

func TestMooJsonWideNumberPositions(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo")
	g := generateFiles(t, map[string]string{"Probe.schema": positionSchema})
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(`--!strict
local inputs = require("./inputs")
local P: any = require("./ProbeTable")
local function check(name: string, want: boolean)
 local v = P.NewRoot()
 local r: any = {}
 local ok = P.RootFromJson(v, inputs[name], r)
 if (want and not ok) or (not want and (ok or r.reason ~= "kind_mismatch")) then
  print("BAD " .. name .. " " .. tostring(r.reason))
 end
end
`)
	for _, c := range positionCases {
		inputs[c.name+"number"] = []byte(c.number)
		inputs[c.name+"quoted"] = []byte(c.quoted)
		fmt.Fprintf(&b, "check(%q, false)\ncheck(%q, true)\n", c.name+"number", c.name+"quoted")
	}
	inputs["narrow"] = []byte(`{"narrow":7}`)
	inputs["fixed32"] = []byte(`{"fixed32":1.5}`)
	b.WriteString("check(\"narrow\", true)\ncheck(\"fixed32\", true)\n")
	if out := g.run(t, b.String(), inputs); strings.Contains(out, "BAD ") {
		t.Fatal(out)
	}
}

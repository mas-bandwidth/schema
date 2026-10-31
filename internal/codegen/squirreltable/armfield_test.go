package squirreltable_test

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"strings"
	"testing"
)

// M18 rejects a second payload codec for union arms. Framing may differ;
// every payload must flow through the field reader used for ordinary fields.
func squirrelArmFieldViolations(src string) []string {
	var out []string
	for _, f := range []struct{ name, callee string }{
		{"readArmPayload", "readField("}, {"readUnionInto", "readArmPayload("},
	} {
		_, tail, ok := strings.Cut(src, f.name+" = function(")
		if !ok {
			out = append(out, "missing "+f.name)
			continue
		}
		body, _, ok := strings.Cut(tail, "\n};")
		if !ok {
			out = append(out, "unterminated "+f.name)
			continue
		}
		if !strings.Contains(body, f.callee) {
			out = append(out, f.name+" does not call "+f.callee)
		}
		for _, call := range []string{"readPrim(", "utf8Decode(", "wideDecode(", "wideClamp(", "clampScalar(", "bodyRead(", "readArrayBody(", "readArrayElems(", "readMapField("} {
			if strings.Contains(body, call) {
				out = append(out, f.name+" decodes a payload with "+call)
			}
		}
	}
	return out
}
func TestSquirrelArmIsAFieldLine(t *testing.T) {
	for _, v := range squirrelArmFieldViolations(string(squirreltable.Runtime()["schema_table_load.nut"])) {
		t.Error(v)
	}
}
func TestSquirrelArmFieldLineNegativeControl(t *testing.T) {
	src := string(squirreltable.Runtime()["schema_table_load.nut"])
	for _, c := range []struct{ anchor, plant, call string }{
		{"  local p = arm.payload;\n", "  if (p.shape == \"str\") utf8Decode(sub.b, sub.at, sub.end-sub.at);\n", "utf8Decode("},
		{"  inst.Type = arm.tag;\n", "  bodyRead(null,sub,T,table,inst,report,st,true);\n", "bodyRead("},
	} {
		if strings.Count(src, c.anchor) != 1 {
			t.Fatalf("plant anchor must occur once: %q", c.anchor)
		}
		got := squirrelArmFieldViolations(strings.Replace(src, c.anchor, c.anchor+c.plant, 1))
		if len(got) != 1 || !strings.Contains(got[0], c.call) {
			t.Fatalf("per-shape reader plant: %v", got)
		}
	}
}

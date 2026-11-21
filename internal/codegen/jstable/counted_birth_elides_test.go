package jstable

// A COUNTED ARRAY IS BORN AT ITS MINIMUM AND ITS BIRTH VALUE ELIDES (F-115,
// docs/SPEC-TABLES.md §22.2). A `[2..4]int32` field is born holding two
// default elements; the message and variable writers leave the field out at
// exactly that value, as Go's reference does (`020000` on the message form, a
// 10-byte variable file), and write it once one element is live. Every
// JavaScript write path is checked: the message save, the variable save, the
// save of a value read from `{}`, and the retain save (on a variable root,
// the only kind retention takes).

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const countedBirthSchema = `package probe

fixed table Root {
    few [2..4]int32
}

table Var {
    few [2..4]int32
}
`

// countedBirthModule is the basename of the generated module whose text
// carries marker.
func countedBirthModule(t *testing.T, u *ir.Unit, marker string) string {
	t.Helper()
	for name, b := range generatedTree(u) {
		if strings.HasSuffix(name, ".js") && strings.Contains(string(b), marker) {
			return strings.TrimSuffix(name, ".js")
		}
	}
	t.Fatalf("no generated module carries %q", marker)
	return ""
}

const countedBirthProbe = `import * as F from "./%s.js";
import * as M from "./%s.js";
import * as V from "./%s.js";

const hex = (b) => Buffer.from(b).toString("hex");
function msg(f) {
  const rep = {};
  const n = M.RootMeasureMessages([f], rep);
  if (n < 0) { throw new Error("MeasureMessages " + JSON.stringify(rep)); }
  const out = new Uint8Array(n);
  if (M.RootSaveMessages([f], out, rep) !== n) { throw new Error("SaveMessages " + JSON.stringify(rep)); }
  return hex(out);
}
function retain(v) {
  const ret = new V.TableVariableRetain(new Uint8Array(65536), new Array(1024));
  const rep = new V.TableVariableReport();
  const out = V.VarSaveRetain(v, ret, rep);
  if (!out) { throw new Error("SaveRetain"); }
  return hex(out);
}
const f = new F.Root();
console.log("fresh_fixed " + f.fewCount + "," + f.few[0] + "," + f.few[1]);
const r = new V.Root();
console.log("fresh_root " + r.few.length + "," + r.few[0] + "," + r.few[1]);
const v = new V.Var();
console.log("fresh_var " + v.few.length + "," + v.few[0] + "," + v.few[1]);
console.log("msg_birth " + msg(f));
console.log("root_birth " + hex(V.RootSave(r)));
const jrep = new V.TableVariableReport();
const jr = V.RootFromJson("{}", jrep);
console.log("json_birth " + hex(V.RootSave(jr)));
console.log("var_birth " + hex(V.VarSave(v)));
console.log("retain_birth " + retain(v));
f.few[1] = 7;
console.log("msg_live " + msg(f));
r.few[1] = 7;
console.log("root_live " + hex(V.RootSave(r)));
v.few[1] = 7;
console.log("var_live " + hex(V.VarSave(v)));
console.log("retain_live " + retain(v));
`

// countedBirthWant is Go's reference bytes for the two one-field roots at
// their birth value and with element 1 set to 7, on the message form (the
// fixed Root) and the variable form (both).
func countedBirthWant(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	model := tabletext.NewModel(u)
	want := map[string]string{"fresh_fixed": "2,0,0", "fresh_root": "2,0,0", "fresh_var": "2,0,0"}
	for _, root := range []string{"Root", "Var"} {
		st := model.Lookup(root)
		if st == nil {
			t.Fatalf("the reference unit has no %s", root)
		}
		inst := model.New(st)
		fv, ok := inst.FieldByKey("few")
		if !ok || fv.Count != 2 {
			t.Fatalf("the reference %s is not born at two elements", root)
		}
		for _, stage := range []string{"birth", "live"} {
			if stage == "live" {
				fv.Elems[1].I, fv.Elems[1].U = 7, 7
			}
			vr, err := tablewire.EncodeVariable(model, inst)
			if err != nil {
				t.Fatalf("EncodeVariable %s: %v", root, err)
			}
			if root == "Var" {
				want["var_"+stage] = hex.EncodeToString(vr)
				want["retain_"+stage] = hex.EncodeToString(vr)
				continue
			}
			want["root_"+stage] = hex.EncodeToString(vr)
			msg, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
			if err != nil {
				t.Fatalf("EncodeMessages: %v", err)
			}
			want["msg_"+stage] = hex.EncodeToString(msg)
		}
	}
	want["json_birth"] = want["root_birth"]
	if want["msg_birth"] != "020000" || len(want["root_birth"]) != 20 {
		t.Fatalf("the reference moved: message %s, variable %s (want 020000 and 10 bytes)", want["msg_birth"], want["root_birth"])
	}
	return want
}

// TestJSCountedBirthElides holds the JavaScript writers to Go's bytes for a
// counted array at its birth value and with one live element (F-115).
func TestJSCountedBirthElides(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	u := loadSchemaString(t, "counted_birth.schema", countedBirthSchema)
	want := countedBirthWant(t, u)
	probe := fmt.Sprintf(countedBirthProbe,
		countedBirthModule(t, u, "export function RootFixedWriteBody("),
		messageRootBase(u, findTable(t, u, "Root")),
		countedBirthModule(t, u, "export function VarSaveRetain("),
	)
	out, err := jsRunMessageProbe(t, node, u, nil, probe)
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), " ")
		got[k] = v
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %q, want %q (Go's reference)", k, got[k], w)
		}
	}
}

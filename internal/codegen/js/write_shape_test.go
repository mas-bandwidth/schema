package js

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// writeShapeSchema declares one field of every kind whose JavaScript value
// has a shape the writer must check before it reads the value: records (a
// field, an element, an arm payload), unions, arrays, a bool, a string and
// bytes.
const writeShapeSchema = `package wshape

type Arm
{
    n int32 | min = -5, max = 5
}

union Pick
{
    arm Arm
    ping
}

type ShapeProbe
{
    ok   bool
    a    Arm
    u    Pick
    xs   [..2]Arm
    fs   [2]int8
    us   [..2]Pick
    s    string(4)
    y    bytes(4)
}
`

// writeShapeScript writes one valid ShapeProbe through both tiers, then one
// per value of the wrong shape, and prints "<tier> <label>=<verdict>": refused
// (false, or -1 from the flat tier), accepted, or threw.
const writeShapeScript = `import * as S from "@mas-bandwidth/serialize";
import * as M from "./ShapeProbe.js";
import * as F from "./ShapeProbeFlat.js";

function verdicts(v) {
  let r, f;
  try {
    r = M.WriteShapeProbe(new S.WriteStream(new Uint8Array(256)), v) === false ? "refused" : "accepted";
  } catch (e) {
    r = "threw " + e.name;
  }
  try {
    f = F.WriteShapeProbeFlat(new DataView(new Uint8Array(256).buffer), v) === -1 ? "refused" : "accepted";
  } catch (e) {
    f = "threw " + e.name;
  }
  return [r, f];
}

function row(label, mut) {
  const v = new M.ShapeProbe();
  mut(v);
  const [r, f] = verdicts(v);
  console.log("runtime " + label + "=" + r);
  console.log("flat " + label + "=" + f);
}

row("valid", (v) => { v.xsCount = 1; v.u.type = M.PickType.Arm; });
row("union_null", (v) => { v.u = null; });
row("arm_payload_null", (v) => { v.u.type = M.PickType.Arm; v.u.arm = null; });
row("arm_payload_undefined", (v) => { v.u.type = M.PickType.Arm; v.u.arm = undefined; });
row("struct_field_null", (v) => { v.a = null; });
row("array_null", (v) => { v.xsCount = 1; v.xs = null; });
row("array_short", (v) => { v.xsCount = 1; v.xs = []; });
row("array_struct_elem_null", (v) => { v.xsCount = 1; v.xs[0] = null; });
row("fixed_array_short", (v) => { v.fs = [1]; });
row("array_union_elem_null", (v) => { v.usCount = 1; v.us[0] = null; });
row("bool_integer", (v) => { v.ok = 1; });
row("string_null", (v) => { v.s = null; v.sLength = 1; });
row("bytes_array", (v) => { v.y = [1, 2]; v.yLength = 2; });
row("bytes_short", (v) => { v.y = new Uint8Array(1); v.yLength = 2; });
{
  const [r, f] = verdicts(null);
  console.log("runtime value_is_null=" + r);
  console.log("flat value_is_null=" + f);
}
`

// TestJsWriteRefusesEveryValueShape pins the JavaScript writers' shape checks
// (SPEC §5.1; JavaScript refuses a bad value in every build): under checked
// and NODE_ENV=production, the runtime tier answers false and the flat tier
// -1 for every value of the wrong shape, never a TypeError from reading a
// field of null or undefined, and the valid value is accepted.
func TestJsWriteRefusesEveryValueShape(t *testing.T) {
	t.Parallel()
	ast, perrs := parser.Parse("ShapeProbe.schema", []byte(writeShapeSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "ShapeProbe.schema", Name: "ShapeProbe.schema", Base: "ShapeProbe", Bytes: []byte(writeShapeSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "node (runs both JavaScript write tiers under checked and production)")
	node := jsFlatNode(t)
	serializeDir := serializeJSDir(t)
	for _, prod := range []bool{false, true} {
		mode := "checked"
		if prod {
			mode = "production"
		}
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			nm := filepath.Join(dir, "node_modules", "@mas-bandwidth")
			if err := os.MkdirAll(nm, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(serializeDir, filepath.Join(nm, "serialize")); err != nil {
				t.Fatal(err)
			}
			for name, data := range out {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(writeShapeScript), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := testproc.Command(t, node, filepath.Join(dir, "run.mjs"))
			cmd.Env = os.Environ()
			if prod {
				cmd.Env = append(cmd.Env, "NODE_ENV=production")
			}
			b, cerr := cmd.CombinedOutput()
			if cerr != nil {
				t.Fatalf("%s: %v\n%s", mode, cerr, b)
			}
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			for _, line := range lines {
				k, v, _ := strings.Cut(line, "=")
				want := "refused"
				if strings.HasSuffix(k, " valid") {
					want = "accepted"
				}
				if v != want {
					t.Errorf("%s %s: %q, want %s", mode, k, v, want)
				}
			}
			if len(lines) != 30 {
				t.Fatalf("%s: the probe printed %d lines, want 30:\n%s", mode, len(lines), b)
			}
		})
	}
}

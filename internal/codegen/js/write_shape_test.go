package js

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// writeShapeSchema declares one field of every kind whose JavaScript value
// has a shape the writer must check before it reads the value: records (a
// field, an element, an arm payload), unions, arrays, a bool, a string and
// bytes; and the kinds whose JavaScript storage a coercing call would
// convert rather than refuse: flags (a BigInt, narrow and wide wire), a bare
// float32, a float64 and a compressed float32.
const writeShapeSchema = `package wshape

type Arm
{
    n int32 | min = -5, max = 5
}

flags Mode { A, B, C }

// Len opens with a field a string or a function also answers: its elements
// keep the object guard inside the loop.
type Len
{
    length int32 | min = 0, max = 100
}

flags Wide | max = 40 { W }

// Outer nests an element loop inside an element loop.
type Outer
{
    inner [..2]Arm
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
    m    Mode
    w    Wide
    f    float32
    d    float64
    c    float32 | min = -10, max = 10, resolution = 0.5
    ls   [..2]Len
    os   [..2]Outer
}
`

// writeShapeScript writes one valid ShapeProbe through both tiers, then one
// per value of the wrong shape, and prints "<tier> <label>=<verdict>": refused
// (false, or -1 from the flat tier), accepted, or threw. A row labelled
// rethrow_<Name>_... holds an object element whose own getter or Proxy trap
// throws <Name>: the writer must let that error through, never answer -1,
// even when another element is not an object.
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
row("flags_number", (v) => { v.m = 1; });
row("flags_null", (v) => { v.m = null; });
row("flags_undefined", (v) => { v.m = undefined; });
row("flags_string", (v) => { v.m = "1"; });
row("wide_flags_number", (v) => { v.w = 1; });
row("wide_flags_string", (v) => { v.w = "1"; });
row("float32_null", (v) => { v.f = null; });
row("float32_true", (v) => { v.f = true; });
row("float32_string", (v) => { v.f = "2.5"; });
row("float32_array", (v) => { v.f = [3]; });
row("float64_null", (v) => { v.d = null; });
row("float64_string", (v) => { v.d = "2.5"; });
row("cfloat_null", (v) => { v.c = null; });
row("cfloat_true", (v) => { v.c = true; });
row("cfloat_string", (v) => { v.c = "2.5"; });
row("cfloat_array", (v) => { v.c = [3]; });
row("array_struct_elem_undefined", (v) => { v.xsCount = 1; v.xs[0] = undefined; });
row("array_struct_elem_number", (v) => { v.xsCount = 1; v.xs[0] = 7; });
row("array_struct_elem_string", (v) => { v.xsCount = 1; v.xs[0] = "ab"; });
row("array_union_elem_undefined", (v) => { v.usCount = 1; v.us[0] = undefined; });
row("array_union_elem_number", (v) => { v.usCount = 1; v.us[0] = 7; });
row("len_elem_string", (v) => { v.lsCount = 1; v.ls[0] = "abcd"; });
row("len_elem_function", (v) => { v.lsCount = 1; v.ls[0] = function (a) { return a; }; });
const boom = () => { const o = new M.Arm(); Object.defineProperty(o, "n", { get() { throw new Error("boom"); } }); return o; };
const trap = () => new Proxy(new M.Arm(), { get() { throw new RangeError("trap"); } });
row("rethrow_Error_getter_then_number", (v) => { v.xsCount = 2; v.xs[0] = boom(); v.xs[1] = 7; });
row("rethrow_Error_getter_then_null", (v) => { v.xsCount = 2; v.xs[0] = boom(); v.xs[1] = null; });
row("rethrow_RangeError_proxy_then_null", (v) => { v.xsCount = 2; v.xs[0] = trap(); v.xs[1] = null; });
row("rethrow_Error_union_tag_getter_then_null", (v) => { v.usCount = 2; const u = new M.Pick(); Object.defineProperty(u, "type", { get() { throw new Error("boom"); } }); v.us[0] = u; v.us[1] = null; });
row("rethrow_Error_inner_getter_then_outer_null", (v) => { v.osCount = 2; const o = new M.Outer(); o.innerCount = 1; o.inner[0] = boom(); v.os[0] = o; v.os[1] = null; });
// A function, arrow or class carrying the element's field names as its own
// properties: every field guard would pass it, so only the object test
// refuses it.
row("elem_function_with_fields", (v) => { v.xsCount = 1; const f = function () {}; f.n = 3; v.xs[0] = f; });
row("elem_class_static_fields", (v) => { v.xsCount = 1; v.xs[0] = class { static n = 3; }; });
row("elem_arrow_with_fields", (v) => { v.xsCount = 1; const f = () => 0; f.n = 3; v.xs[0] = f; });
row("union_elem_function_with_tag_and_arm", (v) => { v.usCount = 1; const f = function () {}; f.type = M.PickType.Arm; f.arm = new M.Arm(); v.us[0] = f; });
row("nested_elem_function_with_inner", (v) => { v.osCount = 1; const f = function () {}; f.innerCount = 0; f.inner = []; v.os[0] = f; });
// Accessors that answer differently on a second read: each tier gets its
// own fresh value, since the accessor keeps state.
function writeRuntime(v) {
  try {
    return M.WriteShapeProbe(new S.WriteStream(new Uint8Array(256)), v) === false ? "refused" : "accepted";
  } catch (e) {
    return "threw " + e.name;
  }
}
function writeFlat(v) {
  try {
    return F.WriteShapeProbeFlat(new DataView(new Uint8Array(256).buffer), v) === -1 ? "refused" : "accepted";
  } catch (e) {
    return "threw " + e.name;
  }
}
function rowFresh(label, mk) {
  console.log("runtime " + label + "=" + mk(writeRuntime));
  console.log("flat " + label + "=" + mk(writeFlat));
}
// storage whose index 0 throws once, then answers undefined
rowFresh("rethrow_Error_storage_index_throws_once", (write) => {
  const v = new M.ShapeProbe();
  v.xsCount = 1;
  let n = 0;
  v.xs = new Proxy([new M.Arm()], { get(t, k) { if (k === "0") { if (n++ === 0) throw new Error("boom"); return undefined; } return t[k]; } });
  return write(v);
});
// a count getter that answers 1 to every read but the last, the loop
// condition after element 0, which throws; storage holds one element. A
// clean run counts the reads first, so the throw lands on that last read.
rowFresh("rethrow_Error_count_getter_throws_late", (write) => {
  const mk = (limit) => {
    const v = new M.ShapeProbe();
    v.xs = [new M.Arm()];
    let n = 0;
    Object.defineProperty(v, "xsCount", { get() { n++; if (n === limit) throw new Error("late"); return 1; } });
    return [v, () => n];
  };
  const [clean, reads] = mk(-1);
  const verdict = write(clean);
  if (verdict !== "accepted") return "clean run " + verdict;
  return write(mk(reads())[0]);
});
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
				if _, label, _ := strings.Cut(k, " "); strings.HasPrefix(label, "rethrow_") {
					name, _, _ := strings.Cut(strings.TrimPrefix(label, "rethrow_"), "_")
					want = "threw " + name
				}
				if v != want {
					t.Errorf("%s %s: %q, want %s", mode, k, v, want)
				}
			}
			if len(lines) != 100 {
				t.Fatalf("%s: the probe printed %d lines, want 100:\n%s", mode, len(lines), b)
			}
		})
	}
}

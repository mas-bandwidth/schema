package js

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// TestJsRuntimeWriteRefusesNonFiniteAndWideRangeInProduction pins SPEC §5.1 on
// the JavaScript runtime-tier Write<Type> functions: under both checked and
// NODE_ENV=production builds, bare float32/float64 writers must refuse NaN and
// +/-Infinity (including finite doubles that overflow float32) and ranged
// 128-bit integer writers must refuse values outside [min, max].
func TestJsRuntimeWriteRefusesNonFiniteAndWideRangeInProduction(t *testing.T) {
	t.Parallel()

	src := `package wprobe

type RuntimeProbe
{
    f32 float32
    f64 float64
    i128 int128 = 0 | min = -100, max = 100
}
`
	ast, perrs := parser.Parse("RuntimeProbe.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "RuntimeProbe.schema", Name: "RuntimeProbe.schema", Base: "RuntimeProbe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	mod := string(out["RuntimeProbe.js"])
	writeFn := emittedFunction(t, mod, "WriteRuntimeProbe")
	for _, want := range []string{
		"Number.isFinite(Math.fround(value.f32))",
		"Number.isFinite(value.f64)",
		"value.i128 < -100n || value.i128 > 100n",
	} {
		if !strings.Contains(writeFn, want) {
			t.Errorf("WriteRuntimeProbe missing guard %q:\n%s", want, writeFn)
		}
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node (runs runtime-tier WriteRuntimeProbe under checked and production)")
		node := jsFlatNode(t)
		root := repoRootForRuntimeProbe(t)
		serializeDir := filepath.Join(filepath.Dir(root), "serialize.js")
		if _, statErr := os.Stat(filepath.Join(serializeDir, "src", "index.js")); statErr != nil {
			t.Skipf("serialize.js sibling not staged at %s: %v", serializeDir, statErr)
		}

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
				script := `import * as S from "@mas-bandwidth/serialize";
import * as M from "./RuntimeProbe.js";

function tryWrite(mut) {
  const v = new M.RuntimeProbe();
  mut(v);
  const ws = new S.WriteStream(new Uint8Array(64));
  try {
    if (!M.WriteRuntimeProbe(ws, v)) return false;
    ws.flush();
    return true;
  } catch (_) {
    return false;
  }
}

if (!tryWrite((v) => { v.f32 = -0.0; v.f64 = 1.5; v.i128 = -100n; })) {
  throw new Error("valid boundary write refused");
}
for (const [name, mut] of [
  ["f32 NaN", (v) => { v.f32 = NaN; }],
  ["f32 +Inf", (v) => { v.f32 = Infinity; }],
  ["f32 overflow", (v) => { v.f32 = 1e300; }],
  ["f64 NaN", (v) => { v.f64 = NaN; }],
  ["f64 -Inf", (v) => { v.f64 = -Infinity; }],
  ["i128 below min", (v) => { v.i128 = -101n; }],
  ["i128 above max", (v) => { v.i128 = 101n; }],
]) {
  if (tryWrite(mut)) {
    throw new Error("WriteRuntimeProbe accepted " + name);
  }
}
console.log("ok");
`
				if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(script), 0o644); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(node, filepath.Join(dir, "run.mjs"))
				env := os.Environ()
				if prod {
					env = append(env, "NODE_ENV=production")
				}
				cmd.Env = env
				b, cerr := cmd.CombinedOutput()
				if cerr != nil || strings.TrimSpace(string(b)) != "ok" {
					t.Fatalf("%s: err=%v out=%s", mode, cerr, b)
				}
			})
		}
	})
}

func repoRootForRuntimeProbe(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

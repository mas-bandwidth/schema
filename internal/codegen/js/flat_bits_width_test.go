package js

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJsFlatWriteRefusesWideBits pins SPEC §5.1 row 2 on the JavaScript flat
// tier: a bits(N) value outside [0, 2^N - 1], or a non-integer one, is
// refused with -1 in every build. The pure half inspects the generated flat
// writer for the guard on a bits(7) and a bits(40) field; the gated half runs
// the generated module under node and requires the refusal at both widths.
func TestJsFlatWriteRefusesWideBits(t *testing.T) {
	t.Parallel()

	flat := generateWideBitsProbeFlat(t)
	write := emittedFunction(t, flat, "WriteProbeFlat")
	for _, want := range []string{
		"!Number.isInteger(value.a) || value.a < 0 || value.a > 127",
		"value.b < 0n || value.b > 1099511627775n",
		"return -1;",
	} {
		if !strings.Contains(write, want) {
			t.Errorf("flat writer must refuse a bits(N) value outside its width; missing %q", want)
		}
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node (it runs the generated flat codec)")
		node := jsFlatNode(t)
		for _, production := range []bool{false, true} {
			label := "unset"
			if production {
				label = "production"
			}
			t.Run(label, func(t *testing.T) {
				runWideBitsProbe(t, node, flat, production)
			})
		}
	})
}

// generateWideBitsProbeFlat compiles a type carrying one bits(7) and one
// bits(40) field.
func generateWideBitsProbeFlat(t *testing.T) string {
	t.Helper()
	src := `package clif

type Probe
{
    a bits(7)
    b bits(40)
}
`
	ast, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	flat, ok := out["ProbeFlat.js"]
	if !ok {
		t.Fatalf("no flat module: %v", keys(out))
	}
	return string(flat)
}

// runWideBitsProbe runs the generated flat module under node with NODE_ENV
// unset or set to production.
func runWideBitsProbe(t *testing.T, node, flat string, production bool) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeFlat.js"), []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import { WriteProbeFlat, ReadProbeFlat } from './ProbeFlat.js';

const view = new DataView(new ArrayBuffer(64));

if (WriteProbeFlat(view, { a: 200, b: 0n }) !== -1) {
  throw new Error('flat writer accepted 200 in bits(7)');
}
if (WriteProbeFlat(view, { a: 1.5, b: 0n }) !== -1) {
  throw new Error('flat writer accepted a non-integer in bits(7)');
}
if (WriteProbeFlat(view, { a: -1, b: 0n }) !== -1) {
  throw new Error('flat writer accepted -1 in bits(7)');
}
const n7 = WriteProbeFlat(view, { a: 127, b: 0n });
if (n7 < 0) {
  throw new Error('flat writer refused the bits(7) max');
}
const out7 = { a: 0, b: 0n };
if (!ReadProbeFlat(view, n7 * 8, out7) || out7.a !== 127 || out7.b !== 0n) {
  throw new Error('the bits(7) max did not read back: ' + out7.a + ', ' + out7.b);
}

const max40 = (1n << 40n) - 1n;
if (WriteProbeFlat(view, { a: 0, b: 1n << 40n }) !== -1) {
  throw new Error('flat writer accepted one past the bits(40) max');
}
if (WriteProbeFlat(view, { a: 0, b: -1n }) !== -1) {
  throw new Error('flat writer accepted -1 in bits(40)');
}
const n40 = WriteProbeFlat(view, { a: 0, b: max40 });
if (n40 < 0) {
  throw new Error('flat writer refused the bits(40) max');
}
const out40 = { a: 0, b: 0n };
if (!ReadProbeFlat(view, n40 * 8, out40) || out40.a !== 0 || out40.b !== max40) {
  throw new Error('the bits(40) max did not read back: ' + out40.a + ', ' + out40.b);
}

console.log('OK');
`
	if err := os.WriteFile(filepath.Join(dir, "harness.mjs"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "harness.mjs")
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "NODE_ENV=") {
			env = append(env, kv)
		}
	}
	if production {
		env = append(env, "NODE_ENV=production")
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("probe output: %s", out)
	}
}

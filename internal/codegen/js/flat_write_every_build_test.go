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

// TestJsFlatWriterChecksEveryBuild pins the write rule for a refusing leg
// (SPEC §5.1): JavaScript has no assert idiom, so the flat writer refuses a
// value its declaration does not admit in every build, production included.
// The pure half inspects the generated flat module: one exported writer per
// type that holds the range guard, no second writer variant and no writer
// chosen on the build mode. The gated half runs the generated module under
// node with NODE_ENV unset and with NODE_ENV=production and requires, in
// both, that one past the range max is refused with -1 and the max itself
// writes and reads back.
func TestJsFlatWriterChecksEveryBuild(t *testing.T) {
	t.Parallel()

	flat := generateRangedProbeFlat(t)
	write := emittedFunction(t, flat, "WriteProbeFlat")
	if !strings.Contains(write, "return -1;") || !strings.Contains(write, "> 1000") {
		t.Errorf("the exported flat writer must hold the range guard:\n%s", write)
	}
	for _, forbidden := range []string{
		"PRODUCTION",
		"NODE_ENV",
		"FlatProduction",
		"FlatChecked",
		"export const WriteProbeFlat",
	} {
		if strings.Contains(flat, forbidden) {
			t.Errorf("the flat module must emit one writer for every build; found %q", forbidden)
		}
	}
	if n := strings.Count(flat, "function WriteProbeFlat(") + strings.Count(flat, "function writeProbeFlat"); n != 1 {
		t.Errorf("the flat module must emit exactly one Probe writer, found %d", n)
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
				runRangedProbe(t, node, flat, production)
			})
		}
	})
}

// generateRangedProbeFlat compiles a type carrying one ranged integer.
func generateRangedProbeFlat(t *testing.T) string {
	t.Helper()
	src := `package clif

type Probe
{
    a int32 | min = -5, max = 1000
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

// runRangedProbe runs the generated flat module under node with NODE_ENV
// unset or set to production.
func runRangedProbe(t *testing.T, node, flat string, production bool) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeFlat.js"), []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import { WriteProbeFlat, ReadProbeFlat } from './ProbeFlat.js';

const view = new DataView(new ArrayBuffer(64));

if (WriteProbeFlat(view, { a: 1001 }) !== -1) {
  throw new Error('flat writer accepted one past the range max');
}
const n = WriteProbeFlat(view, { a: 1000 });
if (n < 0) {
  throw new Error('flat writer refused the range max');
}
const out = { a: 0 };
if (!ReadProbeFlat(view, n * 8, out) || out.a !== 1000) {
  throw new Error('the range max did not read back: ' + out.a);
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

package js

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJsFlatCompressedFloatClampsTheTopOfRange pins the SPEC's normative
// integer clamp for a compressed float whose step count sits in [2^23, 2^24),
// where a float32 ulp is 1 and n*miv + 0.5 at n = 1.0 is a tie that rounds to
// even: without the clamp the writer emits maxInt + 1 for a legal top-of-range
// value, which the generated reader then refuses (8388609) or which overflows
// the 24-bit field (16777215). The pure half reads the emitted text; the gated
// half writes the top value and reads it back through node, byte-comparing the
// wire against the C++ reference's clamp to the step count.
func TestJsFlatCompressedFloatClampsTheTopOfRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		max    string
		maxInt uint64
	}{
		{"8388609", "8388609.0", 8388609},
		{"16777215", "16777215.0", 16777215},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flat := generateProbeFlat(t, tc.max)
			want := fmt.Sprintf("if (v > %d) { v = %d; }", tc.maxInt, tc.maxInt)
			if n := strings.Count(flat, want); n != 2 {
				t.Fatalf("both writer variants must clamp to the step count; found %d of %q", n, want)
			}
		})
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node (it compiles and runs the generated flat codec)")
		node := jsFlatNode(t)
		for _, tc := range cases {
			tc := tc
			for _, production := range []bool{false, true} {
				production := production
				label := "checked"
				if production {
					label = "production"
				}
				t.Run(tc.name+"/"+label, func(t *testing.T) {
					runCompressedFloatProbe(t, node, tc.max, tc.maxInt, production)
				})
			}
		}
	})
}

func generateProbeFlat(t *testing.T, max string) string {
	t.Helper()
	src := fmt.Sprintf(`package clif

type Probe
{
    f float32 | min = 0.0, max = %s, resolution = 1.0
}
`, max)
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

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// jsFlatNode is the leg's own toolchain: $NODE, the dist symlink, then PATH.
func jsFlatNode(t *testing.T) string {
	t.Helper()
	if n := os.Getenv("NODE"); n != "" {
		if _, err := os.Stat(n); err == nil {
			return n
		}
	}
	matches, _ := filepath.Glob("../../../dist/node-*/bin/node")
	for _, m := range matches {
		if abs, err := filepath.Abs(m); err == nil {
			return abs
		}
	}
	if n, err := exec.LookPath("node"); err == nil {
		return n
	}
	t.Skip("no node toolchain: see make/js.mk's NODE")
	return ""
}

// runCompressedFloatProbe writes the generated flat module beside a harness
// that writes the top-of-range value and reads it back, then runs it. The
// expected bytes are the C++ reference's: the clamp to the step count is
// normative, so the field carries maxInt little-endian, LSB first.
func runCompressedFloatProbe(t *testing.T, node, max string, maxInt uint64, production bool) {
	t.Helper()
	dir := t.TempDir()
	flat := generateProbeFlat(t, max)
	if err := os.WriteFile(filepath.Join(dir, "ProbeFlat.js"), []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}
	expect := make([]string, 3)
	for i := range expect {
		expect[i] = fmt.Sprintf("%d", (maxInt>>(8*uint(i)))&0xff)
	}
	harness := fmt.Sprintf(`import { WriteProbeFlat, ReadProbeFlat } from './ProbeFlat.js';

const TOP = %s;
const EXPECT = [%s];
const buf = new ArrayBuffer(64);
const view = new DataView(buf);
const out = { F: TOP };
const n = WriteProbeFlat(out, view);
if (n !== 3) { throw new Error('write returned ' + n + ' bytes, want 3'); }
const bytes = new Uint8Array(buf, 0, 3);
for (let i = 0; i < 3; i++) {
  if (bytes[i] !== EXPECT[i]) {
    throw new Error('byte ' + i + ' = ' + bytes[i] + ', want ' + EXPECT[i]);
  }
}
const back = { F: -1 };
if (!ReadProbeFlat(back, view, 24)) { throw new Error('the reader refused the writer bytes'); }
if (Math.fround(back.F) !== Math.fround(TOP)) {
  throw new Error('roundtrip ' + back.F + ' != ' + TOP);
}
console.log('OK ' + n);
`, max, strings.Join(expect, ", "))
	if err := os.WriteFile(filepath.Join(dir, "harness.mjs"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "harness.mjs")
	cmd.Dir = dir
	env := os.Environ()
	if production {
		env = append(env, "NODE_ENV=production")
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK 3") {
		t.Fatalf("probe output: %s", out)
	}
}

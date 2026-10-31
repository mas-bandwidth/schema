package rusttable

// A FLOAT CROSSES TWO WIDTHS BY BIT SURGERY, NEVER BY CONVERSION
// (docs/PORTING.md M21; SPEC.md §4.3, docs/SPEC-TABLES.md §4): the Rust leg's
// wire has not landed the finite rule (tables-rust-wire-fuzz-all runs with
// --finite-owed tables-finite/rust), so a NaN still reaches a cell and the
// method still binds. A float32 the variable wire carries into a float64 cell
// (kind 10 widening into 11) goes through the variable runtime's
// table_widen_f32, and the message form's float rung through
// table_message_widen_f32. Both are compiled out of the generated runtimes,
// BYTE FOR BYTE as emitted, and run over every float32 class: signalling and
// quiet NaNs with payloads in both signs, the infinities, the zeros, a
// subnormal and the largest finite value. A NaN must come out with its 23
// payload bits in the TOP of the double's 52 and the quiet bit exactly as the
// writer wrote it; every other value is the ordinary conversion.
// tables-rust-float-nan-negative-control puts the hardware conversion back.
// The fixed form's widening is the same surgery, held by t04R21.

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const floatNaNSrc = `package probe

fixed table Pin
{
    x float32
}

table Wide
{
    ratio float64
    scale float32
}
`

// rustFnSource cuts one top-level `pub fn name(` out of a generated module, to
// the closing brace at column 0.
func rustFnSource(t *testing.T, module, name string) string {
	t.Helper()
	start := strings.Index(module, "pub fn "+name+"(")
	if start < 0 {
		t.Fatalf("the generated runtime defines no %s", name)
	}
	end := strings.Index(module[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("%s has no closing brace", name)
	}
	return module[start : start+end+3]
}

// floatWidenOracle is §4's widening on the bits: the payload carried for a
// NaN, the ordinary (exact) conversion for everything else.
func floatWidenOracle(bits uint32) uint64 {
	if bits&0x7f80_0000 == 0x7f80_0000 && bits&0x007f_ffff != 0 {
		return uint64(bits>>31)<<63 | 0x7ff0_0000_0000_0000 | uint64(bits&0x007f_ffff)<<29
	}
	return math.Float64bits(float64(math.Float32frombits(bits)))
}

var floatNaNProbes = []uint32{
	0x7fa0_0001, 0xffa0_0001, // signalling, a payload at both ends, both signs
	0x7f80_0001, 0x7fbf_ffff, // the smallest and the largest signalling payload
	0x7fc0_0000, 0xffc0_0001, 0x7fff_ffff, // quiet, with and without a payload
	0x7f80_0000, 0xff80_0000, // the infinities
	0x0000_0000, 0x8000_0000, 0x0000_0001, 0x3f80_0000, 0x7f7f_ffff, 0xc2f6_e979,
}

// TestRustFloatWideningKeepsNaNPayloads is M21 for the Rust leg.
func TestRustFloatWideningKeepsNaNPayloads(t *testing.T) {
	slowtest.Gate(t, "rustc (it compiles and runs the generated widening probe)")
	files := generate(t, floatNaNSrc)
	variable := string(files[VariableRuntimeModule+".rs"])
	message := string(files[MessageRuntimeModule+".rs"])
	codec := string(files["probe_table.rs"])
	if variable == "" || message == "" || codec == "" {
		t.Fatalf("the probe unit did not emit the variable runtime, the message runtime and its codec: %v", keysOf(files))
	}
	// the wire's float rung is the one place a float32 lands in a wider cell,
	// and it goes through the surgery, never through `as f64`
	if !strings.Contains(codec, "if kind == 10 { table_widen_f32(raw as u32) } else { f64::from_bits(raw) }") {
		t.Error("NAN: the float64 cell's kind-10 rung does not widen through table_widen_f32")
	}

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	var main strings.Builder
	main.WriteString(rustFnSource(t, variable, "table_widen_f32"))
	main.WriteString("\n")
	main.WriteString(rustFnSource(t, message, "table_message_widen_f32"))
	main.WriteString("\nfn main() {\n    let probes: &[u32] = &[")
	for _, p := range floatNaNProbes {
		fmt.Fprintf(&main, "0x%08x, ", p)
	}
	main.WriteString("];\n    for &b in probes {\n")
	main.WriteString("        println!(\"{:08x} {:016x} {:016x}\", b, table_widen_f32(b).to_bits(), table_message_widen_f32(b));\n")
	main.WriteString("    }\n}\n")

	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(work, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the widening probe did not build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the widening probe did not run: %v\n%s", err, out)
	}
	seen := 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 3 {
			t.Fatalf("an unreadable probe line: %q", line)
		}
		in, err1 := strconv.ParseUint(parts[0], 16, 32)
		variableGot, err2 := strconv.ParseUint(parts[1], 16, 64)
		messageGot, err3 := strconv.ParseUint(parts[2], 16, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			t.Fatalf("an unreadable probe line: %q", line)
		}
		want := floatWidenOracle(uint32(in))
		if variableGot != want {
			t.Errorf("NAN: table_widen_f32(0x%08x) = 0x%016x, want 0x%016x", in, variableGot, want)
		}
		if messageGot != want {
			t.Errorf("NAN: table_message_widen_f32(0x%08x) = 0x%016x, want 0x%016x", in, messageGot, want)
		}
		seen++
	}
	if seen != len(floatNaNProbes) {
		t.Fatalf("the probe answered %d of %d values", seen, len(floatNaNProbes))
	}
}

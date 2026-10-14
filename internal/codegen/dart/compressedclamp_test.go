package dart

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDartCompressedFloatClampsTheTopOfRange pins the integer clamp the SPEC
// declares normative (schema#109): once the step count reaches [2^23, 2^24)
// the float32 ulp is 1 and the quantization's `+ 0.5` is a tie at n = 1.0,
// which rounds to even and overshoots the step count. The writer must clamp
// the quantized integer to the step count after the two-rounding quantization,
// exactly as every serialize runtime and the Elixir emitter do.
//
// The two witnesses are the ones the SPEC names: [0, 8388609] and
// [0, 16777215] at resolution 1. For the first the generated reader refuses
// the 8388610 it used to receive; for the second the writer used to put 2^24
// into a 24-bit field. The gated half writes the top value, reads it back and
// byte-compares the writer bytes against the C++ reference's clamp-to-steps
// bytes for the same field.
func TestDartCompressedFloatClampsTheTopOfRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		lit  string
		max  float64
	}{
		{"TopOfFirstUlpBand", "8388609.0", 8388609},
		{"TopOfSecondUlpBand", "16777215.0", 16777215},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "package clamp\n\ntype ClampProbe\n{\n    f float32 | min = 0.0, max = " + c.lit + ", resolution = 1.0\n}\n"
			files := dartGenerateProbe(t, src)

			var all strings.Builder
			for _, b := range files {
				all.Write(b)
			}
			steps, wireBits := ir.CompressedFloatParams(0, c.max, 1.0)
			needle := fmt.Sprintf("if (v > %d) v = %d;", steps, steps)
			if !strings.Contains(all.String(), needle) {
				t.Errorf("emitted writer for max = %s must clamp the quantized integer to the step count:\nwant %q in the generated Dart", c.lit, needle)
			}

			slowtest.Gate(t, "the Dart toolchain (it compiles and runs the generated unit)")
			dartProbeRoundTripsTop(t, files, c.lit, steps, wireBits)
		})
	}
}

// dartGenerateProbe parses, checks and generates one single-file unit.
func dartGenerateProbe(t *testing.T, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse("Clamp.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Clamp.schema", Name: "Clamp.schema", Base: "Clamp",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return files
}

// dartProbeRoundTripsTop compiles and runs the generated unit: it writes the
// top-of-range value, demands the reader accept it and return the exact value,
// and compares the writer's bytes to the C++ reference's clamp-to-steps bytes.
func dartProbeRoundTripsTop(t *testing.T, files map[string][]byte, lit string, steps uint64, wireBits int64) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	driver := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import 'Clamp.dart';

void main() {
  final value = ClampProbe();
  value.f = %s;
  final view = ByteData(8);
  final n = writeClampProbe(value, view);
  final back = ClampProbe();
  if (!readClampProbe(back, view, n * 8)) {
    stderr.writeln('the generated reader refused the writer bytes');
    exit(1);
  }
  if (back.f != %s) {
    stderr.writeln('round trip lost the top value: \${back.f}');
    exit(1);
  }
  final parts = <String>[];
  for (var i = 0; i < 8; i++) {
    parts.add(view.getUint8(i).toRadixString(16).padLeft(2, '0'));
  }
  print(parts.join());
}
`, lit, lit)
	if err := os.WriteFile(filepath.Join(dir, "main.dart"), []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dart", "main.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Dart runtime: %v\n%s", err, out)
	}

	// The C++ reference clamps the quantized integer to the step count, so its
	// bytes are the step count packed little-endian into the field's wire width.
	ref := make([]byte, 8)
	binary.LittleEndian.PutUint64(ref, steps)
	want := hex.EncodeToString(ref)
	if wireBits > 64 {
		t.Fatalf("unexpected wire width %d for the witness", wireBits)
	}
	got := strings.TrimSpace(string(out))
	if got != want {
		t.Errorf("writer bytes for max = %s: got %s, want the C++ reference bytes %s", lit, got, want)
	}
}

package dart

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestDartZeroBitReadFunctionalRefusesBadBounds pins that a generated read
// entry for a type that reads no bits still validates its numBits argument
// before any read: a negative count and a count past the end of the buffer
// are both refused, never accepted.
func TestDartZeroBitReadFunctionalRefusesBadBounds(t *testing.T) {
	t.Parallel()
	src := "package t\n\ntype Ping {}\n"
	f, perrs := parser.Parse("Ping.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Ping.schema", Name: "Ping.schema", Base: "Ping",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	slowtest.Gate(t, "the Dart toolchain (it compiles and runs the generated unit)")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	driver := `import 'dart:io';
import 'dart:typed_data';
import 'Ping.dart';

void main() {
  final value = Ping();
  if (readPing(value, ByteData(0), 999999)) {
    stderr.writeln('expected refusal: numBits past the end of the buffer');
    exit(1);
  }
  if (readPing(value, ByteData(0), -5)) {
    stderr.writeln('expected refusal: a negative numBits');
    exit(1);
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.dart"), []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dart", "main.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Dart runtime: %v\n%s", err, out)
	}
}

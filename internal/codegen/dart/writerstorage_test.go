package dart

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

const writerStorageSchema = `package storage

type Sample
{
    values [..4]uint16
    text   string(8)
    data   bytes(6)
}
`

const writerStorageDriver = `import 'dart:io';
import 'dart:typed_data';
import 'Sample.dart';

const sentinel = 0xa5;

ByteData filled() {
  final view = ByteData(256);
  for (var i = 0; i < view.lengthInBytes; i++) {
    view.setUint8(i, sentinel);
  }
  return view;
}

bool untouched(ByteData view) {
  for (var i = 0; i < view.lengthInBytes; i++) {
    if (view.getUint8(i) != sentinel) return false;
  }
  return true;
}

void refused(String what, Sample value, {required void Function(Sample) set}) {
  set(value);
  final view = filled();
  final n = writeSample(value, view);
  if (n != -1) {
    stderr.writeln('FAIL $what: write returned $n, want -1');
    exit(1);
  }
  if (!untouched(view)) {
    stderr.writeln('FAIL $what: the refused write touched the buffer');
    exit(1);
  }
}

void main() {
  refused('count past storage', Sample(), set: (v) => v.valuesCount = 5);
  refused('text length past storage', Sample(), set: (v) => v.textLength = 9);
  refused('bytes length past storage', Sample(), set: (v) => v.dataLength = 7);

  final value = Sample();
  value.valuesCount = 4;
  value.textLength = 8;
  value.dataLength = 6;
  final view = filled();
  final n = writeSample(value, view);
  if (n <= 0) {
    stderr.writeln('FAIL at storage: write returned $n, want the bound written');
    exit(1);
  }
  if (untouched(view)) {
    stderr.writeln('FAIL at storage: the bound write left the buffer untouched');
    exit(1);
  }
}
`

// TestDartWriterStorageGuardEveryBuild holds the SPEC §5.1 storage rule on the
// packet writer: a counted array's count or a string/bytes length that would
// index past the value's own storage returns -1 before the first store, with
// asserts off, and a value exactly at its storage still writes.
func TestDartWriterStorageGuardEveryBuild(t *testing.T) {
	t.Parallel()
	files := writerStorageFiles(t, writerStorageSchema)

	slowtest.Gate(t, "the Dart toolchain (it compiles and runs the generated unit)")
	out, err := dartWriterStorageRun(t, files)
	if err != nil {
		t.Fatalf("generated Dart writer storage guard: %v\n%s", err, out)
	}
}

// TestDartWriterStorageGuardEveryBuildNegativeControl removes the counted
// array's storage guard from the generated source in memory and requires the
// driver above to go red.
func TestDartWriterStorageGuardEveryBuildNegativeControl(t *testing.T) {
	t.Parallel()
	files := writerStorageFiles(t, writerStorageSchema)

	const guard = "if (value.valuesCount > 4) {"
	src := string(files["Sample.dart"])
	if !strings.Contains(src, guard) {
		t.Fatalf("Sample.dart does not carry the counted-array storage guard %q", guard)
	}
	files["Sample.dart"] = []byte(strings.Replace(src, guard, "if (false) { // SABOTAGED", 1))

	slowtest.Gate(t, "the Dart toolchain (it compiles and runs the generated unit)")
	out, err := dartWriterStorageRun(t, files)
	if err == nil {
		t.Fatalf("the counted-array storage guard is gone and the writer driver stayed green:\n%s", out)
	}
}

func writerStorageFiles(t *testing.T, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse("Sample.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Sample.schema", Name: "Sample.schema", Base: "Sample",
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

func dartWriterStorageRun(t *testing.T, files map[string][]byte) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.dart"), []byte(writerStorageDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dart", "main.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

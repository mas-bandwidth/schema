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

// TestDartWriteAssertsBitsWidth pins SPEC §5.1 contract row 2 on the Dart
// packet writer. A bits(N) value outside [0, 2^N - 1] is a debug assert,
// active under dart --enable-asserts and absent without it. The assert is
// emitted wherever storage is wider than N: a scalar int (64 bits), the
// same field inside a packed run, and a bits(7) on an array element. A
// bits(64), whose int holds exactly 64 bits, emits nothing. The write still
// masks to N bits.
func TestDartWriteAssertsBitsWidth(t *testing.T) {
	t.Parallel()
	const src = "package t\n\n" +
		"type OneBits\n{\n    b bits(7)\n}\n\n" +
		"type FlatBits\n{\n    a uint8\n    b bits(7)\n}\n\n" +
		"type Cell\n{\n    b bits(7)\n}\n\n" +
		"type ArrBits\n{\n    items [2]Cell\n}\n\n" +
		"type Wide32\n{\n    b bits(32)\n}\n\n" +
		"type FullBits\n{\n    b bits(64)\n}\n"
	files := dartGenerateBitsWidth(t, src)
	all := string(files["Wide.dart"])

	one := dartFuncBody(t, all, "int writeOneBits(")
	if !strings.Contains(one, "assert(value.b >= 0);") || !strings.Contains(one, "assert(value.b <= 127);") {
		t.Errorf("writeOneBits does not assert a bits(7) value is in [0, 127]\n%s", one)
	}
	if !strings.Contains(one, "value.b & 0x7f") {
		t.Errorf("writeOneBits dropped the bits(7) mask\n%s", one)
	}

	flat := dartFuncBody(t, all, "int writeFlatBits(")
	if !strings.Contains(flat, "assert(value.b >= 0);") || !strings.Contains(flat, "assert(value.b <= 127);") {
		t.Errorf("writeFlatBits does not assert the packed bits(7)\n%s", flat)
	}
	const packed = "(value.a & 0xff) | ((value.b & 0x7f) << 8)"
	if !strings.Contains(flat, packed) {
		t.Errorf("writeFlatBits did not keep uint8 and bits(7) in one masked run\n%s", flat)
	}

	arr := dartFuncBody(t, all, "int writeArrBits(")
	if !strings.Contains(arr, "assert(e0.b >= 0);") || !strings.Contains(arr, "assert(e0.b <= 127);") {
		t.Errorf("writeArrBits does not assert a bits(7) array element\n%s", arr)
	}

	wide := dartFuncBody(t, all, "int writeWide32(")
	if !strings.Contains(wide, "assert(value.b >= 0);") || !strings.Contains(wide, "assert(value.b <= 4294967295);") {
		t.Errorf("writeWide32 does not assert a bits(32) held in a 64-bit int\n%s", wide)
	}

	full := dartFuncBody(t, all, "int writeFullBits(")
	if strings.Contains(full, "assert(value.b") {
		t.Errorf("writeFullBits asserts a bits(64) whose storage holds exactly 64 bits\n%s", full)
	}

	slowtest.Gate(t, "the Dart toolchain (it compiles and runs the generated unit)")
	dartProbeBitsWidth(t, files)
}

func dartGenerateBitsWidth(t *testing.T, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse("Wide.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Wide.schema", Name: "Wide.schema", Base: "Wide",
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

func dartFuncBody(t *testing.T, src, sig string) string {
	t.Helper()
	i := strings.Index(src, sig)
	if i < 0 {
		t.Fatalf("generated Dart has no %s", sig)
	}
	rest := src[i:]
	open := strings.Index(rest, "{")
	if open < 0 {
		t.Fatalf("%s has no body", sig)
	}
	depth := 0
	for j := open; j < len(rest); j++ {
		switch rest[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:j+1]
			}
		}
	}
	t.Fatalf("%s body does not close", sig)
	return ""
}

// dartProbeBitsWidth runs the generated writer twice. Under --enable-asserts,
// 200 in a bits(7) throws AssertionError as a field, inside the packed run,
// and on an array element; 127 round-trips. Without --enable-asserts the
// same write of 200 does not throw.
func dartProbeBitsWidth(t *testing.T, files map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const driver = `import 'dart:io';
import 'dart:typed_data';
import 'Wide.dart';

void mustThrow(String label, void Function() write) {
  try {
    write();
  } on AssertionError {
    return;
  }
  stderr.writeln('$label: expected AssertionError');
  exit(1);
}

void mustWrite(String label, void Function() write) {
  try {
    write();
  } catch (e) {
    stderr.writeln('$label: unexpected throw: $e');
    exit(1);
  }
}

void main(List<String> args) {
  final asserts = args.isNotEmpty && args[0] == 'asserts';
  final view = ByteData(64);
  final one = OneBits()..b = 200;
  final flat = FlatBits()..a = 1..b = 200;
  final arr = ArrBits();
  arr.items[0].b = 200;
  if (asserts) {
    mustThrow('one', () => writeOneBits(one, view));
    mustThrow('flat', () => writeFlatBits(flat, view));
    mustThrow('arr', () => writeArrBits(arr, view));
  } else {
    mustWrite('one', () => writeOneBits(one, view));
    mustWrite('flat', () => writeFlatBits(flat, view));
    mustWrite('arr', () => writeArrBits(arr, view));
  }

  final ok = OneBits()..b = 127;
  final n = writeOneBits(ok, view);
  final back = OneBits();
  if (!readOneBits(back, view, n * 8) || back.b != 127) {
    stderr.writeln('one round trip: ${back.b}');
    exit(1);
  }
  final fok = FlatBits()..a = 1..b = 127;
  final fn = writeFlatBits(fok, view);
  final fback = FlatBits();
  if (!readFlatBits(fback, view, fn * 8) || fback.a != 1 || fback.b != 127) {
    stderr.writeln('flat round trip: a=${fback.a} b=${fback.b}');
    exit(1);
  }
  final aok = ArrBits();
  aok.items[0].b = 127;
  aok.items[1].b = 127;
  final an = writeArrBits(aok, view);
  final aback = ArrBits();
  if (!readArrBits(aback, view, an * 8) ||
      aback.items[0].b != 127 ||
      aback.items[1].b != 127) {
    stderr.writeln('arr round trip: ${aback.items[0].b} ${aback.items[1].b}');
    exit(1);
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.dart"), []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		args []string
	}{
		{"enable-asserts", []string{"--enable-asserts", "main.dart", "asserts"}},
		{"release", []string{"main.dart"}},
	} {
		cmd := exec.Command("dart", c.args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("dart %s: %v\n%s", c.name, err, out)
		}
		if text := strings.TrimSpace(string(out)); text != "" {
			t.Errorf("dart %s wrote %s", c.name, text)
		}
	}
}

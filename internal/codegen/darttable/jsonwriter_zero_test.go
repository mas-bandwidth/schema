package darttable

// TestDartJsonWriterRefusesZeroByte pins docs/SPEC-TABLES.md §16.3 and §16.14
// W0/W8 (docs/SPEC.md §5.1 row 8): a JSON writer refuses a string, a text-list
// element or a map key that holds a zero byte, as the binary writers do,
// rather than spelling the `\u0000` escape every reader refuses. Dart is an
// assert leg (§5.1), so the refusal is a debug assert: the probe runs under
// --enable-asserts and catches AssertionError. The same value without a zero
// writes the text it wrote before.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const dartJsonZeroSchema = `package probe

table Root
{
    s string(8)
    l []string(8)
    m map[string(8)]int32
}
`

// dartJsonZeroProbe tries every zero site in a debug build (the assert the leg
// owns) and writes the control value's text for the byte comparison.
const dartJsonZeroProbe = `import 'dart:io';

import 'ProbeWire.dart';

bool refuses(Root v) {
  try {
    rootToJson(v);
    return false;
  } on AssertionError {
    return true;
  }
}

void main() {
  var failed = false;
  void expect(bool ok, String what) {
    if (!ok) {
      stderr.writeln('FAIL: $what');
      failed = true;
    }
  }
  for (final zero in <String>['\u0000ab', 'a\u0000b', 'ab\u0000']) {
    expect(refuses(Root()..s = zero), 'string $zero');
    expect(
        refuses(Root()
          ..l = <RootLText>[RootLText()..value = 'ok', RootLText()..value = zero]),
        'text list $zero');
    expect(refuses(Root()..m = <RootMEntry>[RootMEntry()..key = zero..value = 1]),
        'map key $zero');
  }
  final control = Root()
    ..s = 'ok'
    ..l = <RootLText>[RootLText()..value = 'ok', RootLText()..value = 'ok']
    ..m = <RootMEntry>[RootMEntry()..key = 'ok'..value = 1];
  File('control.json').writeAsStringSync(rootToJson(control));
  if (failed) exitCode = 1;
}
`

func TestDartJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	u := buildUnit(t, dartJsonZeroSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(dartJsonZeroProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart JSON writer did not refuse a zero byte: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL:") {
		t.Fatalf("the generated Dart JSON writer did not refuse a zero byte:\n%s", out)
	}

	control, err := os.ReadFile(filepath.Join(dir, "control.json"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "{\n  \"s\": \"ok\",\n  \"l\": [\n    \"ok\",\n    \"ok\"\n  ],\n  \"m\": {\n    \"ok\": 1\n  }\n}\n"
	if string(control) != want {
		t.Fatalf("the control value wrote %q, want %q", control, want)
	}
}

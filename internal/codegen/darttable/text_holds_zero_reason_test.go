package darttable

// A ZERO IN STORED TEXT REFUSES BY ITS OWN NAME (docs/SPEC-TABLES.md §16.3,
// text_holds_zero): a JSON string the read would place in a string(N) field
// and that decodes to U+0000 refuses as text_holds_zero, not as a lone
// surrogate. The surrogate check in _jsonText also turns a zero away (its
// validator is the write side's, which refuses NUL), so the zero rule has to
// answer first for the refusal to carry its name.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const dartTextHoldsZeroSchema = `package probe

table Root {
	name string(8)
}
`

func TestDartTextHoldsZeroRefusesByName(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	files, err := Generate(buildUnit(t, dartTextHoldsZeroSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire := files["ProbeWire.dart"]
	if len(wire) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	// the JSON text is {"name":"a\u0000b"}, the escape spelled in the text
	probe := `import 'dart:io';
import 'ProbeWire.dart';

void main() {
  final r = TableTextReport();
  try {
    rootFromJson('{"name":"a\\u0000b"}', r);
    stdout.writeln(r.refused ? 'refused ${r.reason}' : 'read');
  } on FormatException catch (e) {
    stdout.writeln('refused ${e.message}');
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart text read: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "refused text_holds_zero") {
		t.Fatalf("a zero in a string(N) value answered %q, want a refusal named text_holds_zero", got)
	}
}

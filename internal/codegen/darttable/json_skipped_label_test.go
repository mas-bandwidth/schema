package darttable

// THE LABEL RULES HOLD IN A SKIPPED VALUE (docs/SPEC-TABLES.md §16.7): in a
// pointered root, an object the read skips (here an unknown key's value) keeps
// the text's label numbering in text order. A label alone REFERS, and the text
// must have defined it and closed that definition before this point; a label
// with fields DEFINES, must be new to the whole text, and stays open until the
// skipped object closes, after which a reference to it reads null. The Go
// engine is the oracle: every row's verdict is asked of it first.

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const dartSkippedLabelSchema = `package probe

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}
`

// skippedLabelRows: name, Scene text, verdict.
var dartSkippedLabelRows = [][3]string{
	{"placed-then-skipped-definition", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1, "value": 2}}`, "refused"},
	{"skipped-reference-before-definition", `{"zz": {"&node": 5}, "head": {"&node": 5, "value": 1}}`, "refused"},
	{"skipped-reference-undefined", `{"zz": {"&node": 5}}`, "refused"},
	{"skipped-definition-then-placed-reference", `{"zz": {"&node": 5, "value": 1}, "head": {"&node": 5}}`, "read"},
	{"placed-reference-before-skipped-definition", `{"head": {"&node": 5}, "zz": {"&node": 5, "value": 1}}`, "refused"},
	{"skipped-definition-twice", `{"zz": {"&node": 5, "value": 1}, "yy": {"&node": 5, "value": 2}}`, "refused"},
	{"skipped-definition-then-skipped-reference", `{"zz": {"&node": 5, "value": 1}, "yy": {"&node": 5}}`, "read"},
	{"skipped-reference-inside-its-open-definition", `{"zz": {"&node": 5, "value": {"&node": 5}}}`, "refused"},
	{"placed-definition-then-skipped-reference", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1}}`, "read"},
}

func TestDartJsonSkippedLabelRules(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	u := buildUnit(t, dartSkippedLabelSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	packet := packetFiles(t, u)
	maps.Copy(packet, files)

	model := tabletext.NewModel(u)
	st := model.Lookup("Scene")
	if st == nil {
		t.Fatal("no Scene in the unit")
	}
	work := t.TempDir()
	for name, src := range packet {
		if err := os.WriteFile(filepath.Join(work, name), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var args []string
	for _, row := range dartSkippedLabelRows {
		var report tabletext.Report
		want := "refused"
		if model.Read(model.New(st), []byte(row[1]), &report) {
			want = "read"
		}
		if want != row[2] {
			t.Errorf("%s: the Go engine answered %q, the row %q", row[0], want, row[2])
		}
		name := row[0] + ".json"
		if err := os.WriteFile(filepath.Join(work, name), []byte(row[1]), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, name)
	}
	probe := `import 'dart:io';
import 'ProbeWire.dart';

void main(List<String> args) {
  for (final path in args) {
    final r = TableTextReport();
    try {
      sceneFromJson(File(path).readAsStringSync(), r);
      stdout.writeln(r.refused ? 'refused' : 'read');
    } on FormatException {
      stdout.writeln('refused');
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(work, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart text read: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(got) != len(dartSkippedLabelRows) {
		t.Fatalf("the probe answered %d lines for %d rows: %q", len(got), len(dartSkippedLabelRows), out)
	}
	for i, row := range dartSkippedLabelRows {
		if got[i] != row[2] {
			t.Errorf("%s: Dart answered %q, want %q (%s)", row[0], got[i], row[2], row[1])
		}
	}
}

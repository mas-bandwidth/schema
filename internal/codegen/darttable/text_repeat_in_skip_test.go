package darttable

// A REPEATED KEY INSIDE A SKIPPED UNKNOWN VALUE IS DAMAGE (docs/plans/strict-only.md
// decision 5: a repeated JSON key is damage, the `duplicate` counter is
// retired). The Dart text read used to judge a repeat only among the keys a
// table names, so an unknown key's value — an object it never looks inside —
// carried a repeat straight through to an accepted read. The Go engine
// (internal/tabletext) refuses it at any depth; the Dart read owes the same
// verdict. The negative control plants the old parser (no per-object key set)
// into the emitted ProbeWire.dart and requires the refusing rows to turn into
// reads.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const dartTextRepeatSkipSchema = `package probe

table Root {
	val int32
}
`

// the parser line the fix added, and the planted edit that takes it out
const dartTextRepeatSkipCheck = "if (!keys.add(k)) throw FormatException('text_damaged: repeated key', s, i);"

var dartTextRepeatSkipCases = []struct {
	name string
	json string
}{
	{"unknown-object-repeat", `{"val": 1, "nope": {"a": 1, "a": 2}}`},
	{"unknown-array-object-repeat", `{"val": 1, "nope": [{"a": 1, "a": 2}]}`},
	{"unknown-deep-repeat", `{"nope": {"x": {"y": {"b": true, "b": false}}}, "val": 1}`},
	{"unknown-object-distinct", `{"val": 1, "nope": {"a": 1, "b": 2}}`},
	{"unknown-same-key-two-objects", `{"val": 1, "nope": [{"a": 1}, {"a": 2}]}`},
}

// dartTextRepeatSkipRun answers each case's verdict from the generated Dart
// text read: `refused`, or `read <val> <unknown>`.
func dartTextRepeatSkipRun(t *testing.T, wire []byte) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, tc := range dartTextRepeatSkipCases {
		name := tc.name + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(tc.json), 0o600); err != nil {
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
      final v = rootFromJson(File(path).readAsStringSync(), r);
      stdout.writeln(r.refused ? 'refused' : 'read ${v.val} ${r.unknown}');
    } on FormatException {
      stdout.writeln('refused');
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart text read: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(dartTextRepeatSkipCases) {
		t.Fatalf("the probe answered %d lines for %d cases: %q", len(lines), len(dartTextRepeatSkipCases), out)
	}
	return lines
}

func dartTextRepeatSkipWire(t *testing.T) []byte {
	t.Helper()
	files, err := Generate(buildUnit(t, dartTextRepeatSkipSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire := files["ProbeWire.dart"]
	if len(wire) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	return wire
}

func TestDartTextRepeatedKeyInSkippedValueRefuses(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	u := buildUnit(t, dartTextRepeatSkipSchema)
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("the probe unit declares no Root")
	}
	got := dartTextRepeatSkipRun(t, dartTextRepeatSkipWire(t))
	for i, tc := range dartTextRepeatSkipCases {
		inst := model.New(def)
		var rep tabletext.Report
		want := "refused"
		if model.Read(inst, []byte(tc.json), &rep) {
			// the Go engine accepted: the Dart read owes the value and the count
			want = "read 1 1"
		}
		if got[i] != want {
			t.Errorf("%s: Dart answered %q, the Go engine %q, for %s", tc.name, got[i], want, tc.json)
		}
	}
	// the rows themselves: three repeats refuse, two non-repeats read
	for i, want := range []string{"refused", "refused", "refused", "read 1 1", "read 1 1"} {
		if got[i] != want {
			t.Errorf("%s: answered %q, want %q", dartTextRepeatSkipCases[i].name, got[i], want)
		}
	}
}

func TestDartTextRepeatedKeyInSkippedValueNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	wire := string(dartTextRepeatSkipWire(t))
	if strings.Count(wire, dartTextRepeatSkipCheck) != 1 {
		t.Fatalf("the emitted parser does not carry the per-object key check exactly once")
	}
	planted := strings.Replace(wire, dartTextRepeatSkipCheck, "", 1)
	got := dartTextRepeatSkipRun(t, []byte(planted))
	for i := range 3 {
		if got[i] == "refused" {
			t.Errorf("%s: still refused with the key check taken out; the row does not reach the check", dartTextRepeatSkipCases[i].name)
		}
	}
}

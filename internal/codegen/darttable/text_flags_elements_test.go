package darttable

// A FLAGS ELEMENT THAT IS NOT A STRING REFUSES (docs/SPEC-TABLES.md §16.2).
// The Dart text read used to count every element that matched no variant as
// unknown, so a number, literal, array or object element was accepted. The Go
// engine (internal/tabletext) refuses it kind_mismatch; the Dart read owes the
// same verdict. A string naming an unknown variant keeps its own treatment:
// skipped and counted unknown, the read going on.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const dartJsonFlagsSchema = `package probe

flags Perks { Shielded, Cloaked, Turbo }

table Root {
	perks Perks
}
`

// the emitted reader line that refuses a non-string element, and the planted
// edit that takes it out.
const dartJsonFlagsCheck = "default: if (item is String) { r.unknown++; } else { r.refuse('kind_mismatch'); }"

var dartJsonFlagsCases = []struct {
	name string
	json string
}{
	{"number", `{"perks": [1]}`},
	{"literal", `{"perks": [true]}`},
	{"null", `{"perks": [null]}`},
	{"array", `{"perks": [[]]}`},
	{"object", `{"perks": [{}]}`},
	{"string-then-number", `{"perks": ["Turbo", 1]}`},
	{"control-known", `{"perks": ["Turbo"]}`},
	{"control-unknown", `{"perks": ["NoSuchFlag"]}`},
}

// dartJsonFlagsRun answers each case's verdict from the generated Dart text
// read: `refused`, or `read`.
func dartJsonFlagsRun(t *testing.T, wire []byte) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, tc := range dartJsonFlagsCases {
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
      rootFromJson(File(path).readAsStringSync(), r);
      stdout.writeln(r.refused ? 'refused' : 'read');
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
	if len(lines) != len(dartJsonFlagsCases) {
		t.Fatalf("the probe answered %d lines for %d cases: %q", len(lines), len(dartJsonFlagsCases), out)
	}
	return lines
}

func dartJsonFlagsWire(t *testing.T) []byte {
	t.Helper()
	files, err := Generate(buildUnit(t, dartJsonFlagsSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire := files["ProbeWire.dart"]
	if len(wire) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	return wire
}

func TestDartJsonFlagsElementsAreStrings(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	u := buildUnit(t, dartJsonFlagsSchema)
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("the probe unit declares no Root")
	}
	got := dartJsonFlagsRun(t, dartJsonFlagsWire(t))
	for i, tc := range dartJsonFlagsCases {
		inst := model.New(def)
		var rep tabletext.Report
		want := "refused"
		if model.Read(inst, []byte(tc.json), &rep) {
			want = "read"
		}
		if got[i] != want {
			t.Errorf("%s: Dart answered %q, the Go engine %q, for %s", tc.name, got[i], want, tc.json)
		}
	}
	// The six non-string rows refuse, the two string controls read.
	for i, want := range []string{"refused", "refused", "refused", "refused", "refused", "refused", "read", "read"} {
		if got[i] != want {
			t.Errorf("%s: answered %q, want %q", dartJsonFlagsCases[i].name, got[i], want)
		}
	}
}

func TestDartJsonFlagsElementsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	wire := string(dartJsonFlagsWire(t))
	if strings.Count(wire, dartJsonFlagsCheck) != 1 {
		t.Fatalf("the emitted flags reader does not carry the string check exactly once")
	}
	planted := strings.Replace(wire, dartJsonFlagsCheck, "default: r.unknown++;", 1)
	got := dartJsonFlagsRun(t, []byte(planted))
	for i := range 6 {
		if got[i] == "refused" {
			t.Errorf("%s: still refused with the string check taken out; the row does not reach the check", dartJsonFlagsCases[i].name)
		}
	}
}

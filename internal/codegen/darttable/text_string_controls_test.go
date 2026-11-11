package darttable

// A RAW CONTROL CHARACTER IS NOT A JSON STRING BODY (docs/SPEC-TABLES.md
// §16.2, RFC 8259 §7): a code unit below U+0020 inside a string must be
// escaped. The Go engine (internal/tabletext) refuses one wherever a string
// appears, a skipped unknown value's included; the Dart text read owes the
// same verdict. The escaped spellings of the same characters keep their
// verdicts and values. The negative control plants the old parser (no
// raw-control check) into the emitted ProbeWire.dart and requires the raw
// rows to turn into reads.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const dartTextStringControlSchema = `package probe

table Root {
	name string(64)
}
`

// the parser line the fix added, and the planted edit that takes it out.
const dartTextStringControlCheck = "if (c < 0x20) throw FormatException('malformed', s, i - 1);"

var dartTextStringControlCases = []struct {
	name    string
	json    string
	raw     bool
	value   string // the known field's code units, as four hex digits each
	unknown int
}{
	{"known-raw-u0001", "{\"name\": \"a\x01b\"}", true, "", 0},
	{"known-raw-u001f", "{\"name\": \"a\x1fb\"}", true, "", 0},
	{"known-raw-tab", "{\"name\": \"a\tb\"}", true, "", 0},
	{"known-raw-newline", "{\"name\": \"a\nb\"}", true, "", 0},
	{"skipped-raw-u0001", "{\"name\": \"ok\", \"nope\": \"a\x01b\"}", true, "", 0},
	{"skipped-raw-u001f", "{\"name\": \"ok\", \"nope\": \"a\x1fb\"}", true, "", 0},
	{"skipped-raw-tab", "{\"name\": \"ok\", \"nope\": \"a\tb\"}", true, "", 0},
	{"skipped-raw-newline", "{\"name\": \"ok\", \"nope\": \"a\nb\"}", true, "", 0},
	{"known-escaped-u0001", "{\"name\": \"a\\u0001b\"}", false, "006100010062", 0},
	{"known-escaped-u001f", "{\"name\": \"a\\u001fb\"}", false, "0061001f0062", 0},
	{"known-escaped-tab", "{\"name\": \"a\\tb\"}", false, "006100090062", 0},
	{"known-escaped-newline", "{\"name\": \"a\\nb\"}", false, "0061000a0062", 0},
	{"skipped-escaped-u0001", "{\"name\": \"ok\", \"nope\": \"a\\u0001b\"}", false, "006f006b", 1},
	{"skipped-escaped-u001f", "{\"name\": \"ok\", \"nope\": \"a\\u001fb\"}", false, "006f006b", 1},
	{"skipped-escaped-tab", "{\"name\": \"ok\", \"nope\": \"a\\tb\"}", false, "006f006b", 1},
	{"skipped-escaped-newline", "{\"name\": \"ok\", \"nope\": \"a\\nb\"}", false, "006f006b", 1},
}

// dartTextStringControlRun answers each case's verdict from the generated Dart
// text read: `refused`, or `read <value> <unknown>`.
func dartTextStringControlRun(t *testing.T, wire []byte) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, tc := range dartTextStringControlCases {
		name := tc.name + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(tc.json), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, name)
	}
	probe := `import 'dart:io';
import 'ProbeWire.dart';

String hexUnits(String s) => s.codeUnits.map((c) => c.toRadixString(16).padLeft(4, '0')).join();

void main(List<String> args) {
  for (final path in args) {
    final r = TableTextReport();
    try {
      final v = rootFromJson(File(path).readAsStringSync(), r);
      stdout.writeln(r.refused ? 'refused' : 'read ${hexUnits(v.name)} ${r.unknown}');
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
	if len(lines) != len(dartTextStringControlCases) {
		t.Fatalf("the probe answered %d lines for %d cases: %q", len(lines), len(dartTextStringControlCases), out)
	}
	return lines
}

func dartTextStringControlWire(t *testing.T) []byte {
	t.Helper()
	files, err := Generate(buildUnit(t, dartTextStringControlSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire := files["ProbeWire.dart"]
	if len(wire) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	return wire
}

func TestDartTextStringControlCharacters(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	u := buildUnit(t, dartTextStringControlSchema)
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("the probe unit declares no Root")
	}
	got := dartTextStringControlRun(t, dartTextStringControlWire(t))
	for i, tc := range dartTextStringControlCases {
		inst := model.New(def)
		var rep tabletext.Report
		want := "refused"
		if model.Read(inst, []byte(tc.json), &rep) {
			var b strings.Builder
			for _, c := range inst.Fields[0].Cell.Str {
				fmt.Fprintf(&b, "%04x", c)
			}
			want = fmt.Sprintf("read %s %d", b.String(), rep.Unknown)
			if tc.value != "" && want != fmt.Sprintf("read %s %d", tc.value, tc.unknown) {
				t.Errorf("%s: the Go engine answered %q, want the escaped character's value", tc.name, want)
			}
		} else if !tc.raw {
			t.Errorf("%s: the Go engine refused an escaped spelling", tc.name)
		}
		if got[i] != want {
			t.Errorf("%s: Dart answered %q, the Go engine %q, for %q", tc.name, got[i], want, tc.json)
		}
	}
	// The raw rows refuse; the escaped rows read the character.
	for i, want := range []string{
		"refused", "refused", "refused", "refused",
		"refused", "refused", "refused", "refused",
		"read 006100010062 0", "read 0061001f0062 0", "read 006100090062 0", "read 0061000a0062 0",
		"read 006f006b 1", "read 006f006b 1", "read 006f006b 1", "read 006f006b 1",
	} {
		if got[i] != want {
			t.Errorf("%s: answered %q, want %q", dartTextStringControlCases[i].name, got[i], want)
		}
	}
}

func TestDartTextStringControlCharactersNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	wire := string(dartTextStringControlWire(t))
	if strings.Count(wire, dartTextStringControlCheck) != 1 {
		t.Fatalf("the emitted parser does not carry the raw-control check exactly once")
	}
	planted := strings.Replace(wire, dartTextStringControlCheck, "", 1)
	got := dartTextStringControlRun(t, []byte(planted))
	for i, tc := range dartTextStringControlCases {
		if tc.raw && got[i] == "refused" {
			t.Errorf("%s: still refused with the control check taken out; the row does not reach the check", tc.name)
		}
	}
}

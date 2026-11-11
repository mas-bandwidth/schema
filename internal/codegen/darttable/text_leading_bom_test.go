package darttable

// A LEADING BYTE-ORDER MARK IS SKIPPED, ONCE, AT THE VERY START
// (docs/SPEC-TABLES.md §16.2, §16.12): a text beginning with U+FEFF reads the
// value after it, exactly as the Go engine (internal/tabletext) does, and a
// U+FEFF anywhere else is not whitespace and stays refused. The negative
// control plants the parser without the leading skip into the emitted
// ProbeWire.dart and requires the accepted rows to turn into refusals.

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

const dartTextLeadingBomSchema = `package probe

table Root {
	player_id int32
}
`

// the parser line the fix added, and the planted edit that takes it out.
const dartTextLeadingBomCheck = "if (text.isNotEmpty && text.codeUnitAt(0) == 0xFEFF) p.i = 1;"

var dartTextLeadingBomCases = []struct {
	name string
	json string
}{
	{"mark-then-object", "\uFEFF{\"player_id\": 5}"},
	{"mark-then-empty-object", "\uFEFF{}"},
	{"two-marks-in-a-row", "\uFEFF\uFEFF{\"player_id\": 5}"},
	{"mark-after-whitespace", " \uFEFF{\"player_id\": 5}"},
}

// dartTextLeadingBomRun answers each case's verdict from the generated Dart
// text read: `refused`, or `read <player_id> <unknown>`.
func dartTextLeadingBomRun(t *testing.T, wire []byte) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, tc := range dartTextLeadingBomCases {
		name := tc.name + ".codes"
		var codes []string
		for _, r := range tc.json {
			codes = append(codes, fmt.Sprint(r))
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(codes, " ")), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, name)
	}
	probe := `import 'dart:io';
import 'ProbeWire.dart';

void main(List<String> args) {
  for (final path in args) {
    final codes = File(path)
        .readAsStringSync()
        .trim()
        .split(RegExp(r'\s+'))
        .where((s) => s.isNotEmpty)
        .map(int.parse)
        .toList();
    final r = TableTextReport();
    try {
      final v = rootFromJson(String.fromCharCodes(codes), r);
      stdout.writeln(r.refused ? 'refused' : 'read ${v.playerId} ${r.unknown}');
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
	if len(lines) != len(dartTextLeadingBomCases) {
		t.Fatalf("the probe answered %d lines for %d cases: %q", len(lines), len(dartTextLeadingBomCases), out)
	}
	return lines
}

func dartTextLeadingBomWire(t *testing.T) []byte {
	t.Helper()
	files, err := Generate(buildUnit(t, dartTextLeadingBomSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire := files["ProbeWire.dart"]
	if len(wire) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	return wire
}

func TestDartTextLeadingBom(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	u := buildUnit(t, dartTextLeadingBomSchema)
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("the probe unit declares no Root")
	}
	got := dartTextLeadingBomRun(t, dartTextLeadingBomWire(t))
	for i, tc := range dartTextLeadingBomCases {
		inst := model.New(def)
		var rep tabletext.Report
		want := "refused"
		if model.Read(inst, []byte(tc.json), &rep) {
			want = fmt.Sprintf("read %d %d", inst.Fields[0].Cell.U, rep.Unknown)
		}
		if got[i] != want {
			t.Errorf("%s: Dart answered %q, the Go engine %q, for %q", tc.name, got[i], want, tc.json)
		}
	}
	// The four rows: one mark at the start reads and is not data, and the mark
	// anywhere else stays refused.
	for i, want := range []string{"read 5 0", "read 0 0", "refused", "refused"} {
		if got[i] != want {
			t.Errorf("%s: answered %q, want %q", dartTextLeadingBomCases[i].name, got[i], want)
		}
	}
}

func TestDartTextLeadingBomNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	wire := string(dartTextLeadingBomWire(t))
	if strings.Count(wire, dartTextLeadingBomCheck) != 1 {
		t.Fatalf("the emitted parser does not carry the leading-mark skip exactly once")
	}
	planted := strings.Replace(wire, dartTextLeadingBomCheck, "", 1)
	got := dartTextLeadingBomRun(t, []byte(planted))
	for i := range 2 {
		if got[i] != "refused" {
			t.Errorf("%s: still read with the leading-mark skip taken out; the row does not reach the skip", dartTextLeadingBomCases[i].name)
		}
	}
}

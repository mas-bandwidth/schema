package darttable

// A SCHEMA-NAME STRING IS CAPPED AT 255 BYTES (docs/SPEC-TABLES.md §16.2): an
// object key naming a field, an enum variant name, a flags element, a union arm
// key and an `&node` name, in every object and inside a skipped value, a longer
// one refuses the whole read — and an unknown enum name over 255 bytes refuses
// rather than reading None. The cap never reaches a map key or a string value.
// The Dart read owes the same verdict as the vector manifest and the Go engine
// for every name-cap row under testdata/wire/json-read-closed/.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// dartJsonNameCapSchema is the read-closed unit: a flags field, an enum, a
// union, a map keyed by string, a string field, and a pointered root for the
// `&node` name's site.
const dartJsonNameCapSchema = `package probe

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// dartJsonNameCapRows are the manifest's name-cap rows; the depth and UTF-8
// rows belong to their own checks.
var dartJsonNameCapRows = []string{
	"field-key-255", "field-key-256",
	"skipped-key-255", "skipped-key-256",
	"enum-name-255", "enum-name-256",
	"flags-element-255", "flags-element-256",
	"union-arm-255", "union-arm-256",
	"ampersand-255", "ampersand-256",
	"map-key-long", "string-value-long",
}

type dartJsonNameCapRow struct {
	name    string
	root    string
	json    string
	verdict string
}

func dartJsonNameCapVectors(t *testing.T) (string, []dartJsonNameCapRow) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	wanted := make(map[string]bool, len(dartJsonNameCapRows))
	for _, name := range dartJsonNameCapRows {
		wanted[name] = true
	}
	var rows []dartJsonNameCapRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		if !wanted[fields[0]] {
			continue
		}
		delete(wanted, fields[0])
		rows = append(rows, dartJsonNameCapRow{name: fields[0], root: fields[1], json: fields[2], verdict: fields[3]})
	}
	for _, name := range dartJsonNameCapRows {
		if wanted[name] {
			t.Errorf("%s/MANIFEST.tsv names no name-cap row %q", dir, name)
		}
	}
	if len(rows) != len(dartJsonNameCapRows) {
		t.Fatalf("read %d name-cap rows, want %d", len(rows), len(dartJsonNameCapRows))
	}
	return dir, rows
}

// dartJsonNameCapRun answers each row's verdict from the generated Dart read:
// `read` or `refused`.
func dartJsonNameCapRun(t *testing.T, files map[string][]byte, dir string, rows []dartJsonNameCapRow) []string {
	t.Helper()
	work := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(work, name), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var args []string
	for _, row := range rows {
		body, err := os.ReadFile(filepath.Join(dir, row.json))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		name := row.name + ".json"
		if err := os.WriteFile(filepath.Join(work, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, row.root, name)
	}
	probe := `import 'dart:io';
import 'ProbeWire.dart';

void main(List<String> args) {
  for (var i = 0; i + 1 < args.length; i += 2) {
    final root = args[i];
    final path = args[i + 1];
    final r = TableTextReport();
    try {
      final text = File(path).readAsStringSync();
      if (root == 'Closed') {
        closedFromJson(text, r);
      } else {
        sceneFromJson(text, r);
      }
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
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart text read: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("the probe answered %d lines for %d rows: %q", len(lines), len(rows), out)
	}
	return lines
}

func TestDartJsonNameCap(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	u := buildUnit(t, dartJsonNameCapSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(files["ProbeWire.dart"]) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	packet := packetFiles(t, u)
	for name, src := range files {
		packet[name] = src
	}
	dir, rows := dartJsonNameCapVectors(t)

	// The manifest is the contract and the Go engine is the oracle: they agree
	// before the Dart read is asked to.
	model := tabletext.NewModel(u)
	for _, row := range rows {
		if row.verdict != "read" && row.verdict != "refused" {
			t.Errorf("%s: verdict %q, want read or refused", row.name, row.verdict)
			continue
		}
		st := model.Lookup(row.root)
		if st == nil {
			t.Errorf("%s: no root %q in the unit", row.name, row.root)
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, row.json))
		if err != nil {
			t.Errorf("%s: %v", row.name, err)
			continue
		}
		inst := model.New(st)
		var report tabletext.Report
		want := "refused"
		if model.Read(inst, body, &report) {
			want = "read"
		}
		if want != row.verdict {
			t.Errorf("%s: the Go engine answered %q, the manifest %q", row.name, want, row.verdict)
		}
	}

	got := dartJsonNameCapRun(t, packet, dir, rows)
	for i, row := range rows {
		if got[i] != row.verdict {
			t.Errorf("%s: Dart answered %q, want %q (%s)", row.name, got[i], row.verdict, row.json)
		}
	}
}

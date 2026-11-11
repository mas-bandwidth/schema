package darttable

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// compared on the verdict alone with the Go engine's (internal/tabletext), and
// the engine is first held to the verdict the vectors state. Running the Dart
// SDK makes this a functional test, gated by slowtest. The negative control
// takes the repeat check out of the emitted flags reader and requires the
// repeat rows to turn into reads.

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const dartJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

// dartJsonFlagsNameOnceCheck matches the repeat check the emitted flags reader
// carries before it sets a known variant's bit.
var dartJsonFlagsNameOnceCheck = regexp.MustCompile(`if \(bits & \d+ != 0\) \{ r\.refuse\('text_damaged'\); return def; \} `)

type dartJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func dartJsonFlagsNameOnceRows(t *testing.T) []dartJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(dartJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []dartJsonFlagsNameOnceRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 6 {
			t.Fatalf("MANIFEST.tsv: %d columns, want 6: %q", len(c), line)
		}
		rows = append(rows, dartJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// dartJsonFlagsNameOnceWire generates the Dart for a unit directory and names
// the library declaring root's text read.
func dartJsonFlagsNameOnceWire(t *testing.T, unit, root string) (map[string][]byte, string) {
	t.Helper()
	u := loadDirUnit(t, filepath.Join("../../..", unit))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	fn := string(unicode.ToLower(rune(root[0]))) + root[1:] + "FromJson"
	wire := ""
	for name, src := range files {
		if strings.HasSuffix(name, "Wire.dart") && strings.Contains(string(src), root+" "+fn+"(") {
			wire = name
		}
	}
	if wire == "" {
		t.Fatalf("Generate emitted no library declaring %s", fn)
	}
	maps.Copy(files, packetFiles(t, u))
	return files, wire
}

// dartJsonFlagsNameOnceRun answers each row's verdict from the generated Dart
// text read, `read` or `refused`, with plant applied to the wire library.
func dartJsonFlagsNameOnceRun(t *testing.T, rows []dartJsonFlagsNameOnceRow, plant func(string) string) []string {
	t.Helper()
	unit, root := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != root {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, root)
		}
	}
	files, wire := dartJsonFlagsNameOnceWire(t, unit, root)
	files[wire] = []byte(plant(string(files[wire])))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var args []string
	for _, row := range rows {
		src, err := os.ReadFile(filepath.Join(dartJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, row.text), src, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, row.text)
	}
	fn := string(unicode.ToLower(rune(root[0]))) + root[1:] + "FromJson"
	probe := fmt.Sprintf(`import 'dart:io';
import '%s';

void main(List<String> args) {
  for (final path in args) {
    final r = TableTextReport();
    try {
      %s(File(path).readAsStringSync(), r);
      stdout.writeln(r.refused ? 'refused' : 'read');
    } on FormatException {
      stdout.writeln('refused');
    }
  }
}
`, wire, fn)
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
	if len(lines) != len(rows) {
		t.Fatalf("the probe answered %d lines for %d rows: %q", len(lines), len(rows), out)
	}
	return lines
}

// dartJsonFlagsNameOnceOracle answers the Go engine's verdict for each row,
// and holds it to the verdict the vectors state.
func dartJsonFlagsNameOnceOracle(t *testing.T, rows []dartJsonFlagsNameOnceRow) []string {
	t.Helper()
	var want []string
	for _, row := range rows {
		u := loadDirUnit(t, filepath.Join("../../..", row.unit))
		model := tabletext.NewModel(u)
		def := model.Lookup(row.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no %s", row.name, row.root)
		}
		src, err := os.ReadFile(filepath.Join(dartJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		inst := model.New(def)
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(inst, src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		want = append(want, verdict)
	}
	return want
}

func TestDartJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	rows := dartJsonFlagsNameOnceRows(t)
	want := dartJsonFlagsNameOnceOracle(t, rows)
	got := dartJsonFlagsNameOnceRun(t, rows, func(s string) string { return s })
	for i, row := range rows {
		if got[i] != want[i] {
			t.Errorf("%s: Dart answered %q, the Go engine %q", row.name, got[i], want[i])
		}
	}
}

func TestDartJsonFlagsNameOnceNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	rows := dartJsonFlagsNameOnceRows(t)
	files, wire := dartJsonFlagsNameOnceWire(t, rows[0].unit, rows[0].root)
	if !dartJsonFlagsNameOnceCheck.Match(files[wire]) {
		t.Fatal("the emitted flags reader carries no repeat check")
	}
	got := dartJsonFlagsNameOnceRun(t, rows, func(s string) string {
		return dartJsonFlagsNameOnceCheck.ReplaceAllString(s, "")
	})
	refusing := 0
	for i, row := range rows {
		if row.verdict != "refused" {
			continue
		}
		refusing++
		if got[i] == "refused" {
			t.Errorf("%s: still refused with the repeat check taken out; the row does not reach the check", row.name)
		}
	}
	if refusing == 0 {
		t.Fatal("the vectors carry no refusing row")
	}
}

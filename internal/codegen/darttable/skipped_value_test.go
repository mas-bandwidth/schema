package darttable

// A VALUE THE READER SKIPS IS HELD TO WHAT A READ OF IT WOULD HOLD
// (docs/SPEC-TABLES.md §4, §16.3; docs/rulings.json
// skipped-value-is-validated). The shared vectors under
// testdata/wire/skipped-value/ are read through the leg's own message load and
// its JSON read: an announced entry this reader cannot name is stepped over by
// the announcement's shape, and a value under a key it cannot name is skipped
// by the text grammar, but each is held to every condition a read of it would
// apply, and damage inside it refuses the whole load. The verdict alone is
// compared; a refusal's name is each language's own diagnostic.
//
// Running the Dart SDK makes this a functional test, gated by slowtest.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const dartSkippedValueDir = "../../../testdata/wire/skipped-value"

type dartSkippedValueRow struct {
	file, form, verdict string
}

func dartSkippedValueRows(t *testing.T) []dartSkippedValueRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dartSkippedValueDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []dartSkippedValueRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("MANIFEST.tsv:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("MANIFEST.tsv:%d: form %q, want json or message", i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[2])
		}
		rows = append(rows, dartSkippedValueRow{f[0], f[1], f[2]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// dartSkippedValueRun emits the reader unit's whole surface beside a probe and
// answers every row's verdict, `read` or `refused`, from the generated Dart.
func dartSkippedValueRun(t *testing.T, rows []dartSkippedValueRow) map[string]string {
	t.Helper()
	u := loadDirUnit(t, "../../../test/skipped-value/reader")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, packetFiles(t, u))
	wire := ""
	for name := range files {
		if strings.HasSuffix(name, "Wire.dart") {
			if wire != "" {
				t.Fatalf("Generate emitted two wire libraries, %s and %s", wire, name)
			}
			wire = name
		}
	}
	if wire == "" {
		t.Fatal("Generate emitted no wire library")
	}
	home := runtimeHome(u) + "Fixed.dart"
	rootLib := fixedTableBase(u, "Root") + "Fixed.dart"
	if files[home] == nil || files[rootLib] == nil {
		t.Fatalf("Generate emitted no %s or %s", home, rootLib)
	}

	announcement, err := os.ReadFile(filepath.Join(dartSkippedValueDir, "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  final announced = h.announceRead(vocab, ann, annRep);
`, dartBytes(announcement)))

	var args []string
	dir := t.TempDir()
	for _, row := range rows {
		if row.form == "message" {
			if row.file == "announcement.bin" {
				fmt.Fprintf(&body, "  stdout.writeln('message/%s|' + (announced ? 'read' : 'refused'));\n", row.file)
				continue
			}
			msg, err := os.ReadFile(filepath.Join(dartSkippedValueDir, row.file))
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, `  {
    var verdict = 'refused';
    if (announced) {
      final msg = Uint8List.fromList(%s);
      final v = h.Root();
      h.initRoot(v);
      final values = <h.Root>[v];
      final rep = h.TableReport();
      final count = r.rootLoadMessages(values, msg, vocab, rep);
      if (!rep.refused && count == 1) verdict = 'read';
    }
    stdout.writeln('message/%s|$verdict');
  }
`, dartBytes(msg), row.file)
			continue
		}
		src, err := os.ReadFile(filepath.Join(dartSkippedValueDir, row.file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, row.file), src, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, row.file)
	}

	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';

import '%s' as h;
import '%s' as r;
import '%s' as w;

void main(List<String> args) {
%s
  for (final path in args) {
    final rep = w.TableTextReport();
    var verdict = 'refused';
    try {
      w.rootFromJson(File(path).readAsStringSync(), rep);
      if (!rep.refused) verdict = 'read';
    } on FormatException {
      verdict = 'refused';
    }
    stdout.writeln('json/' + path + '|' + verdict);
  }
}
`, home, rootLib, wire, body.String())
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "skipped_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "skipped_probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart read: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		key, verdict, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("the probe answered %q, not a row", line)
		}
		got[key] = verdict
	}
	return got
}

func TestDartSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	rows := dartSkippedValueRows(t)
	got := dartSkippedValueRun(t, rows)
	for _, row := range rows {
		key := row.form + "/" + row.file
		verdict, ok := got[key]
		if !ok {
			t.Errorf("%s: the probe answered no verdict", key)
			continue
		}
		if verdict != row.verdict {
			t.Errorf("%s: Dart answered %s, the vectors state %s", key, verdict, row.verdict)
		}
	}
}

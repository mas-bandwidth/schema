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

// A RANGED INTEGER ON THE SKIP PATH is rebuilt from the sender's base and held
// to the range its own kind can carry (docs/SPEC-TABLES.md §3.3): an offset
// that spells a value past the kind refuses the load, one at the kind's edge
// reads. The unit is the skip-depth probe's, so slot 8 is an entry Root cannot
// name.
func TestMessageFormSkipRangedDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

fixed table Root {
    a int32
    b bool
    c uint8
}

fixed table Zed {
    d bool
    e bool
    f bool
    g bool
    h bool
}
`)
	out, err := runBatchProbe(t, u, "Root", dartSkipRangedBody)
	if err != nil {
		t.Fatalf("skip_ranged: %v\n%s", err, out)
	}
}

const dartSkipRangedBody = `
  check(h.tableMessageRefBits >= 4, 'skip ranged slot needs four reference bits, got ${h.tableMessageRefBits}');
  final width = h.tableMessageRefBits;

  h.TableVocabulary rangedVocab(int kind, List<int> shape) {
    final sig = Uint8List(9 + shape.length);
    sig.setRange(9, sig.length, shape);
    final entries = List<h.TableMessageEntry>.generate(8, (i) => h.TableMessageEntry(100 + i, 0));
    entries[7] = h.TableMessageEntry(1, kind, sig);
    check(entries[7].shape != null && entries[7].shape!.packing == 1, 'ranged shape for kind $kind');
    final vocab = h.TableVocabulary(entries: entries);
    vocab.count = 8;
    vocab.refBits = width;
    vocab.announced = true;
    return vocab;
  }

  // The unknown field's reference, its offset (low 64 bits first), the end.
  Uint8List batch(int lo, int hi, int bits) {
    final out = <int>[];
    void put(int value, int n) {
      for (var b = 0; b < n; b++) {
        out.add((value >> b) & 1);
      }
    }
    put(8, width);
    put(lo, bits < 64 ? bits : 64);
    if (bits > 64) put(hi, bits - 64);
    put(0, width);
    final buf = Uint8List(2 + ((out.length + 7) >> 3));
    buf[0] = 2;
    buf[1] = 0;
    for (var i = 0; i < out.length; i++) {
      if (out[i] != 0) buf[2 + (i >> 3)] |= 1 << (i & 7);
    }
    return buf;
  }

  void expectOk(String row, h.TableVocabulary vocab, Uint8List buf) {
    final value = h.Root();
    h.initRoot(value);
    final rep = h.TableReport();
    final count = r.rootLoadMessages(<h.Root>[value], buf, vocab, rep);
    check(count == 1 && !rep.refused && rep.unknown == 1, '$row: count=$count reason=${rep.reason} unknown=${rep.unknown}');
  }

  void expectBad(String row, h.TableVocabulary vocab, Uint8List buf) {
    final value = h.Root();
    h.initRoot(value);
    final rep = h.TableReport();
    final count = r.rootLoadMessages(<h.Root>[value], buf, vocab, rep);
    check(count == 0 && rep.refused, '$row: count=$count reason=${rep.reason}');
  }

  // uint8 over 6 bits from base 200: 255 is the kind's edge, 256 is past it.
  final u8 = rangedVocab(6, <int>[1, 6, 0xc8, 0x01]);
  expectOk('skip_ranged_u8_at_edge', u8, batch(55, 0, 6));
  expectBad('skip_ranged_u8_past_kind', u8, batch(56, 0, 6));
  // int8 over 8 bits from base -100 (zigzag 199): 127 reads, 128 refuses.
  final i8 = rangedVocab(2, <int>[1, 8, 0xc7, 0x01]);
  expectOk('skip_ranged_i8_at_edge', i8, batch(227, 0, 8));
  expectBad('skip_ranged_i8_past_kind', i8, batch(228, 0, 8));
  // int128 over 8 bits from base 2^127 - 10: 2^127 - 1 reads, 2^127 refuses.
  final i128 = rangedVocab(18, <int>[1, 8, 0xf6, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f]);
  expectOk('skip_ranged_i128_at_edge', i128, batch(9, 0, 8));
  expectBad('skip_ranged_i128_past_kind', i128, batch(10, 0, 8));
  // uint128 over 128 bits from base 1: an offset of 2^128 - 2 reads
  // 2^128 - 1, and 2^128 - 1 carries past the kind.
  final u128 = rangedVocab(19, <int>[1, 0x80, 0x01, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  expectOk('skip_ranged_u128_at_edge', u128, batch(-2, -1, 128));
  expectBad('skip_ranged_u128_past_kind', u128, batch(-1, -1, 128));
`

// AN UNKNOWN KEY OF AN ENUM-KEYED ARRAY, and its value, are held to the text
// and grammar rules a placed key and value are held to (docs/SPEC-TABLES.md
// §16.3, §4): a key or a string that decodes to U+0000 refuses the read, and
// the same shapes without it read.
func TestDartSkippedKeyedJSONKey(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe

enum Color { Red, Green }

fixed table Root {
    counts [Color]uint8
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, packetFiles(t, u))
	wire := ""
	for name := range files {
		if strings.HasSuffix(name, "Wire.dart") && !strings.Contains(name, "/") {
			wire = name
		}
	}
	if wire == "" {
		t.Fatal("Generate emitted no wire library")
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	rows := []struct{ file, text, verdict string }{
		{"known.json", `{"counts":{"Red":1}}`, "read"},
		{"unknown.json", `{"counts":{"Blue":1}}`, "read"},
		{"unknown-key-zero.json", `{"counts":{"Bl\u0000ue":1}}`, "refused"},
		{"unknown-value-control.json", `{"counts":{"Blue":"ab"}}`, "read"},
		{"unknown-value-zero.json", `{"counts":{"Blue":"a\u0000"}}`, "refused"},
	}
	var args []string
	for _, row := range rows {
		if err := os.WriteFile(filepath.Join(dir, row.file), []byte(row.text), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, row.file)
	}
	probe := fmt.Sprintf(`import 'dart:io';

import '%s' as w;

void main(List<String> args) {
  for (final path in args) {
    final rep = w.TableTextReport();
    var verdict = 'refused';
    try {
      w.rootFromJson(File(path).readAsStringSync(), rep);
      if (!rep.refused) verdict = 'read';
    } on FormatException {
      verdict = 'refused';
    }
    stdout.writeln(path + '|' + verdict);
  }
}
`, wire)
	if err := os.WriteFile(filepath.Join(dir, "keyed_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "keyed_probe.dart"}, args...)...)
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
	for _, row := range rows {
		if got[row.file] != row.verdict {
			t.Errorf("%s: Dart answered %q, want %s", row.file, got[row.file], row.verdict)
		}
	}
}

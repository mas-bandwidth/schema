package darttable

// TestDartReadsRefuseNonCanonical pins the 2026-11-10 rule
// (docs/rulings.json reads-refuse-non-canonical) on the Dart leg: a variable
// form (form 1) or message form (form 2) input that decodes to a good value
// but that no conforming writer of a lawful lineage version would produce is
// refused by every read entry the leg has. The shared vectors and their
// manifest are testdata/wire/non-canonical; each read row reads and re-saves
// to its canonical bytes (the file itself, or the rewrite pinned under
// canonical/), and every other row is refused. A message row is also read
// under another build version, where only a union field written with arm
// reference 0 stays refused (§3.3). The verdict is the contract, so no
// assertion below compares a refusal's reason.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type nonCanonicalVector struct {
	file     string
	form     string
	spelling string
	verdict  string
}

// nonCanonicalManifest reads MANIFEST.tsv: file, form, case, verdict.
func nonCanonicalManifest(t *testing.T, path string) []nonCanonicalVector {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("non-canonical manifest: %v", err)
	}
	var rows []nonCanonicalVector
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("%s:%d: %d fields, want file, form, case, verdict", path, i+1, len(f))
		}
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
			continue
		}
		rows = append(rows, nonCanonicalVector{file: f[0], form: f[1], spelling: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no vector rows", path)
	}
	return rows
}

// nonCanonicalUnit answers the schema and announcement a manifest row belongs
// to. Rows under ../message-one-encoding/ are that fixture's, with its own
// unit and announcement; every other row is the non-canonical unit's.
func nonCanonicalUnit(repo, file string) (schema, announcement string) {
	if strings.HasPrefix(file, "../message-one-encoding/") {
		return filepath.Join(repo, "test", "message-one-encoding", "One.schema"),
			filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin")
	}
	return filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema"),
		filepath.Join(repo, "testdata", "wire", "non-canonical", "announcement.bin")
}

// nonCanonicalExpected is the canonical file a variable read row re-saves to:
// the vector itself, or the canonical rewrite the manifest's generator pins
// beside it for a lawful spelling no writer produces (docs/SPEC-TABLES.md §3).
func nonCanonicalExpected(t *testing.T, vector string, row nonCanonicalVector, data []byte) []byte {
	t.Helper()
	if row.form != "variable" {
		return data
	}
	path := filepath.Join(vector, "canonical", filepath.Base(row.file))
	if _, err := os.Stat(path); err != nil {
		if row.file != "variable-fields-out-of-order.bin" {
			return data
		}
		path = filepath.Join(vector, "variable-canonical.bin")
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return want
}

func nonCanonicalProbe(t *testing.T, u *ir.Unit, row nonCanonicalVector, data, announcement []byte) string {
	t.Helper()
	switch row.form {
	case "variable":
		root := "Root"
		base := fixedTableBase(u, root)
		return fmt.Sprintf(`import 'dart:typed_data';
import './%[1]sWire.dart' as w;
void check(bool ok, String what) { if (!ok) throw StateError(what); }
String hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();
void main() {
  final data = Uint8List.fromList(%[2]s);
  final r = w.TableWireReport();
  final v = w.%[3]sWireLoad(data, r);
  final m = w.%[3]sWireMeasure(data);
  final store = w.TableRetain(65536, List.filled(65536, 0));
  final rr = w.TableWireReport();
  w.%[3]sWireLoadRetain(data, store, rr);
  final saved = r.refused ? '' : hex(w.%[3]sWireSave(v));
  print('plain=${r.refused} retain=${rr.refused} measure=$m saved=$saved');
}
`, base, dartBytes(data), lowerFirst(root))
	case "message":
		root := "Message"
		if strings.HasPrefix(row.file, "../message-one-encoding/") {
			root = "One"
		}
		base := fixedTableBase(u, root)
		home := runtimeHome(u)
		alias := "r"
		imports := fmt.Sprintf("import './%sFixed.dart' as h;\n", home)
		if base != home {
			imports += fmt.Sprintf("import './%sFixed.dart' as r;\n", base)
		} else {
			alias = "h"
		}
		return fmt.Sprintf(`import 'dart:typed_data';
%[1]svoid check(bool ok, String what) { if (!ok) throw StateError(what); }
String hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();
void main() {
  final data = Uint8List.fromList(%[2]s);
  final ann = Uint8List.fromList(%[3]s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final ar = h.TableReport();
  check(h.announceRead(vocab, ann, ar), 'announcement: ' + ar.reason);
  final values = <h.%[4]s>[h.%[4]s()];
  h.init%[4]s(values[0]);
  final rp = h.TableReport();
  final n = %[5]s.%[6]sLoadMessages(values, data, vocab, rp);
  var saved = '';
  if (!rp.refused) {
    final measure = %[5]s.%[6]sMeasureMessages(values, h.TableReport());
    final out = Uint8List(measure);
    final sr = h.TableReport();
    final wrote = %[5]s.%[6]sSaveMessages(values, out, sr);
    if (wrote == measure) { saved = hex(out); }
  }
  // ANOTHER BUILD VERSION (docs/SPEC-TABLES.md §3.3): order and a field at
  // its default are lawful evolution there and read, and the rewrite reads
  // back under this build to the same bytes; a union field written with arm
  // reference 0 is refused at every build.
  vocab.buildVersion ^= 1;
  final other = <h.%[4]s>[h.%[4]s()];
  h.init%[4]s(other[0]);
  final op = h.TableReport();
  final on = %[5]s.%[6]sLoadMessages(other, data, vocab, op);
  var otherCanonical = false;
  if (!op.refused && on == 1) {
    final measure = %[5]s.%[6]sMeasureMessages(other, h.TableReport());
    final out = Uint8List(measure);
    if (%[5]s.%[6]sSaveMessages(other, out, h.TableReport()) == measure) {
      vocab.buildVersion ^= 1;
      final again = <h.%[4]s>[h.%[4]s()];
      h.init%[4]s(again[0]);
      final ap = h.TableReport();
      if (%[5]s.%[6]sLoadMessages(again, out, vocab, ap) == 1 && !ap.refused) {
        final twice = Uint8List(%[5]s.%[6]sMeasureMessages(again, h.TableReport()));
        %[5]s.%[6]sSaveMessages(again, twice, h.TableReport());
        otherCanonical = hex(twice) == hex(out);
      }
    }
  }
  print('count=$n refused=${rp.refused} saved=$saved othercount=$on otherrefused=${op.refused} othercanonical=$otherCanonical');
}
`, imports, dartBytes(data), dartBytes(announcement), root, alias, lowerFirst(root))
	case "announcement":
		home := runtimeHome(u)
		return fmt.Sprintf(`import 'dart:typed_data';
import './%[1]sFixed.dart' as h;
void main() {
  final data = Uint8List.fromList(%[2]s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final r = h.TableReport();
  final ok = h.announceRead(vocab, data, r);
  print('read=$ok refused=${r.refused}');
}
`, home, dartBytes(data))
	}
	t.Fatalf("%s: unknown form %q", row.file, row.form)
	return ""
}

func TestDartReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	repo := filepath.Join("..", "..", "..")
	vector := filepath.Join(repo, "testdata", "wire", "non-canonical")
	dartBin := dartBinary(t)

	units := map[string]*ir.Unit{}
	announcements := map[string][]byte{}
	for _, row := range nonCanonicalManifest(t, filepath.Join(vector, "MANIFEST.tsv")) {
		schema, announcement := nonCanonicalUnit(repo, row.file)
		u, ok := units[schema]
		if !ok {
			u = loadUnit(t, schema)
			units[schema] = u
		}
		ann, ok := announcements[announcement]
		if !ok {
			var err error
			ann, err = os.ReadFile(announcement)
			if err != nil {
				t.Fatalf("%s: announcement: %v", row.file, err)
			}
			announcements[announcement] = ann
		}
		data, err := os.ReadFile(filepath.Join(vector, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: Generate: %v", row.file, err)
		}
		packets, err := dart.Generate(u)
		if err != nil {
			t.Fatalf("%s: packet Generate: %v", row.file, err)
		}
		maps.Copy(files, packets)

		dir := t.TempDir()
		writeFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(nonCanonicalProbe(t, u, row, data, ann)), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBin, "--enable-asserts", "probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: probe did not run: %v\n%s", row.file, err, out)
		}
		line := strings.TrimSpace(string(out))
		fields := map[string]string{}
		for f := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(f, "=")
			if ok {
				fields[k] = v
			}
		}
		canonicalSave := ""
		if row.verdict == "read" && row.form != "announcement" {
			canonicalSave = fmt.Sprintf("%x", nonCanonicalExpected(t, vector, row, data))
		}
		switch row.form {
		case "variable":
			if fields["plain"] != boolWord(row.verdict == "refused") {
				t.Errorf("%s: plain load refused=%s, want %s: %s", row.file, fields["plain"], boolWord(row.verdict == "refused"), line)
			}
			if fields["retain"] != boolWord(row.verdict == "refused") {
				t.Errorf("%s: retaining load refused=%s, want %s: %s", row.file, fields["retain"], boolWord(row.verdict == "refused"), line)
			}
			// The measure sizes a region before a load and answers no
			// verdict on a good-value spelling: like the reference
			// tablewire.FileRegionMeasure it reads the trailer and the
			// framing only, so a wire the load refuses for its content (an
			// explicit null pointer, an unreached record) still measures.
			// It is held to a positive extent on the canonical file.
			measure, _ := strconv.Atoi(fields["measure"])
			if row.verdict == "read" && measure <= 0 {
				t.Errorf("%s: measure=%d, want a positive extent: %s", row.file, measure, line)
			}
			if row.verdict == "read" && fields["saved"] != canonicalSave {
				t.Errorf("%s: re-save = %s, want %s", row.file, fields["saved"], canonicalSave)
			}
		case "message":
			refused := row.verdict == "refused"
			gotRefused := fields["refused"] == "true" || fields["count"] == "0" || fields["count"] == "-1"
			if gotRefused != refused {
				t.Errorf("%s: message load refused=%t, want %t: %s", row.file, gotRefused, refused, line)
			}
			if !refused && fields["saved"] != canonicalSave {
				t.Errorf("%s: re-save = %s, want %s", row.file, fields["saved"], canonicalSave)
			}
			if row.spelling == "union-arm-zero" {
				if fields["otherrefused"] != "true" && fields["othercount"] != "0" && fields["othercount"] != "-1" {
					t.Errorf("%s: another build read a union field written with arm reference 0: %s", row.file, line)
				}
			} else if fields["otherrefused"] != "false" || fields["othercount"] != "1" || fields["othercanonical"] != "true" {
				t.Errorf("%s: another build: want read with a canonical rewrite: %s", row.file, line)
			}
		case "announcement":
			if fields["read"] != "true" || fields["refused"] != "false" {
				t.Errorf("%s: announcement read=%s refused=%s, want read: %s", row.file, fields["read"], fields["refused"], line)
			}
		}
	}
	testDartUnionNoneElementReads(t, dartBin)
	testDartNodeTableOutOfPlaceAlone(t, dartBin)
	testDartMessageUndeclaredField(t, dartBin)
}

// testDartMessageUndeclaredField holds §3.3 (b)'s third same-build refusal:
// under the reader's own build version every field the sender's record
// declares is one this record declares, so a field it does not declare is
// refused; under another build version it is a newer writer's field, skipped
// and counted. The batch is a wider record's, announced once under this
// reader's build version and once under another. The Go engine is the oracle.
func testDartMessageUndeclaredField(t *testing.T, dartBin string) {
	t.Helper()
	const reader = `package undeclared

fixed table Root
{
    tick uint8
}
`
	const writer = `package undeclared

fixed table Root
{
    tick  uint8
    extra uint8
}
`
	u := unitFrom(t, reader)
	w := unitFrom(t, writer)
	wm := tabletext.NewModel(w)
	value := wm.New(wm.Lookup("Root"))
	var report tabletext.Report
	if !wm.Read(value, []byte(`{"tick": 1, "extra": 2}`), &report) {
		t.Fatalf("undeclared field: value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(wm, value)
	if err != nil {
		t.Fatal(err)
	}
	vocab := ir.TableVocabularyBytes(tablewire.MessageVocabulary(w))
	own := ir.BuildVersion(u)
	anns := [][]byte{craftAnnouncement(vocab, own, true, true), craftAnnouncement(vocab, own^1, true, true)}
	m := tabletext.NewModel(u)
	for i, ann := range anns {
		var v tablewire.Vocabulary
		if err := v.AnnounceRead(ann, &tabletext.Report{}); err != nil {
			t.Fatalf("undeclared field: engine announcement %d: %v", i, err)
		}
		inst := m.New(m.Lookup("Root"))
		ok, err := tablewire.DecodeMessage(m, inst, batch, &v, &tabletext.Report{})
		if engineRefused := !ok || err != nil; engineRefused != (i == 0) {
			t.Fatalf("undeclared field: engine refused=%t under announcement %d", engineRefused, i)
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("undeclared field: Generate: %v", err)
	}
	packets, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("undeclared field: packet Generate: %v", err)
	}
	maps.Copy(files, packets)
	base := fixedTableBase(u, "Root")
	home := runtimeHome(u)
	imports := fmt.Sprintf("import './%sFixed.dart' as h;\n", home)
	alias := "h"
	if base != home {
		imports += fmt.Sprintf("import './%sFixed.dart' as r;\n", base)
		alias = "r"
	}
	probe := fmt.Sprintf(`import 'dart:typed_data';
%[1]svoid main() {
  final data = Uint8List.fromList(%[2]s);
  for (final ann in [Uint8List.fromList(%[3]s), Uint8List.fromList(%[4]s)]) {
    final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
    if (!h.announceRead(vocab, ann, h.TableReport())) { print('announcement refused'); return; }
    final values = <h.Root>[h.Root()];
    h.initRoot(values[0]);
    final rp = h.TableReport();
    final n = %[5]s.rootLoadMessages(values, data, vocab, rp);
    print('count=$n refused=${rp.refused} tick=${values[0].tick}');
  }
}
`, imports, dartBytes(batch), dartBytes(anns[0]), dartBytes(anns[1]), alias)
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("undeclared field: probe did not run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "refused=true") || lines[1] != "count=1 refused=false tick=1" {
		t.Errorf("undeclared field: got\n%s\nwant the own-build batch refused, then count=1 refused=false tick=1", out)
	}
}

// testDartNodeTableOutOfPlaceAlone holds the node table to the root body's
// last field (§3.1) on its own: the shared vector's id table is also out of
// first-use order, so it would be refused without the placement rule. Here a
// root field rides after the node table and the id table is in the first-use
// order of that very wire (the root body, then the node records), so only the
// placement is wrong. The Go engine is the oracle for both files.
func testDartNodeTableOutOfPlaceAlone(t *testing.T, dartBin string) {
	t.Helper()
	const schema = `package nodeplace

table Leaf
{
    x uint8
}

table Root
{
    first  *Leaf
    second *Leaf
}
`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"first": {"x": 1}, "second": {"x": 2}}`), &report) {
		t.Fatalf("node table placement: value: %+v", report)
	}
	canonical, err := tablewire.EncodeVariable(m, value)
	if err != nil {
		t.Fatal(err)
	}
	moved := nodeTableMovedBeforeLast(t, canonical)
	for _, c := range []struct {
		data    []byte
		refused bool
	}{{canonical, false}, {moved, true}} {
		inst := m.New(m.Lookup("Root"))
		ok, err := tablewire.DecodeVariable(m, inst, c.data, &tabletext.Report{})
		if engineRefused := !ok || err != nil; engineRefused != c.refused {
			t.Fatalf("node table placement: engine refused=%t, want %t (%x)", engineRefused, c.refused, c.data)
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("node table placement: Generate: %v", err)
	}
	probe := fmt.Sprintf(`import 'dart:typed_data';
import './%[1]sWire.dart' as w;
void main() {
  for (final data in [Uint8List.fromList(%[2]s), Uint8List.fromList(%[3]s)]) {
    final r = w.TableWireReport();
    w.rootWireLoad(data, r);
    final store = w.TableRetain(65536, List.filled(65536, 0));
    final rr = w.TableWireReport();
    w.rootWireLoadRetain(data, store, rr);
    print('plain=${r.refused} retain=${rr.refused}');
  }
}
`, runtimeHome(u), dartBytes(canonical), dartBytes(moved))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node table placement: probe did not run: %v\n%s", err, out)
	}
	const want = "plain=false retain=false\nplain=true retain=true"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("node table placement: got\n%s\nwant\n%s", got, want)
	}
}

// nodeTableMovedBeforeLast respells a canonical variable file of the
// nodeplace unit, [first][second][node table][0] with ids first, second,
// node, ..., as [first][node table][second][0] with ids first, node, second,
// ...: the id table stays in the first-use order of the new wire, and only
// the node table's place moves. The root's fields are kind 17 (an LEB node
// index) and the node table kind 12 (an LEB length and its payload).
func nodeTableMovedBeforeLast(t *testing.T, file []byte) []byte {
	t.Helper()
	leb := func(off int) (uint64, int) {
		var v uint64
		for shift := 0; off < len(file); shift += 7 {
			b := file[off]
			off++
			v |= uint64(b&0x7f) << shift
			if b < 0x80 {
				return v, off
			}
		}
		t.Fatalf("node table placement: LEB past the file (%x)", file)
		return 0, 0
	}
	type field struct {
		ref  uint64
		rest []byte // the kind byte and its payload
	}
	var fields []field
	off := 1
	for {
		ref, next := leb(off)
		if ref == 0 {
			off = next
			break
		}
		start := next
		kind := file[start]
		v, end := leb(start + 1)
		if kind == 12 {
			end += int(v)
		} else if kind != 17 {
			t.Fatalf("node table placement: root field kind %d", kind)
		}
		fields = append(fields, field{ref: ref, rest: file[start:end]})
		off = end
	}
	if len(fields) != 3 || fields[0].ref != 1 || fields[1].ref != 2 || fields[2].ref != 3 || fields[2].rest[0] != 12 {
		t.Fatalf("node table placement: canonical root is not [first][second][node table] (%x)", file)
	}
	ids := file[off:]
	out := []byte{file[0], 1}
	out = append(out, fields[0].rest...)
	out = append(out, 2)
	out = append(out, fields[2].rest...)
	out = append(out, 3)
	out = append(out, fields[1].rest...)
	out = append(out, 0)
	out = append(out, ids[0:8]...)
	out = append(out, ids[16:24]...)
	out = append(out, ids[8:16]...)
	out = append(out, ids[24:]...)
	return out
}

// testDartUnionNoneElementReads is the control for §3.3 (c): the refusal is
// of a union FIELD written with arm reference 0, never of a None element of
// an array of unions, which is positional. The writer's batch carrying one
// reads, and re-saves to the same bytes, under this reader's build version
// and under another.
func testDartUnionNoneElementReads(t *testing.T, dartBin string) {
	t.Helper()
	const schema = `package messagenone

union Choice
{
    item uint8
}

fixed table Root
{
    tick    uint8
    choice  Choice
    choices [..2]Choice
}
`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("union None element: value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("union None element: Generate: %v", err)
	}
	packets, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("union None element: packet Generate: %v", err)
	}
	maps.Copy(files, packets)
	base := fixedTableBase(u, "Root")
	home := runtimeHome(u)
	imports := fmt.Sprintf("import './%sFixed.dart' as h;\n", home)
	alias := "h"
	if base != home {
		imports += fmt.Sprintf("import './%sFixed.dart' as r;\n", base)
		alias = "r"
	}
	probe := fmt.Sprintf(`import 'dart:typed_data';
%[1]sString hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();
void main() {
  final data = Uint8List.fromList(%[2]s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  if (!h.announceRead(vocab, h.tableAnnouncement, h.TableReport())) { print('announcement refused'); return; }
  for (var build = 0; build < 2; build++) {
    final values = <h.Root>[h.Root()];
    h.initRoot(values[0]);
    final rp = h.TableReport();
    final n = %[3]s.rootLoadMessages(values, data, vocab, rp);
    var saved = '';
    if (!rp.refused && n == 1) {
      final out = Uint8List(%[3]s.rootMeasureMessages(values, h.TableReport()));
      %[3]s.rootSaveMessages(values, out, h.TableReport());
      saved = hex(out);
    }
    print('build=$build count=$n refused=${rp.refused} saved=$saved');
    vocab.buildVersion ^= 1;
  }
}
`, imports, dartBytes(batch), alias)
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("union None element: probe did not run: %v\n%s", err, out)
	}
	want := fmt.Sprintf("build=0 count=1 refused=false saved=%[1]x\nbuild=1 count=1 refused=false saved=%[1]x", batch)
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("union None element: got\n%s\nwant\n%s", got, want)
	}
}

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

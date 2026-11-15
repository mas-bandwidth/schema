package darttable

// TestDartReadsRefuseNonCanonical pins the 2026-11-10 rule
// (docs/rulings.json reads-refuse-non-canonical) on the Dart leg: a variable
// form (form 1) or message form (form 2) input that decodes to a good value
// but that no conforming writer of a lawful lineage version would produce is
// refused by every read entry the leg has. The shared vectors and their
// manifest are testdata/wire/non-canonical; each canonical file reads and
// re-saves to the same bytes, and every other file is refused. The verdict is
// the contract, so no assertion below compares a refusal's reason.

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
  print('count=$n refused=${rp.refused} saved=$saved');
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
		row := row
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
		for _, f := range strings.Fields(line) {
			k, v, ok := strings.Cut(f, "=")
			if ok {
				fields[k] = v
			}
		}
		canonicalSave := ""
		if row.verdict == "read" && row.form != "announcement" {
			canonicalSave = fmt.Sprintf("%x", data)
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
		case "announcement":
			if fields["read"] != "true" || fields["refused"] != "false" {
				t.Errorf("%s: announcement read=%s refused=%s, want read: %s", row.file, fields["read"], fields["refused"], line)
			}
		}
	}
}

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

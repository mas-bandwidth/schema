package darttable

// THE MESSAGE FORM'S ANNOUNCEMENT ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
// The message form is a batch of bitpacked bodies under one announced
// vocabulary, and the announcement is an ordinary form-`1` file the compiler
// settles byte for byte. The card names three clauses: the constant
// announcement and its measure, the read that fills a `TableVocabulary` keeping
// the build version, and the refusals the page names.
//
// The bytes are the C++ reference's, pinned by testdata/conformance/tables/
// MANIFEST.txt's `connection` rows. Each subtest's assertion QUOTES the sentence
// of §3.3 it implements, drives the emitted Dart with the leg's own toolchain,
// and compares against the manifest — never against a substring of the source.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// announceConnection is one `connection` row of the manifest: the announcement
// wire file and the unit's build version, both the reference's.
type announceConnection struct {
	bytes        []byte
	buildVersion uint64
}

// announceConnections reads testdata/conformance/tables/MANIFEST.txt's
// `connection` rows. The row is `connection <key> <unit> <build version>
// <announcement wire file>`, and this test keys it by unit so a unit's own file
// is the oracle for its own announcement.
func announceConnections(t *testing.T) map[string]announceConnection {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/tables/MANIFEST.txt")
	if err != nil {
		t.Fatalf("the manifest is not readable: %v", err)
	}
	out := map[string]announceConnection{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] != "connection" {
			continue
		}
		build, err := strconv.ParseUint(strings.TrimPrefix(fields[3], "0x"), 16, 64)
		if err != nil {
			t.Fatalf("the manifest's build version %q for %s is unreadable: %v", fields[3], fields[2], err)
		}
		wire, err := os.ReadFile(filepath.Join("../../..", fields[4]))
		if err != nil {
			t.Fatalf("the manifest's announcement file %s is unreadable: %v", fields[4], err)
		}
		out[fields[2]] = announceConnection{bytes: wire, buildVersion: build}
	}
	if len(out) == 0 {
		t.Fatal("the manifest carries no connection row")
	}
	return out
}

// TestMessageFormAnnounceDart asserts, one subtest per clause of STEP 2, the
// announcement surface the Dart leg owes §3.3.
func TestMessageFormAnnounceDart(t *testing.T) {
	t.Parallel()
	conns := announceConnections(t)
	units := []struct{ unit, path string }{
		{"backenddemo", "../../../tables/backend/Backend.schema"},
		{"vocabdemo", "../../../tables/vocab/Vocab.schema"},
		{"vocab9demo", "../../../tables/vocab9/Vocab9.schema"},
	}

	// 2.1 "Announce and AnnounceMeasure are a compile-time constant byte array
	// and its length, equal byte for byte to the manifest's announcement file."
	t.Run("announcement_constant", func(t *testing.T) {
		t.Parallel()
		for _, un := range units {
			u := loadUnit(t, un.path)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("%s generate: %v", un.unit, err)
			}
			got, gotLen := emittedAnnouncement(t, files)
			conn, ok := conns[un.unit]
			if !ok {
				t.Fatalf("the manifest carries no connection row for %s", un.unit)
			}
			if !bytes.Equal(got, conn.bytes) {
				t.Fatalf("%s: the emitted announcement is not the manifest's file byte for byte", un.unit)
			}
			if gotLen != len(conn.bytes) {
				t.Fatalf("%s: AnnounceMeasure emits %d, the manifest's file is %d bytes", un.unit, gotLen, len(conn.bytes))
			}
		}
	})

	// 2.2 "AnnounceRead accepts each of those files and fills TableVocabulary
	// with the entries the page names, the build version kept."
	t.Run("announce_read", func(t *testing.T) {
		t.Parallel()
		for _, un := range units {
			u := loadUnit(t, un.path)
			conn := conns[un.unit]
			vocabulary := ir.TableVocabulary(u)
			pairs := make([]string, 0, len(vocabulary))
			for _, e := range vocabulary {
				pairs = append(pairs, fmt.Sprintf("[%s,%d]", dartInt(int64(e.Id)), int(e.Kind)))
			}
			body := fmt.Sprintf(`
  final valid = Uint8List.fromList(%s);
  final v = t.TableVocabulary(entries: List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0)));
  final r = t.TableReport();
  check(t.AnnounceRead(v, valid, r),
      'AnnounceRead refused the manifest announcement: %s');
  check(v.announced, 'the accepted vocabulary is not marked announced');
  check(v.count == %d, 'count=${v.count} want=%d');
  check(v.BuildVersion == %s, 'build=${v.BuildVersion} want=%s');
  final want = <List<int>>[%s];
  for (var i = 0; i < want.length; i++) {
    check(v.entries[i].id == want[i][0] && v.entries[i].kind == want[i][1],
        'entry $i is ${v.entries[i].id}/${v.entries[i].kind} want ${want[i]}');
  }
`, dartBytes(conn.bytes), "${r.reason} malformed=${r.malformed}",
				len(vocabulary), len(vocabulary), dartInt(int64(conn.buildVersion)), dartInt(int64(conn.buildVersion)),
				strings.Join(pairs, ","))
			out, err := runAnnounceProbe(t, u, body)
			if err != nil {
				t.Fatalf("%s: AnnounceRead on the manifest's file: %v\n%s", un.unit, err, out)
			}
		}
	})

	// 2.3 "the refusals second_announcement (and refusal is terminal) and
	// vocabulary_too_large (both bounds), and the two strict checks answered
	// `malformed`."
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		u := loadUnit(t, units[0].path)
		conn := conns[units[0].unit]
		vocabBytes := ir.TableVocabularyBytes(ir.TableVocabulary(u))
		noVersion := craftAnnouncement(vocabBytes, ir.BuildVersion(u), false, true)
		noVocab := craftAnnouncement(vocabBytes, ir.BuildVersion(u), true, false)
		body := fmt.Sprintf(`
  final valid = Uint8List.fromList(%s);
  List<t.TableMessageEntry> storage() =>
      List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0));
  // second_announcement, and refusal is terminal
  final v1 = t.TableVocabulary(entries: storage());
  final r1 = t.TableReport();
  check(t.AnnounceRead(v1, valid, r1), 'first announcement refused: %s');
  check(!t.AnnounceRead(v1, valid, r1), 'a second announcement is not refused');
  check(r1.refused && r1.reason == 'second_announcement', 'second reason=${r1.reason}');
  check(!t.AnnounceRead(v1, valid, r1), 'a third announcement is not refused (terminal)');
  check(r1.reason == 'second_announcement', 'terminal reason=${r1.reason}');
  // vocabulary_too_large, both bounds
  final v2 = t.TableVocabulary(entries: storage(), maxEntries: 0);
  final r2 = t.TableReport();
  check(!t.AnnounceRead(v2, valid, r2), 'the entry-count bound is not refused');
  check(r2.refused && r2.reason == 'vocabulary_too_large', 'entry bound reason=${r2.reason}');
  final v3 = t.TableVocabulary(entries: storage(), maxBytes: 1);
  final r3 = t.TableReport();
  check(!t.AnnounceRead(v3, valid, r3), 'the byte bound is not refused');
  check(r3.refused && r3.reason == 'vocabulary_too_large', 'byte bound reason=${r3.reason}');
  // the two strict checks, answered malformed
  final v4 = t.TableVocabulary(entries: storage());
  final r4 = t.TableReport();
  check(!t.AnnounceRead(v4, Uint8List.fromList(%s), r4), 'a body with no build-version is not refused');
  check(!r4.refused && r4.malformed, 'no build-version: malformed=${r4.malformed} refused=${r4.refused}');
  final v5 = t.TableVocabulary(entries: storage());
  final r5 = t.TableReport();
  check(!t.AnnounceRead(v5, Uint8List.fromList(%s), r5), 'a body with no vocabulary is not refused');
  check(!r5.refused && r5.malformed, 'no vocabulary: malformed=${r5.malformed} refused=${r5.refused}');
`, dartBytes(conn.bytes), "${r1.reason} malformed=${r1.malformed}",
			dartBytes(noVersion), dartBytes(noVocab))
		out, err := runAnnounceProbe(t, u, body)
		if err != nil {
			t.Fatalf("the announcement refusals: %v\n%s", err, out)
		}
	})
}

// emittedAnnouncement reads the emitted announcement constant back off the
// generated Dart: the byte array and the length constant beside it. That is the
// emitted surface, not the source text a substring test would settle for.
func emittedAnnouncement(t *testing.T, files map[string][]byte) ([]byte, int) {
	t.Helper()
	var src string
	for _, b := range files {
		src += string(b)
	}
	const marker = "final Uint8List tableAnnouncement = Uint8List.fromList(const <int>["
	i := strings.Index(src, marker)
	if i < 0 {
		t.Fatal("the generated unit carries no announcement constant")
	}
	rest := src[i+len(marker):]
	j := strings.Index(rest, "]")
	if j < 0 {
		t.Fatal("the announcement constant is unterminated")
	}
	var out []byte
	for f := range strings.FieldsSeq(strings.NewReplacer(",", " ", "\n", " ").Replace(rest[:j])) {
		v, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("unreadable announcement byte %q: %v", f, err)
		}
		out = append(out, byte(v))
	}
	const lengthMarker = "const int tableAnnouncementLength = "
	k := strings.Index(src, lengthMarker)
	if k < 0 {
		t.Fatal("the generated unit carries no announcement length")
	}
	field := strings.Fields(src[k+len(lengthMarker):])
	if len(field) == 0 {
		t.Fatal("the announcement length constant carries no value")
	}
	n, err := strconv.Atoi(strings.TrimSuffix(field[0], ";"))
	if err != nil {
		t.Fatalf("the announcement length %q is unreadable: %v", field[0], err)
	}
	return out, n
}

// craftAnnouncement builds a minimal form-`1` announcement that carries exactly
// the fields asked for: the build-version field and the vocabulary field, each
// optional, over the given vocabulary payload. It is how the strict checks are
// driven, one field missing at a time.
func craftAnnouncement(vocab []byte, build uint64, withVersion, withVocabulary bool) []byte {
	body := []byte{}
	var ids []uint64
	next := byte(1)
	if withVersion {
		body = append(body, next, 9)
		body = binary.LittleEndian.AppendUint64(body, build)
		ids = append(ids, ir.TableBuildVersionWireId)
		next++
	}
	if withVocabulary {
		field := []byte{6}
		field = append(field, lebBytes(uint64(len(vocab)))...)
		field = append(field, vocab...)
		body = append(body, next, 14)
		body = append(body, lebBytes(uint64(len(field)))...)
		body = append(body, field...)
		ids = append(ids, ir.TableMessageVocabularyWireId)
	}
	body = append(body, 0)
	out := []byte{1}
	out = append(out, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func lebBytes(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func dartBytes(b []byte) string {
	parts := make([]string, 0, len(b))
	for _, v := range b {
		parts = append(parts, fmt.Sprintf("0x%02x", v))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func dartInt(v int64) string { return strconv.FormatInt(v, 10) }

// announceDartBinary is the leg's own Dart: the DART the Makefile passes, then
// the unpacked SDK, then the PATH. A missing toolchain is a skip, never a green.
func announceDartBinary(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("DART"); env != "" {
		return env
	}
	if dart, err := filepath.Abs("../../../dist/dart-sdk-3.13.2/bin/dart"); err == nil {
		if _, err := os.Stat(dart); err == nil {
			return dart
		}
	}
	if dart, err := exec.LookPath("dart"); err == nil {
		return dart
	}
	t.Skip("no Dart toolchain: set DART, unpack dist/dart-sdk-3.13.2, or put dart on the PATH")
	return ""
}

// runAnnounceProbe writes the unit's emitted announcement surface — the
// announcement runtime and the constant the fixed module lands in its runtime
// home, verbatim from this package's emitters — beside a generated probe and
// runs it with the leg's own toolchain. The surface is executed on its own so a
// defect in an unrelated form does not decide whether the announcement compiles.
func runAnnounceProbe(t *testing.T, u *ir.Unit, body string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	lib := "import 'dart:typed_data';\n\n" + dartMessageAnnounceRuntime + "\n" + dartTableAnnouncementConstant(u)
	if err := os.WriteFile(filepath.Join(dir, "announce.dart"), []byte(lib), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`import 'dart:typed_data';

import './announce.dart' as t;

void check(bool ok, String what) {
  if (!ok) {
    throw StateError(what);
  }
}

void main() {
%s
  print('ok');
}
`, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(announceDartBinary(t), "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

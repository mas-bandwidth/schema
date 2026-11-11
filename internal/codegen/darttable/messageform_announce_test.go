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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
			// The clause names the two FUNCTIONS, so the assertion runs them:
			// AnnounceMeasure is the emitted length and Announce copies the
			// manifest's bytes into the caller's buffer, refusing a short one.
			body := fmt.Sprintf(`
  final want = Uint8List.fromList(%s);
  final measured = t.announceMeasure();
  check(measured == want.length, 'AnnounceMeasure=$measured want=${want.length}');
  final buffer = Uint8List(want.length);
  final wrote = t.announce(buffer);
  check(wrote == want.length, 'Announce=$wrote want=${want.length}');
  var same = true;
  for (var i = 0; i < want.length; i++) { if (buffer[i] != want[i]) { same = false; } }
  check(same, 'Announce did not copy the manifest file');
  check(t.announce(Uint8List(want.length - 1)) == -1, 'a short buffer is not refused');
`, dartBytes(conn.bytes))
			if out, err := runAnnounceProbe(t, u, body); err != nil {
				t.Fatalf("%s: Announce/AnnounceMeasure on the manifest's file: %v\n%s", un.unit, err, out)
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
			vocabulary := tablewire.MessageVocabulary(u)
			pairs := make([]string, 0, len(vocabulary))
			for _, e := range vocabulary {
				pairs = append(pairs, fmt.Sprintf("[%s,%d]", dartInt(int64(e.Id)), int(e.Kind)))
			}
			body := fmt.Sprintf(`
  final valid = Uint8List.fromList(%s);
  final v = t.TableVocabulary(entries: List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0)));
  final r = t.TableReport();
  check(t.announceRead(v, valid, r),
      'AnnounceRead refused the manifest announcement: %s');
  check(v.announced, 'the accepted vocabulary is not marked announced');
  check(v.count == %d, 'count=${v.count} want=%d');
  check(v.BuildVersion == %s, 'build=${v.BuildVersion} want=%s');
  final want = <List<int>>[%s];
  for (var i = 0; i < want.length; i++) {
    check(v.entries[i].id == want[i][0] && v.entries[i].kind == want[i][1],
        'entry $i is ${v.entries[i].id}/${v.entries[i].kind} want ${want[i]}');
  }
`, dartBytes(conn.bytes), "${r.reason} refused=${r.refused}",
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
		vocabBytes := ir.TableVocabularyBytes(tablewire.MessageVocabulary(u))
		vocabCount := len(tablewire.MessageVocabulary(u))
		noVersion := craftAnnouncement(vocabBytes, ir.BuildVersion(u), false, true)
		noVocab := craftAnnouncement(vocabBytes, ir.BuildVersion(u), true, false)
		// A later minor's added field: an id the vocabulary does not name,
		// carried at an ordinary kind. §3.3: "Every other field of its body is
		// ordinary and tolerant, so an unknown one is skipped and counted and
		// the announcement can GAIN a field in a later minor without a lockstep
		// redeploy."
		withUnknown := craftAnnouncementWithUnknown(vocabBytes, ir.BuildVersion(u), 0x0123456789abcdef, 1, []byte{0x00})
		// "THE TWO RESERVED IDS OF THE ANNOUNCEMENT ITSELF ARE NOT IN THE
		// VOCABULARY" (§3.3): an entry carrying one is malformed.
		reservedEntry := binary.LittleEndian.AppendUint64(nil, ir.TableBuildVersionWireId)
		reservedEntry = append(reservedEntry, 1)
		reservedVocab := craftAnnouncement(reservedEntry, ir.BuildVersion(u), true, true)
		// A repeated entry is malformed: two identical entries disagree on one
		// name's shape, which §3.3's "one NAME can appear at two shapes" allows
		// only when the triples differ.
		dupEntry := binary.LittleEndian.AppendUint64(nil, 0x0123456789abcdef)
		dupEntry = append(dupEntry, 1)
		dupVocab := append(append([]byte{}, dupEntry...), dupEntry...)
		dupAnnounce := craftAnnouncement(dupVocab, ir.BuildVersion(u), true, true)
		body := fmt.Sprintf(`
  final valid = Uint8List.fromList(%s);
  List<t.TableMessageEntry> storage() =>
      List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0));
  // second_announcement, and refusal is terminal
  final v1 = t.TableVocabulary(entries: storage());
  final r1 = t.TableReport();
  check(t.announceRead(v1, valid, r1), 'first announcement refused: %s');
  check(!t.announceRead(v1, valid, r1), 'a second announcement is not refused');
  check(r1.refused && r1.reason == 'second_announcement', 'second reason=${r1.reason}');
  check(!t.announceRead(v1, valid, r1), 'a third announcement is not refused (terminal)');
  check(r1.reason == 'second_announcement', 'terminal reason=${r1.reason}');
  // vocabulary_too_large, both bounds
  final v2 = t.TableVocabulary(entries: storage(), maxEntries: 0);
  final r2 = t.TableReport();
  check(!t.announceRead(v2, valid, r2), 'the entry-count bound is not refused');
  check(r2.refused && r2.reason == 'vocabulary_too_large', 'entry bound reason=${r2.reason}');
  final v3 = t.TableVocabulary(entries: storage(), maxBytes: 1);
  final r3 = t.TableReport();
  check(!t.announceRead(v3, valid, r3), 'the byte bound is not refused');
  check(r3.refused && r3.reason == 'vocabulary_too_large', 'byte bound reason=${r3.reason}');
  // the two strict checks, answered malformed
  final v4 = t.TableVocabulary(entries: storage());
  final r4 = t.TableReport();
  check(!t.announceRead(v4, Uint8List.fromList(%s), r4), 'a body with no build-version is not refused');
  check(r4.refused && r4.reason == 'wire_damaged', 'no build-version: reason=${r4.reason} refused=${r4.refused}');
  final v5 = t.TableVocabulary(entries: storage());
  final r5 = t.TableReport();
  check(!t.announceRead(v5, Uint8List.fromList(%s), r5), 'a body with no vocabulary is not refused');
  check(r5.refused && r5.reason == 'wire_damaged', 'no vocabulary: reason=${r5.reason} refused=${r5.refused}');
  // an unknown field is skipped and counted, never malformed: only the two
  // strict checks damage, so a later minor's added field is tolerated.
  final v6 = t.TableVocabulary(entries: storage());
  final r6 = t.TableReport();
  check(t.announceRead(v6, Uint8List.fromList(%s), r6),
      'an unknown field is not tolerated: %s');
  check(v6.announced && !r6.refused,
      'unknown field: reason=${r6.reason} refused=${r6.refused}');
  check(r6.unknown == 1, 'unknown=${r6.unknown} want=1');
  check(v6.count == %d, 'count=${v6.count} want=%d');
  // a reserved id in the vocabulary is malformed, not a refusal
  final v7 = t.TableVocabulary(entries: storage());
  final r7 = t.TableReport();
  check(!t.announceRead(v7, Uint8List.fromList(%s), r7), 'a reserved-id entry is not refused');
  check(r7.refused && r7.reason == 'wire_damaged', 'reserved entry: reason=${r7.reason} refused=${r7.refused}');
  // a repeated entry is malformed
  final v8 = t.TableVocabulary(entries: storage());
  final r8 = t.TableReport();
  check(!t.announceRead(v8, Uint8List.fromList(%s), r8), 'a repeated entry is not refused');
  check(r8.refused && r8.reason == 'wire_damaged', 'repeated entry: reason=${r8.reason} refused=${r8.refused}');
`, dartBytes(conn.bytes), "${r1.reason} refused=${r1.refused}",
			dartBytes(noVersion), dartBytes(noVocab),
			dartBytes(withUnknown), "${r6.reason} refused=${r6.refused} unknown=${r6.unknown} count=${v6.count}",
			vocabCount, vocabCount,
			dartBytes(reservedVocab), dartBytes(dupAnnounce))
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
	_, after, ok := strings.Cut(src, marker)
	if !ok {
		t.Fatal("the generated unit carries no announcement constant")
	}
	rest := after
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
	_, after0, ok0 := strings.Cut(src, lengthMarker)
	if !ok0 {
		t.Fatal("the generated unit carries no announcement length")
	}
	field := strings.Fields(after0)
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

// craftAnnouncementWithUnknown builds a form-`1` announcement carrying the two
// required fields AND one further field under an id the vocabulary does not
// name, so the tolerant body walk is driven: §3.3 "Every other field of its
// body is ordinary and tolerant, so an unknown one is skipped and counted".
func craftAnnouncementWithUnknown(vocab []byte, build, unknownId uint64, kind byte, payload []byte) []byte {
	body := []byte{}
	var ids []uint64
	body = append(body, 1, 9)
	body = binary.LittleEndian.AppendUint64(body, build)
	ids = append(ids, ir.TableBuildVersionWireId)
	field := []byte{6}
	field = append(field, lebBytes(uint64(len(vocab)))...)
	field = append(field, vocab...)
	body = append(body, 2, 14)
	body = append(body, lebBytes(uint64(len(field)))...)
	body = append(body, field...)
	ids = append(ids, ir.TableMessageVocabularyWireId)
	body = append(body, 3, kind)
	body = append(body, payload...)
	ids = append(ids, unknownId)
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
// the unpacked SDK, then the PATH. When SCHEMA_REQUIRE_CORPUS is set, a missing
// toolchain fails the test rather than skipping.
func announceDartBinary(t *testing.T) string {
	slowtest.Gate(t, "an external toolchain")

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
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal("SCHEMA_REQUIRE_CORPUS is set and no Dart toolchain was found (DART, dist/dart-sdk-3.13.2/bin/dart, or PATH)")
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
	return runAnnounceProbeWithRuntime(t, u, dartMessageAnnounceRuntime, body)
}

func runAnnounceProbeWithRuntime(t *testing.T, u *ir.Unit, runtimeSrc, body string) (string, error) {
	slowtest.Gate(t, "an external toolchain")

	t.Helper()
	dir := t.TempDir()
	lib := "import 'dart:typed_data';\n\n" + runtimeSrc + "\n" + dartTableAnnouncementConstant(u)
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

// TestDartAnnounceIdTableCount checks that announceRead enforces §3 and §3.3 on
// the trailer's id count and distinctness, matching tablewire.Vocabulary.AnnounceRead.
func TestDartAnnounceIdTableCount(t *testing.T) {
	t.Parallel()
	out, err := runDartAnnounceIdTableCount(t, dartMessageAnnounceRuntime)
	if err != nil {
		t.Fatalf("announceRead id-table count checks failed: %v\n%s", err, out)
	}
}

// TestDartAnnounceIdTableCountNegativeControl removes the trailer-count guards
// from the emitted source in memory and requires TestDartAnnounceIdTableCount's
// probe to go red.
func TestDartAnnounceIdTableCountNegativeControl(t *testing.T) {
	t.Parallel()
	const fullCountCheck = `  if (trailerCount < 0 ||
      trailerCount > 4096 ||
      trailerCount > (bytes.length - 9) ~/ 8) {
    report.Damage();
    return false;
  }`
	const endCheck = `  if (end < 1) {
    report.Damage();
    return false;
  }`
	const indexedLoop = `  for (var k = 0; k < trailerCount; k++) {
    final id = view.getUint64(end + k * 8, Endian.little);`
	const offsetLoop = `  for (var i = end; i < end + trailerCount * 8; i += 8) {
    final id = view.getUint64(i, Endian.little);`

	replaceExact := func(t *testing.T, s, old, repl string) string {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("dartMessageAnnounceRuntime missing expected snippet:\n%s", old)
		}
		return strings.Replace(s, old, repl, 1)
	}

	controls := []struct {
		name   string
		mutate func(t *testing.T, s string) string
	}{
		{
			name: "without_count_check",
			mutate: func(t *testing.T, s string) string {
				return replaceExact(t, s, fullCountCheck, "")
			},
		},
		{
			name: "without_4096_ceiling",
			mutate: func(t *testing.T, s string) string {
				return replaceExact(t, s, "trailerCount > 4096 ||", "")
			},
		},
		{
			name: "without_signed_count_guards",
			mutate: func(t *testing.T, s string) string {
				s = replaceExact(t, s, "trailerCount < 0 ||", "")
				s = replaceExact(t, s, endCheck, "")
				return replaceExact(t, s, indexedLoop, offsetLoop)
			},
		},
	}
	for _, tc := range controls {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sabotaged := tc.mutate(t, dartMessageAnnounceRuntime)
			out, err := runDartAnnounceIdTableCount(t, sabotaged)
			if err == nil {
				t.Fatalf("%s unexpectedly passed:\n%s", tc.name, out)
			}
		})
	}
}

func fnv32a(b []byte) uint32 {
	h := uint32(2166136261)
	for _, v := range b {
		h ^= uint32(v)
		h *= 16777619
	}
	return h
}

func paddedAnnouncementTrailer(prefix []byte, baseIDs []uint64, totalCount int) []byte {
	out := make([]byte, 0, len(prefix)+totalCount*8+8)
	out = append(out, prefix...)
	used := make(map[uint64]struct{}, totalCount)
	for _, id := range baseIDs {
		out = binary.LittleEndian.AppendUint64(out, id)
		used[id] = struct{}{}
	}
	nextID := uint64(0x5100000000000001)
	for len(used) < totalCount {
		if _, exists := used[nextID]; !exists &&
			nextID != ir.TableBuildVersionWireId &&
			nextID != ir.TableMessageVocabularyWireId {
			used[nextID] = struct{}{}
			out = binary.LittleEndian.AppendUint64(out, nextID)
		}
		nextID++
	}
	return binary.LittleEndian.AppendUint64(out, uint64(totalCount))
}

func runDartAnnounceIdTableCount(t *testing.T, runtimeSrc string) (string, error) {
	t.Helper()
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	valid := tablewire.Announce(u)
	if len(valid) < 9 {
		t.Fatalf("valid announcement too short: %d", len(valid))
	}
	trueCount := binary.LittleEndian.Uint64(valid[len(valid)-8:])
	bodyLen := len(valid) - 8 - int(trueCount)*8
	if bodyLen < 1 {
		t.Fatalf("invalid announcement trailer layout: len=%d count=%d", len(valid), trueCount)
	}
	bodyPrefix := append([]byte(nil), valid[:bodyLen]...)
	trueIDs := make([]uint64, trueCount)
	for i := range trueIDs {
		off := bodyLen + i*8
		trueIDs[i] = binary.LittleEndian.Uint64(valid[off : off+8])
	}

	withCount := func(base []byte, count uint64) []byte {
		out := append([]byte(nil), base[:len(base)-8]...)
		return binary.LittleEndian.AppendUint64(out, count)
	}
	shortWithCount := func(count uint64) []byte {
		out := []byte{1}
		return binary.LittleEndian.AppendUint64(out, count)
	}

	dupTrailer := append([]byte(nil), bodyPrefix...)
	for _, id := range trueIDs {
		dupTrailer = binary.LittleEndian.AppendUint64(dupTrailer, id)
	}
	dupTrailer = binary.LittleEndian.AppendUint64(dupTrailer, trueIDs[0])
	dupTrailer = binary.LittleEndian.AppendUint64(dupTrailer, trueCount+1)

	pad4096 := paddedAnnouncementTrailer(bodyPrefix, trueIDs, 4096)
	pad4097 := paddedAnnouncementTrailer(bodyPrefix, trueIDs, 4097)
	unterminated20000 := paddedAnnouncementTrailer([]byte{1}, nil, 20000)

	type wireCase struct {
		name     string
		wire     []byte
		dartExpr string
		wantOK   bool
	}
	cases := []wireCase{
		{"valid", valid, fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(valid)), true},
		{"high_bit_count_short_wire", shortWithCount((uint64(1) << 63) | 1), fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(shortWithCount((uint64(1)<<63)|1))), false},
		{"min_i64_count_short_wire", shortWithCount(uint64(1) << 63), fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(shortWithCount(uint64(1)<<63))), false},
		{"all_ones_count_short_wire", shortWithCount(^uint64(0)), fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(shortWithCount(^uint64(0)))), false},
		{"high_bits_61_63_count", withCount(valid, trueCount|(uint64(1)<<61)|(uint64(1)<<63)), fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(withCount(valid, trueCount|(uint64(1)<<61)|(uint64(1)<<63)))), false},
		{"high_bit_61_count", withCount(valid, trueCount|(uint64(1)<<61)), fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(withCount(valid, trueCount|(uint64(1)<<61)))), false},
		{"count_past_wire_length", withCount(valid, uint64((len(valid)-9)/8+1)), fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(withCount(valid, uint64((len(valid)-9)/8+1)))), false},
		{"duplicate_trailer_id", dupTrailer, fmt.Sprintf("Uint8List.fromList(%s)", dartBytes(dupTrailer)), false},
		{"count_at_4096_limit", pad4096, "paddedTrailer(bodyPrefix, baseIds, 4096)", true},
		{"count_above_4096_limit", pad4097, "paddedTrailer(bodyPrefix, baseIds, 4097)", false},
		{"unterminated_20000_ids", unterminated20000, "paddedTrailer(Uint8List.fromList(<int>[1]), const <int>[], 20000)", false},
	}

	var dartChecks strings.Builder
	baseIDLiterals := make([]string, len(trueIDs))
	for i, id := range trueIDs {
		baseIDLiterals[i] = dartU64Literal(id)
	}
	fmt.Fprintf(&dartChecks, `
  int fnv32a(Uint8List b) {
    var h = 0x811c9dc5;
    for (var i = 0; i < b.length; i++) {
      h = ((h ^ b[i]) * 0x01000193) & 0xffffffff;
    }
    return h;
  }

  Uint8List paddedTrailer(Uint8List prefix, List<int> seedIds, int totalCount) {
    final out = Uint8List(prefix.length + totalCount * 8 + 8);
    out.setRange(0, prefix.length, prefix);
    final view = ByteData.sublistView(out);
    final used = <int>{};
    var at = prefix.length;
    for (final id in seedIds) {
      view.setUint64(at, id, Endian.little);
      at += 8;
      used.add(id);
    }
    var nextId = 0x5100000000000001;
    while (used.length < totalCount) {
      if (!used.contains(nextId) &&
          nextId != %s &&
          nextId != %s) {
        used.add(nextId);
        view.setUint64(at, nextId, Endian.little);
        at += 8;
      }
      nextId += 1;
    }
    view.setUint64(at, totalCount, Endian.little);
    return out;
  }

  List<t.TableMessageEntry> storage() =>
      List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0));

  void assertCase(
    String name,
    Uint8List wire,
    int wantLen,
    int wantFnv,
    bool wantOk,
    String wantReason,
    int wantEntries,
  ) {
    check(wire.length == wantLen, '$name: wire length=${wire.length} want=$wantLen');
    check(fnv32a(wire) == wantFnv, '$name: wire checksum=${fnv32a(wire)} want=$wantFnv');
    final vocab = t.TableVocabulary(entries: storage());
    final report = t.TableReport();
    bool gotOk;
    try {
      gotOk = t.announceRead(vocab, wire, report);
    } catch (e) {
      check(false, '$name: announceRead threw $e');
      return;
    }
    check(gotOk == wantOk,
        '$name: announceRead=$gotOk want=$wantOk reason=${report.reason} refused=${report.refused}');
    if (wantOk) {
      check(vocab.announced && !report.refused,
          '$name: announced=${vocab.announced} refused=${report.refused} reason=${report.reason}');
      check(vocab.count == wantEntries,
          '$name: entries=${vocab.count} want=$wantEntries');
    } else {
      check(!vocab.announced && report.refused && report.reason == wantReason,
          '$name: announced=${vocab.announced} refused=${report.refused} reason=${report.reason} wantReason=$wantReason');
    }
  }

  final bodyPrefix = Uint8List.fromList(%s);
  final baseIds = <int>[%s];
`,
		dartU64Literal(ir.TableBuildVersionWireId),
		dartU64Literal(ir.TableMessageVocabularyWireId),
		dartBytes(bodyPrefix),
		strings.Join(baseIDLiterals, ", "),
	)

	for _, tc := range cases {
		var goVocab tablewire.Vocabulary
		var goReport tabletext.Report
		goErr := goVocab.AnnounceRead(tc.wire, &goReport)
		goOK := goErr == nil
		if goOK != tc.wantOK {
			t.Fatalf("%s: Go AnnounceRead ok=%v (err=%v), test expected ok=%v", tc.name, goOK, goErr, tc.wantOK)
		}
		wantReason := ""
		wantEntries := 0
		if goOK {
			if !goVocab.Announced() || goReport.Refused {
				t.Fatalf("%s: Go AnnounceRead succeeded with announced=%v refused=%v", tc.name, goVocab.Announced(), goReport.Refused)
			}
			wantEntries = len(goVocab.Entries())
		} else {
			wantReason = tablewire.RefusalReason(goErr)
			if wantReason != "wire_damaged" || !goReport.Refused {
				t.Fatalf("%s: Go AnnounceRead reason=%q refused=%v err=%v, want wire_damaged", tc.name, wantReason, goReport.Refused, goErr)
			}
		}
		fmt.Fprintf(&dartChecks, "  assertCase(%q, %s, %d, %d, %v, %q, %d);\n",
			tc.name, tc.dartExpr, len(tc.wire), fnv32a(tc.wire), goOK, wantReason, wantEntries)
	}

	return runAnnounceProbeWithRuntime(t, u, runtimeSrc, dartChecks.String())
}

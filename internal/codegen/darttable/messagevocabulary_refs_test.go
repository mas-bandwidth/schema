package darttable

// THE MESSAGE FORM RESOLVES EVERY REFERENCE THROUGH THE PEER'S VOCABULARY
// (docs/SPEC-TABLES.md §3.3): a reference is `bits_required(0, E)` bits, slot
// `k` names the ANNOUNCED entry `k`, and the entry's id chooses this root's
// own field. The receiver matches ids against its descriptors, so two peers at
// different build versions is the ordinary case. The bytes here come from the
// Go engine, never from the Dart writer under test, and the read is compared
// with the Go engine's read of the same bytes and the same announcement.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// peerVocabularySchema is the reader's unit: a scalar, an enum, a ranged
// scalar, a counted array and a union arm, the references a body walk takes.
const peerVocabularySchema = `package probe

enum Region
{
    NA
    EU
    APAC
    SA
}

union Body
{
    tally int32 | min = 0, max = 1000
    flag  bool
}

fixed table Root
{
    player_id uint64 = 0
    region    Region = NA
    score     int32 = 0 | min = 0, max = 200000
    tags      [..8]uint16 | min = 0, max = 60000
    body      Body
}
`

const peerRootJSON = `{"player_id":301050681195367,"region":"EU","score":34567,"tags":[1,2,3],"body":{"tally":7}}`

func peerUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	return unitFrom(t, src)
}

func peerAddedNames(t *testing.T) *ir.Unit {
	t.Helper()
	var extras strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&extras, "    extra%d uint32 = 0\n", i)
	}
	src := strings.Replace(peerVocabularySchema,
		"    body      Body\n}", "    body      Body\n"+extras.String()+"}", 1)
	return peerUnit(t, src)
}

func peerSwapped(t *testing.T) *ir.Unit {
	t.Helper()
	src := strings.Replace(peerVocabularySchema,
		"    player_id uint64 = 0\n    region    Region = NA\n",
		"    region    Region = NA\n    player_id uint64 = 0\n", 1)
	return peerUnit(t, src)
}

func peerOneAhead(t *testing.T) *ir.Unit {
	t.Helper()
	src := strings.Replace(peerVocabularySchema,
		"    player_id uint64 = 0\n",
		"    ahead     uint32 = 0\n    player_id uint64 = 0\n", 1)
	return peerUnit(t, src)
}

func peerNarrowRanges(t *testing.T) *ir.Unit {
	t.Helper()
	src := strings.NewReplacer(
		"max = 200000", "max = 100000",
		"max = 60000", "max = 30000",
	).Replace(peerVocabularySchema)
	return peerUnit(t, src)
}

func peerFourEntries(t *testing.T) *ir.Unit {
	t.Helper()
	return peerUnit(t, `package probe

fixed table Small
{
    a bool
    b bool
    c bool
    d bool
}
`)
}

// peerBatch reads the value with the Go engine and writes the announcement and
// the batch, so the wire is the reference's and not the Dart writer's.
func peerBatch(t *testing.T, u *ir.Unit, root, value string) (announce, batch []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	if !m.Read(inst, []byte(value), &report) || !report.Silent() {
		t.Fatalf("the peer value is not readable: %+v", report)
	}
	batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatalf("encode the peer batch: %v", err)
	}
	return tablewire.Announce(u), batch
}

// goPeerRead loads the same announcement and batch with the Go engine for the
// reader's unit and answers its verdict and report.
func goPeerRead(t *testing.T, u *ir.Unit, root string, announce, batch []byte) (int, tabletext.Report, bool) {
	t.Helper()
	var vocabulary tablewire.Vocabulary
	var announceReport tabletext.Report
	if err := vocabulary.AnnounceRead(announce, &announceReport); err != nil {
		t.Fatalf("the Go engine refuses the announcement: %v", err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, batch, &vocabulary, &report)
	if err != nil {
		return 0, report, false
	}
	return n, report, ok
}

// refBits is the announced width of one reference (docs/SPEC-TABLES.md §3.3).
func refBits(u *ir.Unit) int {
	return ir.TableMessageRefBits(len(tablewire.MessageVocabulary(u)))
}

// craftRef builds a one-body batch whose first reference is `ref` at `bits`.
func craftRef(ref uint64, bits int) []byte {
	out := make([]byte, 2+(bits+7)/8)
	out[0], out[1] = 2, 0
	for i := 0; i < bits; i++ {
		if (ref>>i)&1 != 0 {
			out[2+(i>>3)] |= 1 << (i & 7)
		}
	}
	return out
}

// runBatchProbeEdited is runBatchProbe with the generated sources handed
// through edit before they are written, which is the negative control's hook.
func runBatchProbeEdited(t *testing.T, u *ir.Unit, root, body string, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate table: %v", err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := runtimeHome(u) + "Fixed.dart"
	rootLib := fixedTableBase(u, root) + "Fixed.dart"
	probe := strings.NewReplacer("@HOME@", home, "@ROOT@", rootLib, "@BODY@", body).Replace(batchProbeTemplate)
	if err := os.WriteFile(filepath.Join(dir, "batch_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(announceDartBinary(t), "--enable-asserts", "batch_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// validRootBody drives the receiver's Root from the peer's announcement and
// batch and requires the values the Go engine reads from the same bytes.
func validRootBody(announce, batch []byte) string {
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final ar = h.TableReport();
  check(h.announceRead(vocab, ann, ar), 'the announcement is refused: ' + ar.reason);
  final v = h.Root();
  h.initRoot(v);
  final rep = h.TableReport();
  final count = r.rootLoadMessages(<h.Root>[v], msg, vocab, rep);
  check(count == 1 && !rep.refused, 'load count=$count refused=${rep.refused} reason=${rep.reason}');
  check(rep.unknown == 0 && rep.widened == 0,
      'report unknown=${rep.unknown} widened=${rep.widened}');
  check(v.playerId == 301050681195367, 'playerId=${v.playerId}');
  check(v.region == 2, 'region=${v.region}');
  check(v.score == 34567, 'score=${v.score}');
  check(v.tagsCount == 3, 'tagsCount=${v.tagsCount}');
  check(v.tags[0] == 1 && v.tags[1] == 2 && v.tags[2] == 3, 'tags=${v.tags[0]},${v.tags[1]},${v.tags[2]}');
  check(v.body.type == 1, 'body=${v.body.type}');
  check(v.body.tally == 7, 'tally=${v.body.tally}');
`, dartBytes(announce), dartBytes(batch))
}

func damagedBody(announce, batch []byte) string {
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final ar = h.TableReport();
  check(h.announceRead(vocab, ann, ar), 'the announcement is refused: ' + ar.reason);
  final v = h.Root();
  h.initRoot(v);
  final rep = h.TableReport();
  final count = r.rootLoadMessages(<h.Root>[v], msg, vocab, rep);
  check(count == 0 && rep.refused && rep.reason == 'wire_damaged',
      'expected wire_damaged, count=$count refused=${rep.refused} reason=${rep.reason}');
`, dartBytes(announce), dartBytes(batch))
}

// TestDartMessageReadsThePeerVocabulary is the cross-build read: the peer's
// announcement and batch, written by the Go engine from a second unit, must
// load the same values and report on the emitted Dart as on the Go engine.
func TestDartMessageReadsThePeerVocabulary(t *testing.T) {
	t.Parallel()
	dartBinary(t) // a missing toolchain FAILS under SCHEMA_REQUIRE_CORPUS

	reader := peerUnit(t, peerVocabularySchema)
	readerBits := refBits(reader)

	valid := []struct {
		name string
		unit *ir.Unit
	}{
		{"added_names", peerAddedNames(t)},
		{"swapped_declarations", peerSwapped(t)},
		{"one_field_ahead", peerOneAhead(t)},
		{"moved_ranges", peerNarrowRanges(t)},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			announce, batch := peerBatch(t, tc.unit, "Root", peerRootJSON)
			n, report, ok := goPeerRead(t, reader, "Root", announce, batch)
			if !ok || n != 1 || report.Refused || report.Unknown != 0 || report.Widened != 0 {
				t.Fatalf("the Go engine does not read the peer batch: n=%d ok=%v report=%+v", n, ok, report)
			}
			out, err := runBatchProbe(t, reader, "Root", validRootBody(announce, batch))
			if err != nil {
				t.Fatalf("%s: the Dart read does not match the Go engine: %v\n%s", tc.name, err, out)
			}
		})
	}

	// The reference widths themselves: the added-names peer is WIDER, and the
	// one-field-ahead peer is the same width with a moved slot.
	if got := refBits(peerAddedNames(t)); got <= readerBits {
		t.Fatalf("the added-names peer is not wider: peer=%d reader=%d", got, readerBits)
	}
	if got := refBits(peerOneAhead(t)); got != readerBits {
		t.Fatalf("the one-field-ahead peer changes the reference width: peer=%d reader=%d", got, readerBits)
	}

	// A reference above the announced entry count, read at the announced
	// width, is damage (docs/SPEC-TABLES.md §3.3).
	t.Run("above_announced_count", func(t *testing.T) {
		t.Parallel()
		peer := peerAddedNames(t)
		entries := len(tablewire.MessageVocabulary(peer))
		bits := refBits(peer)
		above := uint64(entries + 1)
		if above >= 1<<bits {
			t.Fatalf("the fixture cannot spell a reference above its count: entries=%d bits=%d", entries, bits)
		}
		batch := craftRef(above, bits)
		if n, _, ok := goPeerRead(t, reader, "Root", tablewire.Announce(peer), batch); ok || n != 0 {
			t.Fatalf("the Go engine accepts a reference above the announced count: n=%d ok=%v", n, ok)
		}
		out, err := runBatchProbe(t, reader, "Root", damagedBody(tablewire.Announce(peer), batch))
		if err != nil {
			t.Fatalf("Dart accepts a reference above the announced count: %v\n%s", err, out)
		}
	})

	// Four entries announced is a three-bit reference: a compiled slot this
	// reader declares but the vocabulary does not is damage, never a value.
	t.Run("below_announced_count", func(t *testing.T) {
		t.Parallel()
		peer := peerFourEntries(t)
		if entries := len(tablewire.MessageVocabulary(peer)); entries != 4 {
			t.Fatalf("the four-entry fixture announces %d entries", entries)
		}
		if bits := refBits(peer); bits != 3 {
			t.Fatalf("the four-entry fixture announces %d-bit references", bits)
		}
		batch := craftRef(5, refBits(peer))
		if n, _, ok := goPeerRead(t, reader, "Root", tablewire.Announce(peer), batch); ok || n != 0 {
			t.Fatalf("the Go engine accepts a reference above the announced count: n=%d ok=%v", n, ok)
		}
		out, err := runBatchProbe(t, reader, "Root", damagedBody(tablewire.Announce(peer), batch))
		if err != nil {
			t.Fatalf("Dart accepts a compiled slot the announced vocabulary does not name: %v\n%s", err, out)
		}
	})
}

// TestDartMessageReadsThePeerVocabularyNegativeControl puts the reference read
// back to the reader's compiled width in the emitted source and requires the
// wider peer's cross-build read to go red.
func TestDartMessageReadsThePeerVocabularyNegativeControl(t *testing.T) {
	t.Parallel()
	dartBinary(t)

	reader := peerUnit(t, peerVocabularySchema)
	peer := peerAddedNames(t)
	if refBits(peer) <= refBits(reader) {
		t.Fatalf("the control needs a peer at another reference width")
	}
	announce, batch := peerBatch(t, peer, "Root", peerRootJSON)
	out, err := runBatchProbeEdited(t, reader, "Root", validRootBody(announce, batch), func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, "r.bits.get(r.vocabulary.refBits)") {
				files[name] = []byte(strings.ReplaceAll(s, "r.bits.get(r.vocabulary.refBits)", "r.bits.get(tableMessageRefBits)"))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("the emitted source carries no announced reference read to replace")
		}
	})
	if err == nil {
		t.Fatalf("the compiled-width reference read still reads the cross-build batch:\n%s", out)
	}
}

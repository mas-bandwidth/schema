package darttable

// THE WALKED-ELEMENT SKIP ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
//
// An announced array whose elements are walked one at a time holds an element
// that rides no bits to be damage: the walk's element consumed nothing, so the
// next element would begin where this one did and the count alone would buy
// the iterations. The Go reference's walk refuses that shape, and the Dart
// reader must answer the same verdict for the same bytes.
//
// The hostile shape is an array of up to 2^24 elements whose element is an
// array of exactly one payload-free element. Its inner count has width zero
// and its inner element rides no bits, so every outer element consumes
// nothing. A batch carrying a 2^24 outer count is a few bytes.

import (
	"encoding/binary"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const skipProgressSchema = `package probe

fixed table Root {
    x uint8
    y uint8
}
`

// skipProgressAnnouncement appends the hostile entry to the unit's own message
// vocabulary and rebuilds the announcement around it. The returned slot is the
// entry's one-based reference.
func skipProgressAnnouncement(t *testing.T, u *ir.Unit) (announcement []byte, slot uint64, refBits int) {
	t.Helper()
	hostile := ir.TableVocabularyEntry{
		Id:   0x5a5a5a5a00000001,
		Kind: ir.TableKindArray,
		Shape: ir.TableMessageShape{
			Min:  0,
			Max:  1 << 24,
			Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{
				Min:   1,
				Max:   1,
				Elem:  ir.TableKindNoPayload,
				Inner: &ir.TableMessageShape{},
			},
		},
	}
	entries := append(tablewire.MessageVocabulary(u), hostile)
	vocabulary := ir.TableVocabularyBytes(entries)
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, ir.BuildVersion(u))
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{uint8(ir.TableKindU8)}
	body = binary.AppendUvarint(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = binary.AppendUvarint(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out, uint64(len(entries)), ir.TableMessageRefBits(len(entries))
}

// skipProgressBatch is one body: the hostile reference, its 2^24 count at the
// announced width, the zero reference that ends the body, and the pad.
func skipProgressBatch(slot uint64, refBits int, width int, count uint64) []byte {
	out := []byte{ir.TableWireMessageForm, 0}
	var acc uint64
	var at int
	put := func(v uint64, bits int) {
		for i := range bits {
			if (v>>i)&1 != 0 {
				acc |= 1 << at
			}
			at++
			if at == 8 {
				out = append(out, byte(acc))
				acc, at = 0, 0
			}
		}
	}
	put(slot, refBits)
	put(count, width)
	put(0, refBits)
	if at > 0 {
		out = append(out, byte(acc))
	}
	return out
}

func skipProgressBody(announcement, wire []byte) string {
	return strings.NewReplacer(
		"@ANN@", dartBytes(announcement),
		"@WIRE@", dartBytes(wire),
	).Replace(`
  final ann = Uint8List.fromList(@ANN@);
  final wire = Uint8List.fromList(@WIRE@);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the announcement is refused: ${annRep.reason}');
  final value = h.Root();
  h.initRoot(value);
  final rep = h.TableReport();
  final count = r.rootLoadMessages(<h.Root>[value], wire, vocab, rep);
  check(count == 0 && rep.refused,
      'skip progress: count=$count refused=${rep.refused} reason=${rep.reason}');
`)
}

// runSkipProgressProbe emits the unit's surface, optionally edits the emitted
// source in memory, and runs the probe with the leg's own toolchain. A missing
// toolchain fails under SCHEMA_REQUIRE_CORPUS rather than skipping.
func runSkipProgressProbe(t *testing.T, u *ir.Unit, root, body string, edit func(map[string][]byte)) (string, error) {
	slowtest.Gate(t, "an external toolchain")

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
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "batch_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// skipProgressFixture builds the announcement, batch and probe both the Go
// engine and the emitted Dart read.
func skipProgressFixture(t *testing.T) (u *ir.Unit, announcement []byte, body string, wire []byte) {
	t.Helper()
	u = unitFrom(t, skipProgressSchema)
	announcement, slot, refBits := skipProgressAnnouncement(t, u)
	width := ir.TableMessageBitsRequired(0, 1<<24)
	wire = skipProgressBatch(slot, refBits, width, 1<<24)
	body = skipProgressBody(announcement, wire)
	return u, announcement, body, wire
}

// TestDartMessageSkipMakesProgress requires the emitted Dart reader to refuse
// a walked element that consumes no bits, the same verdict the Go engine
// reaches for the same announcement and the same bytes.
func TestDartMessageSkipMakesProgress(t *testing.T) {
	t.Parallel()
	u, announcement, body, wire := skipProgressFixture(t)

	// THE GO ENGINE refuses the same bytes at once.
	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(announcement, &announced); err != nil {
		t.Fatalf("the Go engine refused the announcement: %v %+v", err, announced)
	}
	model := tabletext.NewModel(u)
	var goReport tabletext.Report
	ok, err := tablewire.DecodeMessage(model, model.New(findTable(t, u, "Root")), wire, &vocabulary, &goReport)
	if ok || err == nil || !goReport.Refused {
		t.Fatalf("the Go engine did not refuse the batch: ok=%v err=%v report=%+v", ok, err, goReport)
	}

	out, err := runSkipProgressProbe(t, u, "Root", body, nil)
	if err != nil {
		t.Fatalf("the Dart leg did not reach the Go engine's verdict (refused): %v\n%s", err, out)
	}
}

// TestDartMessageSkipMakesProgressNegativeControl takes the progress test out
// of the emitted source in memory. The walk then finishes the announced count
// and accepts the batch, so the main test is red on the verdict.
func TestDartMessageSkipMakesProgressNegativeControl(t *testing.T) {
	t.Parallel()
	u, _, body, _ := skipProgressFixture(t)

	out, err := runSkipProgressProbe(t, u, "Root", body, func(files map[string][]byte) {
		const from = "        final before = r.bits.offset;\n" +
			"        if (!tableMessageSkip(r, s.elem, inner, depth + 1)) return false;\n" +
			"        if (r.bits.offset == before) return false;\n"
		const to = "        if (!tableMessageSkip(r, s.elem, inner, depth + 1)) return false;\n"
		removed := 0
		for name, src := range files {
			text := string(src)
			if strings.Contains(text, from) {
				removed += strings.Count(text, from)
				files[name] = []byte(strings.ReplaceAll(text, from, to))
			}
		}
		if removed == 0 {
			t.Fatal("the progress test moved in the emitted runtime")
		}
	})
	if err == nil {
		t.Fatalf("the unguarded walk stayed green:\n%s", out)
	}
	if !strings.Contains(out, "skip progress: count=1 refused=false") {
		t.Fatalf("the control did not fail on the verdict:\n%s", out)
	}
}

package darttable

// THE ANNOUNCEMENT'S SHAPE VALIDATION IS THE ENGINE'S (docs/SPEC-TABLES.md
// §3.3): a generated Dart announceRead accepts one entry exactly when
// ir.DecodeShape accepts its shape, and a batch's unknown-entry skip uses the
// shape the announcement decoded. The probes below append ONE entry to a unit's
// own announcement, fix the vocabulary lengths up, and compare the emitted Dart
// with the Go engine on the same bytes.

import (
	"bytes"
	"encoding/binary"
	"fmt"
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

const announceShapeId = uint64(0x8877665544332211)

type announceShapeCase struct {
	name        string
	entry       []byte
	want        bool
	payloadBits int
}

func announceShapeEntry(kind byte, shape ...byte) []byte {
	out := binary.LittleEndian.AppendUint64(nil, announceShapeId)
	out = append(out, kind)
	return append(out, shape...)
}

func announceShapeCases() []announceShapeCase {
	arrayMax := announceShapeEntry(ir.TableKindArray, 0)
	arrayMax = append(arrayMax, lebBytes(1<<40)...)
	arrayMax = append(arrayMax, ir.TableKindI8, 0)
	return []announceShapeCase{
		{
			// array [0..1] of array [0..1] of int8, the int8's packing byte
			// missing: DecodeShape refuses the inner int8 shape.
			name:  "nested_array_truncated",
			entry: announceShapeEntry(ir.TableKindArray, 0, 1, ir.TableKindArray, 0, 1, ir.TableKindI8),
		},
		{
			// The same nested array whole: DecodeShape reads it, so announceRead
			// must too.
			name:        "nested_array_wellformed",
			entry:       announceShapeEntry(ir.TableKindArray, 0, 2, ir.TableKindArray, 0, 2, ir.TableKindI8, 0),
			want:        true,
			payloadBits: 2,
		},
		{
			// array [0..4] of string(16), a text list (§2.13).
			name:        "array_of_string",
			entry:       announceShapeEntry(ir.TableKindArray, 0, 4, ir.TableKindString, 16),
			want:        true,
			payloadBits: 3,
		},
		{
			// keyed array (4) of keyed array (4) of a raw uint8.
			name:        "keyed_array_of_keyed_array",
			entry:       announceShapeEntry(ir.TableKindKeyed, 4, ir.TableKindKeyed, 4, ir.TableKindU8, 0),
			want:        true,
			payloadBits: 3,
		},
		{
			// A timestamp, whose shape carries no bytes.
			name:        "timestamp",
			entry:       announceShapeEntry(ir.TableKindTimestamp),
			want:        true,
			payloadBits: 64,
		},
		{
			// A string max above MaxInt32 is a hostile width.
			name:  "string_max_above_int32",
			entry: announceShapeEntry(ir.TableKindString, lebBytes(1<<40)...),
		},
		{
			name:  "string_max_2p62",
			entry: announceShapeEntry(ir.TableKindString, lebBytes(1<<62)...),
		},
		{
			// An array whose min exceeds its max.
			name:  "array_min_above_max",
			entry: announceShapeEntry(ir.TableKindArray, 5, 2, ir.TableKindI8, 0),
		},
		{
			// An array max above TableMessageListMax is a hostile width.
			name:  "array_max_above_list_max",
			entry: arrayMax,
		},
	}
}

// announceShapeBatch builds a one-body batch whose only field names the
// appended entry (slot `ref`) and steps over a payload of payloadBits zero bits.
func announceShapeBatch(refBits, ref, payloadBits int) []byte {
	var bits []int
	put := func(v, width int) {
		for i := range width {
			bits = append(bits, (v>>i)&1)
		}
	}
	put(ref, refBits)
	for range payloadBits {
		bits = append(bits, 0)
	}
	put(0, refBits)
	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	out[1] = 0 // one body
	for i, b := range bits {
		if b != 0 {
			out[2+(i>>3)] |= byte(1 << (i & 7))
		}
	}
	return out
}

// runAnnounceShapeProbe emits the unit's whole surface and runs the probe,
// letting a control edit the emitted source in memory first.
func runAnnounceShapeProbe(t *testing.T, u *ir.Unit, root, body string, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	slowtest.Gate(t, "dart (it runs the generated announce shape probe)")
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

func announceShapeProbeBody(root string, cases []announceShapeCase, anns, batches [][]byte) string {
	lower := lowerFirst(root)
	var body strings.Builder
	fmt.Fprintf(&body, "  final vocabCap = 8192;\n")
	fmt.Fprintf(&body, "  void runCase(String name, List<int> annBytes, List<int> batchBytes, bool wantAnnounce) {\n")
	fmt.Fprintf(&body, "    final v = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(vocabCap, (i) => h.TableMessageEntry(0, 0)));\n")
	fmt.Fprintf(&body, "    final ar = h.TableReport();\n")
	fmt.Fprintf(&body, "    var accepted = false;\n")
	fmt.Fprintf(&body, "    var problem = '';\n")
	fmt.Fprintf(&body, "    try {\n")
	fmt.Fprintf(&body, "      accepted = h.announceRead(v, Uint8List.fromList(annBytes), ar);\n")
	fmt.Fprintf(&body, "    } catch (e) {\n")
	fmt.Fprintf(&body, "      problem = 'announceRead threw $e';\n")
	fmt.Fprintf(&body, "    }\n")
	fmt.Fprintf(&body, "    final value = h.%s();\n", root)
	fmt.Fprintf(&body, "    h.init%s(value);\n", root)
	fmt.Fprintf(&body, "    final values = <h.%s>[value];\n", root)
	fmt.Fprintf(&body, "    final lr = h.TableReport();\n")
	fmt.Fprintf(&body, "    var count = -2;\n")
	fmt.Fprintf(&body, "    try {\n")
	fmt.Fprintf(&body, "      count = r.%sLoadMessages(values, Uint8List.fromList(batchBytes), v, lr);\n", lower)
	fmt.Fprintf(&body, "    } catch (e) {\n")
	fmt.Fprintf(&body, "      problem = problem.isEmpty ? 'LoadMessages threw $e' : '$problem; LoadMessages threw $e';\n")
	fmt.Fprintf(&body, "    }\n")
	fmt.Fprintf(&body, "    check(problem.isEmpty, '$name: $problem');\n")
	fmt.Fprintf(&body, "    check(accepted == wantAnnounce, '$name: announceRead=$accepted want=$wantAnnounce reason=${ar.reason}');\n")
	fmt.Fprintf(&body, "    if (wantAnnounce) {\n")
	fmt.Fprintf(&body, "      check(count == 1 && !lr.refused && lr.unknown == 1,\n")
	fmt.Fprintf(&body, "          '$name: load count=$count refused=${lr.refused} unknown=${lr.unknown} reason=${lr.reason}');\n")
	fmt.Fprintf(&body, "    } else {\n")
	fmt.Fprintf(&body, "      check(count <= 0 && lr.refused && lr.unknown == 0,\n")
	fmt.Fprintf(&body, "          '$name: load count=$count refused=${lr.refused} unknown=${lr.unknown} reason=${lr.reason}');\n")
	fmt.Fprintf(&body, "    }\n")
	fmt.Fprintf(&body, "  }\n")
	for i, c := range cases {
		fmt.Fprintf(&body, "  runCase(%q, %s, %s, %t);\n", c.name, dartBytes(anns[i]), dartBytes(batches[i]), c.want)
	}
	return body.String()
}

func TestDartAnnounceShapesAsTheEngine(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	root := "LoginRequest"
	vocabulary := tablewire.MessageVocabulary(u)
	base := ir.TableVocabularyBytes(vocabulary)
	if got := craftAnnouncement(base, ir.BuildVersion(u), true, true); !bytes.Equal(got, tablewire.Announce(u)) {
		t.Fatalf("the crafted base announcement is not the unit's own")
	}
	total := len(vocabulary) + 1
	refBits := ir.TableMessageRefBits(total)
	m := tabletext.NewModel(u)
	st := findTable(t, u, root)

	cases := announceShapeCases()
	anns := make([][]byte, len(cases))
	batches := make([][]byte, len(cases))
	for i, c := range cases {
		vocab := append(append([]byte{}, base...), c.entry...)
		ann := craftAnnouncement(vocab, ir.BuildVersion(u), true, true)
		batch := announceShapeBatch(refBits, total, c.payloadBits)
		anns[i], batches[i] = ann, batch

		var v tablewire.Vocabulary
		var ar tabletext.Report
		err := v.AnnounceRead(ann, &ar)
		gotAnn := err == nil && v.Announced()
		if gotAnn != c.want {
			t.Fatalf("%s: the engine's AnnounceRead=%t, the case wants %t (%v %+v)", c.name, gotAnn, c.want, err, ar)
		}
		inst := m.New(st)
		var br tabletext.Report
		count, ok, berr := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, batch, &v, &br)
		if c.want {
			if berr != nil || !ok || count != 1 || br.Refused || br.Unknown != 1 {
				t.Fatalf("%s: the engine's batch read count=%d ok=%t err=%v report=%+v", c.name, count, ok, berr, br)
			}
		} else if berr == nil || ok || count != 0 {
			t.Fatalf("%s: the engine's batch read count=%d ok=%t err=%v, want a refusal", c.name, count, ok, berr)
		}
	}

	body := announceShapeProbeBody(root, cases, anns, batches)
	out, err := runAnnounceShapeProbe(t, u, root, body, nil)
	if err != nil {
		t.Fatalf("the emitted announce shape probe diverged from the engine: %v\n%s", err, out)
	}
}

func TestDartAnnounceShapesAsTheEngineNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	root := "LoginRequest"
	vocabulary := tablewire.MessageVocabulary(u)
	base := ir.TableVocabularyBytes(vocabulary)
	total := len(vocabulary) + 1
	refBits := ir.TableMessageRefBits(total)
	cases := announceShapeCases()
	anns := make([][]byte, len(cases))
	batches := make([][]byte, len(cases))
	for i, c := range cases {
		anns[i] = craftAnnouncement(append(append([]byte{}, base...), c.entry...), ir.BuildVersion(u), true, true)
		batches[i] = announceShapeBatch(refBits, total, c.payloadBits)
	}
	body := announceShapeProbeBody(root, cases, anns, batches)
	const step = "final inner = decode(sig, at, end, s.elem);"
	out, err := runAnnounceShapeProbe(t, u, root, body, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, step) {
				files[name] = []byte(strings.Replace(s, step, "final inner = (TableMessageShape(), at);", 1))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("the emitted source carries no nested-shape step")
		}
	})
	if err == nil {
		t.Fatalf("removing the nested-shape step from the emitted source unexpectedly passed:\n%s", out)
	}
}

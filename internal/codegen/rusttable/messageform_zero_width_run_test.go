package rusttable

// A RUN OF ZERO-WIDTH ELEMENTS IS BOUNDED BY THE BATCH (§3.3). An endpoint's
// announcement is untrusted, and one unknown entry can announce an array whose
// count rides no bits over an element that itself rides none: the element is a
// nested array of fixed count, or an enum-keyed array of fixed count, or a
// fixed run over a zero-width element. Walking a count of 2^31 one element at a
// time would cost that many iterations for a few bytes of wire, so the walk
// steps the rest of the run in one and refuses a remaining count the bits left
// could not back, exactly as the Go engine does (internal/tablewire
// messagedecode.go). A small honest count still loads and counts its unknown
// field once.
//
// THE ENGINE IS THE ORACLE. Every case decodes the same announcement and batch
// with internal/tablewire in this test, and the Rust leg must agree on the
// verdict: accepted (one body, unknown once) or refused. The verdict only is
// pinned, never a time or a reason name (docs/rulings.json, refusal-verdict
// only).
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1, as finitefloat_test.go does. The Go half reads
// only the generator's output and the oracle: no clock, no socket, no network.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const messageZeroWidthSrc = `package zerowidth

fixed table Probe
{
    x uint8
}
`

// messageZeroWidthBatch is one body that names the unknown entry once and ends
// its body at the zero reference.
var messageZeroWidthBatch = []byte{ir.TableWireMessageForm, 0, 0x01}

func TestRustMessageZeroWidthRunIsBounded(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, messageZeroWidthSrc)
	m := tabletext.NewModel(u)
	wide := ir.TableMessageListMax
	flat := func() *ir.TableMessageShape { return &ir.TableMessageShape{} }
	cases := []struct {
		name   string
		entry  ir.TableVocabularyEntry
		honest bool
	}{
		{
			// the element is itself an array of fixed count zero
			name: "nested_array_of_zero",
			entry: ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000001, Kind: ir.TableKindArray,
				Shape: ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
					Inner: &ir.TableMessageShape{Min: 0, Max: 0, Elem: ir.TableKindBool, Inner: flat()}}},
		},
		{
			// the element is an enum-keyed array of fixed count zero
			name: "inner_keyed_zero",
			entry: ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000002, Kind: ir.TableKindArray,
				Shape: ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindKeyed,
					Inner: &ir.TableMessageShape{Max: 0, Elem: ir.TableKindBool, Inner: flat()}}},
		},
		{
			// the element is a fixed run over a zero-width element
			name: "inner_fixed_zero_width_run",
			entry: ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000003, Kind: ir.TableKindArray,
				Shape: ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
					Inner: &ir.TableMessageShape{Min: 3, Max: 3, Elem: ir.TableKindNoPayload, Inner: flat()}}},
		},
		{
			// a count the batch's bits back is the shape a conforming writer
			// produces, and it still loads its unknown field once
			name:   "honest_small_count",
			honest: true,
			entry: ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000004, Kind: ir.TableKindArray,
				Shape: ir.TableMessageShape{Min: 3, Max: 3, Elem: ir.TableKindArray,
					Inner: &ir.TableMessageShape{Min: 2, Max: 2, Elem: ir.TableKindNoPayload, Inner: flat()}}},
		},
	}

	var body string
	for _, c := range cases {
		announcement := messageZeroWidthAnnouncement(t, c.entry)

		// THE ORACLE: the same bytes through the Go engine.
		v := &tablewire.Vocabulary{}
		var arep tabletext.Report
		if err := v.AnnounceRead(announcement, &arep); err != nil {
			t.Fatalf("%s: the engine refused the announcement: %v", c.name, err)
		}
		var erep tabletext.Report
		got, err := tablewire.DecodeMessage(m, m.New(m.Lookup("Probe")), messageZeroWidthBatch, v, &erep)
		engineOK := err == nil && got
		engineUnknown := erep.Unknown == 1
		if c.honest && (!engineOK || !engineUnknown) {
			t.Fatalf("%s: the engine did not load the honest run: ok=%v unknown=%d err=%v report=%+v", c.name, got, erep.Unknown, err, erep)
		}
		if !c.honest && engineOK {
			t.Fatalf("%s: the engine accepted the hostile run, so the case tests nothing", c.name)
		}

		body += fmt.Sprintf(`
    {
        const ANN: [u8; %d] = [%s];
        const BATCH: [u8; %d] = [%s];
        let mut storage = vec![TableMessageEntry::default(); 16];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &ANN, &mut arep);
        let mut values = vec![ProbeRow::default(); 1];
        let mut lrep = TableReport::default();
        let (count, ok) = probe_load_messages(&mut values, &vocab, &BATCH, &mut lrep);
        let accepted = ok && count == 1;
        let want = %t;
        let unknown = if want { lrep.unknown == 1 } else { true };
        say(%q, announced && accepted == want && unknown,
            format!("announced={announced} count={count} ok={ok} report={lrep:?}"));
    }
`, len(announcement), rustBytes(announcement), len(messageZeroWidthBatch), rustBytes(messageZeroWidthBatch),
			c.honest, c.name)
	}
	out := runMessageZeroWidthProbe(t, u, body)
	for _, c := range cases {
		if line := messageBatchLine(out, c.name); line != "BATCH "+c.name+" ok" {
			t.Errorf("%s: the Rust verdict disagrees with the engine: %s", c.name, line)
		}
	}
}

// messageZeroWidthAnnouncement is the announcement's bytes for one entry: the
// form byte, the build version, the vocabulary field, the body's zero
// reference, and the id table's two reserved ids and count.
func messageZeroWidthAnnouncement(t *testing.T, entry ir.TableVocabularyEntry) []byte {
	t.Helper()
	vocabulary := entry.Encode(nil)
	if _, _, ok := ir.DecodeShape(vocabulary[9:], entry.Kind); !ok {
		t.Fatalf("the announcement refuses the shape, so the case tests nothing")
	}
	out := []byte{ir.TableWireForm}
	out = append(out, 1, uint8(ir.TableKindU64))
	out = binary.LittleEndian.AppendUint64(out, 0)
	out = append(out, 2, uint8(ir.TableKindArray))
	field := []byte{uint8(ir.TableKindU8)}
	field = appendMessageLeb(field, uint64(len(vocabulary)))
	field = append(field, vocabulary...)
	out = appendMessageLeb(out, uint64(len(field)))
	out = append(out, field...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// runMessageZeroWidthProbe writes the unit's table modules beside a crate root
// and builds them with rustc alone, optimised so a wide element run finishes
// even where the run is not yet bounded. The runtimes are self-contained, so no
// packet module and no cargo workspace is needed.
func runMessageZeroWidthProbe(t *testing.T, u *ir.Unit, body string) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(`
fn say(id: &str, ok: bool, why: String) {
    if ok {
        println!("BATCH {id} ok");
    } else {
        println!("BATCH {id} BAD {why}");
    }
}

fn main() {
`)
	main.WriteString(body)
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "zerowidthprobe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	return string(out)
}

func appendMessageLeb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

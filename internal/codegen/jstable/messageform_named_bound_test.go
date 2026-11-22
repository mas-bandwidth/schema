package jstable

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type boundBits struct {
	b []byte
	n int
}

func (w *boundBits) put(v uint64, n int) {
	for i := range n {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		w.b[w.n/8] |= byte(v>>uint(i)&1) << uint(w.n%8)
		w.n++
	}
}

func (w *boundBits) align() {
	for w.n%8 != 0 {
		w.put(0, 1)
	}
}

func boundLeb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// boundAnnouncement is a form-1 announcement of exactly these entries under a
// build version that is not the probe unit's.
func boundAnnouncement(entries []ir.TableVocabularyEntry) []byte {
	vocabulary := ir.TableVocabularyBytes(entries)
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, 0x0123456789abcdef)
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{uint8(ir.TableKindU8)}
	body = boundLeb(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = boundLeb(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// TestJsMessageNamedReadRefusesOverBound holds the JavaScript message reader's
// NAMED read to the Go engine (docs/SPEC-TABLES.md §3.3, §4): a string, bytes
// or array longer than this reader's bound, or longer than the sender's
// ANNOUNCED max its length's or count's bits can spell past, is REFUSED with
// the engine's reason. Nothing is cut and counted. A length or count at both
// bounds reads.
func TestJsMessageNamedReadRefusesOverBound(t *testing.T) {
	slowtest.Gate(t, "node")
	t.Parallel()
	const schemaSrc = `package probe

enum Color { Red, Green, Blue }

fixed table Msg
{
    keep  uint32
    text  string(8)
    blob  bytes(8)
    list  [..4]uint8
    pair  [2]uint8
    keyed [Color]uint8
}
`
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	baseFiles, err := js.Generate(u)
	if err != nil {
		t.Fatalf("js.Generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range baseFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ProbeTable.js"), files["ProbeTable.js"], 0o600); err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup("Msg")
	node := jsNode(t)
	field := func(name string) *ir.Field {
		for _, f := range u.Tables["Msg"].Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("Msg.%s is not declared", name)
		return nil
	}

	// text writes a string of length n under an announced max
	text := func(max int64, n int) ([]ir.TableVocabularyEntry, func(w *boundBits, refBits int)) {
		e := ir.TableFieldEntry(field("text"))
		e.Shape.Max = max
		return []ir.TableVocabularyEntry{e}, func(w *boundBits, refBits int) {
			w.put(1, refBits)
			w.put(uint64(n), ir.TableMessageBitsRequired(0, max))
			w.align()
			for range n {
				w.put('a', 8)
			}
		}
	}
	// counted writes a count of n elements of byte kind under an announced
	// [min, max]; keyed fields name a key reference before each element
	counted := func(name string, max int64, n int) ([]ir.TableVocabularyEntry, func(w *boundBits, refBits int)) {
		f := field(name)
		e := ir.TableFieldEntry(f)
		e.Shape.Min = 0
		e.Shape.Max = max
		entries := []ir.TableVocabularyEntry{e}
		keyed := f.KeyEnumRef != nil
		if keyed {
			for i := range f.KeyEnumRef.Variants {
				entries = append(entries, ir.TableVocabularyEntry{Id: ir.TableWireId(f.KeyEnumRef.VariantWireName(i))})
			}
		}
		return entries, func(w *boundBits, refBits int) {
			w.put(1, refBits)
			w.put(uint64(n), ir.TableMessageBitsRequired(0, max))
			if !keyed {
				w.align()
			}
			for i := range n {
				if keyed {
					w.put(uint64(i+2), refBits)
				}
				w.put(1, 8)
			}
		}
	}

	type bodyFn = func(w *boundBits, refBits int)
	for _, tc := range []struct {
		name   string
		build  func() ([]ir.TableVocabularyEntry, bodyFn)
		reason string // the engine's verdict: "" reads
	}{
		{"string_at_both_bounds", func() ([]ir.TableVocabularyEntry, bodyFn) { return text(15, 8) }, ""},
		{"string_over_reader_bound", func() ([]ir.TableVocabularyEntry, bodyFn) { return text(15, 10) }, tablewire.ReasonLengthOverBound},
		{"string_over_announced_max", func() ([]ir.TableVocabularyEntry, bodyFn) { return text(5, 7) }, tablewire.ReasonLengthOverBound},
		{"bytes_at_both_bounds", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("blob", 15, 8) }, ""},
		{"bytes_over_reader_bound", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("blob", 15, 10) }, tablewire.ReasonLengthOverBound},
		{"bytes_over_announced_max", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("blob", 5, 7) }, tablewire.ReasonCountOverBound},
		{"array_at_both_bounds", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("list", 7, 4) }, ""},
		{"array_over_reader_bound", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("list", 7, 5) }, tablewire.ReasonCountOverBound},
		{"array_over_announced_max", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("list", 2, 3) }, tablewire.ReasonCountOverBound},
		{"fixed_array_over_reader_bound", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("pair", 7, 3) }, tablewire.ReasonCountOverBound},
		{"keyed_at_announced_max", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("keyed", 2, 2) }, ""},
		{"keyed_over_announced_max", func() ([]ir.TableVocabularyEntry, bodyFn) { return counted("keyed", 2, 3) }, tablewire.ReasonCountOverBound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries, body := tc.build()
			announce := boundAnnouncement(entries)
			var vrep tabletext.Report
			v := &tablewire.Vocabulary{}
			if err := v.AnnounceRead(announce, &vrep); err != nil {
				t.Fatalf("the engine refused the announcement: %v", err)
			}
			var w boundBits
			w.put(2, 8) // form byte
			w.put(0, 8) // M - 1
			body(&w, v.RefBits())
			w.put(0, v.RefBits())
			batch := w.b

			var rep tabletext.Report
			_, ok, derr := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(def)}, batch, v, &rep)
			if derr != nil && !tablewire.ReadRefusal(derr) {
				t.Fatalf("engine DecodeMessages returned an error: %v", derr)
			}
			if got := tablewire.RefusalReason(derr); ok != (tc.reason == "") || got != tc.reason {
				t.Fatalf("the engine's verdict moved: ok=%t reason=%q, want reason %q (%v)", ok, got, tc.reason, derr)
			}

			announcePath := filepath.Join(dir, "announce_"+tc.name+".bin")
			if err := os.WriteFile(announcePath, announce, 0o600); err != nil {
				t.Fatal(err)
			}
			probe := fmt.Sprintf(`import { readFileSync } from "node:fs";
import { Msg, TableVocabulary, AnnounceRead, MsgLoadMessages } from "./ProbeTable.js";
const v = new TableVocabulary();
v.Init(new Array(16));
if (!AnnounceRead(v, new Uint8Array(readFileSync(%q)), {})) { throw new Error("announce refused"); }
const rep = {};
const n = MsgLoadMessages([new Msg()], v, new Uint8Array(%s), rep);
console.log("JS loaded=" + (n === 1 && !rep.Damaged) + "|reason=" + (rep.Reason ?? ""));
`, announcePath, jsByteList(batch))
			probePath := filepath.Join(dir, "probe_"+tc.name+".mjs")
			if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := testproc.Command(t, node, probePath)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node probe failed: %v\n%s", err, out)
			}
			want := fmt.Sprintf("loaded=%t|reason=%s", tc.reason == "", tc.reason)
			got := ""
			for line := range strings.SplitSeq(string(out), "\n") {
				if after, ok := strings.CutPrefix(line, "JS "); ok {
					got = after
				}
			}
			if got != want {
				t.Fatalf("JS=%q, want %q (the engine's verdict)\n%s", got, want, out)
			}
		})
	}
}

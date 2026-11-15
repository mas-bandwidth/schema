package mootable_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A message-form integer lands only inside the reader field's own kind
// (docs/SPEC-TABLES.md §3.3, §4). Decoded at the sender's announced packing,
// offset plus base, an int8 holds -128..127, an int32 its 32-bit range, and a
// uint8 0..255. A value outside that kind refuses the read and delivers
// nothing; one inside it lands exactly. The verdict is the contract
// (docs/rulings.json refusal-verdict-only): never a reason name.

// Each field declares a default of 1, so no batch below writes a field at its
// default: the kind's own ends and the planted 0 are all values a conforming
// writer writes, never elides (docs/SPEC-TABLES.md §3.3 (a)).
const intDomainSchema = "package probe\n\nfixed table Ints\n{\n    i int8 = 1\n    j int32 = 1\n    u uint8 = 1\n}\n"

// intDomainEnd is one field's announced ranged packing and the offset the
// batch writes at that width. The value the read reconstructs is base plus
// the offset.
type intDomainEnd struct {
	base   *big.Int
	bits   int64
	offset uint64
}

type intDomainCase struct {
	name   string
	ends   map[string]intDomainEnd
	refuse bool
	want   string // "refuse", or "accept" and the three values
}

type intDomainField struct {
	name string
	slot int
	kind uint8
}

type intDomainPart struct {
	slot   int
	bits   int64
	offset uint64
}

// intDomainCases are the reader's own kind, re-declared ranged. Three batches
// reconstruct a value outside the kind: one past its maximum, one below its
// minimum, and a sum past 2^53. Two batches land on the kind's own ends.
// The uint8 below zero is the 64-bit pattern of that step (every bit set):
// an unsigned base cannot spell a negative, and that pattern is outside 0..255.
func intDomainCases() []intDomainCase {
	two53 := new(big.Int).Lsh(big.NewInt(1), 53)
	under := new(big.Int).SetUint64(math.MaxUint64)
	end := func(base int64, offset uint64) intDomainEnd {
		return intDomainEnd{base: big.NewInt(base), bits: 1, offset: offset}
	}
	wide := func(base *big.Int, offset uint64) intDomainEnd {
		return intDomainEnd{base: base, bits: 1, offset: offset}
	}
	return []intDomainCase{
		{
			name:   "one_past_maximum",
			refuse: true,
			ends: map[string]intDomainEnd{
				"i": end(127, 1),        // 128
				"j": end(2147483647, 1), // 2147483648
				"u": end(255, 1),        // 256
			},
		},
		{
			name:   "below_minimum",
			refuse: true,
			ends: map[string]intDomainEnd{
				"i": end(-129, 0),
				"j": end(-2147483649, 0),
				"u": wide(under, 0),
			},
		},
		{
			name:   "far_outside",
			refuse: true,
			ends: map[string]intDomainEnd{
				"i": wide(two53, 1), // 2^53 + 1
				"j": wide(two53, 1),
				"u": wide(two53, 1),
			},
		},
		{
			name: "kind_minimum",
			ends: map[string]intDomainEnd{
				"i": end(-128, 0),
				"j": end(-2147483648, 0),
				"u": end(0, 0),
			},
			want: "accept -128 -2147483648 0",
		},
		{
			name: "kind_maximum",
			ends: map[string]intDomainEnd{
				"i": end(127, 0),
				"j": end(2147483647, 0),
				"u": end(255, 0),
			},
			want: "accept 127 2147483647 255",
		},
	}
}

func intDomainLEB(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// intDomainAnnounce is the unit's own announcement shape (a form-1 file: the
// build version, then the vocabulary) over the entries the caller names.
func intDomainAnnounce(version uint64, entries []ir.TableVocabularyEntry) []byte {
	vocabulary := ir.TableVocabularyBytes(entries)
	out := []byte{ir.TableWireForm, 1, byte(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, version)
	out = append(out, 2, byte(ir.TableKindArray))
	body := append([]byte{byte(ir.TableKindU8)}, intDomainLEB(uint64(len(vocabulary)))...)
	body = append(body, vocabulary...)
	out = append(out, intDomainLEB(uint64(len(body)))...)
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

type intDomainBits struct {
	b []byte
	n int
}

func (w *intDomainBits) put(v uint64, n int) {
	for i := range n {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>uint(i)&1 == 1 {
			w.b[w.n/8] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

func (w *intDomainBits) align() {
	for w.n%8 != 0 {
		w.put(0, 1)
	}
}

// intDomainBatch is one body: the count, each field's reference and offset,
// the terminating reference, and the pad.
func intDomainBatch(refBits int, parts []intDomainPart) []byte {
	var w intDomainBits
	w.put(0, 8)
	for _, p := range parts {
		w.put(uint64(p.slot), refBits)
		w.put(p.offset, int(p.bits))
	}
	w.put(0, refBits)
	w.align()
	return append([]byte{ir.TableWireMessageForm}, w.b...)
}

func intDomainLayout(t *testing.T, u *ir.Unit) []intDomainField {
	t.Helper()
	st := u.Tables["Ints"]
	if st == nil {
		t.Fatal("Ints is not a table")
	}
	byID := map[uint64]string{}
	for _, f := range st.Fields {
		byID[ir.TableFieldWireId(f)] = f.Name
	}
	entries := tablewire.MessageVocabulary(u)
	if len(entries) != len(st.Fields) {
		t.Fatalf("the vocabulary has %d entries, Ints has %d fields", len(entries), len(st.Fields))
	}
	var fields []intDomainField
	seen := map[string]bool{}
	for i, e := range entries {
		name := byID[e.Id]
		if name == "" {
			t.Fatalf("vocabulary entry %d is not a field of Ints", i)
		}
		seen[name] = true
		fields = append(fields, intDomainField{name: name, slot: i + 1, kind: e.Kind})
	}
	for _, f := range st.Fields {
		if !seen[f.Name] {
			t.Fatalf("field %s is not in the vocabulary", f.Name)
		}
	}
	return fields
}

func intDomainWire(t *testing.T, u *ir.Unit, fields []intDomainField, ends map[string]intDomainEnd) (ann, msg []byte) {
	t.Helper()
	entries := append([]ir.TableVocabularyEntry(nil), tablewire.MessageVocabulary(u)...)
	at := map[string]int{}
	for i, f := range fields {
		at[f.name] = i
	}
	var parts []intDomainPart
	for _, f := range fields {
		e, ok := ends[f.name]
		if !ok || e.base == nil || e.bits <= 0 || (e.bits < 64 && e.offset>>uint(e.bits) != 0) {
			t.Fatalf("%s: the ranged end does not fit", f.name)
		}
		entries[at[f.name]].Shape = ir.TableMessageShape{
			Packing: ir.TableMessageRanged,
			Bits:    e.bits,
			Base:    e.base,
		}
		parts = append(parts, intDomainPart{slot: f.slot, bits: e.bits, offset: e.offset})
	}
	ann = intDomainAnnounce(ir.BuildVersion(u), entries)
	msg = intDomainBatch(ir.TableMessageRefBits(len(entries)), parts)
	return ann, msg
}

func intDomainProbe(g *generatedUnit) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + `
local vocabulary = MS.NewVocabulary()
local report: any = {}
if not MS.AnnounceRead(vocabulary, inputs.ann, report) then
	print("ANNOUNCE")
	return
end
local n, values = MS.LoadMessages(vocabulary, "Ints", inputs.message, 256, report)
if n < 1 or report.damaged or report.refused or report.reason ~= nil then
	if #values ~= 0 then
		print("VALUES")
		return
	end
	print("refuse")
	return
end
local v = values[1]
print(string.format("accept %d %d %d", v.I, v.J, v.U))
`
}

func intDomainRun(t *testing.T, g *generatedUnit, ann, msg []byte) string {
	t.Helper()
	return g.run(t, intDomainProbe(g), map[string][]byte{"ann": ann, "message": msg})
}

func intDomainUnit(t *testing.T) (*generatedUnit, *ir.Unit, []intDomainField) {
	t.Helper()
	g := generateFiles(t, map[string]string{"Ints.schema": intDomainSchema})
	own := tablewire.Announce(g.unit)
	if built := intDomainAnnounce(ir.BuildVersion(g.unit), tablewire.MessageVocabulary(g.unit)); !bytes.Equal(own, built) {
		t.Fatalf("rebuilding the unit's announcement changed it (%d bytes against %d)", len(built), len(own))
	}
	return g, g.unit, intDomainLayout(t, g.unit)
}

func TestMooMessageIntegerHoldsTheReadersKind(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g, u, fields := intDomainUnit(t)
	for _, c := range intDomainCases() {
		ann, msg := intDomainWire(t, u, fields, c.ends)
		got := intDomainRun(t, g, ann, msg)
		want := "refuse"
		if !c.refuse {
			want = c.want
		}
		if got != want {
			t.Errorf("%s: moo %q, want %q", c.name, got, want)
		}
	}
}

// The domain refusal, and the line a planted substitution of 0 replaces.
// The product source does not carry that substitution.
const (
	intDomainAnchor = "\t\tif n == nil or n < klo or n > khi then\n\t\t\treturn nil, refuseRead(t, \"out_of_range\")\n\t\tend\n\t\traw = n\n"
	intDomainPlant  = "\t\tlocal held = n or 0\n\t\tif held < klo or held > khi then\n\t\t\treturn nil, refuseRead(t, \"out_of_range\")\n\t\tend\n\t\traw = held\n"
)

// TestMooMessageIntegerHoldsTheReadersKindNegativeControl restores the
// substitution of 0 for a value no moo number holds, in an emitted copy of
// table_message.moo. The far-outside row must then land: 0 is inside every
// kind in the batch, so the domain refusal is what holds that row.
func TestMooMessageIntegerHoldsTheReadersKindNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g, u, fields := intDomainUnit(t)
	src := string(g.files["table_message.moo"])
	if strings.Count(src, intDomainAnchor) != 1 {
		t.Fatal("the kind-domain anchor is not in table_message.moo exactly once")
	}
	planted := []byte(strings.Replace(src, intDomainAnchor, intDomainPlant, 1))
	if bytes.Contains(planted, []byte("raw = n or 0")) {
		t.Fatal("the plant left the product substitution in place")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_message.moo"), planted, 0o644); err != nil {
		t.Fatal(err)
	}
	var far intDomainCase
	for _, c := range intDomainCases() {
		if c.name == "far_outside" {
			far = c
			break
		}
	}
	if far.name == "" {
		t.Fatal("the far-outside row is missing")
	}
	ann, msg := intDomainWire(t, u, fields, far.ends)
	if got := intDomainRun(t, g, ann, msg); got != "accept 0 0 0" {
		t.Fatalf("the far-outside row did not land 0 once the substitution was restored: %s", got)
	}
}

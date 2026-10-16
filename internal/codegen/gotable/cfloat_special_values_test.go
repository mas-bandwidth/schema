package gotable

// A COMPRESSED FLOAT'S SPECIAL VALUES, IN EVERY FORM (docs/SPEC-TABLES.md
// §3.3, "A COMPRESSED FLOAT'S SPECIAL VALUES"). The rule under test is one rule
// for four forms: a value written as NaN never lands as NaN — it clamps to
// `min` and counts wherever the form clamps (the message WRITER, the fixed and
// variable READS), and the text form, which has no spelling for it, refuses it
// at write; +Inf and -Inf clamp to `max` and `min` and count the same way, or
// are refused by the text form the same way; -0 lands exactly as +0 does, the
// value 0 (in the message form, the INDEX OF 0 for that range), with no sign
// bit and no count.
//
// Two ranges, because a range that starts at 0 cannot tell "the index of 0"
// from "index 0": A is [0, 1] at 0.01 (count 100, 0 is index 0) and B is
// [-1, 1] at 0.01 (count 200, 0 is index 100).
//
// Every assertion is on the RAW BITS: -0 and +0 compare equal under `==`, and
// the question is exactly whether the sign bit moved.

import (
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE QUANTIZER ITSELF, through the Go oracle's ir.TableMessageQuantize and
// TableMessageDequantize: the index each special value takes and the float that
// index names, on both ranges. This is the table the spec states.
func TestCompressedFloatSpecialValuesQuantizer(t *testing.T) {
	type row struct {
		name  string
		in    uint32
		index uint32
		out   uint32
	}
	for _, shape := range []struct {
		name  string
		s     ir.TableMessageShape
		count uint32
		rows  []row
	}{
		{"range_0_1", ir.TableMessageShape{Packing: ir.TableMessageQuantized, QMin: 0, QMax: 1, QRes: 0.01}, 100, []row{
			{"nan_quiet", 0x7FC00000, 0, 0x00000000},
			{"nan_payload", 0x7F800001, 0, 0x00000000},
			{"minus_zero", 0x80000000, 0, 0x00000000},
			{"plus_zero", 0x00000000, 0, 0x00000000},
			{"plus_inf", 0x7F800000, 100, 0x3F800000},
			{"minus_inf", 0xFF800000, 0, 0x00000000},
		}},
		{"range_minus1_1", ir.TableMessageShape{Packing: ir.TableMessageQuantized, QMin: -1, QMax: 1, QRes: 0.01}, 200, []row{
			{"nan_quiet", 0x7FC00000, 0, 0xBF800000},
			{"nan_payload", 0x7F800001, 0, 0xBF800000},
			// -0 and +0 take the INDEX OF 0 for this range, which is 100, and
			// that index decodes to +0 — not index 0, which is min.
			{"minus_zero", 0x80000000, 100, 0x00000000},
			{"plus_zero", 0x00000000, 100, 0x00000000},
			{"plus_inf", 0x7F800000, 200, 0x3F800000},
			{"minus_inf", 0xFF800000, 0, 0xBF800000},
		}},
	} {
		count, _, ok := ir.TableMessageQuantization(shape.s)
		if !ok || count != shape.count {
			t.Fatalf("%s: count %d ok %v, want %d", shape.name, count, ok, shape.count)
		}
		for _, r := range shape.rows {
			index := ir.TableMessageQuantize(shape.s, math.Float32frombits(r.in))
			out := math.Float32bits(ir.TableMessageDequantize(shape.s, index))
			if index != r.index || out != r.out {
				t.Errorf("%s %s: input %#08x -> index %d -> %#08x, want index %d -> %#08x", shape.name, r.name, r.in, index, out, r.index, r.out)
			}
		}
	}
}

const cfSpecial = `package cfspecial

fixed table Reading
{
    a float32 | min = 0, max = 1, resolution = 0.01
    b float32 | min = -1, max = 1, resolution = 0.01
}
`

// THE FOUR FORMS, through the GENERATED Go: one unit, compiled once, every
// input written into both fields and read back by the form's own reader.
const cfSpecialProbe = `package probe

import ("bytes"; "math"; "testing")

type cfCase struct {
	name   string
	in     uint32
	a, b   uint32 // the bits that land in A [0, 1] and B [-1, 1]
	counts int32  // the clamps the form counts across A and B
	text   bool   // the text form writes it at all
}

var cfCases = []cfCase{
	{"nan", 0x7FC00000, 0x00000000, 0xBF800000, 2, false},
	{"minus_zero", 0x80000000, 0x00000000, 0x00000000, 0, true},
	{"plus_zero", 0x00000000, 0x00000000, 0x00000000, 0, true},
	{"plus_inf", 0x7F800000, 0x3F800000, 0x3F800000, 2, false},
	{"minus_inf", 0xFF800000, 0x00000000, 0xBF800000, 2, false},
}

func cfValue(bits uint32) Reading {
	var r Reading
	ReadingReset(&r)
	r.A = math.Float32frombits(bits)
	r.B = math.Float32frombits(bits)
	return r
}

func cfLanded(t *testing.T, form string, c cfCase, got Reading) {
	t.Helper()
	if a := math.Float32bits(got.A); a != c.a {
		t.Errorf("%s %s: A [0, 1] landed %#08x, want %#08x", form, c.name, a, c.a)
	}
	if b := math.Float32bits(got.B); b != c.b {
		t.Errorf("%s %s: B [-1, 1] landed %#08x, want %#08x", form, c.name, b, c.b)
	}
}

// THE MESSAGE FORM: the writer quantizes and clamps, so the WRITER counts
// (SaveMessages's report); the reader sees only an index inside [0, count]
// and counts nothing.
func TestMessage(t *testing.T) {
	for _, c := range cfCases {
		v := cfValue(c.in)
		values := []*Reading{&v}
		buf := make([]byte, ReadingMeasureMessages(values, nil))
		var wrep TableReport
		if ReadingSaveMessages(values, buf, &wrep) != int64(len(buf)) {
			t.Fatalf("message %s: save", c.name)
		}
		if wrep.Clamped != c.counts {
			t.Errorf("message %s: the writer counted %d clamps, want %d", c.name, wrep.Clamped, c.counts)
		}
		ann := make([]byte, AnnounceMeasure())
		Announce(ann)
		var storage [64]TableMessageEntry
		var vocab TableVocabulary
		vocab.Init(storage[:])
		var arep TableReport
		if !AnnounceRead(&vocab, ann, &arep) {
			t.Fatalf("announce read: %+v", arep)
		}
		got := make([]Reading, 1)
		var rrep TableReport
		if n, ok := ReadingLoadMessages(got, &vocab, buf, &rrep); n != 1 || !ok {
			t.Fatalf("message %s: load n=%d ok=%v %+v", c.name, n, ok, rrep)
		}
		if rrep.Clamped != 0 {
			t.Errorf("message %s: the reader counted %d clamps, want 0", c.name, rrep.Clamped)
		}
		cfLanded(t, "message", c, got[0])
	}
}

// THE MESSAGE WRITER'S INDEX, read straight off the bits it puts: -0 takes the
// INDEX OF 0 for the range (100 on [-1, 1]), exactly as +0 does. The body
// above cannot show it, because a field equal to its default is not written.
func TestMessageIndex(t *testing.T) {
	shape := TableMessageShape{Kind: 10, Packing: 2, Bits: 8, QMin: -1, QDelta: 2, QCount: 200}
	for _, c := range []struct {
		name  string
		in    uint32
		index uint64
		count int32
	}{
		{"nan", 0x7FC00000, 0, 1},
		{"minus_zero", 0x80000000, 100, 0},
		{"plus_zero", 0x00000000, 100, 0},
		{"plus_inf", 0x7F800000, 200, 1},
		{"minus_inf", 0xFF800000, 0, 1},
	} {
		var buf [8]byte
		w := TableBitWriter{Buffer: buf[:]}
		var clamped int32
		tableMessageWriteScalar(&w, &clamped, shape, uint64(c.in), 0)
		r := TableBitReader{Buffer: buf[:]}
		index, _ := r.Get(8)
		if index != c.index || clamped != c.count {
			t.Errorf("message index %s: index %d counted %d, want index %d counted %d", c.name, index, clamped, c.index, c.count)
		}
	}
}

// THE FIXED FORM: forge the hostile bits into a valid record because the Go
// writer refuses values that its reader would clamp. The READ counts.
func TestFixed(t *testing.T) {
	for _, c := range cfCases {
		v := cfValue(0)
		buf := make([]byte, ReadingFixedMeasure(1))
		if c.counts > 0 && ReadingFixedSave([]Reading{cfValue(c.in)}, buf) >= 0 {
			t.Fatalf("fixed %s: writer accepted a value its reader clamps", c.name)
		}
		if ReadingFixedSave([]Reading{v}, buf) < 0 {
			t.Fatalf("fixed %s: save", c.name)
		}
		record := buf[len(buf)-ReadingFixedRecordBytes:]
		tableFixedPut32(record[8:], c.in)
		tableFixedPut32(record[12:], c.in)
		var got [1]Reading
		var plan [64]TableFixedEntry
		var rep TableReport
		if n := ReadingFixedLoad(got[:], buf, plan[:], &rep); n != 1 {
			t.Fatalf("fixed %s: load n=%d why=%q", c.name, n, rep.Reason)
		}
		if rep.Clamped != c.counts {
			t.Errorf("fixed %s: the reader counted %d clamps, want %d", c.name, rep.Clamped, c.counts)
		}
		cfLanded(t, "fixed", c, got[0])
	}
}

// THE VARIABLE FORM: replace two known finite field payloads with hostile
// bits. The writer's refusal cannot be used to create a reader test record.
func TestVariable(t *testing.T) {
	for _, c := range cfCases {
		v := cfValue(0x3F000000) // 0.5, present in both fields
		buf := make([]byte, ReadingMeasure(&v))
		if c.counts > 0 {
			invalid := cfValue(c.in)
			if ReadingSave(&invalid, buf) >= 0 {
				t.Fatalf("variable %s: writer accepted a value its reader clamps", c.name)
			}
		}
		if ReadingSave(&v, buf) != int64(len(buf)) {
			t.Fatalf("variable %s: save", c.name)
		}
		var from, to [4]byte
		tableFixedPut32(from[:], 0x3F000000)
		tableFixedPut32(to[:], c.in)
		replaced := 0
		for i := 0; i+4 <= len(buf); i++ {
			if bytes.Equal(buf[i:i+4], from[:]) {
				copy(buf[i:i+4], to[:])
				replaced++
				i += 3
			}
		}
		if replaced != 2 {
			t.Fatalf("variable %s: patched %d payloads, want 2", c.name, replaced)
		}
		var got Reading
		var rep TableReport
		if !ReadingLoad(&got, buf, &rep) {
			t.Fatalf("variable %s: load %+v", c.name, rep)
		}
		if rep.Clamped != c.counts {
			t.Errorf("variable %s: the reader counted %d clamps, want %d", c.name, rep.Clamped, c.counts)
		}
		cfLanded(t, "variable", c, got)
	}
}

// THE TEXT FORM: JSON has no spelling for a NaN or an infinity, so the WRITER
// REFUSES each (measure and write both -1) and nothing lands; a -0 is spelled,
// and the READ lands it as +0, uncounted. A text spelling -0 by hand reads the
// same way.
func TestText(t *testing.T) {
	for _, c := range cfCases {
		v := cfValue(c.in)
		n := ReadingToJsonMeasure(&v)
		if !c.text {
			if n != -1 {
				t.Errorf("text %s: the writer measured %d, want the refusal -1", c.name, n)
			}
			var sink [256]byte
			if got := ReadingToJson(&v, sink[:]); got != -1 {
				t.Errorf("text %s: ReadingToJson wrote %d, want the refusal -1", c.name, got)
			}
			// Pin each range independently so a nonfinite value in B [-1, 1] is
			// refused by both ReadingToJsonMeasure and ReadingToJson even when
			// A [0, 1] is finite, and vice versa.
			var onlyA, onlyB Reading
			ReadingReset(&onlyA)
			onlyA.A = math.Float32frombits(c.in)
			if got := ReadingToJsonMeasure(&onlyA); got != -1 {
				t.Errorf("text %s A [0, 1]: ReadingToJsonMeasure = %d, want -1", c.name, got)
			}
			if got := ReadingToJson(&onlyA, sink[:]); got != -1 {
				t.Errorf("text %s A [0, 1]: ReadingToJson = %d, want -1", c.name, got)
			}
			ReadingReset(&onlyB)
			onlyB.B = math.Float32frombits(c.in)
			if got := ReadingToJsonMeasure(&onlyB); got != -1 {
				t.Errorf("text %s B [-1, 1]: ReadingToJsonMeasure = %d, want -1", c.name, got)
			}
			if got := ReadingToJson(&onlyB, sink[:]); got != -1 {
				t.Errorf("text %s B [-1, 1]: ReadingToJson = %d, want -1", c.name, got)
			}
			continue
		}
		if n < 0 {
			t.Fatalf("text %s: the writer refused a finite value", c.name)
		}
		buf := make([]byte, n)
		if ReadingToJson(&v, buf) != n {
			t.Fatalf("text %s: write", c.name)
		}
		var got Reading
		var rep TableReport
		if !ReadingFromJson(&got, buf, &rep) {
			t.Fatalf("text %s: read %+v of %s", c.name, rep, buf)
		}
		if rep.Clamped != c.counts || rep.KindMismatch != 0 {
			t.Errorf("text %s: clamped %d kind_mismatch %d, want %d and 0", c.name, rep.Clamped, rep.KindMismatch, c.counts)
		}
		cfLanded(t, "text", c, got)
	}
	var got Reading
	var rep TableReport
	if !ReadingFromJson(&got, []byte("{\"a\": -0, \"b\": -0.0}\n"), &rep) {
		t.Fatalf("text -0 by hand: %+v", rep)
	}
	cfLanded(t, "text", cfCases[1], got)
}
`

func TestCompressedFloatSpecialValuesEveryForm(t *testing.T) {
	cfRuntime(t)
	out, err := runVersionProbe(t, cfSpecial, nil, cfSpecialProbe)
	if err != nil {
		t.Fatalf("a compressed float's special values did not land by the one rule in every form: %v\n%s", err, out)
	}
}

package tablewire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The schema review of 2026-11-21 (TABLEWIRE-2, -3, -4 and the bytes(N) Min
// cell of the message cross-leg tables): one body must get ONE verdict whether
// the reader names the field or skips it, and the verdict is the spec's
// (docs/SPEC-TABLES.md §3.3). Every case below builds the announced entry and
// the body by hand and reads it twice: once into a reader that declares the
// field (the NAMED path) and once into a reader whose vocabulary names the
// same payload under an id it does not declare (the SKIP path).

// namedSkipCase is one announced entry for a reader field `x`, one body
// payload for it, and the verdict both paths must give.
type namedSkipCase struct {
	name   string
	reader string // the reader's declaration of x
	entry  func(f *ir.Field) []ir.TableVocabularyEntry
	write  func(w *bitWriter, refBits int)
	bad    bool
}

// namedSkipRead reads one batch of one body carrying `x` and answers the
// verdict. skip announces x under an id the reader does not declare.
func namedSkipRead(t *testing.T, c namedSkipCase, skip bool) (bool, *tabletext.Report, *tabletext.Instance) {
	t.Helper()
	m := covModel(t, "package namedskip\nenum Mode { A, B, C }\nfixed table R { "+c.reader+" }\n")
	root := m.Lookup("R")
	entries := c.entry(root.Fields[0])
	if skip {
		entries[0].Id = 0x5a5a5a5a00000001
	}
	// buildVersion 0 is not the reader's own, so an unknown id is the
	// ordinary evolution case, skipped and counted
	v := &Vocabulary{entries: entries, announced: true}
	refBits := v.RefBits()
	var w bitWriter
	w.put(0, 8) // M - 1
	w.put(1, refBits)
	c.write(&w, refBits)
	w.put(0, refBits)
	w.align()
	data := append([]byte{ir.TableWireMessageForm}, w.b...)
	inst := m.New(root)
	var report tabletext.Report
	ok, err := DecodeMessage(m, inst, data, v, &report)
	if ok != (err == nil) {
		t.Fatalf("ok=%v with err=%v", ok, err)
	}
	return ok, &report, inst
}

func msgRangedShape(bits int64, base *big.Int) ir.TableMessageShape {
	return ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: bits, Base: base}
}

func msgPow2(n uint) *big.Int { return new(big.Int).Lsh(big.NewInt(1), n) }

// namedSkipCases are the rule's inputs, each must-refuse case beside its
// lawful twin on the same announcement.
func namedSkipCases() []namedSkipCase {
	entry := func(kind uint8, shape ir.TableMessageShape, more ...ir.TableVocabularyEntry) func(f *ir.Field) []ir.TableVocabularyEntry {
		return func(f *ir.Field) []ir.TableVocabularyEntry {
			return append([]ir.TableVocabularyEntry{{Id: ir.TableFieldWireId(f), Kind: kind, Shape: shape}}, more...)
		}
	}
	keyedEntry := func(f *ir.Field) []ir.TableVocabularyEntry {
		out := []ir.TableVocabularyEntry{{Id: ir.TableFieldWireId(f), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindU8, Inner: &ir.TableMessageShape{}}}}
		for i := range f.KeyEnumRef.Variants {
			out = append(out, ir.TableVocabularyEntry{Id: ir.TableWireId(f.KeyEnumRef.VariantWireName(i))})
		}
		return out
	}
	u64Base := new(big.Int).Sub(msgPow2(64), big.NewInt(616))   // 2^64 - 616
	i64Base := new(big.Int).Sub(msgPow2(63), big.NewInt(101))   // 2^63 - 101
	u128Top := new(big.Int).Sub(msgPow2(128), big.NewInt(1))    // 2^128 - 1
	u128Lawful := new(big.Int).Sub(msgPow2(128), big.NewInt(2)) // 2^128 - 2
	u8Bytes := func(min, max int64) ir.TableMessageShape {
		return ir.TableMessageShape{Min: min, Max: max, Elem: ir.TableKindU8, Inner: &ir.TableMessageShape{}}
	}
	return []namedSkipCase{
		// TABLEWIRE-3 (a): a range-free uint64 reader, a writer announcing
		// kind 9 ranged at base 2^64 - 616 over ten bits
		{name: "u64 base+offset past the kind", reader: "x uint64", entry: entry(ir.TableKindU64, msgRangedShape(10, u64Base)),
			write: func(w *bitWriter, _ int) { w.put(1023, 10) }, bad: true},
		{name: "u64 base+offset at the kind's top", reader: "x uint64", entry: entry(ir.TableKindU64, msgRangedShape(10, u64Base)),
			write: func(w *bitWriter, _ int) { w.put(615, 10) }},
		// TABLEWIRE-3 (a), signed: kind 5 ranged at base 2^63 - 101 over seven bits
		{name: "i64 base+offset past the kind", reader: "x int64", entry: entry(ir.TableKindI64, msgRangedShape(7, i64Base)),
			write: func(w *bitWriter, _ int) { w.put(127, 7) }, bad: true},
		{name: "i64 base+offset at the kind's top", reader: "x int64", entry: entry(ir.TableKindI64, msgRangedShape(7, i64Base)),
			write: func(w *bitWriter, _ int) { w.put(100, 7) }},
		// TABLEWIRE-3 (b): `uint8 | min = 200, max = 250` read by a lawful
		// widened uint16: 200 + 63 is a value the announced kind cannot hold
		{name: "u8 widened to u16, sum past the announced kind", reader: "x uint16", entry: entry(ir.TableKindU8, msgRangedShape(6, big.NewInt(200))),
			write: func(w *bitWriter, _ int) { w.put(63, 6) }, bad: true},
		{name: "u8 widened to u16, sum at the announced kind's top", reader: "x uint16", entry: entry(ir.TableKindU8, msgRangedShape(6, big.NewInt(200))),
			write: func(w *bitWriter, _ int) { w.put(55, 6) }},
		// TABLEWIRE-3, the wide branch: a bare uint128 declares no range, so
		// only the announced kind's domain can refuse 1 + (2^128 - 1)
		{name: "u128 base+offset past the kind", reader: "x uint128", entry: entry(ir.TableKindU128, msgRangedShape(128, big.NewInt(1))),
			write: func(w *bitWriter, _ int) { w.putBig(u128Top, 128) }, bad: true},
		{name: "u128 base+offset at the kind's top", reader: "x uint128", entry: entry(ir.TableKindU128, msgRangedShape(128, big.NewInt(1))),
			write: func(w *bitWriter, _ int) { w.putBig(u128Lawful, 128) }},
		// TABLEWIRE-4: a count above the announced max on a positional array
		// whose reader bound is wider
		{name: "array count above the announced max", reader: "x [..10]uint16", entry: entry(ir.TableKindArray, ir.TableMessageShape{Min: 0, Max: 5, Elem: ir.TableKindU16, Inner: &ir.TableMessageShape{}}),
			write: func(w *bitWriter, _ int) {
				w.put(7, 3)
				for range 7 {
					w.put(1, 16)
				}
			}, bad: true},
		{name: "array count at the announced max", reader: "x [..10]uint16", entry: entry(ir.TableKindArray, ir.TableMessageShape{Min: 0, Max: 5, Elem: ir.TableKindU16, Inner: &ir.TableMessageShape{}}),
			write: func(w *bitWriter, _ int) {
				w.put(5, 3)
				for range 5 {
					w.put(1, 16)
				}
			}},
		// TABLEWIRE-4: the same on an enum-keyed array
		{name: "keyed count above the announced max", reader: "x [Mode]uint8", entry: keyedEntry,
			write: func(w *bitWriter, refBits int) {
				w.put(3, 2)
				for k := 2; k <= 4; k++ {
					w.put(uint64(k), refBits)
					w.put(1, 8)
				}
			}, bad: true},
		{name: "keyed count at the announced max", reader: "x [Mode]uint8", entry: keyedEntry,
			write: func(w *bitWriter, refBits int) {
				w.put(2, 2)
				for k := 2; k <= 3; k++ {
					w.put(uint64(k), refBits)
					w.put(1, 8)
				}
			}},
		// bytes(N) is an array of element kind 6: its count rides as
		// count - min, so a `[2..8]uint8` writer's five bytes are 3, align,
		// five bytes
		{name: "bytes under an announced min", reader: "x bytes(8)", entry: entry(ir.TableKindArray, u8Bytes(2, 8)),
			write: func(w *bitWriter, _ int) {
				w.put(3, 3)
				w.align()
				w.bytes([]byte{1, 2, 3, 4, 5})
			}},
		{name: "bytes from a fixed [4]uint8 writer", reader: "x bytes(8)", entry: entry(ir.TableKindArray, u8Bytes(4, 4)),
			write: func(w *bitWriter, _ int) {
				w.align()
				w.bytes([]byte{1, 2, 3, 4})
			}},
		{name: "bytes count above the announced max", reader: "x bytes(8)", entry: entry(ir.TableKindArray, u8Bytes(0, 5)),
			write: func(w *bitWriter, _ int) {
				w.put(7, 3)
				w.align()
				w.bytes([]byte{1, 2, 3, 4, 5, 6, 7})
			}, bad: true},
		{name: "bytes count at the announced max", reader: "x bytes(8)", entry: entry(ir.TableKindArray, u8Bytes(0, 5)),
			write: func(w *bitWriter, _ int) {
				w.put(5, 3)
				w.align()
				w.bytes([]byte{1, 2, 3, 4, 5})
			}},
	}
}

// TestMessageNamedAndSkipOneVerdict holds both paths to one verdict on every
// case, and that verdict to the spec's.
func TestMessageNamedAndSkipOneVerdict(t *testing.T) {
	t.Parallel()
	for _, c := range namedSkipCases() {
		for _, skip := range []bool{false, true} {
			path := "named"
			if skip {
				path = "skip"
			}
			t.Run(c.name+"/"+path, func(t *testing.T) {
				t.Parallel()
				ok, report, _ := namedSkipRead(t, c, skip)
				if c.bad {
					if ok || !report.Refused {
						t.Fatalf("the %s read accepted what the spec refuses: ok=%v report=%+v", path, ok, report)
					}
					return
				}
				if !ok || report.Refused {
					t.Fatalf("the %s read refused a lawful body: report=%+v", path, report)
				}
				if skip && report.Unknown != 1 {
					t.Fatalf("the skipped field was not counted unknown: report=%+v", report)
				}
			})
		}
	}
}

// TestMessageNamedLawfulValues pins what the lawful twins land as on the named
// path: the reconstruction is exact at the announced kind's very top, and a
// bytes(N) count takes the announced min back.
func TestMessageNamedLawfulValues(t *testing.T) {
	t.Parallel()
	want := map[string]func(t *testing.T, x *tabletext.Field){
		"u64 base+offset at the kind's top": func(t *testing.T, x *tabletext.Field) {
			if x.Cell.U != math.MaxUint64 {
				t.Fatalf("x = %d, want 2^64 - 1", x.Cell.U)
			}
		},
		"i64 base+offset at the kind's top": func(t *testing.T, x *tabletext.Field) {
			if x.Cell.I != math.MaxInt64 {
				t.Fatalf("x = %d, want 2^63 - 1", x.Cell.I)
			}
		},
		"u8 widened to u16, sum at the announced kind's top": func(t *testing.T, x *tabletext.Field) {
			if x.Cell.U != 255 {
				t.Fatalf("x = %d, want 255", x.Cell.U)
			}
		},
		"bytes under an announced min": func(t *testing.T, x *tabletext.Field) {
			if string(x.Cell.Str) != "\x01\x02\x03\x04\x05" {
				t.Fatalf("x = %x, want 0102030405", x.Cell.Str)
			}
		},
		"bytes from a fixed [4]uint8 writer": func(t *testing.T, x *tabletext.Field) {
			if string(x.Cell.Str) != "\x01\x02\x03\x04" {
				t.Fatalf("x = %x, want 01020304", x.Cell.Str)
			}
		},
	}
	for _, c := range namedSkipCases() {
		check, ok := want[c.name]
		if !ok {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ok, report, inst := namedSkipRead(t, c, false)
			if !ok {
				t.Fatalf("refused: %+v", report)
			}
			check(t, &inst.Fields[0])
		})
	}
}

// announcementOf is an announcement carrying exactly these entries, built the
// way messageAnnouncement builds a unit's own.
func announcementOf(version uint64, entries []ir.TableVocabularyEntry) []byte {
	vocabulary := ir.TableVocabularyBytes(entries)
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, version)
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{uint8(ir.TableKindU8)}
	body = appendLeb(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = appendLeb(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// distinctEntries is n entries agreeing on id and kind and differing in a
// ranged base, so every pair is distinct and a pairwise scan compares them all.
func distinctEntries(n int) []ir.TableVocabularyEntry {
	out := make([]ir.TableVocabularyEntry, n)
	for i := range out {
		out[i] = ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000001, Kind: ir.TableKindU32, Shape: msgRangedShape(1, big.NewInt(int64(i)))}
	}
	return out
}

// TestMessageAnnounceEntryBoundBeforeTheRest is TABLEWIRE-2's order: the
// entry bound refuses at the entry that passes it, so an entry past the bound
// is never decoded or compared (docs/SPEC-TABLES.md §3.3, "refused by name as
// vocabulary_too_large before an entry is touched"). The third entry repeats
// the first, which the duplicate test would call malformed, and the bound of
// two must answer first.
func TestMessageAnnounceEntryBoundBeforeTheRest(t *testing.T) {
	t.Parallel()
	entries := distinctEntries(2)
	entries = append(entries, entries[0])
	v := &Vocabulary{MaxEntries: 2}
	var report tabletext.Report
	err := v.AnnounceRead(announcementOf(7, entries), &report)
	var refusal *MessageRefusal
	if !errors.As(err, &refusal) || refusal.Reason != ReasonVocabularyTooLarge {
		t.Fatalf("an announcement past the entry bound was answered %v, want vocabulary_too_large", err)
	}
	if v.Announced() {
		t.Fatal("a refused announcement set a vocabulary")
	}
	// the twin at the bound is accepted
	twin := &Vocabulary{MaxEntries: 2}
	if err := twin.AnnounceRead(announcementOf(7, distinctEntries(2)), &tabletext.Report{}); err != nil || !twin.Announced() {
		t.Fatalf("an announcement at the entry bound was refused: %v", err)
	}
}

// TestMessageAnnounceDuplicateTestIsLinear counts the duplicate test's probes
// over vocabularies of growing size, whose entries all share an id and a kind
// (TABLEWIRE-2): the count is one probe an entry, never a pair. A count, not a
// clock: the bound is the work, whatever the machine.
func TestMessageAnnounceDuplicateTestIsLinear(t *testing.T) {
	t.Parallel()
	for _, n := range []int{64, 512, 4096} {
		var ops int
		entries, good, tooLarge := decodeVocabulary(ir.TableVocabularyBytes(distinctEntries(n)), ir.TableVocabularyMaxEntries, &ops)
		if !good || tooLarge || len(entries) != n {
			t.Fatalf("n=%d: good=%v tooLarge=%v entries=%d", n, good, tooLarge, len(entries))
		}
		if ops != n {
			t.Fatalf("n=%d: the duplicate test made %d probes, want %d (one an entry)", n, ops, n)
		}
	}
}

// TestMessageBodyDuplicateTestIsLinear counts the probes the field-id and the
// keyed-slot duplicate tests make over a keyed array of n slots, on the named
// path and on the skip path: one probe a slot and one for the field.
func TestMessageBodyDuplicateTestIsLinear(t *testing.T) {
	t.Parallel()
	for _, n := range []int{16, 128, 512} {
		for _, skip := range []bool{false, true} {
			var variants strings.Builder
			for i := range n {
				if i > 0 {
					variants.WriteString(", ")
				}
				fmt.Fprintf(&variants, "V%d", i)
			}
			m := covModel(t, "package dupops\nenum Big { "+variants.String()+" }\nfixed table R { x [Big]bool }\n")
			root := m.Lookup("R")
			f := root.Fields[0]
			entries := []ir.TableVocabularyEntry{{Id: ir.TableFieldWireId(f), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: int64(n), Elem: ir.TableKindBool, Inner: &ir.TableMessageShape{}}}}
			if skip {
				entries[0].Id = 0x5a5a5a5a00000001
			}
			for i := range n {
				entries = append(entries, ir.TableVocabularyEntry{Id: ir.TableWireId(f.KeyEnumRef.VariantWireName(i))})
			}
			v := &Vocabulary{entries: entries, announced: true}
			refBits := v.RefBits()
			var w bitWriter
			w.put(1, refBits)
			w.put(uint64(n), ir.TableMessageBitsRequired(0, int64(n)))
			for i := range n {
				w.put(uint64(i+2), refBits)
				w.put(1, 1)
			}
			w.put(0, refBits)
			w.align()
			var ops int
			d := &bitDecoder{m: m, v: v, report: &tabletext.Report{}, r: newBitReader(w.b), refBits: refBits, fail: &readFail{}, dupOps: &ops}
			if !d.body(m.New(root)) || d.fail.err != nil {
				t.Fatalf("n=%d skip=%v: the lawful body was refused: %v", n, skip, d.fail.err)
			}
			if ops != n+1 {
				t.Fatalf("n=%d skip=%v: the duplicate tests made %d probes, want %d", n, skip, ops, n+1)
			}
		}
	}
}

// TestMessageQuantizedTopStepAtFloat32 is TABLEWIRE-1's message half: a
// compressed float32 whose max float32 cannot spell exactly (0.1) dequantizes
// its top index to float32(0.1), which is the declared maximum as every
// runtime holds it (SPEC.md §4.6: the range is evaluated at float32). The top
// and the bottom index both read; one index past the top is refused.
func TestMessageQuantizedTopStepAtFloat32(t *testing.T) {
	t.Parallel()
	m := covModel(t, "package quanttop\nfixed table R { x float32 | min = 0, max = 0.1, resolution = 0.01 }\n")
	root := m.Lookup("R")
	entry := ir.TableFieldEntry(root.Fields[0])
	if entry.Shape.Packing != ir.TableMessageQuantized {
		t.Fatalf("the field announces packing %d, want quantized", entry.Shape.Packing)
	}
	count, _, ok := ir.TableMessageQuantization(entry.Shape)
	if !ok {
		t.Fatal("the shape does not derive")
	}
	read := func(index uint64) (bool, float64) {
		v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
		refBits := v.RefBits()
		var w bitWriter
		w.put(0, 8)
		w.put(1, refBits)
		w.put(index, int(entry.Shape.Bits))
		w.put(0, refBits)
		w.align()
		inst := m.New(root)
		ok, _ := DecodeMessage(m, inst, append([]byte{ir.TableWireMessageForm}, w.b...), v, &tabletext.Report{})
		return ok, inst.Fields[0].Cell.F
	}
	if ok, x := read(uint64(count)); !ok || float32(x) != float32(0.1) {
		t.Fatalf("the top index was answered ok=%v x=%v, want float32(0.1)", ok, x)
	}
	if ok, x := read(1); !ok || x <= 0 {
		t.Fatalf("index 1 was answered ok=%v x=%v", ok, x)
	}
	if ok, x := read(0); !ok || x != 0 {
		t.Fatalf("the bottom index was answered ok=%v x=%v, want 0", ok, x)
	}
	if uint64(count)+1 < uint64(1)<<uint(entry.Shape.Bits) {
		if ok, _ := read(uint64(count) + 1); ok {
			t.Fatal("an index past the top was read")
		}
	}
}

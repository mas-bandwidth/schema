package tabledemo

// The schema review of 2026-11-21, the generated Go leg's message form
// (GO-1 and the gotable cells of verify-message.md's Across legs tables): a
// ranged integer is held to its ANNOUNCED kind named and skipped, a union
// FIELD written with arm reference 0 is refused on the skip path too, and the
// duplicate tests of a keyed array and of a skipped body are one look a slot
// and allocate nothing. Each case builds the announcement and the body by
// hand, under a build version that is not this unit's, and reads it with the
// committed generated reader. Unit tests only: no toolchain, no clock.

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type setBits struct {
	b []byte
	n int
}

func (w *setBits) put(v uint64, n int) {
	for i := range n {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		w.b[w.n/8] |= byte(v>>uint(i)&1) << uint(w.n%8)
		w.n++
	}
}

func setLeb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// setAnnouncement is an announcement of exactly these entries under a build
// version that is not this unit's.
func setAnnouncement(entries []ir.TableVocabularyEntry) []byte {
	vocabulary := ir.TableVocabularyBytes(entries)
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, 0x0123456789abcdef)
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{uint8(ir.TableKindU8)}
	body = setLeb(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = setLeb(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// setRead announces the entries and reads one RangedUnsigned body whose fields
// are written by body (after the batch count, before the terminator).
func setRead(t *testing.T, entries []ir.TableVocabularyEntry, body func(w *setBits, refBits int)) (bool, TableReport, *TableVocabulary) {
	t.Helper()
	v := &TableVocabulary{}
	v.Init(make([]TableMessageEntry, len(entries)+1))
	var report TableReport
	if !AnnounceRead(v, setAnnouncement(entries), &report) {
		t.Fatalf("the announcement was refused: %+v", report)
	}
	refBits := int(v.RefBits)
	var w setBits
	w.put(2, 8) // form byte
	w.put(0, 8) // M - 1
	body(&w, refBits)
	w.put(0, refBits)
	values := make([]RangedUnsigned, 1)
	_, ok := RangedUnsignedLoadMessages(values, v, w.b, &report)
	return ok, report, v
}

func setFieldId(t *testing.T, table, field string) uint64 {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range u.Tables[table].Fields {
		if f.Name == field {
			return ir.TableFieldWireId(f)
		}
	}
	t.Fatalf("%s.%s is not declared", table, field)
	return 0
}

const setUnknownId = 0x5a5a5a5a00000001

func setName(i int) ir.TableVocabularyEntry {
	return ir.TableVocabularyEntry{Id: 0x6b6b6b6b00000000 | uint64(i)}
}

// TestGeneratedMessageRangedHeldToAnnouncedKind: `uint8 | min = 200, max = 250`
// announces kind 6 ranged at base 200 over six bits; offset 63 is 263, which
// the announced kind cannot hold. A widened uint16 reader (u16_span) and a
// reader that skips the field must both refuse it, and both take 255.
func TestGeneratedMessageRangedHeldToAnnouncedKind(t *testing.T) {
	t.Parallel()
	named := setFieldId(t, "RangedUnsigned", "u16_span")
	shape := ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 6, Base: big.NewInt(200)}
	for _, id := range []uint64{named, setUnknownId} {
		for _, c := range []struct {
			offset uint64
			bad    bool
		}{{63, true}, {55, false}} {
			ok, report, _ := setRead(t, []ir.TableVocabularyEntry{{Id: id, Kind: ir.TableKindU8, Shape: shape}}, func(w *setBits, refBits int) {
				w.put(1, refBits)
				w.put(c.offset, 6)
			})
			path := "named"
			if id == setUnknownId {
				path = "skip"
			}
			if c.bad && ok {
				t.Errorf("%s: 200 + %d was accepted: %+v", path, c.offset, report)
			}
			if !c.bad && !ok {
				t.Errorf("%s: 200 + %d was refused: %+v", path, c.offset, report)
			}
		}
	}
}

// TestGeneratedMessageSkipRangedElements: a skipped array of ranged elements
// walks each element through the same hold: int8 at base 100 over five bits
// spells 131, which the announced kind cannot hold.
func TestGeneratedMessageSkipRangedElements(t *testing.T) {
	t.Parallel()
	inner := ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 5, Base: big.NewInt(100)}
	entry := ir.TableVocabularyEntry{Id: setUnknownId, Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 2, Max: 2, Elem: ir.TableKindI8, Inner: &inner}}
	for _, c := range []struct {
		second uint64
		bad    bool
	}{{31, true}, {27, false}} {
		ok, report, _ := setRead(t, []ir.TableVocabularyEntry{entry}, func(w *setBits, refBits int) {
			w.put(1, refBits)
			w.put(0, 5)
			w.put(c.second, 5)
		})
		if ok == c.bad {
			t.Errorf("element 100 + %d: ok=%v %+v", c.second, ok, report)
		}
	}
}

// TestGeneratedMessageSkipUnionFieldNone: an unknown union FIELD written with
// arm reference 0 is the encoding no conforming writer emits (§3.3), refused
// on the skip path as on the named one; a None ELEMENT of a skipped array of
// unions stays lawful.
func TestGeneratedMessageSkipUnionFieldNone(t *testing.T) {
	t.Parallel()
	ok, report, _ := setRead(t, []ir.TableVocabularyEntry{{Id: setUnknownId, Kind: ir.TableKindUnion}}, func(w *setBits, refBits int) {
		w.put(1, refBits)
		w.put(0, refBits)
	})
	if ok {
		t.Errorf("a skipped union field at arm 0 was accepted: %+v", report)
	}
	array := ir.TableVocabularyEntry{Id: setUnknownId, Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 1, Max: 1, Elem: ir.TableKindUnion, Inner: &ir.TableMessageShape{}}}
	ok, report, _ = setRead(t, []ir.TableVocabularyEntry{array}, func(w *setBits, refBits int) {
		w.put(1, refBits)
		w.put(0, refBits)
	})
	if !ok || report.Unknown != 1 {
		t.Errorf("a skipped array's None union element was refused: %+v", report)
	}
}

// keyedEntries is an unknown keyed field of n no-payload slots and its n names.
func keyedEntries(n int) []ir.TableVocabularyEntry {
	entries := []ir.TableVocabularyEntry{{Id: setUnknownId, Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: int64(n), Elem: ir.TableKindNoPayload, Inner: &ir.TableMessageShape{}}}}
	for i := range n {
		entries = append(entries, setName(i))
	}
	return entries
}

func keyedBody(n int, keys []int) func(w *setBits, refBits int) {
	return func(w *setBits, refBits int) {
		w.put(1, refBits)
		w.put(uint64(len(keys)), ir.TableMessageBitsRequired(0, int64(n)))
		for _, k := range keys {
			w.put(uint64(k+2), refBits)
		}
	}
}

// TestGeneratedMessageSkipKeyedRepeat: a skipped keyed array's repeated key is
// refused, the first and the last slot included.
func TestGeneratedMessageSkipKeyedRepeat(t *testing.T) {
	t.Parallel()
	const n = 100
	keys := make([]int, n)
	for i := range keys {
		keys[i] = i
	}
	if ok, report, _ := setRead(t, keyedEntries(n), keyedBody(n, keys)); !ok || report.Unknown != 1 {
		t.Fatalf("a lawful skipped keyed array was refused: %+v", report)
	}
	keys[n-1] = 0
	if ok, _, _ := setRead(t, keyedEntries(n), keyedBody(n, keys)); ok {
		t.Fatal("a skipped keyed array naming its first key twice was accepted")
	}
}

// TestGeneratedMessageSetsNest: sets nest. An outer skipped keyed array whose
// slots are tables carrying an inner keyed array over the same names is
// lawful, and the outer array's repeat after the inner one closed is still
// found; so is a skipped body's repeated field id after a nested body named it.
func TestGeneratedMessageSetsNest(t *testing.T) {
	t.Parallel()
	inner := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000002, Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindNoPayload, Inner: &ir.TableMessageShape{}}}
	outer := ir.TableVocabularyEntry{Id: setUnknownId, Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindTable, Inner: &ir.TableMessageShape{}}}
	entries := []ir.TableVocabularyEntry{outer, setName(0), setName(1), inner}
	// refs: 1 outer, 2 name0, 3 name1, 4 inner
	body := func(second int) func(w *setBits, refBits int) {
		return func(w *setBits, refBits int) {
			w.put(1, refBits)
			w.put(2, 2) // two outer slots
			w.put(2, refBits)
			// slot body: the inner keyed array names name0 and name1, then ends
			w.put(4, refBits)
			w.put(2, 2)
			w.put(2, refBits)
			w.put(3, refBits)
			w.put(0, refBits)
			w.put(uint64(second), refBits)
			w.put(0, refBits) // the second slot's body is empty
		}
	}
	if ok, report, _ := setRead(t, entries, body(3)); !ok {
		t.Fatalf("lawful nested keyed sets were refused: %+v", report)
	}
	if ok, _, _ := setRead(t, entries, body(2)); ok {
		t.Fatal("the outer keyed array's repeat after an inner array named the same key was accepted")
	}
	// a skipped body: field a, a nested body naming a, then a again
	table := ir.TableVocabularyEntry{Id: setUnknownId, Kind: ir.TableKindTable}
	a := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000003, Kind: ir.TableKindNoPayload}
	nested := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000004, Kind: ir.TableKindTable}
	bodyIds := func(again bool) func(w *setBits, refBits int) {
		return func(w *setBits, refBits int) {
			w.put(1, refBits)
			w.put(2, refBits) // a
			w.put(3, refBits) // nested
			w.put(2, refBits) // a, inside nested: lawful
			w.put(0, refBits)
			if again {
				w.put(2, refBits) // a again in the outer body
			}
			w.put(0, refBits)
		}
	}
	tables := []ir.TableVocabularyEntry{table, a, nested}
	if ok, report, _ := setRead(t, tables, bodyIds(false)); !ok {
		t.Fatalf("a lawful skipped body with a nested body was refused: %+v", report)
	}
	if ok, _, _ := setRead(t, tables, bodyIds(true)); ok {
		t.Fatal("a skipped body naming a field id again after a nested body named it was accepted")
	}
}

// TestGeneratedMessageSkipKeyedAllocatesNothing: the skip path of a keyed
// array past 64 slots allocates nothing (docs/SPEC-TABLES.md §3.3, "the
// library allocates nothing on ... the read path").
func TestGeneratedMessageSkipKeyedAllocatesNothing(t *testing.T) {
	const n = 512
	entries := keyedEntries(n)
	v := &TableVocabulary{}
	v.Init(make([]TableMessageEntry, len(entries)+1))
	var report TableReport
	if !AnnounceRead(v, setAnnouncement(entries), &report) {
		t.Fatalf("the announcement was refused: %+v", report)
	}
	keys := make([]int, n)
	for i := range keys {
		keys[i] = i
	}
	var w setBits
	w.put(2, 8)
	w.put(0, 8)
	keyedBody(n, keys)(&w, int(v.RefBits))
	w.put(0, int(v.RefBits))
	values := make([]RangedUnsigned, 1)
	allocs := testing.AllocsPerRun(20, func() {
		if _, ok := RangedUnsignedLoadMessages(values, v, w.b, &report); !ok {
			t.Fatalf("refused: %+v", report)
		}
	})
	if allocs != 0 {
		t.Fatalf("a skipped keyed array of %d slots allocated %v times a read", n, allocs)
	}
}

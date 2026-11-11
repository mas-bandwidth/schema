package gotable

import "testing"

// TestGoMessageQuantizedIndexAboveCountRefused holds docs/SPEC-TABLES.md §3.3:
// on a message-form read, a quantized float32's step index above the announced
// `count` refuses the batch, exactly as the packet wire refuses it, and is
// never reconstructed and clamped. The ten-bit width over a count of 1000
// spells 1001 and 1023, and both are framing damage.
func TestGoMessageQuantizedIndexAboveCountRefused(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
	q float32 | min = 0, max = 10, resolution = 0.01
}
`
	runGeneratedTables(t, schema, "package probe\n"+quantizedIndexProbe)
}

const quantizedIndexProbe = `
import "testing"

func quantizedSlot(v *TableVocabulary) (TableMessageShape, bool) {
	for i := 0; i < int(v.Count); i++ {
		if v.Entries[i].Shape.Packing == 2 {
			return v.Entries[i].Shape, true
		}
	}
	return TableMessageShape{}, false
}

// raiseIndex rewrites the single field's step index in a one-body batch. The
// body is the field's slot, then the index, both low bit first.
func raiseIndex(batch []byte, fieldBits, width int64, index uint32) []byte {
	out := append([]byte(nil), batch...)
	base := 16 + fieldBits
	for i := int64(0); i < width; i++ {
		at := base + i
		mask := byte(1) << uint(at%8)
		if index>>uint(i)&1 != 0 {
			out[at/8] |= mask
		} else {
			out[at/8] &^= mask
		}
	}
	return out
}

func TestQuantizedIndex(t *testing.T) {
	t.Parallel()
	var v TableVocabulary
	v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	var announced TableReport
	if !AnnounceRead(&v, ann, &announced) {
		t.Fatalf("announce: %+v", announced)
	}
	shape, ok := quantizedSlot(&v)
	if !ok {
		t.Fatal("the vocabulary carries no quantized entry")
	}
	if shape.QCount != 1000 || shape.Bits != 10 {
		t.Fatalf("quantized shape: count=%d bits=%d, want 1000 and 10", shape.QCount, shape.Bits)
	}
	value := Root{Q: 10}
	values := []*Root{&value}
	batch := make([]byte, RootMeasureMessages(values, nil))
	if RootSaveMessages(values, batch, nil) != int64(len(batch)) {
		t.Fatal("save")
	}
	storage := make([]Root, 1)
	if count, ok := RootLoadMessages(storage, &v, batch, nil); !ok || count != 1 || storage[0].Q != 10 {
		t.Fatalf("the index at count: count=%d ok=%v q=%v", count, ok, storage[0].Q)
	}
	top := uint32(1)<<uint(shape.Bits) - 1
	for _, index := range []uint32{shape.QCount + 1, top} {
		storage[0] = Root{}
		count, ok := RootLoadMessages(storage, &v, raiseIndex(batch, v.RefBits, shape.Bits, index), nil)
		if ok || count != 0 {
			t.Fatalf("index %d above count %d was accepted as q=%v: count=%d ok=%v", index, shape.QCount, storage[0].Q, count, ok)
		}
	}
}
`

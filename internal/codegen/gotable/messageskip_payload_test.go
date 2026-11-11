package gotable

import "testing"

// TestGoMessageSkipChecksPayload holds the message form's skip path to the
// checks a named read applies to the same payload (docs/SPEC-TABLES.md §3.3,
// §4): a keyed array's key resolves and does not repeat, a string's bytes are
// well formed with no zero, a float is finite and a quantized index is within
// its count, an array count is within its announced range, and a nested body
// names no field twice. A lawful batch still loads and counts the skipped
// entry unknown; damaging the skipped payload refuses the batch.
func TestGoMessageSkipChecksPayload(t *testing.T) {
	t.Parallel()
	schema := `package probe

enum SkipColor { SkipRed, SkipGreen, SkipBlue }

fixed table Reader
{
  keep int32
}

fixed table WriterKeys
{
  keep int32
  keys [SkipColor]int32
}
fixed table WriterText
{
  keep int32
  text string(8)
}
fixed table WriterFloat
{
  keep int32
  ratio float32
}
fixed table WriterQuant
{
  keep int32
  q float32 | min = 0.0, max = 1.0, resolution = 0.01
}
fixed table WriterArray
{
  keep int32
  items [..2]int32 | min = 0, max = 0
}
fixed table SkipBody
{
  a int32
  b int32
}
fixed table WriterNested
{
  keep int32
  body SkipBody
}
`
	runGeneratedTables(t, schema, messageSkipPayloadProbe)
}

const messageSkipPayloadProbe = `package probe

import (
	"math"
	"testing"
)

// The helper builds the table surfaces only, so the enum's core type is the
// probe's to declare, exactly as the unit's core file does.
type SkipColor uint8

const (
	SkipColorNone      SkipColor = 0
	SkipColorSkipRed   SkipColor = 1
	SkipColorSkipGreen SkipColor = 2
	SkipColorSkipBlue  SkipColor = 3
	SkipColorMax       SkipColor = 3
)

func EnumNameSkipColor(SkipColor) string { return "" }

func skipPut(b []byte, at, n int64, v uint64) {
	for i := int64(0); i < n; i++ {
		mask := byte(1) << uint((at+i)&7)
		if v>>uint(i)&1 != 0 {
			b[(at+i)>>3] |= mask
		} else {
			b[(at+i)>>3] &^= mask
		}
	}
}

func skipPayloadBit() int64 { return int64(2*8 + TableMessageRefBitsHere) }

func skipAnnounce(t *testing.T) *TableVocabulary {
	t.Helper()
	announcement := make([]byte, AnnounceMeasure())
	if Announce(announcement) != int64(len(announcement)) {
		t.Fatal("announce")
	}
	vocabulary := new(TableVocabulary)
	vocabulary.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var report TableReport
	if !AnnounceRead(vocabulary, announcement, &report) {
		t.Fatalf("the reader refused the lawful announcement: %+v", report)
	}
	return vocabulary
}

func skipLawful(t *testing.T, data []byte) {
	t.Helper()
	var values [1]Reader
	var report TableReport
	count, ok := ReaderLoadMessages(values[:], skipAnnounce(t), data, &report)
	if !ok || count != 1 || report.Unknown != 1 {
		t.Fatalf("a lawful batch with one skipped field: count=%d ok=%v %+v", count, ok, report)
	}
}

func skipRefused(t *testing.T, data []byte) {
	t.Helper()
	var values [1]Reader
	var report TableReport
	count, ok := ReaderLoadMessages(values[:], skipAnnounce(t), data, &report)
	if ok || count != 0 {
		t.Fatalf("a damaged skipped payload was accepted: count=%d ok=%v %+v", count, ok, report)
	}
}

func skipSave[T any](t *testing.T, values []*T, measure func([]*T, *TableReport) int64, save func([]*T, []byte, *TableReport) int64) []byte {
	t.Helper()
	size := measure(values, nil)
	if size < 2 {
		t.Fatalf("measure messages: %d", size)
	}
	data := make([]byte, size)
	if save(values, data, nil) != int64(len(data)) {
		t.Fatal("save messages")
	}
	return data
}

func skipEntrySlot(id uint64) uint64 {
	for i := range tableMessageEntries {
		if tableMessageEntries[i].ID == id {
			return uint64(i + 1)
		}
	}
	return 0
}

func skipPayloadSlot() uint64 {
	for i := range tableMessageEntries {
		if tableMessageEntries[i].ID < 0xfffffffffffffffd && tableMessageEntries[i].Shape.Kind != 0 {
			return uint64(i + 1)
		}
	}
	return 0
}

func TestMessageSkipPayloadChecks(t *testing.T) {
	t.Parallel()
	t.Run("keyed_key_not_name", func(t *testing.T) {
		t.Parallel()
		value := &WriterKeys{Keys: [SkipColorMax]int32{7, 0, 0}}
		good := skipSave(t, []*WriterKeys{value},
			func(v []*WriterKeys, r *TableReport) int64 { return WriterKeysMeasureMessages(v, r) },
			func(v []*WriterKeys, b []byte, r *TableReport) int64 { return WriterKeysSaveMessages(v, b, r) })
		skipLawful(t, good)
		slot := skipPayloadSlot()
		if slot == 0 {
			t.Fatal("no payload entry to name")
		}
		bad := append([]byte(nil), good...)
		countBits := int64(tableMessageBits(uint64(SkipColorMax)))
		skipPut(bad, skipPayloadBit()+countBits, int64(TableMessageRefBitsHere), slot)
		skipRefused(t, bad)
	})
	t.Run("keyed_key_repeats", func(t *testing.T) {
		t.Parallel()
		value := &WriterKeys{Keys: [SkipColorMax]int32{7, 8, 0}}
		good := skipSave(t, []*WriterKeys{value},
			func(v []*WriterKeys, r *TableReport) int64 { return WriterKeysMeasureMessages(v, r) },
			func(v []*WriterKeys, b []byte, r *TableReport) int64 { return WriterKeysSaveMessages(v, b, r) })
		skipLawful(t, good)
		id, named := SkipColorSkipRed.TableEnumID()
		if !named {
			t.Fatal("the first key has no name")
		}
		slot := tableMessageNameSlot(id)
		if slot == 0 {
			t.Fatal("the first key has no slot")
		}
		bad := append([]byte(nil), good...)
		countBits := int64(tableMessageBits(uint64(SkipColorMax)))
		first := skipPayloadBit() + countBits
		second := first + int64(TableMessageRefBitsHere) + 32
		skipPut(bad, second, int64(TableMessageRefBitsHere), slot)
		skipRefused(t, bad)
	})
	t.Run("string_zero_byte", func(t *testing.T) {
		t.Parallel()
		value := &WriterText{TextLength: 2}
		copy(value.Text[:], "AB")
		good := skipSave(t, []*WriterText{value},
			func(v []*WriterText, r *TableReport) int64 { return WriterTextMeasureMessages(v, r) },
			func(v []*WriterText, b []byte, r *TableReport) int64 { return WriterTextSaveMessages(v, b, r) })
		skipLawful(t, good)
		bad := append([]byte(nil), good...)
		lengthBits := int64(tableMessageBits(8))
		start := 2 + (int64(TableMessageRefBitsHere)+lengthBits+7)/8
		bad[start] = 0
		skipRefused(t, bad)
	})
	t.Run("float_not_finite", func(t *testing.T) {
		t.Parallel()
		value := &WriterFloat{Ratio: 1.5}
		good := skipSave(t, []*WriterFloat{value},
			func(v []*WriterFloat, r *TableReport) int64 { return WriterFloatMeasureMessages(v, r) },
			func(v []*WriterFloat, b []byte, r *TableReport) int64 { return WriterFloatSaveMessages(v, b, r) })
		skipLawful(t, good)
		bad := append([]byte(nil), good...)
		skipPut(bad, skipPayloadBit(), 32, uint64(math.Float32bits(float32(math.Inf(1)))))
		skipRefused(t, bad)
	})
	t.Run("quantized_index_above_count", func(t *testing.T) {
		t.Parallel()
		value := &WriterQuant{Q: 0.5}
		good := skipSave(t, []*WriterQuant{value},
			func(v []*WriterQuant, r *TableReport) int64 { return WriterQuantMeasureMessages(v, r) },
			func(v []*WriterQuant, b []byte, r *TableReport) int64 { return WriterQuantSaveMessages(v, b, r) })
		skipLawful(t, good)
		bad := append([]byte(nil), good...)
		indexBits := int64(tableMessageBits(100))
		skipPut(bad, skipPayloadBit(), indexBits, 127)
		skipRefused(t, bad)
	})
	t.Run("array_count_above_max", func(t *testing.T) {
		t.Parallel()
		value := &WriterArray{ItemsCount: 1}
		good := skipSave(t, []*WriterArray{value},
			func(v []*WriterArray, r *TableReport) int64 { return WriterArrayMeasureMessages(v, r) },
			func(v []*WriterArray, b []byte, r *TableReport) int64 { return WriterArraySaveMessages(v, b, r) })
		skipLawful(t, good)
		bad := append([]byte(nil), good...)
		countBits := int64(tableMessageBits(2))
		skipPut(bad, skipPayloadBit(), countBits, 3)
		skipRefused(t, bad)
	})
	t.Run("nested_body_repeats_field", func(t *testing.T) {
		t.Parallel()
		value := &WriterNested{Body: SkipBody{A: 7, B: 8}}
		good := skipSave(t, []*WriterNested{value},
			func(v []*WriterNested, r *TableReport) int64 { return WriterNestedMeasureMessages(v, r) },
			func(v []*WriterNested, b []byte, r *TableReport) int64 { return WriterNestedSaveMessages(v, b, r) })
		skipLawful(t, good)
		slot := skipEntrySlot(SkipBodyTableFields[0].ID)
		if slot == 0 {
			t.Fatal("the nested field has no slot")
		}
		bad := append([]byte(nil), good...)
		second := skipPayloadBit() + int64(TableMessageRefBitsHere) + 32
		skipPut(bad, second, int64(TableMessageRefBitsHere), slot)
		skipRefused(t, bad)
	})
}
`

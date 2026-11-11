package gotable

import "testing"

// TestGoMessageMeasureRefusesWhatSaveRefuses holds MeasureMessages to the
// write contract the save beside it keeps: a batch the save refuses for an
// off-range value is one the measure refuses too, so a caller who sizes by the
// measure is never handed a refusal by the save for the same values. A value
// the save writes measures exactly the bytes it writes, with and without the
// integrity trailer (docs/SPEC.md §5.1, docs/SPEC-TABLES.md §3.3).
func TestGoMessageMeasureRefusesWhatSaveRefuses(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Plain {
	q float32 | min = 0, max = 10, resolution = 0.01
	name string(4)
	items [..3]int32
}

fixed table Sealed | integrity = "crc32c" {
	q float32 | min = 0, max = 10, resolution = 0.01
	name string(4)
	items [..3]int32
}
`
	const probe = `package probe

import (
	"math"
	"testing"
)

func refused[T any](t *testing.T, name string, measure func([]*T, *TableReport) int64, save func([]*T, []byte, *TableReport) int64, value *T) {
	t.Helper()
	values := []*T{value}
	var sink [4096]byte
	if n := save(values, sink[:], nil); n != -1 {
		t.Fatalf("%s: SaveMessages=%d, want -1", name, n)
	}
	if n := measure(values, nil); n != -1 {
		t.Fatalf("%s: MeasureMessages=%d, want -1", name, n)
	}
}

func measured[T any](t *testing.T, name string, measure func([]*T, *TableReport) int64, save func([]*T, []byte, *TableReport) int64, value *T) {
	t.Helper()
	values := []*T{value}
	n := measure(values, nil)
	if n < 0 {
		t.Fatalf("%s: MeasureMessages=%d, want a size", name, n)
	}
	if got := save(values, make([]byte, n), nil); got != n {
		t.Fatalf("%s: SaveMessages wrote %d, want the measured %d", name, got, n)
	}
}

func TestPlainMeasureRefusesWhatSaveRefuses(t *testing.T) {
	valid := Plain{}
	valid.Q = 5
	copy(valid.Name[:], []byte("abcd"))
	valid.NameLength = 4
	valid.Items = [3]int32{1, 2, 3}
	valid.ItemsCount = 3
	measured(t, "plain/valid", PlainMeasureMessages, PlainSaveMessages, &valid)

	above := Plain{}
	above.Q = 11
	refused(t, "plain/q_above_max", PlainMeasureMessages, PlainSaveMessages, &above)

	below := Plain{}
	below.Q = -0.5
	refused(t, "plain/q_below_min", PlainMeasureMessages, PlainSaveMessages, &below)

	nan := Plain{}
	nan.Q = float32(math.NaN())
	refused(t, "plain/q_nan", PlainMeasureMessages, PlainSaveMessages, &nan)

	long := Plain{}
	copy(long.Name[:], []byte("abcd"))
	long.NameLength = 5
	refused(t, "plain/name_past_bound", PlainMeasureMessages, PlainSaveMessages, &long)

	many := Plain{}
	many.ItemsCount = 4
	refused(t, "plain/items_past_bound", PlainMeasureMessages, PlainSaveMessages, &many)
}

func TestSealedMeasureRefusesWhatSaveRefuses(t *testing.T) {
	valid := Sealed{}
	valid.Q = 5
	copy(valid.Name[:], []byte("abcd"))
	valid.NameLength = 4
	valid.Items = [3]int32{1, 2, 3}
	valid.ItemsCount = 3
	measured(t, "sealed/valid", SealedMeasureMessages, SealedSaveMessages, &valid)

	above := Sealed{}
	above.Q = 11
	refused(t, "sealed/q_above_max", SealedMeasureMessages, SealedSaveMessages, &above)

	below := Sealed{}
	below.Q = -0.5
	refused(t, "sealed/q_below_min", SealedMeasureMessages, SealedSaveMessages, &below)

	nan := Sealed{}
	nan.Q = float32(math.NaN())
	refused(t, "sealed/q_nan", SealedMeasureMessages, SealedSaveMessages, &nan)

	long := Sealed{}
	copy(long.Name[:], []byte("abcd"))
	long.NameLength = 5
	refused(t, "sealed/name_past_bound", SealedMeasureMessages, SealedSaveMessages, &long)

	many := Sealed{}
	many.ItemsCount = 4
	refused(t, "sealed/items_past_bound", SealedMeasureMessages, SealedSaveMessages, &many)
}
`
	runGeneratedTables(t, schema, probe)
}

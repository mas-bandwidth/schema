package main

import (
	"errors"
	"math"
	"math/big"
	"reflect"
	"testing"
)

// TestWriteRefusalStoreHoldsEveryKindToItsRange holds the one checked
// conversion to each integer kind's own range: the ends are stored exactly,
// and one past either end is `unrepresentable` and leaves the storage as it
// was. Before the helper, every kind but `int` narrowed.
func TestWriteRefusalStoreHoldsEveryKindToItsRange(t *testing.T) {
	type named uint8
	var (
		i8  int8
		i16 int16
		i32 int32
		i64 int64
		u8  uint8
		u16 uint16
		u32 uint32
		u64 uint64
		en  named
	)
	two64 := new(big.Int).Lsh(big.NewInt(1), 64)
	rows := []struct {
		name     string
		ptr      any
		min, max *big.Int
	}{
		{"int8", &i8, big.NewInt(math.MinInt8), big.NewInt(math.MaxInt8)},
		{"int16", &i16, big.NewInt(math.MinInt16), big.NewInt(math.MaxInt16)},
		{"int32", &i32, big.NewInt(math.MinInt32), big.NewInt(math.MaxInt32)},
		{"int64", &i64, big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)},
		{"uint8", &u8, big.NewInt(0), big.NewInt(math.MaxUint8)},
		{"uint16", &u16, big.NewInt(0), big.NewInt(math.MaxUint16)},
		{"uint32", &u32, big.NewInt(0), big.NewInt(math.MaxUint32)},
		{"uint64", &u64, big.NewInt(0), new(big.Int).Sub(two64, big.NewInt(1))},
		{"named uint8", &en, big.NewInt(0), big.NewInt(math.MaxUint8)},
	}
	read := func(fv reflect.Value) *big.Int {
		if fv.CanInt() {
			return big.NewInt(fv.Int())
		}
		return new(big.Int).SetUint64(fv.Uint())
	}
	for _, row := range rows {
		fv := reflect.ValueOf(row.ptr).Elem()
		for _, end := range []*big.Int{row.min, row.max} {
			if err := writeRefusalGoStore(fv, end); err != nil {
				t.Errorf("%s: %s is in range and was not stored: %v", row.name, end, err)
			} else if got := read(fv); got.Cmp(end) != 0 {
				t.Errorf("%s: stored %s, holds %s", row.name, end, got)
			}
		}
		held := read(fv)
		below := new(big.Int).Sub(row.min, big.NewInt(1))
		above := new(big.Int).Add(row.max, big.NewInt(1))
		wide := new(big.Int).Add(two64, big.NewInt(1))
		for _, past := range []*big.Int{below, above, wide, new(big.Int).Neg(wide)} {
			if err := writeRefusalGoStore(fv, past); !errors.Is(err, errWriteRefusalUnrepresentable) {
				t.Errorf("%s: %s is past the kind and answered %v, want unrepresentable", row.name, past, err)
			}
			if got := read(fv); got.Cmp(held) != 0 {
				t.Errorf("%s: a refused store of %s changed the storage from %s to %s", row.name, past, held, got)
			}
		}
	}
	var f float64
	if err := writeRefusalGoStore(reflect.ValueOf(&f).Elem(), big.NewInt(1)); err == nil || errors.Is(err, errWriteRefusalUnrepresentable) {
		t.Errorf("a float is not integer storage: answered %v", err)
	}
}

// TestWriteRefusalCountLengthAndRealAreChecked holds the companions and the
// float the same way: a count or a length its int32 cannot hold, a unit a
// byte cannot hold, and a finite decimal a float32 cannot hold are
// `unrepresentable`, never the low bits or an infinity.
func TestWriteRefusalCountLengthAndRealAreChecked(t *testing.T) {
	type holder struct {
		Items      [4]uint8
		ItemsCount int32
		Note       [4]uint8
		NoteLength int32
		Wide       [4]uint16
		WideLength int32
		Ratio      float32
		Exact      float64
	}
	set := func(h *holder, field, kind, text string) error {
		owner := reflect.ValueOf(h).Elem()
		fv, ok := writeRefusalGoField(owner, field)
		if !ok {
			t.Fatalf("no field %s", field)
		}
		return writeRefusalGoSet(owner, fv, field, kind, text)
	}
	unrepresentable := []struct{ field, kind, text string }{
		{"items", "count", "4294967296"},
		{"items", "count", "2147483648"},
		{"items", "count", "-2147483649"},
		{"note", "length", "4294967296"},
		{"note", "length", "2147483648"},
		{"note", "units", "0100"},
		{"ratio", "real", "1e39"},
		{"ratio", "real", "-1e39"},
	}
	for _, row := range unrepresentable {
		h := holder{ItemsCount: 3, NoteLength: 2, Note: [4]uint8{'x', 'y'}, Ratio: 1.5}
		before := h
		if err := set(&h, row.field, row.kind, row.text); !errors.Is(err, errWriteRefusalUnrepresentable) {
			t.Errorf("%s %s %s: answered %v, want unrepresentable", row.field, row.kind, row.text, err)
		}
		if h != before {
			t.Errorf("%s %s %s: a refused store changed the value: %+v", row.field, row.kind, row.text, h)
		}
	}
	var h holder
	stored := []struct {
		field, kind, text string
		ok                func() bool
	}{
		{"items", "count", "9", func() bool { return h.ItemsCount == 9 }},
		{"items", "count", "-1", func() bool { return h.ItemsCount == -1 }},
		{"items", "count", "2147483647", func() bool { return h.ItemsCount == math.MaxInt32 }},
		{"note", "length", "17", func() bool { return h.NoteLength == 17 && h.Note == [4]uint8{'a', 'a', 'a', 'a'} }},
		{"note", "length", "2", func() bool { return h.NoteLength == 2 }},
		{"note", "bytes", "61FF62", func() bool { return h.NoteLength == 3 && h.Note == [4]uint8{0x61, 0xff, 0x62, 'a'} }},
		{"wide", "units", "0061D800", func() bool { return h.WideLength == 2 && h.Wide == [4]uint16{0x61, 0xd800} }},
		{"ratio", "real", "0.1", func() bool { return h.Ratio == float32(0.1) }},
		{"ratio", "float", "inf", func() bool { return math.IsInf(float64(h.Ratio), 1) }},
		{"ratio", "float", "nan", func() bool { return h.Ratio != h.Ratio }},
		{"exact", "real", "1e39", func() bool { return h.Exact == 1e39 }},
	}
	for _, row := range stored {
		if err := set(&h, row.field, row.kind, row.text); err != nil {
			t.Errorf("%s %s %s: %v", row.field, row.kind, row.text, err)
		} else if !row.ok() {
			t.Errorf("%s %s %s: stored a different value: %+v", row.field, row.kind, row.text, h)
		}
	}
}

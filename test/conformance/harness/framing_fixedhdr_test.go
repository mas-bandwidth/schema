package main

import (
	"bytes"
	"os"
)

// THE FIXED-FORM HEADER AND COUNT VECTORS (docs/SPEC-TABLES.md §3.4, the load
// steps and the count rows of the fixed record): each is the lawful form-3
// instance fixedevo_v3_full with ONE defect patched in, so the verdict is the
// rule's and no implementation's output. Reserved bytes `1..7` must be zero,
// 20 bytes is the minimum, the form byte reads before the length, a counted
// array's count and a text's length are read SIGNED (below 0 or above the
// bound refuses), and a non-zero byte in a dead slot is damage.
func init() {
	full, err := os.ReadFile("../../../testdata/conformance/tables/wire/fixedevo_v3_full.bin")
	if err != nil {
		panic(err)
	}
	find := func(needle []byte) int {
		if bytes.Count(full, needle) != 1 {
			panic("fixed header vectors: the needle is not unique in fixedevo_v3_full")
		}
		return bytes.Index(full, needle)
	}
	// bounded_arr's count (6) beside its first slot (int64 min), and label's
	// length (32) beside its first rocket
	arr := find([]byte{6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x80})
	txt := find([]byte{0x20, 0, 0, 0, 0xf0, 0x9f, 0x9a, 0x80})
	add := func(name string, f func(b []byte) []byte) {
		framingVectors = append(framingVectors, framingVector{
			name: name, unit: "tblfixedevo", root: "EvoRoot",
			raw: f(append([]byte(nil), full...)),
		})
	}
	set := func(off int, v byte) func(b []byte) []byte {
		return func(b []byte) []byte { b[off] = v; return b }
	}
	neg := func(off int) func(b []byte) []byte {
		return func(b []byte) []byte { copy(b[off:], []byte{0xff, 0xff, 0xff, 0xff}); return b }
	}
	add("sp_fixed_reserved_byte_1", set(1, 1))
	add("sp_fixed_reserved_byte_4", set(4, 0x80))
	add("sp_fixed_reserved_byte_7", set(7, 1))
	add("sp_fixed_nineteen_bytes", func(b []byte) []byte { return b[:19] })
	add("sp_fixed_form_one", set(0, 1))
	add("sp_fixed_form_two", set(0, 2))
	add("sp_fixed_form_four", set(0, 4))
	add("sp_fixed_count_negative", neg(arr))
	add("sp_fixed_count_over_bound", set(arr, 7))
	add("sp_fixed_dead_slot_nonzero", set(arr, 5))
	add("sp_fixed_text_length_negative", neg(txt))
	add("sp_fixed_text_length_over_bound", set(txt, 33))
}

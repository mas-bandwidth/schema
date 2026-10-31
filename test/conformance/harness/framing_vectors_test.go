package main

import (
	"encoding/binary"
	"encoding/hex"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FRAMING VECTORS (docs/LAYERS/1-framing.md), hand-built. Each one is a
// byte script and the rule it exercises, by the law-home's number; the bytes
// are the vector, the manifest's `framing` row holds the read report it owes
// and the value file it owes, and both were derived from the rule text, never
// from an implementation's output. framing_test.go pins the bytes to
// testdata/wire/tables/framing/<name>.bin and runs every vector through the
// engine.
//
// Kinds, from docs/SPEC-TABLES.md §3: 4 i32, 6 u8, 10 f32, 12 string, 13 table,
// 14 array, 15 union, 16 enum-keyed array, 17 pointer, 30 enum, 32 no payload.
// A body is `ref, kind, payload ... 0`; a reference is the 1-based slot of the
// id table the vector names in `ids`.

type framingVector struct {
	name, unit, root string
	// ids is the id table in slot order. A name is hashed as a field, variant
	// or arm id (ir.TableWireId); "node-table" is the reserved node-table id;
	// "build-version" the reserved build-version id; "type:T" is table T's
	// type id, the id a node record carries (§3.1).
	ids []string
	// body is the root body, terminator included; the file is the form byte,
	// the body, the entries and the count.
	body []byte
	// raw, when set, is the whole file verbatim, for the vectors about the
	// frame itself. entries, when set, replaces the entries ids would write;
	// count, when non-nil, replaces the entry count the entries imply.
	raw     []byte
	entries []uint64
	count   *uint64
	// held marks a vector the law-home holds OUT of the manifest, on a
	// FINDING: its bytes are pinned so the ruling can land it, and nothing
	// reads it until then.
	held bool
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func b(bytes ...byte) []byte { return bytes }

func i32(v int32) []byte {
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], uint32(v))
	return out[:]
}

func f32(v float32) []byte {
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], math.Float32bits(v))
	return out[:]
}

func u64(v uint64) []byte {
	var out [8]byte
	binary.LittleEndian.PutUint64(out[:], v)
	return out[:]
}

// leb, one canonical LEB128, is messagerules_test.go's helper.

//go:fix inline
func ptr(v uint64) *uint64 { return new(v) }

func mustHex(s string) []byte {
	out, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return out
}

// The tblr1 Cfg id table every Cfg vector shares, in slot order (the
// numbering the r1_cfg golden uses for its first ten), so that a byte script
// reads against one map: 1 grade, 2 Silver, 3 effect, 4 ward, 5 charge, 6
// buff, 7 multiplier, 8 grades, 9 Gold, 10 tally, 11 Bronze, 12 ping, 13
// amount, 14 boost, 15 power, 16 zzz (an id no Cfg declares).
var cfgIds = []string{"grade", "Silver", "effect", "ward", "charge", "buff", "multiplier", "grades", "Gold", "tally", "Bronze", "ping", "amount", "boost", "power", "zzz"}

// gradeSilver is `grade = Silver`: ref 1, kind 30, ref 2.
var gradeSilver = b(1, 30, 2)

// buffHalf is `buff { multiplier = 1.5 }` as a whole kind-13 field: ref 6,
// kind 13, L = 7, then the body `ref 7, kind 10, 1.5, 0`.
var buffBody = cat(b(7, 10), f32(1.5), b(0))
var buffHalf = cat(b(6, 13, 7), buffBody)

// The listdemo Squad id table: 1 roster, 2 key, 3 value, 4 count, 5 name.
var squadIds = []string{"roster", "key", "value", "count", "name"}

// squadEntry is one map entry `{ key = k, value = { count = c } }` as its
// kind-13 element: L = 14, then `ref 2 kind 6 k | ref 3 kind 13 L 7 [ref 4
// kind 4 c 0] | 0`.
func squadEntry(k byte, c int32) []byte {
	return cat(b(14, 2, 6, k, 3, 13, 7, 4, 4), i32(c), b(0, 0))
}

// name50 is Squad's `name = 50`.
var name50 = cat(b(5, 4), i32(50))

// The listdemo Bounded id table: 1 items, 2 v, 3 tag.
var boundedIds = []string{"items", "v", "tag"}

// unitElem is one `Unit { v }` element of items: L = 7, `ref 2 kind 4 v 0`.
func unitElem(v int32) []byte { return cat(b(7, 2, 4), i32(v), b(0)) }

var tag42 = cat(b(3, 4), i32(42))

// The graphdemo ListNode id table: 1 value, 2 name, 3 next, 4 the reserved
// node-table id, 5 ListNode's type id.
var listNodeIds = []string{"value", "name", "next", "node-table", "type:ListNode"}

// listNodeRecord is one node record of `ListNode { value = v }`: type ref 5,
// length 7, the body `ref 1 kind 4 v 0`.
func listNodeRecord(v int32) []byte { return cat(b(5, 7, 1, 4), i32(v), b(0)) }

// nodeTable is the node-table field over the records given: ref 4, kind 12,
// L, then node_count and the records (§3.1).
func nodeTable(count uint64, records ...[]byte) []byte {
	payload := cat(append([][]byte{leb(count)}, records...)...)
	return cat(b(4, 12), leb(uint64(len(payload))), payload)
}

var rootValue1 = cat(b(1, 4), i32(1))
var nextIs2 = b(3, 17, 2)

var framingVectors = []framingVector{
	// ---------------------------------------------------------------- A. the file frame
	// F1: form byte 0 is never assigned and refuses before anything is read.
	{name: "frame_form_zero", unit: "tblr1", root: "Cfg", raw: b(0)},
	// F1, §3.3: form 2 stored as a file refuses by name.
	{name: "frame_form_two_as_file", unit: "tblr1", root: "Cfg", raw: cat(b(2, 0), u64(0))},
	// F1: form 4 is a planned row of the registry and no reader carries it.
	{name: "frame_form_four_planned", unit: "tblr1", root: "Cfg", raw: cat(b(4, 0), u64(0))},
	// F5 (finding N6, settled): a buffer of no bytes has no form byte to read
	// and fewer than eight bytes for a table, and is malformed, not refused.
	{name: "frame_empty_buffer", unit: "tblr1", root: "Cfg", raw: []byte{}},
	// F2–F4: the ten-byte empty table reads clean.
	{name: "frame_empty_table", unit: "tblr1", root: "Cfg", ids: nil, body: b(0)},
	// F5: fewer than eight bytes in the file.
	{name: "frame_seven_bytes", unit: "tblr1", root: "Cfg", raw: b(1, 0, 0, 0, 0, 0, 0)},
	// F5: eight bytes, the count would have to begin at the form byte.
	{name: "frame_eight_bytes", unit: "tblr1", root: "Cfg", raw: b(1, 0, 0, 0, 0, 0, 0, 0)},
	// F4 then L2 at the root: nine bytes is a readable table of no entries over
	// a root body of no bytes, which has no terminator.
	{name: "frame_nine_bytes", unit: "tblr1", root: "Cfg", raw: cat(b(1), u64(0))},
	// F5: a count whose 8 × count + 8 runs past the front of the file.
	{name: "frame_count_past_front", unit: "tblr1", root: "Cfg", ids: nil, body: b(0), count: ptr(2)},
	// F7: an entry no body references counts nothing.
	{name: "frame_unreferenced_entry", unit: "tblr1", root: "Cfg", ids: []string{"zzz"}, body: b(0)},
	// F5: the count one above the entries the file holds.
	{name: "frame_count_one_over", unit: "tblr1", root: "Cfg", ids: []string{"zzz"}, body: b(0), count: ptr(2)},
	// F5: a byte between the root's terminator and the first entry.
	{name: "frame_slack_after_terminator", unit: "tblr1", root: "Cfg", ids: nil, body: b(0, 0)},
	// F5: one id in two entries.
	{name: "frame_duplicate_entry", unit: "tblr1", root: "Cfg", ids: []string{"grade", "grade"}, body: b(0)},
	// F6: 4096 entries is the bound and resolves; 4097 is malformed.
	{name: "frame_entries_4096", unit: "tblr1", root: "Cfg", entries: counting(4096), body: b(0)},
	{name: "frame_entries_4097", unit: "tblr1", root: "Cfg", entries: counting(4097), body: b(0)},
	// F8: the entry count itself is the last legal slot and resolves.
	{name: "frame_ref_last_slot", unit: "tblr1", root: "Cfg", ids: []string{"Silver", "grade"}, body: b(2, 30, 1, 0)},
	// F9, L2: a reference above the count stops the root with grade standing.
	{name: "frame_ref_above_count", unit: "tblr1", root: "Cfg", ids: []string{"grade", "Silver"}, body: cat(gradeSilver, b(3, 4), i32(0), b(0))},
	// F10: a reference spelled `81 00` is 1 with a redundant byte.
	{name: "frame_ref_non_canonical", unit: "tblr1", root: "Cfg", ids: []string{"grade", "Silver"}, body: b(0x81, 0, 30, 2, 0)},
	// F10: the terminator spelled `80 00`.
	{name: "frame_terminator_non_canonical", unit: "tblr1", root: "Cfg", ids: []string{"grade", "Silver"}, body: cat(gradeSilver, b(0x80, 0))},
	// F10: a length past ten bytes.
	{name: "frame_leb_eleven_bytes", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(6, 13, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0), b(0))},
	// F10: a tenth byte with a bit above the 64th value bit.
	{name: "frame_leb_tenth_byte_overflow", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(6, 13, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x02), b(0))},
	// R2, R3: kinds outside the closed set are not skippable, at a position the
	// reader cannot name: the unknown id is counted when it is met (§4,
	// "skipped by its length, counted"), and the skip that follows is the
	// framing damage.
	{name: "frame_kind_unknown_37", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(16, 37, 2, 0)},
	{name: "frame_kind_reserved_34", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(16, 34, 2, 0)},
	{name: "frame_kind_zero", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(16, 0, 2, 0)},
	// R3, R4 (finding N9, settled): the same three kinds at a position the
	// reader NAMES: the kind is judged against the declaration first, so
	// `kind_mismatch` counts, and the skip that follows is the damage.
	{name: "frame_kind_unknown_37_named", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(1, 37, 2, 0)},
	{name: "frame_kind_reserved_34_named", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(1, 34, 2, 0)},
	{name: "frame_kind_zero_named", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(1, 0, 2, 0)},
	// R2, R3: an unknown id skips by each of the four rules and counts once.
	{name: "frame_unknown_id_fixed_width", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(16, 4), i32(42), gradeSilver, b(0))},
	{name: "frame_unknown_id_length", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(16, 13, 3, 0xAA, 0xBB, 0xCC), gradeSilver, b(0))},
	{name: "frame_unknown_id_union", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(16, 15, 2, 4, 4), i32(42), gradeSilver, b(0))},
	{name: "frame_unknown_id_union_empty", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(16, 15, 0), gradeSilver, b(0))},
	{name: "frame_unknown_id_reference", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(16, 17, 5), gradeSilver, b(0))},
	// R4: a named field under another kind skips by the WIRE kind's rule, and
	// the field after it lands.
	{name: "frame_kind_mismatch_skips_by_wire_kind", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(1, 4), i32(0), buffHalf, b(0))},
	// R5: an id carried twice in one body is damage: no writer produces it.
	{name: "frame_repeat_id", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(1, 30, 9), b(0))},
	// R5 at an id this reader cannot name: still one body carrying one id twice.
	{name: "frame_repeat_unknown_id", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(16, 4), i32(1), b(16, 4), i32(2), gradeSilver, b(0))},
	// R6: the build-version id in a file body is malformed on that body.
	{name: "frame_reserved_build_version_id", unit: "tblr1", root: "Cfg", ids: []string{"grade", "Silver", "build-version"}, body: cat(gradeSilver, b(3, 4), i32(0), b(0))},

	// ---------------------------------------------------------------- B. nested bodies
	// L3: buff's terminator a byte early; the field defaults, grade lands after.
	{name: "nested_terminator_early", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 8), buffBody, b(0xFF), gradeSilver, b(0))},
	// L1: L exactly the body, the last field of the root.
	{name: "nested_L_exact", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, buffHalf, b(0))},
	// L3, L2: L one past, swallowing the root's terminator: the body ends
	// early at its own terminator and defaults, and the root is left without
	// one.
	{name: "nested_L_plus_one", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(6, 13, 8), buffBody, b(0))},
	// L4: L past the root body; the root stops with grade standing.
	{name: "nested_L_past_body", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(6, 13, 9), buffBody, b(0))},
	// R5 inside a nested body: buff carries multiplier twice.
	{name: "nested_repeat_id", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 13, 7, 10), f32(1.5), b(7, 10), f32(2.5), b(0), gradeSilver, b(0))},
	// R2 inside a nested body: the unknown counts, the body reads on.
	{name: "nested_unknown_id_inside", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 13, 16, 4), i32(42), buffBody, gradeSilver, b(0))},
	// F9 inside a nested body: the body stops before decoding anything, and
	// the root reads on past L.
	{name: "nested_ref_above_count_inside", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 3, 99, 4, 0xFF), gradeSilver, b(0))},
	// HELD (finding N1, reported to the parent): the whole multiplier field and
	// no terminator, the damage landing exactly at L. The engine and the C++
	// reference both keep multiplier 1.5.
	{name: "nested_terminator_missing", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 6, 7, 10), f32(1.5), gradeSilver, b(0))},
	// HELD (finding N1, reported to the parent): the whole multiplier field,
	// then a field under kind 37, the damage landing before L. The engine and
	// the C++ reference both refill the body to its defaults, multiplier 1.
	{name: "nested_damage_midway", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 9, 7, 10), f32(1.5), b(7, 37, 0), gradeSilver, b(0))},

	// ---------------------------------------------------------------- C. arrays
	// H2: L = 0 and L = 1 are inert.
	{name: "array_L0_inert", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(8, 14, 0), gradeSilver, b(0))},
	{name: "array_L1_inert", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(8, 14, 1, 30), gradeSilver, b(0))},
	// H1: L = 2 is the empty array.
	{name: "array_L2_empty", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(8, 14, 2, 30, 0, 0)},
	// H6: the count at the bound, and one past it.
	{name: "array_count_at_bound", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(8, 14, 6, 30, 4, 2, 9, 11, 2, 0)},
	{name: "array_count_bound_plus_one", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(8, 14, 7, 30, 5, 2, 9, 11, 2, 9, 0)},
	// H5: a count of two over one element's bytes.
	{name: "array_count_over_length", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(8, 14, 3, 30, 2, 2, 0)},
	// H3, H5: the count's second byte is the parent's next reference (ref 2,
	// `past`): count = 2 + 2·128 = 258, the elements bounded by L cover none,
	// and the parent resumes at that byte.
	{name: "array_header_crosses_L_unbounded", unit: "listdemo", root: "Ints", ids: []string{"values", "after"}, body: cat(b(1, 14, 2, 4, 0x82, 2, 4), i32(42), b(0))},
	// H3, H5, H6: the same on a bounded array: count = 2 + 1·128 = 130, past
	// the bound of 4, so clamped counts too.
	{name: "array_header_crosses_L_bounded", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(8, 14, 2, 30, 0x82), gradeSilver, b(0))},
	// H3, H4: the crossed header under a foreign element kind is a kind
	// mismatch and nothing else.
	{name: "array_header_crosses_L_kind_mismatch", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(8, 14, 2, 4, 0x82), gradeSilver, b(0))},
	// H4: the element kind inside L.
	{name: "array_elem_kind_mismatch", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(8, 14, 3, 4, 1, 2, 0)},
	// F10 at a count: `80 00`.
	{name: "array_count_non_canonical", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(8, 14, 3, 30, 0x80, 0, 0)},
	// H3 at the end of the buffer: a count that cannot complete even in the
	// parent, with the root's own terminator gone with it.
	{name: "array_header_incomplete_at_end", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(8, 14, 2, 30, 0x80))},
	// L3 at an element: the second element's terminator early, so it takes
	// its own defaults and the array keeps its count.
	{name: "array_elem_terminator_early", unit: "listdemo", root: "Bounded", ids: boundedIds, body: cat(b(1, 14, 19, 13, 2), unitElem(5), b(8, 2, 4), i32(6), b(0, 0xFF), tag42, b(0))},
	// L4 at an element: the second element's L past the array body.
	{name: "array_elem_L_past_body", unit: "listdemo", root: "Bounded", ids: boundedIds, body: cat(b(1, 14, 18, 13, 2), unitElem(5), b(9, 2, 4), i32(6), b(0), tag42, b(0))},

	// ---------------------------------------------------------------- D. maps
	// H3, H4 (the #2400 class): element kind 14 where a map declares 13, the
	// count's second byte being the parent's next reference (ref 5, `name`).
	{name: "map_header_crosses_L_kind_mismatch", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 2, 14, 0x83), name50, b(0))},
	// H3, H5: the crossed header under the right element kind: count = 5 +
	// 5·128 = 645 over no entry bytes.
	{name: "map_header_crosses_L", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 2, 13, 0x85), name50, b(0))},
	// H8, L3: the second entry's terminator early stops the map with its
	// ascending prefix.
	{name: "map_entry_terminator_early", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 33, 13, 2), squadEntry(2, 20), b(15, 2, 6, 9, 3, 13, 7, 4, 4), i32(90), b(0, 0, 0xFF), name50, b(0))},
	// L4 at an entry: the second entry's L past the map's body.
	{name: "map_entry_L_past_map", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 32, 13, 2), squadEntry(2, 20), b(20, 2, 6, 9, 3, 13, 7, 4, 4), i32(90), b(0, 0), name50, b(0))},
	// H8: a descending key.
	{name: "map_descending", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 32, 13, 2), squadEntry(9, 90), squadEntry(2, 20), name50, b(0))},
	// H5 at a map: a count of two over one entry.
	{name: "map_count_over_length", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 17, 13, 2), squadEntry(2, 20), name50, b(0))},
	// H2 at a map.
	{name: "map_L1_inert", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 1, 13), name50, b(0))},
	// THE KNOWN WITNESS, verbatim: the Dart wire-fuzz mutant #2400 fixed (ci-full
	// run 41205503830, seed 24845619678, corpus seed list_of_maps, random pass
	// #5292), the second Squad's roster arriving as `02 0e 02 | 0e 83`. The
	// row's report is the oracle's pinned answer, which
	// TestWireFuzzMapHeaderCrossingItsLengthIsAKindMismatch holds; the rest of
	// the mutant's damage is the random pass's, so the row owes the report
	// alone.
	{name: "map_header_crosses_L_dart_mutant", unit: "listdemo", root: "Army", raw: mustHex("01010e580d022a020e200d020e030602040d0705041401000000000e030609040d0705045a0000000000060432000000002a020e020e830603040d0705041500000000000e030608040d0705045b0000000000060433000000000704080000000026a9020c9b014878424f4f300d39841cec105b36094ac93dea0ce83094fde47c74a279448ee2e5b1861b638ebaadbc90e9ea716f0f0182bf0700000000000000")},

	// ---------------------------------------------------------------- E. enum-keyed arrays
	// H3, H5 at kind 16: count = 2 + 1·128 = 130 over no triple bytes.
	{name: "keyed_header_crosses_L", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 2, 4, 0x82), gradeSilver, b(0))},
	// H3, H4 at kind 16 (the class Java, Luau and Squirrel get wrong): element
	// kind 30 where int32 is declared.
	{name: "keyed_header_crosses_L_kind_mismatch", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 2, 30, 0x82), gradeSilver, b(0))},
	// H7: a key reference of 0 after one landed slot.
	{name: "keyed_key_zero", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 14, 4, 2, 2, 4), i32(7), b(0, 4), i32(0), b(0))},
	// §3.2: a key this reader cannot name skips by its L and counts unknown;
	// the slot after it lands.
	{name: "keyed_unknown_key", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 14, 4, 2, 16, 4), i32(0), b(9, 4), i32(3), b(0))},
	// L4 at a triple: the second triple's L past the body.
	{name: "keyed_triple_L_past_body", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 14, 4, 2, 2, 4), i32(7), b(9, 9), i32(3), b(0))},
	// H5 at kind 16: a count of two over one triple.
	{name: "keyed_count_over_length", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 8, 4, 2, 2, 4), i32(7), b(0))},
	// H4 at kind 16, inside L.
	{name: "keyed_elem_kind_mismatch", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 8, 30, 1, 2, 4), i32(7), b(0))},
	// §3.2: one slot key twice in one keyed body, which no writer produces.
	{name: "keyed_repeat_slot", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 14, 4, 2, 2, 4), i32(7), b(2, 4), i32(3), b(0))},
	// §3.2: a slot key this reader cannot name, twice.
	{name: "keyed_repeat_unknown_slot", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 14, 4, 2, 16, 4), i32(7), b(16, 4), i32(3), b(0))},
	// H2 at kind 16.
	{name: "keyed_L1_inert", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 1, 4), gradeSilver, b(0))},

	// ---------------------------------------------------------------- U. union arms
	// H9: a fixed-width arm whose L is not its width.
	{name: "union_arm_L_not_width", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(3, 15, 13, 4, 3, 0, 0, 0), gradeSilver, b(0))},
	// H9: L the width, the arm lands.
	{name: "union_arm_width_exact", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(3, 15, 13, 4, 4), i32(42), b(0))},
	// L4 at an arm: L past the root body.
	{name: "union_arm_L_past_body", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(3, 15, 13, 4, 9), i32(42), b(0))},
	// L3 at a body arm: the terminator early, the union None.
	{name: "union_body_arm_terminator_early", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(3, 15, 4, 13, 8, 5, 10), f32(2.5), b(0, 0xFF), gradeSilver, b(0))},
	// H9: a payload-free arm with L = 1.
	{name: "union_void_arm_L_nonzero", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(3, 15, 12, 32, 1, 0xFF), gradeSilver, b(0))},
	// H9: the payload-free arm as written.
	{name: "union_void_arm", unit: "tblr1", root: "Cfg", ids: cfgIds, body: b(3, 15, 12, 32, 0, 0)},
	// R2 at an arm: an arm this reader cannot name.
	{name: "union_unknown_arm", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(3, 15, 16, 4, 4), i32(42), gradeSilver, b(0))},
	// H9: an arm id of 0 in a field is the empty union and carries nothing.
	{name: "union_arm_zero_in_field", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(3, 15, 0), gradeSilver, b(0))},

	// ---------------------------------------------------------------- T. the node table
	// T1: one record, the pointer resolving to it.
	{name: "node_clean", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(1, listNodeRecord(2)), b(0))},
	// T3: node_count 2 over one record.
	{name: "node_count_disagrees", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(2, listNodeRecord(2)), b(0))},
	// T2: a record whose length runs past the field.
	{name: "node_record_L_past_field", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(1, cat(b(5, 9, 1, 4), i32(2), b(0))), b(0))},
	// T2: bytes left over inside the field.
	{name: "node_bytes_left_over", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(1, listNodeRecord(2), b(0xFF)), b(0))},
	// T2: the field under a kind other than 12.
	{name: "node_field_wrong_kind", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, b(4, 13, 10, 1), listNodeRecord(2), b(0))},
	// T4: a type id reference of 0.
	{name: "node_type_ref_zero", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(1, cat(b(0, 7, 1, 4), i32(2), b(0))), b(0))},
	// T7: an index above node_count + 1; that pointer null, the field after it
	// landing.
	{name: "node_index_above_count", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, b(3, 17, 3), b(2, 12, 1, 'a'), nodeTable(1, listNodeRecord(2)), b(0))},
	// §3.1: index 0 is null.
	{name: "node_index_null", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, b(3, 17, 0), nodeTable(1, listNodeRecord(2)), b(0))},
	// R6, T5: the reserved id inside a record body stops that body.
	{name: "node_reserved_id_in_record", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(1, b(5, 4, 4, 12, 0, 0)), b(0))},
	// T1, R5: the node table riding twice is the root body carrying one id
	// twice, which is damage.
	{name: "node_table_twice", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(1, listNodeRecord(2)), nodeTable(1, listNodeRecord(3)), b(0))},
	// T6: a reader that cannot name the reserved id counts one unknown.
	{name: "node_unknown_to_fixed_reader", unit: "tblr1", root: "Cfg", ids: append(append([]string{}, cfgIds...), "node-table"), body: cat(b(17, 12, 3, 1, 0, 0), gradeSilver, b(0))},
	// L4 at the node-table field: its L past the root body.
	{name: "node_table_L_past_body", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, b(4, 12, 99, 1), listNodeRecord(2), b(0))},
}

// counting is n distinct raw entries, 1..n: a reader checks distinctness and
// nothing else about an entry nobody references (F6, F7).
func counting(n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = uint64(i + 1)
	}
	return out
}

// bytes assembles one vector's file: the form byte, the body, the entries in
// slot order, then the count (docs/SPEC-TABLES.md §3).
func (v framingVector) bytes(u *units) ([]byte, error) {
	if v.raw != nil {
		return v.raw, nil
	}
	entries := v.entries
	if entries == nil {
		for _, name := range v.ids {
			id, err := framingId(u, v.unit, name)
			if err != nil {
				return nil, err
			}
			entries = append(entries, id)
		}
	}
	count := uint64(len(entries))
	if v.count != nil {
		count = *v.count
	}
	out := append([]byte{ir.TableWireForm}, v.body...)
	for _, e := range entries {
		out = append(out, u64(e)...)
	}
	return append(out, u64(count)...), nil
}

func framingId(u *units, unitKey, name string) (uint64, error) {
	switch {
	case name == "node-table":
		return ir.TableNodeWireId, nil
	case name == "build-version":
		return ir.TableBuildVersionWireId, nil
	case len(name) > 5 && name[:5] == "type:":
		unit, err := u.get(unitKey)
		if err != nil {
			return 0, err
		}
		st := tabletext.NewModel(unit).Lookup(name[5:])
		if st == nil {
			return 0, errUnknownType(name)
		}
		return ir.TableTypeId(st.WireName()), nil
	}
	return ir.TableWireId(name), nil
}

type errUnknownType string

func (e errUnknownType) Error() string { return "framing vector names no table " + string(e) }

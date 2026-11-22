package main

import (
	"encoding/binary"
	"os"
)

// THE REGRESSION VECTORS OF 2026-11-22 (docs/fix-ledger-1122.md): each holds
// one rule a per-language fix of that day established, as a shared row.
//
//   - rr_map_key_repeated: a map whose binary entries repeat a key (2, then 2
//     again) is damage, never a second value or a duplicate the reader keeps
//     (docs/SPEC-TABLES.md §2.8, §4: a map key written twice is damage).
//   - rr_fixed_zero_records: the lawful form-3 instance fixedevo_v3_full cut
//     after its header and layout, so it holds no record. A fixed-form file
//     holds at least one record; `rest == 0` refuses (§3.4, the load steps,
//     step 9).
func init() {
	framingVectors = append(framingVectors, framingVector{
		name: "rr_map_key_repeated", unit: "listdemo", root: "Squad", ids: squadIds,
		body: cat(b(1, 14, 32, 13, 2), squadEntry(2, 20), squadEntry(2, 90), name50, b(0)),
	})
	full, err := os.ReadFile("../../../testdata/conformance/tables/wire/fixedevo_v3_full.bin")
	if err != nil {
		panic(err)
	}
	layout := int(binary.LittleEndian.Uint32(full[16:20]))
	framingVectors = append(framingVectors, framingVector{
		name: "rr_fixed_zero_records", unit: "tblfixedevo", root: "EvoRoot",
		raw: append([]byte(nil), full[:20+layout]...),
	})
}

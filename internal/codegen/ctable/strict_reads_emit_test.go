package ctable

import (
	"strings"
	"testing"
)

// The fast lane of the 2026-11-21 review fixes: no compiler, only the emitted
// text, so a generator change that drops a read check shows here in
// milliseconds. The gated TestCStrictReads and TestCMessageStrictReads run the
// same rules through a compiled reader.
func strictReadsEmitted(t *testing.T, schema string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	return all.String()
}

func TestCStrictReadsEmitted(t *testing.T) {
	t.Parallel()
	message := strictReadsEmitted(t, strictReadsSchema)
	values := strictReadsEmitted(t, cStrictReadsSchema)
	writes := strictReadsEmitted(t, cWriteContractSchema)
	for _, tc := range []struct {
		name, text, want string
	}{
		// message form (C-1, C-2, C-3, union arm 0, bytes(N), named ranged)
		{"a zero-width ranged run is arithmetic (C-1)", message, "elem.value_bits==0)return 1;"},
		{"a skipped ranged value is held to its kind (C-2)", message, "static SCHEMA_UNUSED int table_message_ranged_holds("},
		{"a field id is seen once by id, not by slot (C-3)", message, "if(!table_message_seen_once(seen,r->vocabulary,entry))goto damaged;"},
		{"a slot carries the id's first slot (C-3)", message, "uint16_t id_slot;"},
		{"an unknown field is skipped as a field, arm 0 refused", message, "if(!table_message_skip_field(r,entry))goto damaged;"},
		{"bytes(N) adds the announced Min", message, " n+=(uint64_t)"},
		{"a named count past the announced Max is damage", message, " if(n>(uint64_t)"},
		{"a message count below Min is refused", message, "if(n<2)r->report->refused=1, r->report->refuse_reason=SCHEMA_TABLE_REFUSE_OUT_OF_RANGE;"},
		// counted arrays: born at Min, below Min refused on every read path
		{"reset sets the birth count", values, "value->arr_count = 2;"},
		{"the file read refuses a count below Min", values, "if ( count < 2 ) { r->report->refused=1, r->report->refuse_reason=SCHEMA_TABLE_REFUSE_OUT_OF_RANGE; }"},
		{"the fixed read counts a count below Min", values, "if ( value->arr_count < 2 ) { (*out_of_range)++; }"},
		{"the JSON read refuses a count below Min", values, "if ( f->array_min > 0 && placed < f->array_min )"},
		// a float32 range at float32 on the JSON read; -0 is +0
		{"the JSON read holds a float32 range at float32", values, "if ( nv < (float) f->range_min )"},
		{"the JSON read folds a compressed -0", values, "nv = nv + 0.0f;"},
		{"the file read folds a compressed -0", values, "decoded_f = decoded_f + 0.0f;"},
		{"the file writer stores +0 for a compressed -0", values, "table_float_to_bits( ( value->f ) + 0.0f )"},
		{"the fixed writer stores +0 for a compressed -0", values, "( value->f ) + 0.0f );"},
		// the variable save asserts SPEC.md §5.1 rows 1, 2, 3 and 10 in a debug build
		{"row 1 is a debug assert on a field, element and arm", writes, "(§5.1 row 1) */"},
		{"row 2 is a debug assert", writes, "(§5.1 row 2) */"},
		{"row 3 below Min is a debug assert", writes, "(§5.1 row 3) */"},
		{"row 10 is a debug assert", writes, "(§5.1 row 10) */"},
		// C-4: a later record's hash refusal resets what landed
		{"a record hash refusal resets records 0..k-1 (C-4)", values, "if ( table_fixed_get64( at ) != hash )\n        {\n            int64_t j;\n            for ( j = 0; j < k; ++j ) { root_reset( values + j ); }\n            memset( report, 0, sizeof( *report ) ); report->refused = 1; report->reason = SCHEMA_TABLE_NO_LAYOUT;"},
	} {
		if !strings.Contains(tc.text, tc.want) {
			t.Errorf("%s: the emitted C lacks %q", tc.name, tc.want)
		}
	}
}

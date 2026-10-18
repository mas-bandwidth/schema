package javatable

import "fmt"

type versioningRow struct {
	name    string
	widen   bool
	widened int
}

var versioningRows = []versioningRow{
	// the LIST rows (§5.7)
	{name: "field_append"},
	{name: "field_deprecate"},
	{name: "field_undeprecate"},
	{name: "enum_append"},
	{name: "enum_width", widen: true},
	{name: "union_append"},
	{name: "union_arm_payload_widen"}, // an APPEND despite its name (§5.7)
	{name: "flags_append"},
	{name: "keyed_array_enum_append"},
	{name: "nested_append"},
	{name: "rename_without_was"},
	// the NUMBER rows (§5.7)
	{name: "array_bounded_grow"},
	{name: "array_elem_widen", widen: true, widened: 4},
	{name: "array_fixed_grow"},
	{name: "bits_grow", widen: true},
	{name: "bytes_grow"},
	{name: "constant_grow"},
	{name: "fixed_I_grow", widen: true},
	{name: "fixed_I_grow_element", widen: true},
	{name: "float_widen", widen: true},
	{name: "int_widen", widen: true},
	{name: "optional_add"},
	{name: "range_widen"},
	{name: "string_grow"},
	{name: "uint_widen", widen: true},
	{name: "wstring_grow"},
}

// sameHashRows move no layout byte and no digest byte, so they have NO second
// column: both sides take the identity plan and the file reads in BOTH
// directions, which is the test that the hash, and only the hash, is the
// version (§5.7).
var sameHashRows = map[string]bool{
	"rename_without_was": true,
	"field_deprecate":    true,
	"field_undeprecate":  true,
}

// probeRun is one probe's answer: the report line's fields and the two dumps.
type probeRun struct {
	n            int
	refused      bool
	reason       string
	malformed    bool
	unknown      int
	kindMismatch int
	widened      int
	clamped      int
	layoutHash   string
	fresh        map[string]string
	value        map[string]string
}

// widenedError is the §5.4/§5.9 #33 verdict for the widened counter alone: an
// APPEND row owes zero, a WIDTH row owes nonzero, and a width row that declares
// an exact figure owes EXACTLY that — a widened run is never folded, so each
// element is its own widen entry and a folded read would report fewer and fail.
func widenedError(row versioningRow, r probeRun) string {
	if !row.widen {
		if r.widened != 0 {
			return fmt.Sprintf("widened=%d, want 0: an append the reader knows is not an event (§5.4)", r.widened)
		}
		return ""
	}
	if row.widened != 0 {
		if r.widened != row.widened {
			return fmt.Sprintf("widened=%d, want EXACTLY %d: a widened run is never folded, so each element is its own widen entry (§5.9 #33)", r.widened, row.widened)
		}
		return ""
	}
	if r.widened == 0 {
		return "widened=0, want nonzero: a width row widens once per entry per record (§5.4)"
	}
	return ""
}

// The RAW JSON VALUE in the IR (docs/SPEC-TABLES.md §16.13).
//
// A table line's `json = "value"` is a text-form fact, like `json = "array"`
// (§16.10): it moves no wire byte, the projection, the build version, the lock
// or the tables baseline. It is carried on the TABLE so the compiler can refuse
// it for every target but Go; the checker records it after it has held the
// declaration to §16.13's shape.
package ir

import (
	"sort"
)

// MarkJsonValue records that st is declared `json = "value"` (§16.13).
func (s *Struct) MarkJsonValue() {
	if s != nil {
		s.JsonValue = true
	}
}

// IsJsonValue reports a table declared `json = "value"` (§16.13).
func (s *Struct) IsJsonValue() bool {
	return s != nil && s.JsonValue
}

// TableJsonValues lists the tables declared `json = "value"` (§16.13), sorted —
// the names a target that does not carry the construct puts in its refusal.
func TableJsonValues(u *Unit) []string {
	var out []string
	for name, st := range u.Tables {
		if st != nil && st.JsonValue {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

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
	"sync"
)

// jsonValues marks the tables declared `json = "value"`. It is kept beside the
// declarations rather than in [Struct] so the marker cannot drift into a
// projection or a baseline by accident: nothing that walks the IR sees it
// unless it asks for this construct.
var jsonValues sync.Map // map[*Struct]struct{}

// MarkJsonValue records that st is declared `json = "value"` (§16.13).
func (s *Struct) MarkJsonValue() {
	if s != nil {
		jsonValues.Store(s, struct{}{})
	}
}

// IsJsonValue reports a table declared `json = "value"` (§16.13).
func (s *Struct) IsJsonValue() bool {
	if s == nil {
		return false
	}
	_, ok := jsonValues.Load(s)
	return ok
}

// TableJsonValues lists the tables declared `json = "value"` (§16.13), sorted —
// the names a target that does not carry the construct puts in its refusal.
func TableJsonValues(u *Unit) []string {
	var out []string
	for name, st := range u.Tables {
		if st != nil && st.IsJsonValue() {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

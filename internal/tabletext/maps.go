// MAPS in the instance model (docs/SPEC-TABLES.md §2.8).
//
// A map rides as kind 14 over element kind 13 — an array of the generated
// `{ key, value }` entry — so nothing here is a second framing. What is the
// map's own is the ORDER: entries are written in ASCENDING KEY ORDER with no
// key twice, and that is a WRITER's rule the reader verifies with one compare
// an entry.
package tabletext

import (
	"bytes"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// MapKey is one entry's key, in the two shapes a key has: the bytes of a
// `string(N)` and the value of an integer. Only one is meaningful, and the
// declaration says which.
type MapKey struct {
	Str []byte
	U   uint64
	I   int64
}

// MapKeyIsString reports whether a map's key is a `string(N)`.
func MapKeyIsString(f *ir.Field) bool { return ir.MapKeyField(f).Type.Kind == ir.TString }

// MapKeyOf reads one entry's key out of its instance.
func MapKeyOf(f *ir.Field, entry *Instance) MapKey {
	cell := &entry.Fields[0].Cell
	if MapKeyIsString(f) {
		return MapKey{Str: cell.Str}
	}
	return MapKey{U: cell.U, I: cell.I}
}

// MapKeyOrder is the comparison the sort and the reader's ascending check both
// use: byte-wise over the common prefix and then by length for a string key,
// and the numeric order for an integer one — signed where the declaration is.
func MapKeyOrder(f *ir.Field, a, b MapKey) int {
	key := ir.MapKeyField(f)
	if MapKeyIsString(f) {
		return bytes.Compare(a.Str, b.Str)
	}
	if key.Type.Kind == ir.TInt && key.Type.Signed {
		switch {
		case a.I < b.I:
			return -1
		case a.I > b.I:
			return 1
		}
		return 0
	}
	switch {
	case a.U < b.U:
		return -1
	case a.U > b.U:
		return 1
	}
	return 0
}

// MapEntryStruct is the generated entry table a map field's entries are
// instances of.
func MapEntryStruct(f *ir.Field) *ir.Struct { return f.MapEntry }

// NewMapEntry is one entry at its declared defaults, key included.
func (m *Model) NewMapEntry(f *ir.Field) *Instance { return m.New(f.MapEntry) }

// MapKeyFits is the reader's bound on a key, and KEYS NEVER CLAMP: a key past
// the declaration's capacity drops its whole entry and counts `clamped`, one
// per entry (docs/SPEC-TABLES.md §2.8), because a clamped key would merge two
// entries into one identity.
func MapKeyFits(f *ir.Field, key MapKey) bool {
	if !MapKeyIsString(f) {
		return true
	}
	return len(key.Str) <= int(ir.MapKeyField(f).Type.Size)
}

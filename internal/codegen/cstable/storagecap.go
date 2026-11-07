package cstable

import (
	"math"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// storageWireFloor is the longest wire of root that cannot command more than
// the storage cap (docs/SPEC-TABLES.md §11), so a managed Load of a wire no
// longer than it needs no measure pass to hold the cap. Every storage byte a
// measure counts is the root's own record or a term a wire byte pays for: a
// list or map count is at most the bytes left in its span, a node record and
// a text blob each cost at least one wire byte, and each term adds at most an
// alignment's padding and a 16-byte attribution entry. So one wire byte
// commands at most the unit's largest record or element plus that slack.
func storageWireFloor(u *ir.Unit, root *ir.Struct) int64 {
	if !ir.VariableTables(u)[root.Name] {
		return math.MaxInt64
	}
	unit := int64(16)
	consider := func(n int64) {
		if n > unit {
			unit = n
		}
	}
	field := func(f *ir.Field) {
		switch {
		case f.IsMap():
			consider(ir.RecordLayout(u, f.MapEntry).Size)
		case f.IsList():
			size, _ := ir.ListElementLayout(u, f)
			consider(size)
		}
	}
	for _, st := range u.Tables {
		consider(ir.RecordLayout(u, st).Size)
		for _, f := range st.Fields {
			field(f)
		}
	}
	for _, unions := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range unions {
			for _, v := range un.Variants {
				if !v.Void() {
					field(v.F)
				}
			}
		}
	}
	perByte := unit + 128 + 16
	room := ir.MaxSizeBytes - ir.RecordLayout(u, root).Size - 128
	if room <= 0 {
		return 0
	}
	return room / perByte
}

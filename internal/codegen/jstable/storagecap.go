package jstable

import "github.com/mas-bandwidth/schema/v2/ir"

// storageGateBytes is the longest wire a load of root reads without asking
// the measure whether it passes the storage cap (docs/SPEC-TABLES.md §11).
// Every term the region measure adds is paid for by distinct wire bytes: a
// list or map element by its count floor (at least one byte), a node record by
// its type reference and length, a text extent by its own bytes. So one wire
// byte commands at most the largest element, entry or record in the unit,
// plus an alignment pad, a region rounding and a node's attribution entry. A
// wire no longer than the answer measures under the cap, and the load skips
// the measure; a longer one is measured before anything is built. 0 means
// every wire is measured.
func storageGateBytes(u *ir.Unit, root *ir.Struct) int64 {
	align := ir.TableRegionAlign(u)
	each := int64(0)
	record := func(st *ir.Struct) {
		l := ir.RecordLayout(u, st)
		each = max(each, l.Size+l.Align)
		for _, f := range st.Fields {
			switch {
			case f.IsMap():
				m := ir.RecordLayout(u, f.MapEntry)
				each = max(each, m.Size+m.Align)
			case f.IsList():
				size, a := ir.ListElementLayout(u, f)
				each = max(each, size+a)
			}
		}
	}
	for _, st := range u.Tables {
		record(st)
	}
	for _, st := range u.Structs {
		record(st)
	}
	// a text extent is at most 9 bytes over its own length, rounded
	each += 2*align + 16 + 32
	fixed := ir.RecordLayout(u, root).Size + 4*align + 16
	if fixed >= ir.MaxSizeBytes {
		return 0
	}
	return (ir.MaxSizeBytes - fixed) / each
}

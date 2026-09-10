package darttable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// the plan's ops, and the whole set. The IDENTITY plan carries copy;
// the other ops are what a plan compiled from another writer's layout adds.
const (
	fixedOpCopy    = 0 // move size bytes
	fixedOpCount   = 1 // a count: clamp it to the reader's own bound
	fixedOpText    = 2 // a length, then the units
	fixedOpOrdinal = 3 // a variant ordinal, remapped through the plan's own table
	fixedOpWiden   = 4 // a narrower source into a wider destination
	fixedOpConst   = 5 // a constant this reader's own storage takes: a remapped union tag
	fixedOpWidenF  = 6 // f32 into f64, §4's float rung
)

// fixedNoGuard is the guard an entry that belongs to no union arm carries.
const fixedNoGuard = -1

// fixedPlanEntry is one plan entry, in the eight lanes the Dart runtime reads
// it through.
type fixedPlanEntry struct {
	op    int
	src   int64
	dst   int64
	size  int64
	aux   int64
	guard int64
	arg   int
	meta  int
	note  string
}

// THE IDENTITY PLAN IS ONE RUN (docs/SPEC-TABLES.md §3.4; Glenn's ruling: no
// prefill on identity path; straight-line scatter with decode clamps).
// When a record's hash equals this build's own, the plan is a single copy
// of the entire body image into the reader's storage. Every declared bound,
// range, count, length, union tag, and enum ordinal is checked and clamped
// in the decode projection.
func fixedIdentityPlan(st *ir.Struct) []fixedPlanEntry {
	bytes := fixedTypeBytes(st)
	if bytes == 0 {
		return nil
	}
	return []fixedPlanEntry{{
		op:    fixedOpCopy,
		src:   0,
		dst:   0,
		size:  bytes,
		guard: fixedNoGuard,
		note:  st.Name + ", whole",
	}}
}


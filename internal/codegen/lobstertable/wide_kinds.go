// The wide kinds on the table wire: a `timestamp` (kind 36), the one-sided
// integer range, the enum (30) and escape (31) kinds, `wstring(N)` (kind 33)
// and a plain float's -0, NaN and infinity (docs/SPEC-TABLES.md §2.11, §3,
// §4; SPEC.md §4.13; docs/PORT-LOBSTER.md, THE LOAD and THE NUMBER MODEL).
//
// The descriptor already carries every fact the walk needs — a timestamp is
// `k: 36` with `timestamp: true`, a one-sided range arrives as the two-sided
// range the checker filled (ir.Field.RangeOneSided is bookkeeping, not a
// projection token), an enum is `s: "enum"` with its variants, and wide text
// is `s: "str"` with `wide: true` — so the per-table module needs no extra
// code. This part exists so the group joins the descriptor emitter the way
// every later construct does (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT) and
// states where its rules live: runtime/wide_kinds.lobster replaces the
// scalar shape to spell kind 36 as the signed i64 it is, and
// runtime/schema_save.lobster already writes it at kind_width(36) = 8.
package lobstertable

import "github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"

func init() { Register("wide-kinds", emitWideKinds) }

// emitWideKinds is deliberately empty: every wide-kind fact is a descriptor
// column (docs/SPEC-TABLES.md §8.1), which lobstertable.go emits for all
// tables at once, and the wire rules are the runtime's. The empty part still
// runs in name order, so a failure to place it is visible in the module.
func emitWideKinds(*lobster.Gen) error { return nil }

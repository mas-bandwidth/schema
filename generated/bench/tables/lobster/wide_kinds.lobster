// wide_kinds.lobster — the table runtime's WIDE KINDS
// (docs/SPEC-TABLES.md §2.11, §3, §4; SPEC.md §4.13; docs/PORT-LOBSTER.md,
// THE LOAD and THE NUMBER MODEL). It joins load.lobster through the shape
// hook and never edits it: the last registration of a key wins, so replacing
// "scalar" here is the one line this file needs.
//
// Kind 36 is the timestamp (SPEC.md §4.13): an eight-byte two's-complement
// int64 of nanoseconds since the Unix epoch, and NOT the int64 kind 5 — a
// respelling between them is a kind mismatch and neither widens into the
// other. The scalar shape reads every other kind exactly as load.lobster's
// scalar_at does, which is where the clamps, defaults and widening live.
//
// The other wide kinds need no shape of their own here: enum (30), escape
// (31) and wstring (33) are dispatched by load.lobster's enum, skip and wstr
// shapes, the 128-bit and fixed-point family (18-29) is the scalar shape's
// kind_wide branch, a one-sided range reaches the descriptor as the
// two-sided range the checker filled (ir.Field.RangeOneSided), and
// schema_save.lobster writes kind 36 at kind_width(36) = 8.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// wide_timestamp_at decodes one kind 36 payload (docs/SPEC-TABLES.md §2.11):
// eight bytes little-endian, two's complement, stored as Lobster's signed
// int. A payload that does not fit its frame is damage, as every fixed-width
// scalar's is (§4).
def wide_timestamp_at(r:Reader, cell:Value, fd:FieldDescriptor) -> bool:
    let rep = r.st.report
    if not has(r, 8):
        rep.malformed = true
        return false
    cell.i = read_int64_le(r.b, r.off)
    r.off += 8
    return true

// WideScalarShape is load.lobster's scalar shape with kind 36 named: a
// timestamp field, array element or union arm is the signed i64 it is laid
// out as. Every other kind falls through to scalar_at unchanged.
class WideScalarShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        if sl.fd.k == 36:
            return wide_timestamp_at(r, sl.cell, sl.fd)
        return scalar_at(r, sl.cell, sl.fd, sl.fd.k, sl.fd.k)
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        if ek == 36:
            return wide_timestamp_at(r, sl.elems[i], elem_of(sl.fd))
        return scalar_at(r, sl.elems[i], elem_of(sl.fd), sl.fd.ek, ek)
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        if fv.fd.k == 36:
            return wide_timestamp_at(r, fv.cell, fv.fd)
        return scalar_at(r, fv.cell, fv.fd, fv.fd.k, fv.fd.k)

let _wide_scalar = register_shape(WideScalarShape { key: "scalar" })

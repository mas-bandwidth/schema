// descriptors_load.lobster — deprecated enum reads over descriptor metadata.
// This codec is emitted only for units with tables; the view imports no load.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .descriptors

namespace schema_rt

class DeprecatedEnumShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let ok = enum_cell(r, sl.cell, sl.fd)
        if ok and sl.cell.i > 0:
            if is_deprecated_variant(sl.fd.t, variant_name(sl.fd, sl.cell.i)):
                sl.cell.i = enum_default(sl.fd)
        return ok
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        let e = elem_of(sl.fd)
        let ok = enum_cell(r, sl.elems[i], e)
        if ok and sl.elems[i].i > 0:
            if is_deprecated_variant(e.t, variant_name(e, sl.elems[i].i)):
                sl.elems[i].i = 0
        return ok
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let ok = enum_cell(r, fv.cell, fv.fd)
        if ok and fv.cell.i > 0:
            if is_deprecated_variant(fv.fd.t, variant_name(fv.fd, fv.cell.i)):
                fv.cell.i = enum_default(fv.fd)
        if ok and r.off != r.lo + n:
            r.st.report.malformed = true
            return false
        return ok

let _descriptors_enum = register_shape(DeprecatedEnumShape { key: "enum" })


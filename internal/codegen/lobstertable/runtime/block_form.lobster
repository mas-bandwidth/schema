// block_form.lobster — the BLOCK form's builder (docs/SPEC-TABLES.md §19.1)
// and its reflective read (§19.2), beside the table descriptors
// (docs/PORT-LOBSTER.md, THE REGISTER: M7, I5).
//
// Nothing here knows a table: the layout is data the emitter writes beside the
// descriptors, and this is the ONE walk over it.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .schema_tables
import .load

namespace schema_rt

class BlockField:
    name:string
    offset:int
    size:int
    kind:int
    out_of_line:bool = false
    offset_of_offset:int = -1
    count_offset:int = -1
    stride_offset:int = -1
    stride:int = 0
    is_array:bool = false
    counted:bool = false
    optional:bool = false
    bound:int = 0
    elem_size:int = 0
    present_offset:int = -1
    elem:string = ""
    // an out-of-line array's declared [..N] maximum and where its rows may
    // start: aligned to max(64, alignof(element)) (§19.1)
    max:int = 0
    start_align:int = 64

class BlockRecord:
    name:string = ""
    size:int = 0
    align:int = 0
    fields:[BlockField] = []

class BlockLayout:
    table:string = ""
    max_bytes:int = 0
    build_version:int = 0
    projection:BlockRecord = BlockRecord {}
    records:[BlockRecord] = []

let block_registry:[BlockLayout] = []

def register_block(b:BlockLayout) -> BlockLayout:
    push(block_registry, b)
    return b

def lookup_block(name:string) -> BlockLayout?:
    for(block_registry) b:
        if b.table == name:
            return b
    return nil

def block_record(b:BlockLayout, name:string) -> BlockRecord?:
    if b.projection.name == name:
        return b.projection
    for(b.records) r:
        if r.name == name:
            return r
    return nil

// block_non_finite names the first NaN or infinity the last block_read or
// block_build met, <Owner>.<field> (docs/SPEC-TABLES.md §3.3): a block float
// is finite, refused on its raw bits by the read and by the builder.
var block_non_finite = ""

def block_float_at(fd:FieldDescriptor, data:string, at:int, size:int) -> float:
    if size == 4:
        let bits = read_uint32_le(data, at)
        if not finite_f32_bits(bits):
            if block_non_finite == "":
                block_non_finite = float_owner(fd)
            return 0.0
        return read_float32_le(data, at)
    let bits = read_int64_le(data, at)
    if not finite_f64_bits(bits):
        if block_non_finite == "":
            block_non_finite = float_owner(fd)
        return 0.0
    return read_float64_le(data, at)

def block_float_put(fd:FieldDescriptor, data:string, at:int, size:int, x:float) -> string:
    let ok = if size == 4: finite_f32(x) else: finite_f64(x)
    if not ok and block_non_finite == "":
        block_non_finite = float_owner(fd)
    return if size == 4: write_float32_le(data, at, x) else: write_float64_le(data, at, x)

let block_magic = 0x4b4c42414d484353
// block_magic_swapped is the same word in the other order (§19.1)
let block_magic_swapped = 0x5343484d41424c4b
let block_byte_order = 1

def bq(s:string) -> string:
    var out = "\""
    for(s) c, i:
        if c >= 0x20 and c < 0x7f and c != 34 and c != 92:
            out += substring(s, i, 1)
        else:
            out += "\\x" + lowercase(number_to_string(c, 16, 2))
    return out + "\""

def bx(v:int, digits:int) -> string:
    return lowercase(number_to_string(v, 16, digits))

def bscalar(sl:Slot, bf:BlockField) -> string:
    let s = sl.fd.s
    if s == "bool":
        return if sl.cell.b: "true" else: "false"
    if s == "float":
        if bf.elem_size == 4:
            return "0x" + bx(f32bits(sl.cell.f), 8)
        return "0x" + bx(f64bits(sl.cell.f), 16)
    if s == "int" and bf.elem_size == 16:
        return "{sl.cell.w.hi}:{sl.cell.w.lo}"
    return "{sl.cell.i}"

def bread_int(s:string, at:int, size:int, signed:bool) -> int:
    if size == 1:
        let v = read_uint8_le(s, at)
        return if signed and v >= 0x80: v - 0x100 else: v
    if size == 2:
        let v = read_uint16_le(s, at)
        return if signed and v >= 0x8000: v - 0x10000 else: v
    if size == 4:
        let v = read_uint32_le(s, at)
        return if signed and v >= 0x80000000: v - 0x100000000 else: v
    return if signed: read_int64_le(s, at) else: read_uint64_le(s, at)

def bwrite_int(s:string, at:int, size:int, v:int) -> string:
    if size == 1:
        return write_int8_le(s, at, v)
    if size == 2:
        return write_int16_le(s, at, v)
    if size == 4:
        return write_int32_le(s, at, v)
    return write_int64_le(s, at, v)

// block_read_record reads one record at `at` into the Record model the load
// walk builds (docs/PORT-LOBSTER.md, THE LOAD), so a built block and a loaded
// value are the same shape.
def block_read_record(bl:BlockLayout, br:BlockRecord, d:TableDescriptor, data:string, at:int) -> Record:
    let rec = new_record(d)
    for(br.fields) bf, i:
        if bf.out_of_line:
            continue
        let sl = rec.fields[i]
        block_read_field(bl, bf, sl, data, at)
    return rec

def block_read_field(bl:BlockLayout, bf:BlockField, sl:Slot, data:string, at:int) -> void:
    let f = sl.fd
    let base = at + bf.offset
    let s = f.s
    if bf.counted and (s == "str" or s == "text" or s == "bytes"):
        sl.count = read_int32_le(data, at + bf.count_offset)
        sl.cell.s = substring(data, base, sl.count)
    elif bf.is_array and s == "arr":
        let e = elem_of(f)
        for(sl.elems) v, k:
            block_read_elem(bl, bf, v, e, data, base + k * bf.elem_size)
    elif s == "tab":
        if sl.cell.tab:
            let sub = block_record(bl, bf.elem)
            if sub:
                sl.cell.tab = block_read_record(bl, sub, table_of(f.t), data, base)
    elif s == "bool":
        sl.cell.b = read_uint8_le(data, base) != 0
    elif s == "float":
        sl.cell.f = block_float_at(f, data, base, bf.elem_size)
    elif s == "int":
        if bf.elem_size == 16:
            sl.cell.w = Int128 { lo: read_int64_le(data, base), hi: read_int64_le(data, base + 8) }
        else:
            sl.cell.i = bread_int(data, base, bf.elem_size, not f.signed or f.s == "enum")
    elif s == "enum":
        sl.cell.i = bread_int(data, base, bf.elem_size, false)
    if bf.optional:
        sl.present = read_uint8_le(data, at + bf.present_offset) != 0

def block_read_elem(bl:BlockLayout, bf:BlockField, v:Value, fd:FieldDescriptor, data:string, at:int):
    if bf.elem != "":
        let sub = block_record(bl, bf.elem)
        assert sub
        v.tab = block_read_record(bl, sub, table_of(bf.elem), data, at)
    else:
        let s = fd.s
        if s == "bool":
            v.b = read_uint8_le(data, at) != 0
        elif s == "float":
            v.f = block_float_at(fd, data, at, bf.elem_size)
        elif s == "int" and bf.elem_size == 16:
            v.w = Int128 { lo: read_int64_le(data, at), hi: read_int64_le(data, at + 8) }
        else:
            v.i = bread_int(data, at, bf.elem_size, false)

// block_read is the block's whole read: nil for a block this build does not
// open, and nil for one holding a NaN or an infinity in any float, named in
// block_non_finite (docs/SPEC-TABLES.md §3.3).
def block_read(root:string, data:string) -> Record?:
    block_non_finite = ""
    let rec = block_read_all(root, data)
    if block_non_finite != "":
        return nil
    return rec

def block_read_all(root:string, data:string) -> Record?:
    let bl = lookup_block(root)
    if not bl:
        return nil
    if block_open_reason(root, data, 0, false) != "ok":
        return nil
    let rec = block_read_record(bl, bl.projection, table_of(root), data, 0)
    for(bl.projection.fields) bf, i:
        if not bf.out_of_line:
            continue
        let sl = rec.fields[i]
        let off = read_uint64_le(data, bf.offset)
        let count = read_uint32_le(data, bf.offset + 8)
        let stride = read_uint32_le(data, bf.offset + 12)
        let sub = block_record(bl, bf.elem)
        if not sub:
            continue
        sl.count = count
        sl.elems = []
        for(count) r:
            let v = block_read_record(bl, sub, table_of(bf.elem), data, off + r * stride)
            push(sl.elems, Value { tab: v })
    return rec

def block_open(root:string, data:string) -> bool:
    return block_open_reason(root, data, 0, false) == "ok"

// block_open_reason is the block form's Open (docs/SPEC-TABLES.md §19.2;
// internal/codegen/gotable's emitBlockOpen): every prologue and layout word
// read before the final base-alignment check, in §19.2's order, answering the
// first failing clause as the refusal enum's value name, or "ok". data is
// exactly the extent the caller claims; lead is how far past a 64-aligned
// base its buffer sits, and nobuf is a NULL buffer.
def block_open_reason(root:string, data:string, lead:int, nobuf:bool) -> string:
    let bl = lookup_block(root)
    assert bl
    if nobuf:
        return "unaligned_base"
    let bytes = length(data)
    let head = bl.projection.size
    if bytes < head:
        return "truncated"
    let magic = read_uint64_le(data, 0)
    if magic != block_magic:
        if magic == block_magic_swapped:
            return "foreign_order"
        return "not_a_cook"
    if read_uint64_le(data, 16) != block_byte_order:
        return "not_a_cook"
    if read_uint64_le(data, 8) != bl.build_version:
        return "wrong_build_version"
    var used = head
    for(bl.projection.fields) bf:
        if not bf.out_of_line:
            continue
        // every number here comes from the instance: an offset with the top
        // bit set reads negative and falls to the first bound below, and
        // count and stride are bounded before they multiply
        let offset_of = read_uint64_le(data, bf.offset_of_offset)
        let count = read_uint32_le(data, bf.count_offset)
        let stride = read_uint32_le(data, bf.stride_offset)
        if stride != bf.stride:
            return "bad_layout"
        if count > bf.max:
            return "bad_layout"
        if offset_of < head or offset_of % bf.start_align != 0:
            return "bad_layout"
        if offset_of > bytes:
            return "bad_layout"
        let rows = count * stride
        if rows > bytes - offset_of:
            return "bad_layout"
        if offset_of + rows > used:
            used = offset_of + rows
    let padding = (64 - used % 64) % 64
    if padding > bytes - used:
        return "truncated"
    if lead % 64 != 0:
        return "unaligned_base"
    return "ok"

// block_dump is THE ROW DUMP (testdata/conformance/tables/FORMAT.md): the
// projection's own leaves, then every array row, in descriptor order.
def block_dump(root:string, data:string) -> string:
    let bl = lookup_block(root)
    assert bl
    let rec = block_read(root, data)
    assert rec
    var out = "projection {bl.projection.name} @0\n"
    out += block_dump_record(bl, bl.projection, rec, "")
    for(bl.projection.fields) bf, i:
        if not bf.out_of_line:
            continue
        let sl = rec.fields[i]
        let off = read_uint64_le(data, bf.offset)
        let count = read_uint32_le(data, bf.offset + 8)
        let stride = read_uint32_le(data, bf.offset + 12)
        let sub = block_record(bl, bf.elem)
        assert sub
        out += "array {bf.name} {sub.name} @{off} count={count} stride={stride}\n"
        for(count) r:
            let rowrec = sl.elems[r].tab
            assert rowrec
            out += "row {r} @{off + r * stride}\n"
            out += block_dump_record(bl, sub, rowrec, "")
    return out

def block_dump_record(bl:BlockLayout, br:BlockRecord, rec:Record, prefix:string) -> string:
    var out = ""
    for(br.fields) bf, i:
        if bf.out_of_line:
            continue
        let sl = rec.fields[i]
        let name = if prefix == "": bf.name else: prefix + "." + bf.name
        if bf.counted and (sl.fd.s == "str" or sl.fd.s == "text" or sl.fd.s == "bytes"):
            out += "  {name} = {bq(sl.cell.s)} len={sl.count}\n"
        elif bf.is_array and sl.fd.s == "arr":
            for(sl.elems) v, k:
                if bf.elem != "":
                    let sub = block_record(bl, bf.elem)
                    assert sub
                    assert v.tab
                    out += block_dump_record(bl, sub, v.tab, "{name}[{k}]")
                else:
                    out += "  {name}[{k}] = {bscalar_elem(v, sl.fd.e, bf)}\n"
        elif sl.fd.s == "tab":
            let sub = block_record(bl, bf.elem)
            if sub:
                if sl.cell.tab:
                    out += block_dump_record(bl, sub, sl.cell.tab, name)
        else:
            out += "  {name} = {bscalar(sl, bf)}\n"
        if bf.optional:
            out += "  {name}#present = " + (if sl.present: "true" else: "false") + "\n"
    return out

def bscalar_elem(v:Value, fd:FieldDescriptor?, bf:BlockField) -> string:
    assert fd
    let s = fd.s
    if s == "bool":
        return if v.b: "true" else: "false"
    if s == "float":
        if bf.elem_size == 4:
            return "0x" + bx(f32bits(v.f), 8)
        return "0x" + bx(f64bits(v.f), 16)
    return "{v.i}"

def balign(at:int, a:int) -> int:
    return (at + a - 1) / a * a

// block_build is §19.1's builder: one storage sized from the declared maxima,
// the prologue and every triple written once, then the rows. It refuses a
// count past its array's maximum by asserting, exactly as Begin returns false,
// and a NaN or an infinity in any float by answering "" with the field named
// in block_non_finite (docs/SPEC-TABLES.md §3.3).
def block_build(root:string, rec:Record) -> string:
    block_non_finite = ""
    let data = block_build_all(root, rec)
    if block_non_finite != "":
        return ""
    return data

def block_build_all(root:string, rec:Record) -> string:
    let bl = lookup_block(root)
    assert bl
    var data = zeros(bl.max_bytes)
    data = write_int64_le(data, 0, block_magic)
    data = write_int64_le(data, 8, bl.build_version)
    data = write_int64_le(data, 16, block_byte_order)
    var offset = bl.projection.size
    for(bl.projection.fields) bf, i:
        let sl = rec.fields[i]
        if bf.out_of_line:
            let sub = block_record(bl, bf.elem)
            let a = if sub and sub.align > 64: sub.align else: 64
            offset = balign(offset, a)
            let count = length(sl.elems)
            assert count <= bf.bound
            data = write_int64_le(data, bf.offset, offset)
            data = write_int32_le(data, bf.offset + 8, count)
            data = write_int32_le(data, bf.offset + 12, bf.stride)
            offset += count * bf.stride
        else:
            data = block_write_field(bl, bf, sl, data, 0)
    for(bl.projection.fields) bf, i:
        if not bf.out_of_line:
            continue
        let sl = rec.fields[i]
        let off = read_uint64_le(data, bf.offset)
        let stride = read_uint32_le(data, bf.offset + 12)
        let sub = block_record(bl, bf.elem)
        assert sub
        for(sl.elems) v, r:
            assert v.tab
            data = block_write_record(bl, sub, v.tab, data, off + r * stride)
    return data

def block_write_record(bl:BlockLayout, br:BlockRecord, rec:Record, data:string, at:int) -> string:
    for(br.fields) bf, i:
        if bf.out_of_line:
            continue
        data = block_write_field(bl, bf, rec.fields[i], data, at)
    return data

def block_write_field(bl:BlockLayout, bf:BlockField, sl:Slot, data:string, at:int) -> string:
    let f = sl.fd
    let base = at + bf.offset
    let s = f.s
    if bf.counted and (s == "str" or s == "text" or s == "bytes"):
        data = write_substring(data, base, sl.cell.s, false)
        data = write_int32_le(data, at + bf.count_offset, sl.count)
    elif bf.is_array and s == "arr":
        for(sl.elems) v, k:
            if bf.elem != "":
                let sub = block_record(bl, bf.elem)
                assert sub
                assert v.tab
                data = block_write_record(bl, sub, v.tab, data, base + k * bf.elem_size)
            else:
                data = block_write_elem(bf, v, elem_of(f), data, base + k * bf.elem_size)
    elif s == "tab":
        let sub = block_record(bl, bf.elem)
        if sub and sl.cell.tab:
            data = block_write_record(bl, sub, sl.cell.tab, data, base)
    elif s == "bool":
        data = write_int8_le(data, base, if sl.cell.b: 1 else: 0)
    elif s == "float":
        data = block_float_put(f, data, base, bf.elem_size, sl.cell.f)
    elif s == "int" and bf.elem_size == 16:
        data = write_int64_le(data, base, sl.cell.w.lo)
        data = write_int64_le(data, base + 8, sl.cell.w.hi)
    else:
        data = bwrite_int(data, base, bf.elem_size, sl.cell.i)
    if bf.optional:
        data = write_int8_le(data, at + bf.present_offset, if sl.present: 1 else: 0)
    return data

def block_write_elem(bf:BlockField, v:Value, fd:FieldDescriptor, data:string, at:int) -> string:
    let s = fd.s
    if s == "bool":
        return write_int8_le(data, at, if v.b: 1 else: 0)
    if s == "float":
        return block_float_put(fd, data, at, bf.elem_size, v.f)
    if s == "int" and bf.elem_size == 16:
        let d = write_int64_le(data, at, v.w.lo)
        return write_int64_le(d, at + 8, v.w.hi)
    return bwrite_int(data, at, bf.elem_size, v.i)

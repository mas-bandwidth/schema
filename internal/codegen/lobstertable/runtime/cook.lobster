// cook.lobster — the cooked form: the header check, the region writer and the
// read back into the record model (docs/SPEC-TABLES.md §7, §7.1, §7.2, §7.6;
// docs/PORT-LOBSTER.md, THE CARD MAP). A cook is a header and then the region
// `Lock` would pack, written verbatim (internal/tablecook/cook.go, region.go,
// extent.go and uncook.go): Open is O(1) in the file's size and checks
// identity only; CookMeasure and Cook derive the depth-first numbering and
// write it; ReadCook turns it back into the Record a wire save writes.
//
// THE MAGIC IS READ BYTEWISE FIRST, because it settles the byte order every
// other header word is written in: this build's constant either is what a
// little-endian store produces or its byte reversal, which names the other
// order, or it is not a cook at all. The data offset is DERIVED from the
// header's alignment, never stored.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .schema_tables
import .load
import .schema_save
import .fixed

namespace schema_rt

// cook_magic is "SCHMCOOK" read as ASCII in the low-to-high order a
// little-endian store produces (internal/tablecook.Magic).
let cook_magic = 0x4B4F4F434D484353
// cook_magic_swapped is the same word in the other order: what a big-endian
// cook's first eight bytes read as on a little-endian reader.
let cook_magic_swapped = 0x5343484D434F4F4B

let cook_header_bytes = 64
let cook_byte_order_little = 1
let cook_byte_order_big = 2
let cook_directory_entry = 16

// cook_bswap64 reverses a word's bytes, the read a big-endian cook needs on
// every header word (docs/PORT-LOBSTER.md, THE NUMBER MODEL: no unsigned
// shift, so the mask follows each shift).
def cook_bswap64(x:int) -> int:
    let b0 = x & 0xff
    let b1 = (x >> 8) & 0xff
    let b2 = (x >> 16) & 0xff
    let b3 = (x >> 24) & 0xff
    let b4 = (x >> 32) & 0xff
    let b5 = (x >> 40) & 0xff
    let b6 = (x >> 48) & 0xff
    let b7 = (x >> 56) & 0xff
    return (b0 << 56) | (b1 << 48) | (b2 << 40) | (b3 << 32) | (b4 << 24) | (b5 << 16) | (b6 << 8) | b7

def cook_bswap32(x:int) -> int:
    return ((x & 0xff) << 24) | ((x & 0xff00) << 8) | ((x >> 8) & 0xff00) | ((x >> 24) & 0xff)

def cook_bswap16(x:int) -> int:
    return ((x & 0xff) << 8) | ((x >> 8) & 0xff)

def cook_word(data:string, at:int, big:bool) -> int:
    let v = read_uint64_le(data, at)
    if big:
        return cook_bswap64(v)
    return v

def cook_word32(data:string, at:int, big:bool) -> int:
    let v = read_uint32_le(data, at)
    if big:
        return cook_bswap32(v)
    return v

// cook_align_up rounds v up to a multiple of a (a is a power of two).
def cook_align_up(v:int, a:int) -> int:
    return (v + a - 1) & ~(a - 1)

// cook_open is `Open`'s check for one table (docs/SPEC-TABLES.md §7, §7.1;
// internal/tablecook.ReadHeader): the magic, the order it establishes, the
// build version, every reserved word zero, the alignment, and both part
// lengths against the size the caller passed — a truncated file refuses.
def cook_open(data:string, build:int, root_align:int, root_size:int) -> bool:
    let bytes = length(data)
    if bytes < cook_header_bytes:
        return false
    let magic = read_uint64_le(data, 0)
    var big = false
    if magic == cook_magic:
        big = false
    elif magic == cook_magic_swapped:
        big = true
    else:
        return false
    let order = cook_word(data, 16, big)
    if order != (if big: cook_byte_order_big else: cook_byte_order_little):
        return false
    if cook_word(data, 8, big) != build:
        return false
    if cook_word(data, 48, big) != 0 or cook_word(data, 56, big) != 0:
        return false
    let alignment = cook_word(data, 40, big)
    if alignment < 8 or alignment > 64 or (alignment & (alignment - 1)) != 0:
        return false
    if root_align > 0 and alignment % root_align != 0:
        return false
    let data_offset = cook_align_up(cook_header_bytes, alignment)
    let data_length = cook_word(data, 24, big)
    let attrib_length = cook_word(data, 32, big)
    if data_offset > bytes:
        return false
    if data_length < 0 or data_length > bytes - data_offset:
        return false
    if attrib_length != bytes - data_offset - data_length:
        return false
    if data_length < root_size:
        return false
    return true

// ---- the layout, as the emitter folds it (docs/SPEC-TABLES.md §7.2) ----

class CookField:
    off:int = 0
    size:int = 0
    count:int = -1
    present:int = -1
    elem:int = 0
    esize:int = 0
    ecount:int = -1
    epresent:int = -1
    lsize:int = 0
    lalign:int = 1
    lvalue:int = 0
    lcount:int = -1
    tag:int = 0
    arm:int = 0

class CookTable:
    name:string
    size:int = 0
    align:int = 8
    record:int = 0
    extent:bool = false
    fields:[CookField] = []

class CookUnion:
    name:string
    size:int = 0
    tag:int = 0
    arm:int = 0
    extent:bool = false
    arms:[CookField] = []

let cook_tables:[CookTable] = []
let cook_unions:[CookUnion] = []

def cook_table(t:CookTable) -> CookTable:
    push(cook_tables, t)
    return t

def cook_union_of(u:CookUnion) -> CookUnion:
    push(cook_unions, u)
    return u

def cook_table_of(name:string) -> CookTable?:
    for(cook_tables) t:
        if t.name == name:
            return t
    return nil

def cook_union_named(name:string) -> CookUnion?:
    for(cook_unions) u:
        if u.name == name:
            return u
    return nil

// ---- output buffer (BYTE BUFFERS: a write returns the string) ----

class CookOut:
    b:string

def co_u8(o:CookOut, at:int, v:int):
    o.b = write_int8_le(o.b, at, v)

def co_u16(o:CookOut, at:int, v:int, big:bool):
    if big:
        v = cook_bswap16(v & 0xffff)
    o.b = write_int16_le(o.b, at, v)

def co_u32(o:CookOut, at:int, v:int, big:bool):
    if big:
        v = cook_bswap32(v)
    o.b = write_int32_le(o.b, at, v)

def co_u64(o:CookOut, at:int, v:int, big:bool):
    if big:
        v = cook_bswap64(v)
    o.b = write_int64_le(o.b, at, v)

def co_bytes(o:CookOut, at:int, s:string):
    o.b = write_substring(o.b, at, s, false)

// ---- the numbering (docs/SPEC-TABLES.md §3.1, §7.6; Go's NumberPacking) ----

class CookNode:
    kind:int = 0
    rec:Record? = nil
    blob:string? = nil
    off:int = 0
    record:int = 0
    extent:int = 0

class CookGraph:
    nodes:[CookNode] = []
    recs:[Record] = []
    rec_index:[int] = []
    blobs:[string] = []
    blob_index:[int] = []
    open:[bool] = []
    cycle:bool = false

def cg_rec_index(g:CookGraph, rec:Record) -> int:
    for(g.recs) r, i:
        if r == rec:
            return g.rec_index[i]
    return 0

def cg_blob_index(g:CookGraph, b:string) -> int:
    for(g.blobs) x, i:
        if x == b:
            return g.blob_index[i]
    return 0

def cg_add_rec(g:CookGraph, rec:Record) -> int:
    let at = cg_rec_index(g, rec)
    if at != 0:
        if g.open[at - 1]:
            g.cycle = true
        return at
    push(g.nodes, CookNode { kind: 1, rec: rec })
    let idx = length(g.nodes)
    push(g.recs, rec)
    push(g.rec_index, idx)
    push(g.open, true)
    cg_visit_edges(g, rec)
    g.open[idx - 1] = false
    return idx

def cg_add_blob(g:CookGraph, b:string, kind:int) -> int:
    let at = cg_blob_index(g, b)
    if at != 0:
        return at
    let idx = length(g.nodes) + 1
    push(g.nodes, CookNode { kind: kind, blob: b })
    push(g.blobs, b)
    push(g.blob_index, idx)
    return idx

def cg_visit_value(g:CookGraph, v:Value, d:FieldDescriptor) -> void:
    let s = d.s
    if s == "tab":
        let t = v.tab
        if t:
            cg_visit_edges(g, t)
    elif s == "ptr":
        let n = v.node
        if n:
            cg_add_rec(g, n)
    elif s == "sblob":
        let b = v.blob
        if b:
            cg_add_blob(g, b, 2)
    elif s == "bblob":
        let b = v.blob
        if b:
            cg_add_blob(g, b, 3)
    elif s == "union":
        cg_visit_union(g, v, d)

def cg_visit_union(g:CookGraph, v:Value, d:FieldDescriptor) -> void:
    if v.i == 0:
        return
    let un = lookup_union(d.t)
    assert un
    if v.i < 1 or v.i > length(un.arms):
        return
    let arm = un.arms[v.i - 1]
    if arm.s == "tab":
        let t = v.tab
        if t:
            cg_visit_edges(g, t)
    elif arm.s == "arr":
        let sl = v.arm
        if sl:
            let e = elem_of(arm)
            let n = save_live_count(arm, sl)
            var i = 0
            while i < n:
                cg_visit_value(g, sl.elems[i], e)
                i++
    elif arm.s == "union":
        let sl = v.arm
        if sl:
            cg_visit_union(g, sl.cell, arm)
    else:
        let sl = v.arm
        if sl:
            cg_visit_value(g, sl.cell, arm)

def cg_visit_edges(g:CookGraph, rec:Record) -> void:
    for(rec.fields) sl:
        let f = sl.fd
        if f.present != "" and not sl.present:
            continue
        if f.s == "map":
            for(sg_map_order(f, sl)) at:
                let er = sl.entries[at].tab
                if er:
                    cg_visit_edges(g, er)
            continue
        if f.s == "arr":
            let e = elem_of(f)
            let n = save_live_count(f, sl)
            var i = 0
            while i < n:
                cg_visit_value(g, sl.elems[i], e)
                i++
            continue
        cg_visit_value(g, sl.cell, f)

// ---- the record writer (docs/SPEC-TABLES.md §7.2, §7.6) ----

class CookWrite:
    o:CookOut
    g:CookGraph
    big:bool

def cw_ref(c:CookWrite, at:int, rec:Record?) -> void:
    if rec:
        let to = cg_rec_index(c.g, rec)
        if to != 0:
            co_u64(c.o, at, cook_header_bytes + c.g.nodes[to - 1].off - at, c.big)
            return
    co_u64(c.o, at, 0, c.big)

def cw_ref_blob(c:CookWrite, at:int, b:string) -> void:
    let to = cg_blob_index(c.g, b)
    if to != 0:
        co_u64(c.o, at, cook_header_bytes + c.g.nodes[to - 1].off - at, c.big)
        return
    co_u64(c.o, at, 0, c.big)

def cw_int(c:CookWrite, at:int, width:int, v:int) -> void:
    if width <= 1:
        co_u8(c.o, at, v & 0xff)
    elif width == 2:
        co_u16(c.o, at, v, c.big)
    elif width == 4:
        co_u32(c.o, at, v, c.big)
    else:
        co_u64(c.o, at, v, c.big)

def cw_wide(c:CookWrite, at:int, v:Int128, width:int) -> void:
    if width <= 8:
        cw_int(c, at, width, v.lo)
        return
    if c.big:
        co_u64(c.o, at, v.hi, true)
        co_u64(c.o, at + 8, v.lo, true)
    else:
        co_u64(c.o, at, v.lo, false)
        co_u64(c.o, at + 8, v.hi, false)

def cw_str(c:CookWrite, at:int, value:string, used:int, bufsize:int, countoff:int) -> void:
    var n = used
    if n < 0:
        n = 0
    if n > bufsize:
        n = bufsize
    if n > 0:
        co_bytes(c.o, at, substring(value, 0, n))
    if countoff >= 0:
        co_u32(c.o, at + countoff, n, c.big)

def cw_units(c:CookWrite, at:int, units:[int], bufsize:int, countoff:int) -> void:
    var n = length(units)
    if n * 2 > bufsize:
        n = bufsize / 2
    var i = 0
    while i < n:
        co_u16(c.o, at + i * 2, units[i] & 0xffff, c.big)
        i++
    if countoff >= 0:
        co_u32(c.o, at + countoff, n, c.big)

def cw_field(c:CookWrite, at:int, sl:Slot, cf:CookField, fd:FieldDescriptor) -> void:
    if fd.present != "":
        if not sl.present:
            return
        if cf.present >= 0:
            co_u8(c.o, at + cf.present, 1)
    let s = fd.s
    if s == "tab":
        let t = sl.cell.tab
        if t:
            let ct = cook_table_of(fd.t)
            assert ct
            cw_record(c, at, t, ct)
    elif s == "ptr":
        cw_ref(c, at, sl.cell.node)
    elif s == "sblob" or s == "bblob":
        let b = sl.cell.blob
        if b:
            cw_ref_blob(c, at, b)
    elif s == "union":
        cw_union(c, at, sl.cell, fd, cf)
    elif s == "str":
        if fd.wide:
            cw_units(c, at, sl.cell.units, cf.size, cf.count)
        else:
            cw_str(c, at, sl.cell.s, sl.count, cf.size, cf.count)
    elif s == "text":
        cw_str(c, at, sl.cell.s, length(sl.cell.s), cf.size, cf.count)
    elif s == "bytes":
        cw_str(c, at, sl.cell.s, sl.count, cf.size, cf.count)
    elif s == "bool":
        co_u8(c.o, at, if sl.cell.b: 1 else: 0)
    elif s == "float":
        if fd.f32:
            co_u32(c.o, at, save_narrow32(sl.cell.f), c.big)
        else:
            co_u64(c.o, at, f64bits(sl.cell.f), c.big)
    elif s == "enum":
        cw_int(c, at, cf.size, sl.cell.i)
    elif s == "arr":
        cw_array(c, at, sl, cf, fd)
    elif s == "map":
        return
    else:
        if fd.dom == "int128":
            cw_wide(c, at, sl.cell.w, cf.size)
        else:
            cw_int(c, at, cf.size, sl.cell.i)

def cw_elem(c:CookWrite, at:int, e:FieldDescriptor, v:Value, vsize:int, countoff:int, tag:int, arm:int) -> void:
    let s = e.s
    if s == "ptr":
        cw_ref(c, at, v.node)
    elif s == "sblob" or s == "bblob":
        let b = v.blob
        if b:
            cw_ref_blob(c, at, b)
    elif s == "tab":
        let t = v.tab
        if t:
            let ct = cook_table_of(e.t)
            assert ct
            cw_record(c, at, t, ct)
    elif s == "union":
        cw_union(c, at, v, e, CookField { tag: tag, arm: arm })
    elif s == "str":
        if e.wide:
            cw_units(c, at, v.units, vsize, countoff)
        else:
            cw_str(c, at, v.s, length(v.s), vsize, countoff)
    elif s == "text":
        cw_str(c, at, v.s, length(v.s), vsize, countoff)
    elif s == "bytes":
        cw_str(c, at, v.s, length(v.s), vsize, countoff)
    elif s == "bool":
        co_u8(c.o, at, if v.b: 1 else: 0)
    elif s == "float":
        if e.f32:
            co_u32(c.o, at, save_narrow32(v.f), c.big)
        else:
            co_u64(c.o, at, f64bits(v.f), c.big)
    elif s == "enum":
        cw_int(c, at, vsize, v.i)
    else:
        if e.dom == "int128":
            cw_wide(c, at, v.w, vsize)
        else:
            cw_int(c, at, vsize, v.i)

def cw_array(c:CookWrite, at:int, sl:Slot, cf:CookField, fd:FieldDescriptor) -> void:
    if fd.array == "list":
        return
    let e = elem_of(fd)
    let n = length(sl.elems)
    var i = 0
    while i < n:
        cw_elem(c, at + i * cf.elem, e, sl.elems[i], cf.esize, cf.ecount, cf.tag, cf.arm)
        i++
    if fd.array == "counted" and cf.count >= 0:
        co_u32(c.o, at + cf.count, sl.count, c.big)

def cw_union(c:CookWrite, at:int, v:Value, fd:FieldDescriptor, cf:CookField) -> void:
    let un = cook_union_named(fd.t)
    let ud = lookup_union(fd.t)
    if not un or not ud:
        return
    cw_int(c, at, un.tag, v.i)
    if v.i == 0 or v.i > length(un.arms) or v.i > length(ud.arms):
        return
    let af = un.arms[v.i - 1]
    let arm = ud.arms[v.i - 1]
    if arm.s == "void":
        return
    if arm.s == "tab":
        let t = v.tab
        if t:
            let ct = cook_table_of(arm.t)
            assert ct
            cw_record(c, at + un.arm, t, ct)
        return
    let sl = v.arm
    if sl:
        cw_field(c, at + un.arm, sl, af, arm)

def cw_record(c:CookWrite, base:int, rec:Record, t:CookTable) -> void:
    var i = 0
    while i < length(rec.fields) and i < length(t.fields):
        cw_field(c, base + t.fields[i].off, rec.fields[i], t.fields[i], rec.fields[i].fd)
        i++

// ---- the extent carver (docs/SPEC-TABLES.md §2.9, §7.2; extent.go) ----

class CookCarve:
    write:bool
    o:CookOut? = nil
    big:bool = false
    g:CookGraph? = nil
    extent_at:int = 0
    at:int = 0

def cc_has_field(c:CookCarve, fd:FieldDescriptor) -> bool:
    if fd.s == "tab":
        let t = cook_table_of(fd.t)
        return t and t.extent
    if fd.s == "union":
        let u = cook_union_named(fd.t)
        return u and u.extent
    return false

def cc_record(c:CookCarve, recordAt:int, rec:Record, t:CookTable) -> void:
    var i = 0
    while i < length(rec.fields) and i < length(t.fields):
        cc_field(c, recordAt + t.fields[i].off, rec.fields[i], t.fields[i])
        i++

def cc_field(c:CookCarve, at:int, sl:Slot, cf:CookField) -> void:
    let fd = sl.fd
    if fd.present != "" and not sl.present:
        return
    if fd.s == "map":
        return
    if fd.s == "arr" and fd.array == "list":
        cc_list(c, at, sl, cf, fd)
        return
    if fd.s == "ptr" or fd.s == "sblob" or fd.s == "bblob":
        return
    if not cc_has_field(c, fd):
        return
    if fd.s == "arr":
        let e = elem_of(fd)
        let n = length(sl.elems)
        var i = 0
        while i < n:
            cc_elem(c, at + i * cf.elem, e, sl.elems[i], cf)
            i++
        return
    cc_elem(c, at, fd, sl.cell, cf)

def cc_elem(c:CookCarve, at:int, fd:FieldDescriptor, v:Value, cf:CookField) -> void:
    if fd.s == "tab":
        let t = v.tab
        if t:
            let ct = cook_table_of(fd.t)
            if ct:
                cc_record(c, at, t, ct)
    elif fd.s == "union":
        if v.i == 0:
            return
        let ud = lookup_union(fd.t)
        let cu = cook_union_named(fd.t)
        if not ud or not cu or v.i > length(ud.arms):
            return
        let arm = ud.arms[v.i - 1]
        if arm.s == "void":
            return
        if arm.s == "tab":
            let t = v.tab
            if t:
                let ct = cook_table_of(arm.t)
                if ct:
                    cc_record(c, at + cu.arm, t, ct)
            return
        let sl = v.arm
        if sl:
            cc_field(c, at + cu.arm, sl, cu.arms[v.i - 1])

def cc_list(c:CookCarve, at:int, sl:Slot, cf:CookField, fd:FieldDescriptor) -> void:
    let count = sl.count
    c.at = cook_align_up(c.at, cf.lalign)
    let start = c.at
    c.at += count * cf.lsize
    if c.write:
        let o = c.o
        let g = c.g
        assert o
        assert g
        let cw = CookWrite { o: o, g: g, big: c.big }
        if count > 0:
            co_u64(o, at, c.extent_at + start - at, c.big)
        else:
            co_u64(o, at, 0, c.big)
        co_u32(o, at + 8, count, c.big)
        let e = elem_of(fd)
        var i = 0
        while i < count and i < length(sl.elems):
            cw_elem(cw, c.extent_at + start + i * cf.lsize, e, sl.elems[i], cf.lvalue, cf.lcount, cf.tag, cf.arm)
            i++
    let e = elem_of(fd)
    if not cc_has_field(c, e):
        return
    var i = 0
    while i < count and i < length(sl.elems):
        cc_elem(c, c.extent_at + start + i * cf.lsize, e, sl.elems[i], cf)
        i++

def cc_extent(rec:Record, t:CookTable, write:bool, o:CookOut?, big:bool, g:CookGraph?, extent_at:int) -> int:
    let c = CookCarve { write: write, o: o, big: big, g: g, extent_at: extent_at }
    cc_record(c, 0, rec, t)
    return c.at

// ---- measure, write (docs/SPEC-TABLES.md §7.6) ----

def cg_place(g:CookGraph) -> int, int:
    var off = 0
    var align = 8
    var i = 0
    while i < length(g.nodes):
        let n = g.nodes[i]
        if n.kind == 2 or n.kind == 3:
            let b = n.blob
            assert b
            off = cook_align_up(off, 8)
            n.off = off
            var sz = 8 + length(b)
            if n.kind == 2:
                sz++
            off += sz
        else:
            let rec = n.rec
            assert rec
            let t = cook_table_of(rec.d.name)
            assert t
            if t.align > align:
                align = t.align
            off = cook_align_up(off, t.align)
            n.off = off
            n.record = t.record
            if t.extent:
                n.extent = cc_extent(rec, t, false, nil, false, nil, 0)
            else:
                n.extent = 0
            off += n.record + n.extent
        i++
    let data = cook_align_up(off, align)
    return data, align

def cook_plan(rec:Record) -> CookGraph, int, int:
    let g = CookGraph {}
    cg_add_rec(g, rec)
    if g.cycle:
        return g, -1, 8
    let data, align = cg_place(g)
    return g, data, align

def cook_measure(rec:Record, build:int) -> int:
    let g, data, _ = cook_plan(rec)
    if data < 0:
        return -1
    let attrib = length(g.nodes) * cook_directory_entry
    return cook_header_bytes + data + attrib

def cook_write(rec:Record, build:int, big:bool) -> string:
    let g, data, align = cook_plan(rec)
    if data < 0:
        return ""
    let attrib = length(g.nodes) * cook_directory_entry
    let total = cook_header_bytes + data + attrib
    let o = CookOut { b: zeros(total) }
    let order = if big: cook_byte_order_big else: cook_byte_order_little
    co_u64(o, 0, cook_magic, big)
    co_u64(o, 8, build, big)
    co_u64(o, 16, order, big)
    co_u64(o, 24, data, big)
    co_u64(o, 32, attrib, big)
    co_u64(o, 40, align, big)
    co_u64(o, 48, 0, big)
    co_u64(o, 56, 0, big)
    let cw = CookWrite { o: o, g: g, big: big }
    var i = 0
    while i < length(g.nodes):
        let n = g.nodes[i]
        let base = cook_header_bytes + n.off
        if n.kind == 2 or n.kind == 3:
            let b = n.blob
            assert b
            co_u32(o, base, length(b), big)
            co_bytes(o, base + 8, b)
        else:
            let node = n.rec
            assert node
            let t = cook_table_of(node.d.name)
            assert t
            cw_record(cw, base, node, t)
            if t.extent or n.extent != 0:
                let got = cc_extent(node, t, true, o, big, g, base + n.record)
                assert got == n.extent
        i++
    var at = cook_header_bytes + data
    i = 0
    while i < length(g.nodes):
        let n = g.nodes[i]
        co_u64(o, at, n.off, big)
        var tid = 0
        if n.kind == 2:
            tid = string_blob_id
        elif n.kind == 3:
            tid = bytes_blob_id
        else:
            let node = n.rec
            assert node
            tid = node.d.id
        co_u64(o, at + 8, tid, big)
        at += cook_directory_entry
        i++
    return o.b

// ---- the cooked read (docs/SPEC-TABLES.md §7.5; uncook.go) ----

class CookRead:
    data:string
    big:bool = false
    dir:[int] = []
    tids:[int] = []
    nodes:[Record?] = []
    blobs:[string] = []

def cr_word(c:CookRead, at:int) -> int:
    return cook_word(c.data, at, c.big)

def cr_word32(c:CookRead, at:int) -> int:
    return cook_word32(c.data, at, c.big)

def cr_entry(c:CookRead, off:int) -> int:
    var lo = 0
    var hi = length(c.dir) - 1
    while lo <= hi:
        let mid = (lo + hi) / 2
        if c.dir[mid] == off:
            return mid
        elif c.dir[mid] < off:
            lo = mid + 1
        else:
            hi = mid - 1
    return -1

def cr_ref(c:CookRead, at:int) -> int:
    let delta = cr_word(c, at)
    if delta == 0:
        return -1
    return cr_entry(c, at + delta)

def cr_int(c:CookRead, at:int, width:int) -> int:
    if width <= 1:
        return read_uint8_le(c.data, at)
    if width == 2:
        let v = read_uint16_le(c.data, at)
        if c.big:
            return cook_bswap16(v)
        return v
    if width == 4:
        return cr_word32(c, at)
    return cr_word(c, at)

def cr_wide(c:CookRead, at:int) -> Int128:
    if c.big:
        return Int128 { lo: cr_word(c, at + 8), hi: cr_word(c, at) }
    return Int128 { lo: cr_word(c, at), hi: cr_word(c, at + 8) }

def cr_count(c:CookRead, at:int, countoff:int, bufsize:int) -> int:
    if countoff < 0:
        return 0
    let n = cr_word32(c, at + countoff)
    if n < 0 or n > bufsize:
        return 0
    return n

def cr_text(c:CookRead, at:int, countoff:int, bufsize:int) -> string:
    let n = cr_count(c, at, countoff, bufsize)
    return substring(c.data, at, n)

def cr_units(c:CookRead, at:int, countoff:int, bufsize:int) -> [int]:
    let n = cr_count(c, at, countoff, bufsize)
    let units = []
    var i = 0
    while i < n:
        let u = read_uint16_le(c.data, at + i * 2)
        if c.big:
            push(units, cook_bswap16(u))
        else:
            push(units, u)
        i++
    return units

def cr_record(c:CookRead, base:int, rec:Record, t:CookTable) -> void:
    var i = 0
    while i < length(rec.fields) and i < length(t.fields):
        cr_field(c, base + t.fields[i].off, rec.fields[i], t.fields[i])
        i++

def cr_field(c:CookRead, at:int, sl:Slot, cf:CookField) -> void:
    let fd = sl.fd
    let s = fd.s
    if fd.present != "":
        sl.present = c.data[at + cf.present] != 0
        if not sl.present:
            return
    if s == "tab":
        let ct = cook_table_of(fd.t)
        if ct:
            let sub = new_record(table_of(fd.t))
            cr_record(c, at, sub, ct)
            sl.cell.tab = sub
    elif s == "ptr":
        let slot = cr_ref(c, at)
        if slot >= 0:
            sl.cell.node = c.nodes[slot]
    elif s == "sblob" or s == "bblob":
        let slot = cr_ref(c, at)
        if slot >= 0:
            sl.cell.blob = c.blobs[slot]
    elif s == "union":
        cr_union(c, at, sl.cell, fd)
    elif s == "str":
        if fd.wide:
            sl.cell.units = cr_units(c, at, cf.count, cf.size)
            sl.count = cr_count(c, at, cf.count, cf.size)
        else:
            sl.cell.s = cr_text(c, at, cf.count, cf.size)
            sl.count = cr_count(c, at, cf.count, cf.size)
    elif s == "text":
        sl.cell.s = cr_text(c, at, cf.count, cf.size)
    elif s == "bytes":
        sl.cell.s = cr_text(c, at, cf.count, cf.size)
        sl.count = cr_count(c, at, cf.count, cf.size)
    elif s == "bool":
        sl.cell.b = c.data[at] != 0
    elif s == "float":
        if fd.f32:
            sl.cell.f = fixed_f32frombits(cr_word32(c, at))
        else:
            sl.cell.f = f64frombits(cr_word(c, at))
    elif s == "enum":
        sl.cell.i = cr_int(c, at, cf.size)
    elif s == "arr":
        cr_array(c, at, sl, cf, fd)
    elif s == "map":
        return
    else:
        if fd.dom == "int128":
            sl.cell.w = cr_wide(c, at)
        else:
            sl.cell.i = cr_int(c, at, cf.size)

def cr_elem(c:CookRead, at:int, e:FieldDescriptor, v:Value, vsize:int, countoff:int) -> Value:
    let s = e.s
    if s == "ptr":
        let slot = cr_ref(c, at)
        if slot >= 0:
            v.node = c.nodes[slot]
    elif s == "sblob" or s == "bblob":
        let slot = cr_ref(c, at)
        if slot >= 0:
            v.blob = c.blobs[slot]
    elif s == "tab":
        let ct = cook_table_of(e.t)
        if ct:
            let sub = new_record(table_of(e.t))
            cr_record(c, at, sub, ct)
            v.tab = sub
    elif s == "union":
        cr_union(c, at, v, e)
    elif s == "str":
        if e.wide:
            v.units = cr_units(c, at, countoff, vsize)
        else:
            v.s = cr_text(c, at, countoff, vsize)
    elif s == "text":
        v.s = cr_text(c, at, countoff, vsize)
    elif s == "bytes":
        v.s = cr_text(c, at, countoff, vsize)
    elif s == "bool":
        v.b = c.data[at] != 0
    elif s == "float":
        if e.f32:
            v.f = fixed_f32frombits(cr_word32(c, at))
        else:
            v.f = f64frombits(cr_word(c, at))
    elif s == "enum":
        v.i = cr_int(c, at, vsize)
    else:
        if e.dom == "int128":
            v.w = cr_wide(c, at)
        else:
            v.i = cr_int(c, at, vsize)
    return v

def cr_array(c:CookRead, at:int, sl:Slot, cf:CookField, fd:FieldDescriptor) -> void:
    let e = elem_of(fd)
    let n = length(sl.elems)
    var i = 0
    while i < n:
        cr_elem(c, at + i * cf.elem, e, sl.elems[i], cf.esize, cf.ecount)
        i++
    if fd.array == "counted" and cf.count >= 0:
        sl.count = cr_word32(c, at + cf.count)

def cr_union(c:CookRead, at:int, v:Value, fd:FieldDescriptor) -> void:
    let un = cook_union_named(fd.t)
    let ud = lookup_union(fd.t)
    if not un or not ud:
        return
    v.i = cr_int(c, at, un.tag)
    if v.i == 0 or v.i > length(un.arms) or v.i > length(ud.arms):
        return
    let arm = ud.arms[v.i - 1]
    if arm.s == "void":
        return
    if arm.s == "tab":
        let ct = cook_table_of(arm.t)
        if ct:
            let sub = new_record(table_of(arm.t))
            cr_record(c, at + un.arm, sub, ct)
            v.tab = sub
        return
    let sl = new_slot(arm)
    cr_field(c, at + un.arm, sl, un.arms[v.i - 1])
    v.arm = sl

def cook_read(data:string, build:int, desc:TableDescriptor, t:CookTable) -> Record?:
    if not cook_open(data, build, t.align, t.size):
        return nil
    let magic = read_uint64_le(data, 0)
    let big = magic != cook_magic
    let c = CookRead { data: data, big: big }
    let data_offset = cook_align_up(cook_header_bytes, cr_word(c, 40))
    let data_length = cr_word(c, 24)
    let attrib_length = cr_word(c, 32)
    if attrib_length % cook_directory_entry != 0:
        return nil
    let count = attrib_length / cook_directory_entry
    if count <= 0:
        return nil
    var i = 0
    while i < count:
        let e = data_offset + data_length + i * cook_directory_entry
        push(c.dir, cr_word(c, e))
        push(c.tids, cr_word(c, e + 8))
        push(c.nodes, nil)
        push(c.blobs, "")
        i++
    i = 0
    while i < count:
        if c.tids[i] == string_blob_id or c.tids[i] == bytes_blob_id:
            let length_at = data_offset + c.dir[i]
            let n = cr_word32(c, length_at)
            let start = length_at + 8
            var end = start + n
            if i + 1 < count:
                end = data_offset + c.dir[i + 1]
            if start + n > end:
                return nil
            c.blobs[i] = substring(data, start, n)
        else:
            for(registered_tables()) td:
                if td.id == c.tids[i]:
                    c.nodes[i] = new_record(td)
                    break
        i++
    i = 0
    while i < count:
        let rec = c.nodes[i]
        if rec:
            let ct = cook_table_of(rec.d.name)
            if not ct:
                return nil
            cr_record(c, data_offset + c.dir[i], rec, ct)
        i++
    let root = c.nodes[0]
    if not root:
        return nil
    if root.d.id != desc.id:
        return nil
    return root

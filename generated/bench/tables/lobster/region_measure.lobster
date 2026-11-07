// region_measure.lobster — LOAD MEASURE for the Lobster table runtime
// (docs/SPEC-TABLES.md §6.5, §20.3; docs/PORT-LOBSTER.md, THE LOAD). A port
// of the Go engine's tablewire.FileRegionMeasure (internal/tablewire/
// extent.go) onto the descriptors: the region bytes a C reader of a form-1
// wire would ask for before it loads, read off the wire's framing alone.
// Lobster loads into Records, not a region, so nothing here sizes a Lobster
// allocation; the number is the one every port answers for the same bytes,
// which is what lets the wire-fuzz differential's ordinary, retention and
// message arms hold a Lobster leg to the region bound (§6.5) like the others.
// The facts it reads are the descriptors' region members: rsize and ralign on
// a table, lsize and lalign on a list or map field.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// ExtentScan is one record walk's state: the scratch load state the reader
// helpers take, and the bytes the record's lists and maps add.
class ExtentScan:
    st:LoadState
    at:int = 0

def region_round(n:int, align:int) -> int:
    return (n + align - 1) & -align

// extent_body reads one length-framed payload: its span, or false (and the
// reader at its end) when the length runs past it.
def extent_body(r:Reader) -> int, int, bool:
    let n, ok = leb(r)
    if not ok or n < 0 or n > r.hi - r.off:
        r.off = r.hi
        return 0, 0, false
    let lo = r.off
    r.off += n
    return lo, lo + n, true

def extent_field_of(d:TableDescriptor, id:int) -> FieldDescriptor?:
    for(d.fields) f:
        if f.id == id:
            return f
    return nil

// extent_record walks one record's body (Go's extentScan.record): false only
// when a collection's count refuses the region.
def extent_record(s:ExtentScan, d:TableDescriptor, lo:int, hi:int) -> bool:
    let r = reader(s.st, lo, hi)
    while true:
        let ref, ok = leb(r)
        if not ok or ref == 0:
            return true
        let id, named = rid(r, ref)
        if not named or not has(r, 1):
            return true
        let kind = u8(r)
        let found = extent_field_of(d, id)
        if not found:
            if not skip(r, kind):
                return true
            continue
        let f = found
        if kind == 15 and f.s == "union":
            if not extent_union(s, r, lookup_union(f.t)):
                return false
            continue
        if kind != 13 and kind != 14 and kind != 16:
            if not skip(r, kind):
                return true
            continue
        let plo, phi, framed = extent_body(r)
        if not framed:
            return true
        if kind != f.k:
            continue
        if not extent_field(s, f, plo, phi):
            return false
    return true

// extent_field is Go's extentScan.field over one field's payload.
def extent_field(s:ExtentScan, f:FieldDescriptor, lo:int, hi:int) -> bool:
    if f.s == "ptr" or f.s == "sblob" or f.s == "bblob":
        return true
    if f.s == "arr" or f.s == "map":
        let r = reader(s.st, lo, hi)
        if not has(r, 2):
            return true
        let kind = u8(r)
        let expected = if f.s == "map": 13 else: f.ek
        if kind != expected and not widens(kind, expected):
            return true
        let count, ok = leb(r)
        if not ok:
            return true
        if f.s == "map" or f.array == "list":
            if count < 0 or count > 0x7FFFFFFF:
                return false
            var floor = kind_width(kind)
            if kind == 17:
                floor = 1
            if kind == 13:
                floor = 2
            if floor < 1:
                floor = 1
            if count > (r.hi - r.off) / floor:
                return false
            s.at = region_round(s.at, f.lalign)
            s.at += count * f.lsize
        if kind != 13 and kind != 15:
            return true
        let e = elem_of(f)
        var i = 0
        // the count is unsigned on the wire (a ten-byte LEB reads as a
        // negative int here), and the walk is bounded by the body, not by it
        while ult(i, count) and has(r, 1):
            if f.array == "keyed":
                let _k, kok = leb(r)
                if not kok:
                    return true
            if kind == 15:
                if e.s == "union":
                    if not extent_union(s, r, lookup_union(e.t)):
                        return false
                i++
                continue
            let plo, phi, framed = extent_body(r)
            if not framed:
                return true
            var d:TableDescriptor? = nil
            if f.s == "map":
                d = lookup_table(f.t)
            elif e.s == "tab":
                d = lookup_table(e.t)
            if d:
                if not extent_record(s, d, plo, phi):
                    return false
            i++
        return true
    if f.s == "tab":
        let d = lookup_table(f.t)
        if d:
            return extent_record(s, d, lo, hi)
    return true

// extent_union is Go's extentScan.union: the arm header, then the arm's
// payload walked as the arm's own field, or as a nested union.
def extent_union(s:ExtentScan, r:Reader, u:UnionDescriptor?) -> bool:
    let ref, ok = leb(r)
    if not ok:
        r.off = r.hi
        return true
    if ref == 0:
        return true
    let id, named = rid(r, ref)
    if not named or not has(r, 1):
        r.off = r.hi
        return true
    u8(r)
    let plo, phi, framed = extent_body(r)
    if not framed:
        return true
    if not u:
        return true
    for(u.arms) a:
        if a.id != id or a.s == "void" or a.s == "ptr" or a.s == "sblob" or a.s == "bblob":
            continue
        if a.s == "union":
            return extent_union(s, reader(s.st, plo, phi), lookup_union(a.t))
        return extent_field(s, a, plo, phi)
    return true

// region_size is one record's region: its own storage, rounded, then what
// its lists and maps add, rounded again; false when the walk refused.
def region_size(st:LoadState, d:TableDescriptor, lo:int, hi:int) -> int, bool:
    let s = ExtentScan { st: st }
    if not extent_record(s, d, lo, hi):
        return 0, false
    return region_round(region_round(d.rsize, d.ralign) + s.at, d.ralign), true

// load_measure is the region a C reader would ask for to load the wire at the
// root table named root (Go's FileRegionMeasure): the bytes, whether the node
// table was read whole, and whether the framing REFUSES the region (a
// collection count past the int32 storage cap, or one its body cannot hold,
// or a blob past 4 GiB). A wire that cannot be opened answers 0, not whole.
def load_measure(root:string, data:string) -> int, bool, bool:
    if length(data) == 0 or data[0] != 1:
        return 0, false, false
    let ids, first, ok = trailer(data)
    if not ok:
        return 0, false, false
    let d = table_of(root)
    let st = LoadState { data: data, ids: ids, report: LoadReport {}, variable: false, good: false, root: nil, nodes: [], named: [], count_refused: false }
    var total, rok = region_size(st, d, 1, first)
    if not rok:
        return -1, true, true
    let plo, phi, present, framed = node_table_bytes(st, first)
    if present and not framed:
        return 0, false, false
    var records:[NodeRecord] = []
    if present:
        let scanned, sok = scan_node_records(st, plo, phi)
        if not sok:
            return 0, false, false
        records = scanned
    for(records) rec:
        var placed:TableDescriptor? = nil
        for(d.place_tables) name:
            let p = table_of(name)
            if p.id == rec.tid:
                placed = p
        if placed:
            let n, nok = region_size(st, placed, rec.lo, rec.hi)
            if not nok:
                return -1, true, true
            total += n
        elif (rec.tid == bytes_blob_id and d.place_bytes) or (rec.tid == string_blob_id and d.place_str):
            let size = rec.hi - rec.lo
            if size > 0xFFFFFFFF:
                return -1, true, true
            var n = 8 + size
            if rec.tid == string_blob_id:
                n++
            total += region_round(n, d.ralign)
    return total + (length(records) + 1) * 16, true, false

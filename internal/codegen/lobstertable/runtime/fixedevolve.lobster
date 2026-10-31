// fixedevolve.lobster — the fixed form's evolution, plan compiler, runner
// and refusals (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md §1–§5).
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .schema_runtime
import .fixed

namespace schema_rt

let TableFixedForm = 3
let TableFixedHashAt = 8
let TableFixedHeaderBytes = 16
let tableFixedLayoutHeaderBytes = 4
let tableFixedEntryBytes = 17
let tableFixedMaxDepth = 64
let tableFixedRecordMaxBytes = 65536
let tableFixedNoGuard = -1
let tableFixedRemapMax = 0xffff
let tableFixedDstDeprecated = 0x80

let tableFixedCopy = 0
let tableFixedTag = 1
let tableFixedBool = 2
let tableFixedCount = 3
let tableFixedText = 4
let tableFixedOrdinal = 5
let tableFixedWiden = 6
let tableFixedWidenF = 7
let tableFixedConst = 8

let tableFixedTextUtf8 = 1
let tableFixedTextBytes = 3

let TableOpenExact = 0
let TableOpenCompatible = 1
let TableOpenIncompatible = 2
let TableOpenMalformed = 3
let TableOpenRefused = 4
let TableOpenDamaged = 5

let TableRefusePreviousForm = "previous_form"
let TableRefuseMessageFormAsFile = "message_form_as_file"
let TableRefuseNewerForm = "newer_form"
let TableRefuseUnknownForm = "unknown_form"
let TableRefuseNotFixed = "not_fixed"
let TableRefuseLayoutUnknown = "layout_unknown"
let TableRefuseLayoutUnsupported = "layout_unsupported"
let TableRefuseLayoutNewer = "layout_newer"
let TableRefuseLayoutMalformed = "layout_malformed"
let TableRefuseLayoutCountMismatch = "layout_count_mismatch"
let TableRefuseLayoutKindUnknown = "layout_kind_unknown"
let TableRefuseLayoutKindInvalid = "layout_kind_invalid"
let TableRefuseLayoutSizeMismatch = "layout_size_mismatch"
let TableRefuseLayoutTreeUnclosed = "layout_tree_unclosed"
let TableRefuseLayoutTooDeep = "layout_too_deep"
let TableRefuseLayoutRecordTooLarge = "layout_record_too_large"
let TableRefusePlanTooLarge = "plan_too_large"
let TableRefuseNoLayout = "no_layout"
let TableRefuseBatchTooLarge = "batch_too_large"
let TableRefuseTextIllFormed = "text_ill_formed"
let TableRefuseCountOverBound = "count_over_bound"
let TableRefuseLengthOverBound = "length_over_bound"
let TableRefuseTruncated = "truncated"
let TableRefuseReservedNotZero = "reserved_not_zero"
let TableRefuseIntegrityMismatch = "integrity_mismatch"
let TableRefuseInvalidValue = "invalid_value"
let TableRefuseClamped = "clamped"
let TableRefuseKindMismatch = "kind_mismatch"
let TableRefuseNonFiniteFloat = "non_finite_float"

class TableReport:
    verdict:int = 0
    fixed_raised:bool = false
    reason:string = ""
    layout_hash:int = 0
    widened:int = 0
    unknown:int = 0
    kind_mismatch:int = 0
    clamped:int = 0
    duplicate:int = 0
    retained:int = 0
    retain_lost:int = 0
    malformed:bool = false
    // non_finite_float names the first NaN or infinity a fixed read met,
    // <Owner>.<field> (docs/SPEC-TABLES.md §3.3); the read refuses
    non_finite_float:string = ""

def fixed_reset_report(r:TableReport):
    r.verdict = 0
    r.fixed_raised = false
    r.reason = ""
    r.layout_hash = 0
    r.widened = 0
    r.unknown = 0
    r.kind_mismatch = 0
    r.clamped = 0
    r.duplicate = 0
    r.retained = 0
    r.retain_lost = 0
    r.malformed = false
    r.non_finite_float = ""

// fixed_refuse_non_finite is a fixed read's refusal of a NaN or an infinity
// (docs/SPEC-TABLES.md §3.3): the report holds only the name, and the reason
// non_finite_float: <Owner>.<field>, as Go's tableNonFiniteRefused writes it.
def fixed_refuse_non_finite(r:TableReport, field:string) -> int:
    fixed_reset_report(r)
    r.non_finite_float = field
    r.reason = TableRefuseNonFiniteFloat + ": " + field
    return -1

def fixed_refuse(r:TableReport, reason:string) -> int:
    let h = r.layout_hash
    fixed_reset_report(r)
    r.layout_hash = h
    r.verdict = TableOpenRefused
    r.fixed_raised = true
    r.reason = reason
    return -1

def fixed_refuse_hash(r:TableReport, reason:string, hash:int) -> int:
    r.layout_hash = hash
    return fixed_refuse(r, reason)

def fixed_report_equal(a:TableReport, b:TableReport) -> bool:
    return a.verdict == b.verdict and
        a.fixed_raised == b.fixed_raised and
        a.reason == b.reason and
        a.layout_hash == b.layout_hash and
        a.widened == b.widened and
        a.unknown == b.unknown and
        a.kind_mismatch == b.kind_mismatch and
        a.clamped == b.clamped and
        a.duplicate == b.duplicate and
        a.retained == b.retained and
        a.retain_lost == b.retain_lost and
        a.malformed == b.malformed

def u128_lt(a:Int128, b:Int128) -> bool:
    if a.hi != b.hi:
        return ult(a.hi, b.hi)
    return ult(a.lo, b.lo)

def i128_lt(a:Int128, b:Int128) -> bool:
    if a.hi != b.hi:
        return a.hi < b.hi
    return ult(a.lo, b.lo)

class TableFixedGuardLink:
    grd:int = 0
    arg:int = 0
    arg_w:int = 0

class TableFixedEntry:
    src:int = 0
    dst:int = 0
    size:int = 0
    aux:int = 0
    grd:int = -1
    arg:int = 0
    arg_w:int = 0
    grd2:int = 0
    arg2:int = 0
    arg_w2:int = 0
    chain_id:int = 0
    chain:[TableFixedGuardLink] = []
    remap:[int] = []
    op:int = 0
    dst_size:int = 0
    sign:int = 0
    meta:int = 0

def fixed_clone_entry(e:TableFixedEntry) -> TableFixedEntry:
    let ch:[TableFixedGuardLink] = []
    for(e.chain) l:
        push(ch, TableFixedGuardLink { grd: l.grd, arg: l.arg, arg_w: l.arg_w })
    let rm:[int] = []
    for(e.remap) v:
        push(rm, v)
    return TableFixedEntry {
        src: e.src,
        dst: e.dst,
        size: e.size,
        aux: e.aux,
        grd: e.grd,
        arg: e.arg,
        arg_w: e.arg_w,
        grd2: e.grd2,
        arg2: e.arg2,
        arg_w2: e.arg_w2,
        chain_id: e.chain_id,
        chain: ch,
        remap: rm,
        op: e.op,
        dst_size: e.dst_size,
        sign: e.sign,
        meta: e.meta
    }

class TableFixedDst:
    dst:int = 0
    stride:int = 0
    aux:int = 0
    counted:int = 0
    arg:int = 0
    meta:int = 0

class TableFixedHole:
    off:int = 0
    size:int = 0

class TableFixedPlan:
    entries:[TableFixedEntry] = []
    count:int = 0
    split:int = 0

class TableFixedKnownLayout:
    hash:int = 0
    layout:string = ""
    layout_bytes:int = 0
    record:int = 0

class TableFixedLineagePlan:
    entries:[TableFixedEntry] = []
    count:int = 0
    unknown:int = 0
    kind_mismatch:int = 0
    why:string = ""

class TableFixedLayoutEntry:
    id:int = 0
    size:int = 0
    children:int = 0
    kind:int = 0

class TableFixedLayoutView:
    bytes:string = ""
    count:int = 0

def fixed_arg_width(w:int) -> int:
    if w <= 0:
        return 1
    if w > 8:
        return 8
    return w

def fixed_tag_at(src:string, at:int, w:int) -> int:
    let n = fixed_arg_width(w)
    if n == 1:
        return read_uint8_le(src, at)
    if n == 2:
        return read_uint16_le(src, at)
    if n == 4:
        return read_uint32_le(src, at)
    if n == 8:
        return read_uint64_le(src, at)
    var v = 0
    for(n) k:
        v = v | shl(read_uint8_le(src, at + k), 8 * k)
    return v

def fixed_chain_holds(p:TableFixedEntry, src:string, src_base:int) -> bool:
    if length(p.chain) <= 2:
        return true
    for(p.chain) l:
        if fixed_tag_at(src, src_base + l.grd, l.arg_w) != l.arg:
            return false
    return true

def fixed_runs(op:int) -> bool:
    return op == tableFixedCopy or op == tableFixedBool

def fixed_widens(from_k:int, to:int) -> bool:
    if from_k >= 6 and from_k <= 9 and to >= 6 and to <= 9:
        return to > from_k
    if from_k >= 2 and from_k <= 5 and to >= 2 and to <= 5:
        return to > from_k
    if from_k >= 20 and from_k <= 24 and to >= 20 and to <= 24:
        return to > from_k
    if from_k >= 25 and from_k <= 29 and to >= 25 and to <= 29:
        return to > from_k
    return from_k == 10 and to == 11

def fixed_signed_kind(kind:int) -> bool:
    return (kind >= 2 and kind <= 5) or (kind >= 20 and kind <= 24)

def fixed_kind_known(kind:int) -> bool:
    if kind >= 1 and kind <= 30:
        return true
    return kind == 32 or kind == 35

def fixed_ordinal_width(n:int) -> bool:
    return n == 1 or n == 2 or n == 4 or n == 8

def fixed_leaf_size(kind:int, size:int) -> bool, bool:
    switch kind:
        case 1, 2:
            return size == 1, true
        case 3:
            return size == 2, true
        case 4:
            return size == 4, true
        case 5:
            return size == 8, true
        case 6:
            return size == 1 or size == 4, true
        case 7:
            return size == 2 or size == 4, true
        case 8:
            return size == 4, true
        case 9:
            return size == 8, true
        case 10:
            return size == 4, true
        case 11:
            return size == 8, true
        case 17:
            return size == 4, true
        case 18, 19:
            return size == 16, true
        case 20, 25:
            return size == 1, true
        case 21, 26:
            return size == 2, true
        case 22, 27:
            return size == 4, true
        case 23, 28:
            return size == 8, true
        case 24, 29:
            return size == 16, true
        case 32:
            return size == 0, true
        default:
            return true, false

def fixed_partition(plan:[TableFixedEntry], n:int) -> int:
    var split = 0
    for(n) i:
        if plan[i].grd == tableFixedNoGuard:
            let e = plan[i]
            var j = i
            while j > split:
                plan[j] = plan[j - 1]
                j -= 1
            plan[split] = e
            split += 1
    return split

def fixed_chains_equal(a:[TableFixedGuardLink], b:[TableFixedGuardLink]) -> bool:
    if length(a) != length(b):
        return false
    for(length(a)) i:
        if a[i].grd != b[i].grd or a[i].arg != b[i].arg or a[i].arg_w != b[i].arg_w:
            return false
    return true

def fixed_build_plan(scratch:[TableFixedEntry]) -> TableFixedPlan:
    let written = length(scratch)
    let unique_chains:[[TableFixedGuardLink]] = []
    for(written) i:
        let e = scratch[i]
        let k = length(e.chain)
        if k > 0:
            var a = 0
            var b = k - 1
            while a < b:
                let tmp = e.chain[a]
                e.chain[a] = e.chain[b]
                e.chain[b] = tmp
                a += 1
                b -= 1
            e.grd = e.chain[k - 1].grd
            e.arg = e.chain[k - 1].arg
            e.arg_w = e.chain[k - 1].arg_w
            if k >= 2:
                e.grd2 = e.chain[k - 2].grd
                e.arg2 = e.chain[k - 2].arg
                e.arg_w2 = e.chain[k - 2].arg_w
            var cid = -1
            for(length(unique_chains)) u:
                if cid < 0 and fixed_chains_equal(unique_chains[u], e.chain):
                    cid = u + 1
            if cid < 0:
                push(unique_chains, e.chain)
                cid = length(unique_chains)
            e.chain_id = cid
    let raw:[TableFixedEntry] = []
    for(written) i:
        let cur = scratch[i]
        let out = length(raw)
        if out > 0:
            let prev = raw[out - 1]
            if prev.op == cur.op and fixed_runs(cur.op) and
               prev.grd == cur.grd and prev.arg == cur.arg and
               prev.arg_w == cur.arg_w and prev.meta == cur.meta and
               prev.grd2 == cur.grd2 and prev.arg2 == cur.arg2 and
               prev.arg_w2 == cur.arg_w2 and
               prev.chain_id == cur.chain_id and length(prev.chain) == length(cur.chain) and
               prev.src + prev.size == cur.src and
               prev.dst + prev.size == cur.dst:
                prev.size += cur.size
            else:
                push(raw, cur)
        else:
            push(raw, cur)
    let n = length(raw)
    let split = fixed_partition(raw, n)
    return TableFixedPlan { entries: raw, count: n, split: split }

def fixed_entry_at(b:TableFixedLayoutView, i:int) -> TableFixedLayoutEntry:
    if length(b.bytes) == 0 or i < 0 or i >= b.count:
        return TableFixedLayoutEntry { kind: 0xff }
    let base = tableFixedLayoutHeaderBytes + i * tableFixedEntryBytes
    return TableFixedLayoutEntry {
        id: read_uint64_le(b.bytes, base),
        kind: read_uint8_le(b.bytes, base + 8),
        size: read_uint32_le(b.bytes, base + 9),
        children: read_uint32_le(b.bytes, base + 13)
    }

def fixed_subtree(b:TableFixedLayoutView, i:int) -> int:
    if i < 0 or i >= b.count:
        return 0
    var n = 0
    var pending = 1
    var at = i
    while pending > 0 and at < b.count:
        pending -= 1
        n += 1
        pending += fixed_entry_at(b, at).children
        at += 1
        if pending > b.count:
            return n
    return n

def fixed_union_arm_bytes(b:TableFixedLayoutView, i:int) -> int:
    let e = fixed_entry_at(b, i)
    var widest = 0
    var at = i + 1
    var k = 0
    while k < e.children and at < b.count:
        let a = fixed_entry_at(b, at)
        if a.size > widest:
            widest = a.size
        at += fixed_subtree(b, at)
        k += 1
    return widest

def fixed_tag_bytes(b:TableFixedLayoutView, i:int) -> int:
    return fixed_entry_at(b, i).size - fixed_union_arm_bytes(b, i)

class tableFixedCheck:
    layout:TableFixedLayoutView
    why:string = ""

def fixed_check_fail(c:tableFixedCheck, why:string):
    if c.why == "":
        c.why = why

def fixed_check_entry(c:tableFixedCheck, i:int, depth:int) -> int:
    if c.why != "":
        return 0
    if i < 0 or i >= c.layout.count:
        fixed_check_fail(c, TableRefuseLayoutTreeUnclosed)
        return 0
    if depth > tableFixedMaxDepth:
        fixed_check_fail(c, TableRefuseLayoutTooDeep)
        return 0
    let e = fixed_entry_at(c.layout, i)
    if not fixed_kind_known(e.kind):
        fixed_check_fail(c, TableRefuseLayoutKindUnknown)
        return 0
    if e.size > tableFixedRecordMaxBytes:
        fixed_check_fail(c, TableRefuseLayoutRecordTooLarge)
        return 0
    var at = i + 1
    var sum = 0
    var widest = 0
    var first_size = 0
    var second_size = 0
    var first_children = 0
    var first_kind = 0
    var kids_are_variants = true
    for(e.children) k:
        let child = at
        let used = fixed_check_entry(c, child, depth + 1)
        if c.why != "":
            return 0
        let ce = fixed_entry_at(c.layout, child)
        if k == 0:
            first_size = ce.size
            first_kind = ce.kind
            first_children = ce.children
        if k == 1:
            second_size = ce.size
        if ce.kind != 32:
            kids_are_variants = false
        sum += ce.size
        if ce.size > widest:
            widest = ce.size
        if sum > tableFixedRecordMaxBytes:
            fixed_check_fail(c, TableRefuseLayoutRecordTooLarge)
            return 0
        at += used
    let admitted, leaf = fixed_leaf_size(e.kind, e.size)
    if not admitted:
        fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
        return 0
    if leaf:
        if e.children != 0:
            fixed_check_fail(c, TableRefuseLayoutKindInvalid)
            return 0
        return at - i
    switch e.kind:
        case 13:
            if e.size != sum:
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
        case 35:
            if e.children != 1:
                fixed_check_fail(c, TableRefuseLayoutKindInvalid)
                return 0
            if e.size != sum + 1:
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
        case 14:
            if e.children != 1:
                fixed_check_fail(c, TableRefuseLayoutKindInvalid)
                return 0
            if first_size == 0:
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
            let bare = (e.size % first_size) == 0
            let counted = e.size >= 4 and ((e.size - 4) % first_size) == 0
            if not bare and not counted:
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
        case 16:
            if e.children != 2 or first_kind != 30:
                fixed_check_fail(c, TableRefuseLayoutKindInvalid)
                return 0
            if second_size == 0 or (e.size % second_size) != 0:
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
            if (e.size / second_size) < first_children:
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
        case 15:
            if e.children == 0:
                fixed_check_fail(c, TableRefuseLayoutKindInvalid)
                return 0
            if e.size <= widest or not fixed_ordinal_width(e.size - widest):
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
        case 30:
            if not fixed_ordinal_width(e.size):
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
            if e.children != 0 and not kids_are_variants:
                fixed_check_fail(c, TableRefuseLayoutKindInvalid)
                return 0
        case 12:
            if e.children != 0:
                fixed_check_fail(c, TableRefuseLayoutKindInvalid)
                return 0
            if e.size < 4:
                fixed_check_fail(c, TableRefuseLayoutSizeMismatch)
                return 0
        default:
            fixed_check_fail(c, TableRefuseLayoutKindUnknown)
            return 0
    return at - i

def fixed_parse_layout(bytes:string) -> TableFixedLayoutView, string:
    let empty = TableFixedLayoutView {}
    if length(bytes) < tableFixedLayoutHeaderBytes:
        return empty, TableRefuseLayoutMalformed
    let count = read_uint32_le(bytes, 0)
    if count == 0 or count * tableFixedEntryBytes + tableFixedLayoutHeaderBytes != length(bytes):
        return empty, TableRefuseLayoutCountMismatch
    let view = TableFixedLayoutView { bytes: bytes, count: count }
    let root = fixed_entry_at(view, 0)
    if root.kind != 13:
        return empty, TableRefuseLayoutKindInvalid
    if root.size == 0 or root.size > tableFixedRecordMaxBytes:
        return empty, TableRefuseLayoutRecordTooLarge
    let c = tableFixedCheck { layout: view }
    let used = fixed_check_entry(c, 0, 0)
    if c.why != "":
        return empty, c.why
    if used != view.count:
        return empty, TableRefuseLayoutTreeUnclosed
    return view, ""

class tableFixedCompiler:
    plan:[TableFixedEntry] = []
    capacity:int = 0
    count:int = 0
    pool:int = 0
    overflow:bool = false
    too_large:bool = false
    record:int = 0
    arg_w:int = 0
    chain:[TableFixedGuardLink] = []
    chain_at:int = 0
    chain_laid:bool = false
    report:TableReport? = nil

def fixed_lay_guards(c:tableFixedCompiler, links:[TableFixedGuardLink]) -> int:
    let bytes = 16 * length(links)
    let total = 64 * c.capacity
    c.pool = (c.pool + bytes + 7) & (~7)
    if total - c.pool < c.count * 64:
        c.overflow = true
        return 0
    return total - c.pool

def fixed_lay_table(c:tableFixedCompiler, n:int) -> int:
    let bytes = (n + 1) * 2
    let total = 64 * c.capacity
    c.pool += bytes
    if total - c.pool < c.count * 64:
        c.overflow = true
        return 0
    return total - c.pool

def fixed_src_end(e:TableFixedEntry) -> int:
    var end = 0
    switch e.op:
        case tableFixedConst:
            end = 0
            if e.aux == 0 and e.dst_size != 0:
                end = e.src + e.size
        case tableFixedCount:
            end = e.src + 4
        case tableFixedText:
            end = e.src + 4 + e.size
        default:
            end = e.src + e.size
    if e.grd != tableFixedNoGuard:
        let g = e.grd + fixed_arg_width(e.arg_w)
        if g > end:
            end = g
    if e.arg_w2 != 0:
        let g2 = e.grd2 + fixed_arg_width(e.arg_w2)
        if g2 > end:
            end = g2
    return end

def fixed_push(c:tableFixedCompiler, e:TableFixedEntry):
    if e.grd != tableFixedNoGuard:
        e.arg_w = fixed_arg_width(c.arg_w)
    let k = length(c.chain)
    if k > 0 and e.grd != tableFixedNoGuard:
        let in_link = c.chain[k - 1]
        e.grd = in_link.grd
        e.arg = in_link.arg
        e.arg_w = in_link.arg_w
        if k >= 2:
            let out_link = c.chain[k - 2]
            e.grd2 = out_link.grd
            e.arg2 = out_link.arg
            e.arg_w2 = out_link.arg_w
        for(c.chain) l:
            if c.record != 0 and l.grd + fixed_arg_width(l.arg_w) > c.record:
                c.too_large = true
                return
        if not c.chain_laid:
            c.chain_at = fixed_lay_guards(c, c.chain)
            c.chain_laid = true
            if c.overflow:
                return
        e.chain_id = c.chain_at
        let ch:[TableFixedGuardLink] = []
        for(c.chain) l:
            push(ch, TableFixedGuardLink { grd: l.grd, arg: l.arg, arg_w: l.arg_w })
        e.chain = ch
    if c.record != 0 and fixed_src_end(e) > c.record:
        c.too_large = true
        return
    let room = c.capacity - ((c.pool + 63) / 64)
    if c.count >= room:
        c.overflow = true
        return
    push(c.plan, e)
    c.count += 1

def fixed_compile_entry(c:tableFixedCompiler, theirs:TableFixedLayoutView, ti:int, their_at:int, mine:TableFixedLayoutView, mi:int, dst:[TableFixedDst], my_at:int, grd:int, arg:int, census:bool) -> void:
    let te = fixed_entry_at(theirs, ti)
    let me = fixed_entry_at(mine, mi)
    let d = dst[mi]
    if (d.arg & tableFixedDstDeprecated) != 0:
        return
    let at = my_at + d.dst
    let aux_at = my_at + d.aux
    if te.kind != me.kind and me.kind != 35:
        if fixed_widens(te.kind, me.kind):
            let op = if te.kind == 10: tableFixedWidenF else: tableFixedWiden
            let sign = if fixed_signed_kind(te.kind): 1 else: 0
            fixed_push(c, TableFixedEntry {
                src: their_at,
                dst: at,
                size: te.size,
                dst_size: me.size,
                grd: grd,
                arg: arg,
                op: op,
                sign: sign
            })
            return
        if c.report:
            c.report.kind_mismatch += 1
        return
    if me.kind == 35 and te.kind != 35:
        fixed_push(c, TableFixedEntry {
            dst: aux_at,
            size: 1,
            aux: 1,
            grd: grd,
            op: tableFixedConst,
            arg: arg
        })
        fixed_compile_entry(c, theirs, ti, their_at, mine, mi + 1, dst, my_at, grd, arg, census)
        return
    switch me.kind:
        case 35:
            fixed_push(c, TableFixedEntry {
                src: their_at,
                dst: aux_at,
                size: 1,
                grd: grd,
                op: tableFixedBool,
                arg: arg
            })
            fixed_compile_entry(c, theirs, ti + 1, their_at + 1, mine, mi + 1, dst, my_at, grd, arg, census)
        case 13:
            fixed_match_children(c, theirs, ti, their_at, mine, mi, dst, at, grd, arg, census)
        case 14:
            let tel = fixed_entry_at(theirs, ti + 1)
            let mel = fixed_entry_at(mine, mi + 1)
            let head = if d.counted != 0: 4 else: 0
            let their_n = if tel.size != 0: (te.size - head) / tel.size else: 0
            let my_n = if mel.size != 0: (me.size - head) / mel.size else: 0
            let their_base = their_at + head
            let n = if my_n < their_n: my_n else: their_n
            if d.counted != 0:
                fixed_push(c, TableFixedEntry {
                    src: their_at,
                    dst: aux_at,
                    size: n,
                    grd: grd,
                    op: tableFixedCount,
                    arg: arg
                })
            for(n) i:
                fixed_compile_entry(c, theirs, ti + 1, their_base + i * tel.size, mine, mi + 1, dst, at + i * d.stride, grd, arg, census and i == 0)
        case 16:
            let tkey = fixed_entry_at(theirs, ti + 1)
            let mkey = fixed_entry_at(mine, mi + 1)
            let tel = ti + 1 + fixed_subtree(theirs, ti + 1)
            let mel = mi + 1 + fixed_subtree(mine, mi + 1)
            let tee = fixed_entry_at(theirs, tel)
            for(mkey.children) k:
                let key_id = fixed_entry_at(mine, mi + 2 + k).id
                var matched = false
                for(tkey.children) j:
                    if not matched and fixed_entry_at(theirs, ti + 2 + j).id == key_id:
                        matched = true
                        fixed_compile_entry(c, theirs, tel, their_at + j * tee.size, mine, mel, dst, at + k * d.stride, grd, arg, census and k == 0)
        case 15:
            let their_tag = fixed_tag_bytes(theirs, ti)
            let my_tag = fixed_tag_bytes(mine, mi)
            let none_dst_size = if te.children > 0 and te.children <= 255: te.children else: 0
            fixed_push(c, TableFixedEntry {
                src: their_at,
                dst: aux_at,
                size: my_tag,
                grd: grd,
                op: tableFixedConst,
                arg: arg,
                dst_size: none_dst_size
            })
            let saved_w = c.arg_w
            c.arg_w = if their_tag >= 1 and their_tag <= 8: their_tag else: 1
            var my_arm = mi + 1
            for(me.children) k:
                let ma = fixed_entry_at(mine, my_arm)
                var their_arm = ti + 1
                var matched = false
                for(te.children) j:
                    if not matched:
                        let ta = fixed_entry_at(theirs, their_arm)
                        if ta.id == ma.id:
                            matched = true
                            let saved_at = c.chain_at
                            let saved_laid = c.chain_laid
                            push(c.chain, TableFixedGuardLink { grd: their_at, arg: j + 1, arg_w: c.arg_w })
                            c.chain_laid = false
                            fixed_push(c, TableFixedEntry {
                                src: their_at,
                                dst: aux_at,
                                size: my_tag,
                                aux: k + 1,
                                grd: their_at,
                                op: tableFixedConst,
                                arg: j + 1
                            })
                            fixed_compile_entry(c, theirs, their_arm, their_at + their_tag, mine, my_arm, dst, at, their_at, j + 1, census)
                            pop(c.chain)
                            c.chain_at = saved_at
                            c.chain_laid = saved_laid
                        else:
                            their_arm += fixed_subtree(theirs, their_arm)
                my_arm += fixed_subtree(mine, my_arm)
            c.arg_w = saved_w
        case 30:
            if te.size < me.size:
                fixed_push(c, TableFixedEntry {
                    src: their_at,
                    dst: at,
                    size: te.size,
                    grd: grd,
                    op: tableFixedWiden,
                    arg: arg,
                    dst_size: me.size
                })
                return
            let n = te.children
            if n > tableFixedRemapMax:
                c.overflow = true
                return
            let m:[int] = []
            for(n) j:
                let vid = fixed_entry_at(theirs, ti + 1 + j).id
                var landed = 0
                for(me.children) k:
                    if landed == 0 and fixed_entry_at(mine, mi + 1 + k).id == vid:
                        landed = k + 1
                push(m, landed)
            let table_off = fixed_lay_table(c, n)
            fixed_push(c, TableFixedEntry {
                src: their_at,
                dst: at,
                size: te.size,
                aux: table_off,
                grd: grd,
                op: tableFixedOrdinal,
                arg: arg,
                dst_size: me.size,
                remap: m
            })
        case 12:
            var units = me.size - 4
            if te.size - 4 < units:
                units = te.size - 4
            fixed_push(c, TableFixedEntry {
                src: their_at,
                dst: at,
                size: units,
                aux: aux_at,
                grd: grd,
                op: tableFixedText,
                arg: arg,
                meta: d.meta & 0x7f
            })
        default:
            if te.size == me.size:
                let op = if me.kind == 1: tableFixedBool else: tableFixedCopy
                fixed_push(c, TableFixedEntry {
                    src: their_at,
                    dst: at,
                    size: me.size,
                    grd: grd,
                    arg: arg,
                    op: op
                })
            elif te.size < me.size and me.size <= 8:
                fixed_push(c, TableFixedEntry {
                    src: their_at,
                    dst: at,
                    size: te.size,
                    dst_size: me.size,
                    grd: grd,
                    arg: arg,
                    op: tableFixedWiden
                })
            elif c.report:
                c.report.kind_mismatch += 1

def fixed_match_children(c:tableFixedCompiler, theirs:TableFixedLayoutView, ti:int, their_at:int, mine:TableFixedLayoutView, mi:int, dst:[TableFixedDst], my_at:int, grd:int, arg:int, census:bool) -> void:
    let te = fixed_entry_at(theirs, ti)
    let me = fixed_entry_at(mine, mi)
    var my_child = mi + 1
    for(me.children):
        let mc = fixed_entry_at(mine, my_child)
        var their_child = ti + 1
        var their_off = their_at
        var matched = false
        for(te.children):
            if not matched:
                let tc = fixed_entry_at(theirs, their_child)
                if tc.id == mc.id:
                    matched = true
                    fixed_compile_entry(c, theirs, their_child, their_off, mine, my_child, dst, my_at, grd, arg, census)
                else:
                    their_off += tc.size
                    their_child += fixed_subtree(theirs, their_child)
        my_child += fixed_subtree(mine, my_child)
    var tc_at = ti + 1
    for(te.children):
        let tc = fixed_entry_at(theirs, tc_at)
        var named = false
        var mc_at = mi + 1
        for(me.children):
            if not named:
                if fixed_entry_at(mine, mc_at).id == tc.id:
                    named = true
                else:
                    mc_at += fixed_subtree(mine, mc_at)
        if not named and census and c.report:
            c.report.unknown += 1
        tc_at += fixed_subtree(theirs, tc_at)

def fixed_compile(theirs:TableFixedLayoutView, my_layout:string, dst:[TableFixedDst], capacity:int, report:TableReport?) -> [TableFixedEntry], int:
    let empty:[TableFixedEntry] = []
    if capacity <= 0:
        return empty, -1
    let mine, why = fixed_parse_layout(my_layout)
    if why != "":
        return empty, -2
    let c = tableFixedCompiler {
        capacity: capacity,
        record: fixed_entry_at(theirs, 0).size,
        report: report
    }
    fixed_match_children(c, theirs, 0, 0, mine, 0, dst, 0, tableFixedNoGuard, 0, true)
    if c.too_large:
        return empty, -3
    if c.overflow:
        return empty, -1
    let coalesced:[TableFixedEntry] = []
    for(c.count) i:
        let cur = c.plan[i]
        let out = length(coalesced)
        if out > 0:
            let prev = coalesced[out - 1]
            if prev.op == cur.op and fixed_runs(cur.op) and
               prev.grd == cur.grd and prev.arg == cur.arg and
               prev.arg_w == cur.arg_w and prev.meta == cur.meta and
               prev.grd2 == cur.grd2 and prev.arg2 == cur.arg2 and
               prev.arg_w2 == cur.arg_w2 and
               prev.chain_id == cur.chain_id and length(prev.chain) == length(cur.chain) and
               prev.src + prev.size == cur.src and
               prev.dst + prev.size == cur.dst:
                prev.size += cur.size
            else:
                push(coalesced, cur)
        else:
            push(coalesced, cur)
    let out_count = length(coalesced)
    fixed_partition(coalesced, out_count)
    return coalesced, out_count

def fixed_land(cover:string, off:int, n:int) -> string:
    if n <= 0:
        return cover
    let end = length(cover)
    if off < 0 or off >= end:
        return cover
    var last = off + n
    if last > end:
        last = end
    var b = cover
    var i = off
    while i < last:
        b = write_int8_le(b, i, 1)
        i += 1
    return b

def fixed_holes(plan:[TableFixedEntry], count:int, dst_size:int) -> [TableFixedHole]:
    let holes:[TableFixedHole] = []
    if dst_size <= 0:
        return holes
    var cover = zeros(dst_size)
    for(count) i:
        let p = plan[i]
        if p.grd == tableFixedNoGuard:
            switch p.op:
                case tableFixedCopy, tableFixedBool, tableFixedTag, tableFixedConst:
                    cover = fixed_land(cover, p.dst, p.size)
                case tableFixedCount:
                    cover = fixed_land(cover, p.dst, 4)
                case tableFixedText:
                    cover = fixed_land(cover, p.dst, 4)
                    cover = fixed_land(cover, p.aux, p.size)
                case tableFixedWiden:
                    cover = fixed_land(cover, p.dst, p.dst_size)
                case tableFixedWidenF:
                    cover = fixed_land(cover, p.dst, 8)
                case tableFixedOrdinal:
                    let n = if p.dst_size == 1 or p.dst_size == 2 or p.dst_size == 8: p.dst_size else: 4
                    cover = fixed_land(cover, p.dst, n)
                default:
                    pass()
    var i = 0
    while i < dst_size:
        if read_uint8_le(cover, i) != 0:
            i += 1
        else:
            var j = i + 1
            while j < dst_size and read_uint8_le(cover, j) == 0:
                j += 1
            push(holes, TableFixedHole { off: i, size: j - i })
            i = j
    return holes

def fixed_select(known:[TableFixedKnownLayout], hash:int) -> int:
    for(length(known)) i:
        if known[i].hash == hash:
            return i
    return -1

def fixed_lineage_plans(known:[TableFixedKnownLayout], my_layout:string, dst:[TableFixedDst], own:int) -> [TableFixedLineagePlan]:
    let out:[TableFixedLineagePlan] = []
    for(length(known)) i:
        let lane = TableFixedLineagePlan {}
        if known[i].hash != own:
            let parsed, why = fixed_parse_layout(known[i].layout)
            if why != "":
                lane.why = TableRefuseLayoutMalformed
            else:
                var room = 256
                var done = false
                while not done:
                    let census = TableReport {}
                    let entries, made = fixed_compile(parsed, my_layout, dst, room, census)
                    if made == -2:
                        lane.why = TableRefuseLayoutMalformed
                        done = true
                    elif made == -3:
                        lane.why = TableRefuseLayoutRecordTooLarge
                        done = true
                    elif made < 0:
                        if room >= 262144:
                            lane.why = TableRefusePlanTooLarge
                            done = true
                        else:
                            room *= 4
                    else:
                        lane.entries = entries
                        lane.count = made
                        lane.unknown = census.unknown
                        lane.kind_mismatch = census.kind_mismatch
                        done = true
        push(out, lane)
    return out

def fixed_lineage_holes(lanes:[TableFixedLineagePlan], dst_size:int) -> [[TableFixedHole]]:
    let out:[[TableFixedHole]] = []
    for(length(lanes)) i:
        let lane = lanes[i]
        if lane.why == "":
            push(out, fixed_holes(lane.entries, lane.count, dst_size))
        else:
            let empty:[TableFixedHole] = []
            push(out, empty)
    return out

def fixed_plan_max(own:TableFixedPlan, lanes:[TableFixedLineagePlan]) -> int:
    var m = own.count
    for(length(lanes)) i:
        if lanes[i].count > m:
            m = lanes[i].count
    return m

def fixed_utf8_valid_no_nul(p:string, base:int, used:int) -> bool:
    var i = 0
    while i < used:
        let b0 = read_uint8_le(p, base + i)
        if b0 == 0:
            return false
        if b0 < 0x80:
            i += 1
        elif b0 < 0xc2:
            return false
        elif b0 < 0xe0:
            if i + 1 >= used:
                return false
            let b1 = read_uint8_le(p, base + i + 1)
            if (b1 & 0xc0) != 0x80:
                return false
            i += 2
        elif b0 < 0xf0:
            if i + 2 >= used:
                return false
            let b1 = read_uint8_le(p, base + i + 1)
            let b2 = read_uint8_le(p, base + i + 2)
            if (b1 & 0xc0) != 0x80 or (b2 & 0xc0) != 0x80:
                return false
            if b0 == 0xe0 and b1 < 0xa0:
                return false
            if b0 == 0xed and b1 >= 0xa0:
                return false
            i += 3
        elif b0 < 0xf5:
            if i + 3 >= used:
                return false
            let b1 = read_uint8_le(p, base + i + 1)
            let b2 = read_uint8_le(p, base + i + 2)
            let b3 = read_uint8_le(p, base + i + 3)
            if (b1 & 0xc0) != 0x80 or (b2 & 0xc0) != 0x80 or (b3 & 0xc0) != 0x80:
                return false
            if b0 == 0xf0 and b1 < 0x90:
                return false
            if b0 == 0xf4 and b1 >= 0x90:
                return false
            i += 4
        else:
            return false
    return true

def fixed_wide_ill_formed(p:string, base:int, used:int) -> bool:
    var i = 0
    while i < used:
        let u = read_uint16_le(p, base + i * 2)
        if u == 0:
            return true
        if u >= 0xd800 and u <= 0xdbff:
            if i + 1 >= used:
                return true
            let lo = read_uint16_le(p, base + (i + 1) * 2)
            if lo < 0xdc00 or lo > 0xdfff:
                return true
            i += 2
        elif u >= 0xdc00 and u <= 0xdfff:
            return true
        else:
            i += 1
    return false

def fixed_text_ill_formed(meta:int, used:int, p:string, base:int) -> bool:
    if meta == tableFixedTextUtf8:
        return not fixed_utf8_valid_no_nul(p, base, used)
    return false

def fixed_text_refusal(plan:[TableFixedEntry], count:int, src:string, src_base:int) -> string:
    for(count) i:
        let p = plan[i]
        var active = true
        if p.grd != tableFixedNoGuard and fixed_tag_at(src, src_base + p.grd, p.arg_w) != p.arg:
            active = false
        if active and p.arg_w2 != 0 and fixed_tag_at(src, src_base + p.grd2, p.arg_w2) != p.arg2:
            active = false
        if active and length(p.chain) > 2 and not fixed_chain_holds(p, src, src_base):
            active = false
        if active:
            if p.op == tableFixedCount:
                let v = read_int32_le(src, src_base + p.src)
                if v < 0 or v > p.size:
                    return TableRefuseCountOverBound
            elif p.op == tableFixedText:
                let capn = p.size
                let v = read_int32_le(src, src_base + p.src)
                if v < 0 or v > capn:
                    return TableRefuseLengthOverBound
                if fixed_text_ill_formed(p.meta, v, src, src_base + p.src + 4):
                    return TableRefuseTextIllFormed
    return ""

def fixed_copy_run(dst:string, dst_off:int, src:string, src_off:int, n:int) -> string:
    var b = dst
    var k = 0
    while k + 8 <= n:
        b = write_int64_le(b, dst_off + k, read_int64_le(src, src_off + k))
        k += 8
    while k < n:
        b = write_int8_le(b, dst_off + k, read_uint8_le(src, src_off + k))
        k += 1
    return b

def fixed_run(plan:[TableFixedEntry], count:int, src:string, src_base:int, dst_in:string, report:TableReport) -> string, string:
    let why = fixed_text_refusal(plan, count, src, src_base)
    if why != "":
        return dst_in, why
    var dst = dst_in
    var split = count
    for(count) i:
        if split == count and plan[i].grd != tableFixedNoGuard:
            split = i
    for(count) i:
        let p = plan[i]
        var active = true
        if i >= split and p.grd != tableFixedNoGuard and fixed_tag_at(src, src_base + p.grd, p.arg_w) != p.arg:
            active = false
        if active and p.arg_w2 != 0 and fixed_tag_at(src, src_base + p.grd2, p.arg_w2) != p.arg2:
            active = false
        if active and length(p.chain) > 2 and not fixed_chain_holds(p, src, src_base):
            active = false
        if active:
            switch p.op:
                case tableFixedCopy:
                    dst = fixed_copy_run(dst, p.dst, src, src_base + p.src, p.size)
                case tableFixedTag:
                    var tag = fixed_tag_at(src, src_base + p.src, p.size)
                    if ult(p.aux, tag):
                        tag = 0
                        report.unknown += 1
                    for(p.size) k:
                        dst = write_int8_le(dst, p.dst + k, shr(tag, 8 * k) & 0xff)
                case tableFixedBool:
                    for(p.size) k:
                        let v = if read_uint8_le(src, src_base + p.src + k) != 0: 1 else: 0
                        dst = write_int8_le(dst, p.dst + k, v)
                case tableFixedCount:
                    let v = read_int32_le(src, src_base + p.src)
                    dst = write_int32_le(dst, p.dst, v)
                case tableFixedText:
                    let v = read_int32_le(src, src_base + p.src)
                    dst = write_int32_le(dst, p.dst, v)
                    dst = fixed_copy_run(dst, p.aux, src, src_base + p.src + 4, p.size)
                case tableFixedOrdinal:
                    let raw = fixed_tag_at(src, src_base + p.src, p.size)
                    let n_remap = length(p.remap)
                    var v = 0
                    if raw != 0 and not ult(n_remap, raw):
                        v = p.remap[raw - 1]
                    if p.dst_size == 1:
                        dst = write_int8_le(dst, p.dst, v)
                    elif p.dst_size == 2:
                        dst = write_int16_le(dst, p.dst, v)
                    elif p.dst_size == 8:
                        dst = write_int64_le(dst, p.dst, v)
                    else:
                        dst = write_int32_le(dst, p.dst, v)
                case tableFixedWiden:
                    var raw = 0
                    if p.sign != 0:
                        if p.size == 1:
                            raw = read_int8_le(src, src_base + p.src)
                        elif p.size == 2:
                            raw = read_int16_le(src, src_base + p.src)
                        elif p.size == 4:
                            raw = read_int32_le(src, src_base + p.src)
                    else:
                        if p.size == 1:
                            raw = read_uint8_le(src, src_base + p.src)
                        elif p.size == 2:
                            raw = read_uint16_le(src, src_base + p.src)
                        elif p.size == 4:
                            raw = read_uint32_le(src, src_base + p.src)
                    if p.dst_size == 2:
                        dst = write_int16_le(dst, p.dst, raw)
                    elif p.dst_size == 4:
                        dst = write_int32_le(dst, p.dst, raw)
                    elif p.dst_size == 8:
                        dst = write_int64_le(dst, p.dst, raw)
                    else:
                        for(p.dst_size) k:
                            dst = write_int8_le(dst, p.dst + k, shr(raw, 8 * k) & 0xff)
                    report.widened += 1
                case tableFixedWidenF:
                    // every finite float32 widens exactly through the ordinary
                    // conversion; a NaN stays a NaN and an infinity an
                    // infinity, and the finite pass refuses either (§3.3)
                    dst = write_float64_le(dst, p.dst, read_float32_le(src, src_base + p.src))
                    report.widened += 1
                case tableFixedConst:
                    let aux = p.aux
                    for(p.size) k:
                        dst = write_int8_le(dst, p.dst + k, shr(aux, 8 * k) & 0xff)
                default:
                    pass()
    return dst, ""

def fixed_forged_bounds(plan:[TableFixedEntry], count:int, src:string, src_base:int, report:TableReport):
    for(count) i:
        let p = plan[i]
        var active = true
        if p.grd != tableFixedNoGuard and fixed_tag_at(src, src_base + p.grd, p.arg_w) != p.arg:
            active = false
        if active and p.arg_w2 != 0 and fixed_tag_at(src, src_base + p.grd2, p.arg_w2) != p.arg2:
            active = false
        if active and length(p.chain) > 2 and not fixed_chain_holds(p, src, src_base):
            active = false
        if active:
            if p.op == tableFixedOrdinal:
                let raw = fixed_tag_at(src, src_base + p.src, p.size)
                if raw != 0 and ult(length(p.remap), raw):
                    report.clamped += 1
            elif p.op == tableFixedConst:
                if p.aux == 0 and p.dst_size != 0 and p.grd == tableFixedNoGuard:
                    let raw = fixed_tag_at(src, src_base + p.src, p.size)
                    if ult(p.dst_size, raw):
                        report.clamped += 1

def fixed_prefill_holes(img:string, def_bytes:string, holes:[TableFixedHole]) -> string:
    var out = img
    for(holes) h:
        out = fixed_copy_run(out, h.off, def_bytes, h.off, h.size)
    return out

def fixed_report_err(r:TableReport) -> string:
    // a non-finite float outranks every later verdict (§3.3)
    if r.non_finite_float != "":
        return TableRefuseNonFiniteFloat
    if r.fixed_raised and r.verdict == TableOpenRefused:
        if r.reason == TableRefusePreviousForm or r.reason == TableRefuseMessageFormAsFile or r.reason == TableRefuseNewerForm:
            return TableRefuseUnknownForm
        if r.reason != "":
            return r.reason
        return TableRefuseUnknownForm
    if r.reason == TableRefuseTruncated or r.reason == TableRefuseReservedNotZero or r.reason == TableRefuseIntegrityMismatch:
        return r.reason
    if r.verdict == TableOpenRefused or r.reason == TableRefusePreviousForm or r.reason == TableRefuseNewerForm or r.reason == TableRefuseMessageFormAsFile:
        return TableRefuseUnknownForm
    if r.verdict == TableOpenDamaged:
        return "wire_damaged"
    if r.malformed:
        return "malformed"
    if r.clamped > 0:
        return TableRefuseClamped
    if r.kind_mismatch > 0:
        return TableRefuseKindMismatch
    if r.reason != "":
        return r.reason
    return ""

def fixed_crc32c(data:string, len_bytes:int) -> int:
    var crc = 0xffffffff
    for(len_bytes) i:
        crc = (crc ^ read_uint8_le(data, i)) & 0xffffffff
        for(8):
            if (crc & 1) != 0:
                crc = (shr(crc, 1) ^ 0x82f63b78) & 0xffffffff
            else:
                crc = shr(crc, 1) & 0xffffffff
    return (crc ^ 0xffffffff) & 0xffffffff

def fixed_integrity_holds(wire:string) -> bool:
    let n = length(wire) - 4
    return n >= 1 and read_uint32_le(wire, n) == fixed_crc32c(wire, n)

def fixed_integrity_seal(buffer:string, n:int) -> int, string:
    if n < 0 or n + 4 > length(buffer):
        return -1, buffer
    let crc = fixed_crc32c(buffer, n)
    return n + 4, write_int32_le(buffer, n, crc)


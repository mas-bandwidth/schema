// Wire.swift — the variable form, form byte `1` (docs/SPEC-TABLES.md §3,
// §3.1, §3.2, §4, §22.1–§22.5). One read: the whole value or a refusal.

let arrayElemKinds: Set<UInt8> = Set(Array(1...13) + [15, 17] + Array(18...29) + [30])
let keyedElemKinds: Set<UInt8> = Set(Array(1...11) + [13] + Array(18...29) + [30])
let maxDepth = 128
let maxIds = 4096

func damaged(_ why: String = "wire_damaged") -> Refusal { Refusal("wire_damaged: " + why) }

struct NodeRec { let typeId: UInt64; let start: Int; let end: Int }

// MARK: reader

final class WireReader {
    let b: [UInt8]
    var ids: [UInt64] = []
    var maxRef = 0
    var bodyEnd = 0
    var nodes: [NodeRec] = []
    var nodeRecs: [AnyObject?] = []
    var unit: Unit
    var root: TableDecl
    var unknown = 0

    init(_ bytes: [UInt8], unit: Unit, root: TableDecl) { b = bytes; self.unit = unit; self.root = root }

    // canonical LEB128, 64-bit capability
    func leb(_ p: inout Int, _ end: Int) throws -> UInt64 {
        var v: UInt64 = 0
        var shift: UInt64 = 0
        var n = 0
        while true {
            guard p < end else { throw damaged("leb past end") }
            let c = b[p]; p += 1; n += 1
            if n == 10 && c > 1 { throw damaged("leb past 64 bits") }
            v |= UInt64(c & 0x7f) << shift
            if c & 0x80 == 0 {
                if n > 1 && c == 0 { throw damaged("non-minimal leb") }
                return v
            }
            shift += 7
            if n >= 10 { throw damaged("leb too long") }
        }
    }
    func ref(_ p: inout Int, _ end: Int) throws -> Int {
        let r = try leb(&p, end)
        if r == 0 { return 0 }
        guard r <= UInt64(ids.count) else { throw damaged("reference past table") }
        let ri = Int(r)
        if ri > maxRef + 1 { throw Refusal("id_table_order: entry used before its predecessor") }
        if ri == maxRef + 1 { maxRef = ri }
        return ri
    }
    func len(_ p: inout Int, _ end: Int) throws -> Int {
        let l = try leb(&p, end)
        guard l <= UInt64(end - p) else { throw damaged("length past body") }
        return Int(l)
    }
    func u64(_ at: Int) -> UInt64 {
        var v: UInt64 = 0
        for i in 0..<8 { v |= UInt64(b[at + i]) << UInt64(8 * i) }
        return v
    }

    // MARK: the whole read

    func read() throws -> Rec {
        guard b.count >= 1 else { throw damaged("empty buffer") }
        guard b[0] == 1 else { throw Refusal(b[0] == 2 ? "message_form_as_file" : "newer_form") }
        var end = b.count
        if root.integrity {
            guard end >= 5 else { throw damaged("no integrity trailer") }
            let want = UInt32(b[end - 4]) | UInt32(b[end - 3]) << 8 | UInt32(b[end - 2]) << 16 | UInt32(b[end - 1]) << 24
            guard crc32c(b[0..<(end - 4)]) == want else { throw Refusal("integrity_mismatch") }
            end -= 4
        }
        guard end >= 9 else { throw damaged("too short for id table") }
        let count = u64(end - 8)
        guard count <= UInt64(maxIds) else { throw damaged("id table past 4096") }
        let tableStart = end - 8 - 8 * Int(count)
        guard tableStart >= 1 else { throw damaged("id table past front") }
        var seen = Set<UInt64>()
        for k in 0..<Int(count) {
            let id = u64(tableStart + 8 * k)
            guard seen.insert(id).inserted else { throw damaged("id twice") }
            ids.append(id)
        }
        bodyEnd = tableStart
        // pass 1: framing, first-use order, the node table's place
        var p = 1
        try scanBody(&p, bodyEnd, depth: 0, isRoot: true)
        guard p == bodyEnd else { throw damaged("bytes between root and id table") }
        guard maxRef == ids.count else { throw Refusal("id_table_order: unused entry") }
        // pass 2: typed decode
        nodeRecs = nodes.map { n -> AnyObject? in
            if n.typeId == blobBytesId || n.typeId == blobStringId { return nil }
            if let d = unit.tables.values.first(where: { $0.wireId == n.typeId && !$0.isType }) { return newRec(d) }
            return nil
        }
        var blobs: [Blob?] = nodes.map { n in
            if n.typeId == blobBytesId { return Blob(Array(b[n.start..<n.end]), isString: false) }
            if n.typeId == blobStringId { return Blob(Array(b[n.start..<n.end]), isString: true) }
            return nil
        }
        _ = blobs
        let rootRec = newRec(root)
        rootObj = rootRec
        try decodeBody(rootRec, 1, bodyEnd, depth: 0, isRoot: true)
        for (k, n) in nodes.enumerated() {
            if let r = nodeRecs[k] as? Rec { try decodeBody(r, n.start, n.end, depth: 0, isRoot: false) }
        }
        blobs = []
        try checkGraph(rootRec)
        return rootRec
    }
    var rootObj: Rec? = nil
    var blobObjs: [Int: Blob] = [:]

    // MARK: pass 1, the wire alone

    func scanBody(_ p: inout Int, _ end: Int, depth: Int, isRoot: Bool) throws {
        if depth > maxDepth { throw damaged("nesting past 128") }
        var seenIds = Set<UInt64>()
        var sawNodeTable = false
        while true {
            let r = try ref(&p, end)
            if r == 0 {
                guard p == end else { throw damaged("terminator before end of body") }
                return
            }
            if sawNodeTable { throw Refusal("node table not the root body's last field") }
            let id = ids[r - 1]
            guard seenIds.insert(id).inserted else { throw damaged("field twice in one body") }
            guard p < end else { throw damaged("no kind byte") }
            let k = b[p]; p += 1
            if id == reservedBuildVersionId || id == reservedVocabularyId { throw damaged("reserved id out of place") }
            if id == reservedNodeTableId {
                guard isRoot else { throw damaged("node table in nested body") }
                guard k == 12 else { throw damaged("node table under wrong kind") }
                let l = try len(&p, end)
                try scanNodeTable(p, p + l)
                p += l
                sawNodeTable = true
                continue
            }
            try scanPayload(k, &p, end, depth: depth)
        }
    }

    func scanNodeTable(_ start: Int, _ end: Int) throws {
        var p = start
        let n = try leb(&p, end)
        guard n >= 1 else { throw Refusal("empty node table") }
        guard n <= UInt64(end - p) else { throw damaged("node count past field") }
        for _ in 0..<Int(n) {
            let r = try ref(&p, end)
            guard r != 0 else { throw damaged("node record type id 0") }
            let tid = ids[r - 1]
            let l = try len(&p, end)
            let s = p
            if tid == blobBytesId {
                p += l
            } else if tid == blobStringId {
                guard utf8WellFormed(b[s..<(s + l)]) else { throw damaged("string blob not text") }
                p += l
            } else {
                var q = s
                try scanBody(&q, s + l, depth: 0, isRoot: false)
                p = s + l
            }
            nodes.append(NodeRec(typeId: tid, start: s, end: s + l))
        }
        guard p == end else { throw damaged("bytes left in node table") }
    }

    /// One payload of kind `k` at a field position (its own framing prefix).
    func scanPayload(_ k: UInt8, _ p: inout Int, _ end: Int, depth: Int) throws {
        if let w = kindWidth(k) {
            guard end - p >= w else { throw damaged("scalar past body") }
            try checkScalar(k, p)
            p += w
            return
        }
        switch k {
        case 12:
            let l = try len(&p, end)
            guard utf8WellFormed(b[p..<(p + l)]) else { throw damaged("text not well-formed") }
            p += l
        case 13:
            let l = try len(&p, end)
            var q = p
            try scanBody(&q, p + l, depth: depth + 1, isRoot: false)
            p += l
        case 14:
            let l = try len(&p, end)
            try scanArrayBody(p, p + l, depth: depth + 1)
            p += l
        case 16:
            let l = try len(&p, end)
            try scanKeyedBody(p, p + l, depth: depth + 1)
            p += l
        case 15:
            try scanUnion(&p, end, depth: depth)
        case 17:
            _ = try leb(&p, end)
        case 30:
            _ = try ref(&p, end)
        case 31:
            let l = try len(&p, end); p += l
        case 32:
            let l = try len(&p, end)
            guard l == 0 else { throw damaged("kind 32 with payload") }
        default:
            throw damaged("unknown kind \(k)")
        }
    }

    func checkScalar(_ k: UInt8, _ p: Int) throws {
        if k == 1 { guard b[p] <= 1 else { throw damaged("bool not 0 or 1") } }
        if k == 10 {
            let bits = UInt32(b[p]) | UInt32(b[p + 1]) << 8 | UInt32(b[p + 2]) << 16 | UInt32(b[p + 3]) << 24
            guard Float(bitPattern: bits).isFinite else { throw Refusal("non_finite_float") }
        }
        if k == 11 { guard Double(bitPattern: u64(p)).isFinite else { throw Refusal("non_finite_float") } }
    }

    func scanUnion(_ p: inout Int, _ end: Int, depth: Int) throws {
        let r = try ref(&p, end)
        if r == 0 { return }
        guard p < end else { throw damaged("no arm kind") }
        let k = b[p]; p += 1
        let l = try len(&p, end)
        try scanArmPayload(k, p, p + l, depth: depth)
        p += l
    }

    /// An arm's payload: exactly the bytes its kind puts after its own
    /// framing prefix, under the arm's `L`.
    func scanArmPayload(_ k: UInt8, _ s: Int, _ e: Int, depth: Int) throws {
        if let w = kindWidth(k) {
            guard e - s == w else { throw damaged("arm L not its width") }
            try checkScalar(k, s)
            return
        }
        var p = s
        switch k {
        case 12: guard utf8WellFormed(b[s..<e]) else { throw damaged("text not well-formed") }
        case 13: try scanBody(&p, e, depth: depth + 1, isRoot: false)
        case 14: try scanArrayBody(s, e, depth: depth + 1)
        case 16: try scanKeyedBody(s, e, depth: depth + 1)
        case 15:
            try scanUnion(&p, e, depth: depth + 1)
            guard p == e else { throw damaged("bytes after nested union") }
        case 17: _ = try leb(&p, e); guard p == e else { throw damaged("arm L not its index") }
        case 30: _ = try ref(&p, e); guard p == e else { throw damaged("arm L not its reference") }
        case 31: break
        case 32: guard s == e else { throw damaged("kind 32 with payload") }
        default: throw damaged("unknown arm kind \(k)")
        }
    }

    func scanArrayBody(_ s: Int, _ e: Int, depth: Int) throws {
        if depth > maxDepth { throw damaged("nesting past 128") }
        guard e - s >= 2 else { throw damaged("array body too short") }
        var p = s
        let ek = b[p]; p += 1
        guard arrayElemKinds.contains(ek) else { throw damaged("element kind off the list") }
        let n = try leb(&p, e)
        if let w = kindWidth(ek) {
            guard n <= UInt64(e - p) / UInt64(w), Int(n) * w == e - p else { throw damaged("array count vs body") }
            var q = p
            for _ in 0..<Int(n) { try checkScalar(ek, q); q += w }
            return
        }
        guard n <= UInt64(e - p) else { throw damaged("count past body") }
        for _ in 0..<Int(n) { try scanElement(ek, &p, e, depth: depth) }
        guard p == e else { throw damaged("bytes after elements") }
    }

    func scanElement(_ ek: UInt8, _ p: inout Int, _ e: Int, depth: Int) throws {
        switch ek {
        case 12:
            let l = try len(&p, e)
            guard utf8WellFormed(b[p..<(p + l)]) else { throw damaged("text not well-formed") }
            p += l
        case 13:
            let l = try len(&p, e)
            var q = p
            try scanBody(&q, p + l, depth: depth + 1, isRoot: false)
            p += l
        case 15: try scanUnion(&p, e, depth: depth)
        case 17: _ = try leb(&p, e)
        case 30: _ = try ref(&p, e)
        default:
            guard let w = kindWidth(ek), e - p >= w else { throw damaged("element past body") }
            try checkScalar(ek, p); p += w
        }
    }

    func scanKeyedBody(_ s: Int, _ e: Int, depth: Int) throws {
        if depth > maxDepth { throw damaged("nesting past 128") }
        guard e - s >= 2 else { throw damaged("keyed body too short") }
        var p = s
        let ek = b[p]; p += 1
        guard keyedElemKinds.contains(ek) else { throw damaged("keyed element kind off the list") }
        let n = try leb(&p, e)
        guard n <= UInt64(e - p) else { throw damaged("count past body") }
        var keys = Set<Int>()
        for _ in 0..<Int(n) {
            let r = try ref(&p, e)
            guard r != 0 else { throw damaged("keyed slot key 0") }
            guard keys.insert(r).inserted else { throw damaged("keyed slot twice") }
            let l = try len(&p, e)
            try scanArmPayload(ek, p, p + l, depth: depth)
            p += l
        }
        guard p == e else { throw damaged("bytes after slots") }
    }

    // MARK: pass 2, typed

    func decodeBody(_ rec: Rec, _ s: Int, _ e: Int, depth: Int, isRoot: Bool) throws {
        var p = s
        var got = [Bool](repeating: false, count: rec.decl.fields.count)
        while true {
            let r = try leb(&p, e)
            if r == 0 { break }
            let id = ids[Int(r) - 1]
            let k = b[p]; p += 1
            if id == reservedNodeTableId { let l = try len(&p, e); p += l; continue }
            if let fi = rec.decl.byWireId[id] {
                let f = rec.decl.fields[fi]
                got[fi] = true
                rec.vals[fi] = try decodeField(f.ft, k, &p, e, depth: depth, field: f)
                if let tot = f.total, case let .arr(xs) = rec.vals[fi] {
                    var sum = 0
                    for x in xs { if case let .bytes(bb) = x { sum += bb.count } }
                    if sum > tot { throw Refusal("length_over_bound: \(rec.decl.name).\(f.name) total") }
                }
            } else {
                unknown += 1
                try skip(k, &p, e)
            }
        }
        for (i, f) in rec.decl.fields.enumerated() where f.required && !got[i] && guardHolds(f, rec) {
            throw Refusal("missing_required: \(rec.decl.name).\(f.name)")
        }
    }

    func skip(_ k: UInt8, _ p: inout Int, _ e: Int) throws {
        if let w = kindWidth(k) { p += w; return }
        switch k {
        case 12, 13, 14, 16, 31, 32: let l = try len(&p, e); p += l
        case 17, 30: _ = try leb(&p, e)
        case 15:
            let r = try leb(&p, e)
            if r != 0 { p += 1; let l = try len(&p, e); p += l }
        default: throw damaged("unknown kind")
        }
    }

    func fieldName(_ f: Field?) -> String { f.map { $0.name } ?? "element" }

    /// Is wire kind `got` readable as declared kind `want` (same, or a widening)?
    func kindOK(_ want: UInt8, _ got: UInt8) -> Bool {
        if want == got { return true }
        let signed: [UInt8] = [2, 3, 4, 5, 18], unsigned: [UInt8] = [6, 7, 8, 9, 19]
        if let wi = signed.firstIndex(of: want), let gi = signed.firstIndex(of: got) { return gi < wi }
        if let wi = unsigned.firstIndex(of: want), let gi = unsigned.firstIndex(of: got) { return gi < wi }
        if want == 11 && got == 10 { return true }
        return false
    }

    func readScalarRaw(_ k: UInt8, _ p: inout Int) -> UInt128 {
        let w = kindWidth(k)!
        var v: UInt128 = 0
        for i in 0..<w { v |= UInt128(b[p + i]) << UInt128(8 * i) }
        p += w
        return v
    }
    func signedKind(_ k: UInt8) -> Bool { [2, 3, 4, 5, 18, 20, 21, 22, 23, 24, 36].contains(k) }

    func decodeField(_ t: FT, _ k: UInt8, _ p: inout Int, _ e: Int, depth: Int, field: Field?) throws -> Value {
        let want = t.kind
        guard kindOK(want, k) else { throw Refusal("kind_mismatch: \(fieldName(field))") }
        switch t {
        case .array, .map, .keyed:
            let l = try len(&p, e)
            let s = p
            p += l
            if case .keyed = t { return try decodeKeyed(t, s, s + l, depth: depth + 1, field: field) }
            return try decodeArrayBody(t, s, s + l, depth: depth + 1, field: field)
        case .union:
            return try decodeUnion(t, &p, e, depth: depth, field: field)
        default:
            if t.kind == 13 || t.kind == 12 || t.kind == 14 {
                let l = try len(&p, e)
                let s = p
                p += l
                return try decodeArm(t, k, s, s + l, depth: depth, field: field)
            }
            return try decodeInline(t, k, &p, e, field: field)
        }
    }

    /// Scalars at fixed width, enums, pointers.
    func decodeInline(_ t: FT, _ k: UInt8, _ p: inout Int, _ e: Int, field: Field?) throws -> Value {
        switch t {
        case .bool: let v = b[p]; p += 1; return .bool(v == 1)
        case let .int(w, s, lo, hi):
            let gw = kindWidth(k)! * 8
            let v = Big.fromBits(readScalarRaw(k, &p), width: gw, signed: s)
            _ = w
            if let lo = lo, v < lo { throw Refusal("out_of_range: \(fieldName(field))") }
            if let hi = hi, hi < v { throw Refusal("out_of_range: \(fieldName(field))") }
            return .int(v)
        case let .bits(n):
            let gw = kindWidth(k)! * 8
            let v = Big.fromBits(readScalarRaw(k, &p), width: gw, signed: false)
            if n < 128 && v.mag >= (UInt128(1) << UInt128(n)) { throw Refusal("out_of_range: \(fieldName(field))") }
            return .int(v)
        case .flags:
            let gw = kindWidth(k)! * 8
            return .int(Big.fromBits(readScalarRaw(k, &p), width: gw, signed: false))
        case .timestamp:
            return .int(Big.fromBits(readScalarRaw(k, &p), width: 64, signed: true))
        case let .fixed(s, i, f, lo, hi):
            let v = Big.fromBits(readScalarRaw(k, &p), width: i + f, signed: s)
            if let lo = lo, v < lo { throw Refusal("out_of_range: \(fieldName(field))") }
            if let hi = hi, hi < v { throw Refusal("out_of_range: \(fieldName(field))") }
            return .int(v)
        case let .float(w, lo, hi):
            var d: Double
            var out: Value
            if k == 10 {
                let x = Float(bitPattern: UInt32(truncatingIfNeeded: readScalarRaw(k, &p)))
                d = Double(x)
                out = w == 32 ? .f32(x) : .f64(Double(x))
            } else {
                let x = Double(bitPattern: UInt64(truncatingIfNeeded: readScalarRaw(k, &p)))
                d = x
                out = .f64(x)
            }
            if !d.isFinite { throw Refusal("non_finite_float") }
            if lo != nil || hi != nil {
                if let lo = lo, d < lo { throw Refusal("out_of_range: \(fieldName(field))") }
                if let hi = hi, d > hi { throw Refusal("out_of_range: \(fieldName(field))") }
                if d == 0 { out = w == 32 ? .f32(0) : .f64(0) } // a compressed -0 lands as +0
            }
            return out
        case let .enm(en):
            let r = try leb(&p, e)
            if r == 0 { return .int(.zero) }
            let id = ids[Int(r) - 1]
            if let k = en.wireIds.firstIndex(of: id) { return .int(Big(k + 1)) }
            unknown += 1
            return .int(.zero)
        case let .ptr(d):
            let idx = try leb(&p, e)
            return .ptr(try resolveNode(idx, table: d))
        case let .blob(isString):
            let idx = try leb(&p, e)
            return .blob(try resolveBlob(idx, isString: isString))
        default:
            throw Refusal("kind_mismatch: \(fieldName(field))")
        }
    }

    func resolveNode(_ idx: UInt64, table: TableDecl) throws -> Rec? {
        if idx == 0 { return nil }
        if idx == 1 {
            guard rootObj!.decl === table else { throw damaged("pointer to root of another type") }
            return rootObj
        }
        let k = Int(idx) - 2
        guard idx >= 2, k < nodes.count else { throw damaged("node index past table") }
        reached.insert(k)
        if let r = nodeRecs[k] as? Rec {
            guard r.decl === table else { throw damaged("pointer to a record of another type") }
            return r
        }
        let tid = nodes[k].typeId
        if tid == blobBytesId || tid == blobStringId { throw damaged("pointer to a blob") }
        if unit.tables.values.contains(where: { $0.wireId == tid && !$0.isType }) { throw damaged("pointer to a record of another type") }
        unknown += 1
        return nil
    }
    var reached = Set<Int>()

    func resolveBlob(_ idx: UInt64, isString: Bool) throws -> Blob? {
        if idx == 0 { return nil }
        guard idx >= 2 else { throw damaged("blob at root index") }
        let k = Int(idx) - 2
        guard k < nodes.count else { throw damaged("node index past table") }
        reached.insert(k)
        let tid = nodes[k].typeId
        guard tid == (isString ? blobStringId : blobBytesId) else { throw damaged("blob of another type") }
        if blobObjs[k] != nil { throw Refusal("blob_shared") }
        let bl = Blob(Array(b[nodes[k].start..<nodes[k].end]), isString: isString)
        blobObjs[k] = bl
        return bl
    }

    /// A length-framed payload (kind 12, 13, 14 body) between s and e, as a
    /// field's payload or an arm's.
    func decodeArm(_ t: FT, _ k: UInt8, _ s: Int, _ e: Int, depth: Int, field: Field?) throws -> Value {
        switch t {
        case let .str(n):
            if let n = n, e - s > n { throw Refusal("length_over_bound: \(fieldName(field))") }
            return .bytes(Array(b[s..<e]))
        case let .table(d):
            let r = newRec(d)
            try decodeBody(r, s, e, depth: depth + 1, isRoot: false)
            return .rec(r)
        case let .bytes(n):
            var p = s
            let ek = b[p]; p += 1
            guard ek == 6 else { throw Refusal("kind_mismatch: \(fieldName(field))") }
            let c = try leb(&p, e)
            if c > UInt64(n) { throw Refusal("length_over_bound: \(fieldName(field))") }
            return .bytes(Array(b[p..<e]))
        case .array, .map:
            return try decodeArrayBody(t, s, e, depth: depth + 1, field: field)
        case .keyed:
            return try decodeKeyed(t, s, e, depth: depth + 1, field: field)
        case .union:
            var p = s
            let v = try decodeUnion(t, &p, e, depth: depth, field: field)
            return v
        default:
            var p = s
            return try decodeInline(t, k, &p, e, field: field)
        }
    }

    func decodeUnion(_ t: FT, _ p: inout Int, _ e: Int, depth: Int, field: Field?) throws -> Value {
        guard case let .union(un) = t else { fatalError() }
        let r = try leb(&p, e)
        if r == 0 { return .union(-1, nil) }
        let id = ids[Int(r) - 1]
        let k = b[p]; p += 1
        let l = try len(&p, e)
        let s = p
        p += l
        guard let ai = un.arms.firstIndex(where: { $0.wireId == id }) else { unknown += 1; return .union(-1, nil) }
        let arm = un.arms[ai]
        guard let at = arm.ft else {
            guard k == 32 else { throw Refusal("kind_mismatch: arm \(arm.name)") }
            return .union(ai, nil)
        }
        guard kindOK(at.kind, k) else { throw Refusal("kind_mismatch: arm \(arm.name)") }
        let inner: Value
        switch at {
        case .array, .map:
            inner = try decodeArrayBody(at, s, s + l, depth: depth + 1, field: nil)
        case .keyed:
            inner = try decodeKeyed(at, s, s + l, depth: depth + 1, field: nil)
        case .union:
            var q = s
            inner = try decodeUnion(at, &q, s + l, depth: depth + 1, field: nil)
        default:
            inner = try decodeArm(at, k, s, s + l, depth: depth, field: nil)
        }
        return .union(ai, inner)
    }

    func decodeArrayBody(_ t: FT, _ s: Int, _ e: Int, depth: Int, field: Field?) throws -> Value {
        var p = s
        let ek = b[p]; p += 1
        let n = Int(try leb(&p, e))
        if case let .map(kt, vt, vopt) = t {
            guard ek == 13 else { throw Refusal("kind_mismatch: \(fieldName(field))") }
            var out: [(Value, Value)] = []
            for _ in 0..<n {
                let l = try len(&p, e)
                let (kv, vv) = try decodeEntry(kt, vt, vopt, p, p + l, depth: depth + 1)
                if let last = out.last, !keyLess(last.0, kv, kt) { throw damaged("map key not ascending") }
                out.append((kv, vv))
                p += l
            }
            return .map(out)
        }
        guard case let .array(et, ak, eopt) = t else { throw Refusal("kind_mismatch: \(fieldName(field))") }
        let wantEK: UInt8 = eopt ? 13 : et.kind
        guard kindOK(wantEK, ek) else { throw Refusal("kind_mismatch: \(fieldName(field))") }
        switch ak {
        case let .fixed(cap): if n > cap { throw Refusal("count_over_bound: \(fieldName(field))") }
        case let .counted(mn, mx):
            if n > mx { throw Refusal("count_over_bound: \(fieldName(field))") }
            if n < mn { throw Refusal("count_over_bound: \(fieldName(field)) below min") }
        case let .unbounded(mn): if n < mn { throw Refusal("count_over_bound: below min") }
        }
        var xs: [Value] = []
        xs.reserveCapacity(n)
        for _ in 0..<n {
            if eopt {
                let l = try len(&p, e)
                xs.append(try decodeOptElem(et, p, p + l, depth: depth + 1))
                p += l
                continue
            }
            switch et {
            case .table, .str:
                let l = try len(&p, e)
                xs.append(try decodeArm(et, ek, p, p + l, depth: depth, field: nil))
                p += l
            case .union:
                xs.append(try decodeUnion(et, &p, e, depth: depth, field: nil))
            default:
                xs.append(try decodeInline(et, ek, &p, e, field: field))
            }
        }
        if case let .fixed(cap) = ak, xs.count < cap {
            for _ in xs.count..<cap { xs.append(eopt ? .absent : typeDefault(et)) }
        }
        return .arr(xs)
    }

    /// A text-list element `?string(M)`: a body holding `value` when present.
    func decodeOptElem(_ et: FT, _ s: Int, _ e: Int, depth: Int) throws -> Value {
        var p = s
        var out: Value = .absent
        while true {
            let r = try leb(&p, e)
            if r == 0 { break }
            let id = ids[Int(r) - 1]
            let k = b[p]; p += 1
            if id == mapValueId { out = try decodeField(et, k, &p, e, depth: depth, field: nil) }
            else { unknown += 1; try skip(k, &p, e) }
        }
        return out
    }

    func decodeEntry(_ kt: FT, _ vt: FT, _ vopt: Bool, _ s: Int, _ e: Int, depth: Int) throws -> (Value, Value) {
        var p = s
        var kv = typeDefault(kt)
        var vv: Value = vopt ? .absent : typeDefault(vt)
        while true {
            let r = try leb(&p, e)
            if r == 0 { break }
            let id = ids[Int(r) - 1]
            let k = b[p]; p += 1
            if id == mapKeyId { kv = try decodeField(kt, k, &p, e, depth: depth, field: nil) }
            else if id == mapValueId { vv = try decodeField(vt, k, &p, e, depth: depth, field: nil) }
            else { unknown += 1; try skip(k, &p, e) }
        }
        return (kv, vv)
    }

    func decodeKeyed(_ t: FT, _ s: Int, _ e: Int, depth: Int, field: Field?) throws -> Value {
        guard case let .keyed(en, et) = t else { fatalError() }
        var p = s
        let ek = b[p]; p += 1
        guard kindOK(et.kind, ek) else { throw Refusal("kind_mismatch: \(fieldName(field))") }
        let n = Int(try leb(&p, e))
        guard case var .keyed(slots) = typeDefault(t) else { fatalError() }
        for _ in 0..<n {
            let r = try leb(&p, e)
            let id = ids[Int(r) - 1]
            let l = try len(&p, e)
            let ss = p
            p += l
            guard let vi = en.wireIds.firstIndex(of: id) else { unknown += 1; continue }
            switch et {
            case .table, .str, .bytes, .array, .map, .keyed:
                slots[vi] = try decodeArm(et, ek, ss, ss + l, depth: depth, field: nil)
            default:
                var q = ss
                slots[vi] = try decodeInline(et, ek, &q, ss + l, field: nil)
            }
        }
        return .keyed(slots)
    }

    // MARK: the loaded graph: no cycle, no record unreached

    func checkGraph(_ root: Rec) throws {
        for k in 0..<nodes.count where !reached.contains(k) { throw damaged("node record no pointer reaches") }
        var state: [ObjectIdentifier: Int] = [:]   // 1 open, 2 done
        func walkRec(_ r: Rec) throws {
            let id = ObjectIdentifier(r)
            if state[id] == 1 { throw damaged("cycle") }
            if state[id] == 2 { return }
            state[id] = 1
            for v in r.vals { try walkVal(v) }
            state[id] = 2
        }
        func walkVal(_ v: Value) throws {
            switch v {
            case let .rec(r): for x in r.vals { try walkVal(x) }
            case let .ptr(r): if let r = r { try walkRec(r) }
            case let .union(_, x): if let x = x { try walkVal(x) }
            case let .arr(xs), let .keyed(xs): for x in xs { try walkVal(x) }
            case let .map(es): for (a, x) in es { try walkVal(a); try walkVal(x) }
            default: break
            }
        }
        try walkRec(root)
    }
}

/// Key order (§22.2.1): signed by value, unsigned by value, string by bytes.
func keyLess(_ a: Value, _ b: Value, _ kt: FT) -> Bool {
    switch (a, b) {
    case let (.int(x), .int(y)): return x < y
    case let (.bytes(x), .bytes(y)): return x.lexicographicallyPrecedes(y)
    default: return false
    }
}

// MARK: writer

final class WireWriter {
    var ids: [UInt64] = []
    var idIndex: [UInt64: Int] = [:]
    var nodeIndex: [ObjectIdentifier: Int] = [:]
    var nodeList: [AnyObject] = []

    func intern(_ id: UInt64) throws -> UInt64 {
        if let k = idIndex[id] { return UInt64(k) }
        guard ids.count < maxIds else { throw Refusal("invalid_value: more than 4096 ids") }
        ids.append(id)
        idIndex[id] = ids.count
        return UInt64(ids.count)
    }

    // §22.3: number every node, depth-first pre-order, refusing a cycle.
    func number(_ root: Rec) throws {
        var open = Set<ObjectIdentifier>([ObjectIdentifier(root)])
        var blobOwners = Set<ObjectIdentifier>()
        func walkRec(_ r: Rec) throws {
            for (i, f) in r.decl.fields.enumerated() {
                if !guardHolds(f, r) { continue }
                if case .absent = r.vals[i] { continue }
                try walkVal(f.ft, r.vals[i])
            }
        }
        func walkVal(_ t: FT, _ v: Value) throws {
            switch (t, v) {
            case let (.table, .rec(r)): try walkRec(r)
            case let (.ptr, .ptr(r)):
                guard let r = r else { return }
                let id = ObjectIdentifier(r)
                if open.contains(id) { throw Refusal("data_cycle") }
                if nodeIndex[id] != nil { return }
                nodeList.append(r)
                nodeIndex[id] = nodeList.count + 1
                open.insert(id)
                try walkRec(r)
                open.remove(id)
            case let (.blob, .blob(bl)):
                guard let bl = bl else { return }
                let id = ObjectIdentifier(bl)
                guard blobOwners.insert(id).inserted else { throw Refusal("blob_shared") }
                nodeList.append(bl)
                nodeIndex[id] = nodeList.count + 1
            case let (.union(un), .union(a, x)):
                if a >= 0, let at = un.arms[a].ft, let x = x { try walkVal(at, x) }
            case let (.array(et, _, _), .arr(xs)):
                for x in xs { if case .absent = x { continue }; try walkVal(et, x) }
            case let (.keyed(_, et), .keyed(xs)):
                for x in xs where !slotIsDefault(et, x) { try walkVal(et, x) }
            case let (.map(kt, vt, _), .map(es)):
                for (k, x) in sortedEntries(es, kt) { _ = k; if case .absent = x { continue }; try walkVal(vt, x) }
            default: break
            }
        }
        try walkRec(root)
    }

    func save(_ root: Rec) throws -> [UInt8] {
        try number(root)
        var out: [UInt8] = [1]
        try writeFields(root, &out)
        if !nodeList.isEmpty {
            putLEB(&out, try intern(reservedNodeTableId))
            out.append(12)
            var nt: [UInt8] = []
            putLEB(&nt, UInt64(nodeList.count))
            for n in nodeList {
                if let r = n as? Rec {
                    putLEB(&nt, try intern(r.decl.wireId))
                    var body: [UInt8] = []
                    try writeFields(r, &body)
                    body.append(0)
                    putLEB(&nt, UInt64(body.count))
                    nt += body
                } else if let bl = n as? Blob {
                    putLEB(&nt, try intern(bl.isString ? blobStringId : blobBytesId))
                    if bl.isString { try checkText(bl.bytes) }
                    putLEB(&nt, UInt64(bl.bytes.count))
                    nt += bl.bytes
                }
            }
            putLEB(&out, UInt64(nt.count))
            out += nt
        }
        out.append(0)
        for id in ids { le(id, &out) }
        le(UInt64(ids.count), &out)
        if root.decl.integrity { le(crc32c(out), &out) }
        return out
    }

    func checkText(_ b: [UInt8]) throws {
        guard utf8WellFormed(b) else { throw Refusal("invalid_value: text not well-formed") }
    }

    /// A body's fields in declaration order (no terminator).
    func writeFields(_ r: Rec, _ out: inout [UInt8]) throws {
        for (i, f) in r.decl.fields.enumerated() {
            if !guardHolds(f, r) { continue }
            let v = r.vals[i]
            try checkValue(f.ft, v, f)
            if fieldElides(f, v) { continue }
            putLEB(&out, try intern(f.wireId))
            try writeKindAndPayload(f.ft, v, &out)
        }
    }

    /// kind byte then the field payload (with its own framing prefix).
    func writeKindAndPayload(_ t: FT, _ v: Value, _ out: inout [UInt8]) throws {
        out.append(t.kind)
        try writePayload(t, v, &out)
    }

    func writePayload(_ t: FT, _ v: Value, _ out: inout [UInt8]) throws {
        switch t {
        case .str:
            guard case let .bytes(bb) = v else { throw Refusal("invalid_value") }
            try checkText(bb)
            putLEB(&out, UInt64(bb.count)); out += bb
        case .table:
            guard case let .rec(r) = v else { throw Refusal("invalid_value") }
            var body: [UInt8] = []
            try writeFields(r, &body); body.append(0)
            putLEB(&out, UInt64(body.count)); out += body
        case .bytes, .array, .map:
            var body: [UInt8] = []
            try writeArrayBody(t, v, &body)
            putLEB(&out, UInt64(body.count)); out += body
        case .keyed:
            var body: [UInt8] = []
            try writeKeyedBody(t, v, &body)
            putLEB(&out, UInt64(body.count)); out += body
        case .union:
            try writeUnion(t, v, &out)
        default:
            try writeInline(t, v, &out)
        }
    }

    func writeInline(_ t: FT, _ v: Value, _ out: inout [UInt8]) throws {
        switch (t, v) {
        case let (.bool, .bool(x)): out.append(x ? 1 : 0)
        case let (.int(w, _, _, _), .int(x)): putRaw(x.bits(w), w / 8, &out)
        case let (.bits(n), .int(x)): putRaw(x.bits(128), kindWidth(FT.bits(n).kind)!, &out)
        case let (.flags, .int(x)): putRaw(x.bits(64), 8, &out)
        case let (.timestamp, .int(x)): putRaw(x.bits(64), 8, &out)
        case let (.fixed(_, i, f, _, _), .int(x)): putRaw(x.bits(i + f), (i + f) / 8, &out)
        case let (.float, .f32(x)): le(x.bitPattern, &out)
        case let (.float(w, lo, hi), .f64(x)):
            let comp = lo != nil || hi != nil
            let y = comp && x == 0 ? 0.0 : x
            if w == 32 { le(Float(y).bitPattern, &out) } else { le(y.bitPattern, &out) }
        case let (.enm(en), .int(x)):
            let k = Int(x.mag)
            if k == 0 { out.append(0) } else { putLEB(&out, try intern(en.wireIds[k - 1])) }
        case let (.ptr, .ptr(r)):
            if let r = r { putLEB(&out, UInt64(nodeIndex[ObjectIdentifier(r)]!)) } else { out.append(0) }
        case let (.blob, .blob(bl)):
            if let bl = bl { putLEB(&out, UInt64(nodeIndex[ObjectIdentifier(bl)]!)) } else { out.append(0) }
        default: throw Refusal("invalid_value: value does not fit its type")
        }
    }

    func putRaw(_ raw: UInt128, _ w: Int, _ out: inout [UInt8]) {
        for i in 0..<w { out.append(UInt8(truncatingIfNeeded: raw >> UInt128(8 * i))) }
    }

    func writeUnion(_ t: FT, _ v: Value, _ out: inout [UInt8]) throws {
        guard case let .union(un) = t, case let .union(a, x) = v else { throw Refusal("invalid_value") }
        if a < 0 { out.append(0); return }
        guard a < un.arms.count else { throw Refusal("invalid_value: union tag") }
        let arm = un.arms[a]
        putLEB(&out, try intern(arm.wireId))
        guard let at = arm.ft else { out.append(32); out.append(0); return }
        out.append(at.kind)
        var pl: [UInt8] = []
        try writeArmPayload(at, x ?? typeDefault(at), &pl)
        putLEB(&out, UInt64(pl.count)); out += pl
    }

    /// The payload a field of this type puts after its own framing prefix.
    func writeArmPayload(_ t: FT, _ v: Value, _ out: inout [UInt8]) throws {
        try checkValue(t, v, nil)
        switch t {
        case .str:
            guard case let .bytes(bb) = v else { throw Refusal("invalid_value") }
            try checkText(bb); out += bb
        case .table:
            guard case let .rec(r) = v else { throw Refusal("invalid_value") }
            try writeFields(r, &out); out.append(0)
        case .bytes, .array, .map: try writeArrayBody(t, v, &out)
        case .keyed: try writeKeyedBody(t, v, &out)
        case .union: try writeUnion(t, v, &out)
        default: try writeInline(t, v, &out)
        }
    }

    func writeArrayBody(_ t: FT, _ v: Value, _ out: inout [UInt8]) throws {
        switch (t, v) {
        case let (.bytes, .bytes(bb)):
            out.append(6); putLEB(&out, UInt64(bb.count)); out += bb
        case let (.map(kt, vt, vopt), .map(es)):
            out.append(13)
            putLEB(&out, UInt64(es.count))
            for (k, x) in sortedEntries(es, kt) {
                var body: [UInt8] = []
                try checkValue(kt, k, nil)
                if !valueElides(kt, k, nil) { putLEB(&body, try intern(mapKeyId)); try writeKindAndPayload(kt, k, &body) }
                if vopt {
                    if case .absent = x {} else { putLEB(&body, try intern(mapValueId)); try writeKindAndPayload(vt, x, &body) }
                } else {
                    try checkValue(vt, x, nil)
                    if !valueElides(vt, x, nil) { putLEB(&body, try intern(mapValueId)); try writeKindAndPayload(vt, x, &body) }
                }
                body.append(0)
                putLEB(&out, UInt64(body.count)); out += body
            }
        case let (.array(et, _, eopt), .arr(xs)):
            out.append(eopt ? 13 : et.kind)
            putLEB(&out, UInt64(xs.count))
            for x in xs {
                if eopt {
                    var body: [UInt8] = []
                    if case .absent = x {} else { putLEB(&body, try intern(mapValueId)); try writeKindAndPayload(et, x, &body) }
                    body.append(0)
                    putLEB(&out, UInt64(body.count)); out += body
                    continue
                }
                try checkValue(et, x, nil)
                switch et {
                case .str, .table, .bytes, .array, .map, .keyed:
                    var body: [UInt8] = []
                    try writeArmPayload(et, x, &body)
                    putLEB(&out, UInt64(body.count)); out += body
                case .union: try writeUnion(et, x, &out)
                default: try writeInline(et, x, &out)
                }
            }
        default: throw Refusal("invalid_value: array shape")
        }
    }

    func writeKeyedBody(_ t: FT, _ v: Value, _ out: inout [UInt8]) throws {
        guard case let .keyed(en, et) = t, case let .keyed(xs) = v else { throw Refusal("invalid_value") }
        out.append(et.kind)
        let present = xs.indices.filter { !slotIsDefault(et, xs[$0]) }
        putLEB(&out, UInt64(present.count))
        for i in present {
            putLEB(&out, try intern(en.wireIds[i]))
            var pl: [UInt8] = []
            try writeArmPayload(et, xs[i], &pl)
            putLEB(&out, UInt64(pl.count)); out += pl
        }
    }

    /// The write contract (SPEC.md §5.1): a value outside its field's domain
    /// is refused, never clamped.
    func checkValue(_ t: FT, _ v: Value, _ f: Field?) throws {
        try writeCheck(t, v, f)
    }
}

func sortedEntries(_ es: [(Value, Value)], _ kt: FT) -> [(Value, Value)] {
    es.sorted { keyLess($0.0, $1.0, kt) }
}

/// Every write check, shared by the wire and text writers (SPEC.md §5.1).
func writeCheck(_ t: FT, _ v: Value, _ f: Field?) throws {
    let name = f?.name ?? "element"
    switch (t, v) {
    case (_, .absent): return
    case let (.int(w, s, lo, hi), .int(x)):
        if x < Big.minOf(width: w, signed: s) || Big.maxOf(width: w, signed: s) < x { throw Refusal("invalid_value: \(name)") }
        if let lo = lo, x < lo { throw Refusal("invalid_value: \(name)") }
        if let hi = hi, hi < x { throw Refusal("invalid_value: \(name)") }
    case let (.bits(n), .int(x)):
        if x.neg || (n < 128 && x.mag >= UInt128(1) << UInt128(n)) { throw Refusal("invalid_value: \(name)") }
    case let (.fixed(s, i, fr, lo, hi), .int(x)):
        if x < Big.minOf(width: i + fr, signed: s) || Big.maxOf(width: i + fr, signed: s) < x { throw Refusal("invalid_value: \(name)") }
        if let lo = lo, x < lo { throw Refusal("invalid_value: \(name)") }
        if let hi = hi, hi < x { throw Refusal("invalid_value: \(name)") }
    case let (.enm(en), .int(x)):
        if x.neg || x.mag > UInt128(en.max) { throw Refusal("invalid_value: \(name)") }
        if x.mag > 0 && en.deprecated[Int(x.mag) - 1] { throw Refusal("invalid_value: deprecated variant") }
    case let (.flags(fl), .int(x)):
        _ = fl
        if x.neg || x.mag > UInt128(UInt64.max) { throw Refusal("invalid_value: \(name)") }
    case let (.float(w, lo, hi), _):
        var d: Double = 0
        if case let .f32(x) = v { d = Double(x) } else if case let .f64(x) = v { d = x }
        if !d.isFinite { throw Refusal("non_finite_float: \(name)") }
        if w == 32 && !Float(d).isFinite { throw Refusal("non_finite_float: \(name)") }
        if let lo = lo, d < lo { throw Refusal("invalid_value: \(name)") }
        if let hi = hi, d > hi { throw Refusal("invalid_value: \(name)") }
    case let (.str(n), .bytes(bb)):
        if let n = n, bb.count > n { throw Refusal("invalid_value: \(name) length") }
        guard utf8WellFormed(bb) else { throw Refusal("invalid_value: \(name) not text") }
    case let (.bytes(n), .bytes(bb)):
        if bb.count > n { throw Refusal("invalid_value: \(name) length") }
    case let (.array(_, k, _), .arr(xs)):
        switch k {
        case let .fixed(n): if xs.count != n { throw Refusal("invalid_value: \(name) count") }
        case let .counted(mn, mx): if xs.count < mn || xs.count > mx { throw Refusal("invalid_value: \(name) count") }
        case let .unbounded(mn): if xs.count < mn { throw Refusal("invalid_value: \(name) count") }
        }
        if let tot = f?.total {
            var sum = 0
            for x in xs { if case let .bytes(bb) = x { sum += bb.count } }
            if sum > tot { throw Refusal("invalid_value: \(name) total") }
        }
    case let (.union(un), .union(a, _)):
        if a >= un.arms.count { throw Refusal("invalid_value: \(name) tag") }
    default: return
    }
}

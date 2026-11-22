// Mutate.swift — the write-refusal rows' one-field edit
// (testdata/conformance/tables/FORMAT.md, `write-refusal`).

struct Unrepresentable: Error {}

enum Seg { case name(String), index(Int) }

func parsePath(_ p: String) -> [Seg] {
    var out: [Seg] = []
    for part in p.split(separator: ".") {
        var s = Substring(part)
        if let b = s.firstIndex(of: "[") {
            out.append(.name(String(s[..<b])))
            s = s[b...]
            while s.hasPrefix("[") {
                let e = s.firstIndex(of: "]")!
                out.append(.index(Int(s[s.index(after: s.startIndex)..<e])!))
                s = s[s.index(after: e)...]
            }
        } else { out.append(.name(String(s))) }
    }
    return out
}

func hexBytes(_ s: String) -> [UInt8] {
    var out: [UInt8] = []
    var it = s.makeIterator()
    while let a = it.next(), let b = it.next() { out.append(UInt8(String([a, b]), radix: 16)!) }
    return out
}

/// UTF-16 units to bytes; a lone surrogate becomes its 3-byte generalized
/// UTF-8 spelling, the nearest storage this port has that is not text.
// SPEC-GAP 7
func unitsToBytes(_ s: String) -> [UInt8] {
    var units: [UInt32] = []
    var i = s.startIndex
    while i < s.endIndex { let e = s.index(i, offsetBy: 4); units.append(UInt32(s[i..<e], radix: 16)!); i = e }
    var out: [UInt8] = []
    var k = 0
    while k < units.count {
        var u = units[k]
        if u >= 0xD800 && u <= 0xDBFF && k + 1 < units.count && units[k + 1] >= 0xDC00 && units[k + 1] <= 0xDFFF {
            u = 0x10000 + ((u - 0xD800) << 10) + (units[k + 1] - 0xDC00); k += 1
        }
        if u < 0x80 { out.append(UInt8(u)) }
        else if u < 0x800 { out += [UInt8(0xC0 | u >> 6), UInt8(0x80 | u & 63)] }
        else if u < 0x10000 { out += [UInt8(0xE0 | u >> 12), UInt8(0x80 | u >> 6 & 63), UInt8(0x80 | u & 63)] }
        else { out += [UInt8(0xF0 | u >> 18), UInt8(0x80 | u >> 12 & 63), UInt8(0x80 | u >> 6 & 63), UInt8(0x80 | u & 63)] }
        k += 1
    }
    return out
}

func applyEdit(_ root: Rec, path: String, kind: String, value: String) throws {
    let segs = parsePath(path)
    guard case let .name(first) = segs[0], let fi = root.decl.fields.firstIndex(where: { $0.name == first }) else { throw Refusal("bad path") }
    root.vals[fi] = try edit(root.vals[fi], root.decl.fields[fi].ft, Array(segs.dropFirst()), kind, value)
}

func edit(_ v: Value, _ t: FT, _ segs: [Seg], _ kind: String, _ value: String) throws -> Value {
    if segs.isEmpty { return try leaf(v, t, kind, value) }
    switch (segs[0], t, v) {
    case let (.name(n), .table(d), .rec(r)):
        guard let fi = d.fields.firstIndex(where: { $0.name == n }) else { throw Refusal("bad path \(n)") }
        r.vals[fi] = try edit(r.vals[fi], d.fields[fi].ft, Array(segs.dropFirst()), kind, value)
        return .rec(r)
    case let (.name(n), .union(un), .union(a, x)):
        guard let ai = un.arms.firstIndex(where: { $0.name == n }), let at = un.arms[ai].ft else { throw Refusal("bad arm \(n)") }
        let cur = (a == ai ? x : nil) ?? typeDefault(at)
        return .union(ai, try edit(cur, at, Array(segs.dropFirst()), kind, value))
    case let (.index(i), .array(et, _, _), .arr(xs)):
        var ys = xs
        ys[i] = try edit(ys[i], et, Array(segs.dropFirst()), kind, value)
        return .arr(ys)
    case let (.index(i), .keyed(_, et), .keyed(xs)):
        var ys = xs
        ys[i] = try edit(ys[i], et, Array(segs.dropFirst()), kind, value)
        return .keyed(ys)
    case let (.index(i), .map(kt, vt, _), .map(es)):
        var ys = sortedEntries(es, kt)
        guard segs.count >= 2, case let .name(which) = segs[1] else { throw Refusal("map path needs key or value") }
        if which == "key" { ys[i].0 = try edit(ys[i].0, kt, Array(segs.dropFirst(2)), kind, value) }
        else { ys[i].1 = try edit(ys[i].1, vt, Array(segs.dropFirst(2)), kind, value) }
        return .map(ys)
    case let (.index(i), .bytes, .bytes(bb)):
        var ys = bb
        ys[i] = UInt8(value)!
        return .bytes(ys)
    default:
        throw Refusal("path does not fit")
    }
}

func leaf(_ v: Value, _ t: FT, _ kind: String, _ value: String) throws -> Value {
    switch kind {
    case "int", "ordinal":
        let neg = value.hasPrefix("-")
        guard let m = Big.parseDecimal(Substring(neg ? String(value.dropFirst()) : value)) else { throw Unrepresentable() }
        return .int(Big(neg: neg, mag: m.mag))
    case "count":
        guard let n = Int(value), n >= 0, case let .arr(xs) = v else { throw Unrepresentable() }
        var ys = xs
        if n < ys.count { ys.removeLast(ys.count - n) }
        else if n > ys.count {
            guard case let .array(et, _, _) = t else { throw Unrepresentable() }
            let proto = ys.first ?? typeDefault(et)
            while ys.count < n { ys.append(copyValue(proto)) }
        }
        return .arr(ys)
    case "length":
        guard let n = Int(value), n >= 0 else { throw Unrepresentable() }
        return .bytes([UInt8](repeating: 0x61, count: n))
    case "tag":
        guard let k = Int(value), k >= 0, case let .union(un) = t else { throw Unrepresentable() }
        if k == 0 { return .union(-1, nil) }
        if k - 1 < un.arms.count { return .union(k - 1, un.arms[k - 1].ft.map { typeDefault($0) }) }
        return .union(k - 1, nil)
    case "float":
        let d: Double
        switch value {
        case "nan": d = .nan
        case "inf": d = .infinity
        case "-inf": d = -.infinity
        default: guard let x = Double(value) else { throw Unrepresentable() }; d = x
        }
        if case let .float(w, _, _) = t, w == 32 { return .f64(d) } // held wider; the writer rounds to float32 (§16.3)
        return .f64(d)
    case "bytes": return .bytes(hexBytes(value))
    case "units": return .bytes(unitsToBytes(value))
    default: throw Refusal("unknown edit kind \(kind)")
    }
}

func copyValue(_ v: Value) -> Value {
    switch v {
    case let .rec(r): return .rec(Rec(r.decl, r.vals.map(copyValue)))
    case let .arr(xs): return .arr(xs.map(copyValue))
    case let .keyed(xs): return .keyed(xs.map(copyValue))
    case let .map(es): return .map(es.map { (copyValue($0.0), copyValue($0.1)) })
    case let .union(a, x): return .union(a, x.map(copyValue))
    default: return v
    }
}

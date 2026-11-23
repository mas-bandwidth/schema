// Value.swift — one dynamic value model for every table of a unit, built
// from the declarations (SPEC.md §4.2 zero initialization and specified
// defaults; SPEC-TABLES.md §2).

final class Rec {
    let decl: TableDecl
    var vals: [Value]
    init(_ d: TableDecl, _ v: [Value]) { decl = d; vals = v }
}
final class Blob {
    var bytes: [UInt8]
    let isString: Bool
    init(_ b: [UInt8], isString: Bool) { bytes = b; self.isString = isString }
}

indirect enum Value {
    case bool(Bool)
    case int(Big)            // integers, bits, enum ordinal, flags mask, fixed raw, timestamp
    case f32(Float)
    case f64(Double)
    case bytes([UInt8])      // string(N), string, bytes(N)
    case rec(Rec)            // a table or type held by value
    case union(Int, Value?)  // arm index, -1 = None; payload nil for a payload-free arm
    case ptr(Rec?)
    case blob(Blob?)
    case arr([Value])
    case keyed([Value])      // slot k-1 for ordinal k
    case map([(Value, Value)])
    case absent              // an absent optional (field, list element or map value)
}

let reservedNodeTableId: UInt64 = 0xFFFFFFFFFFFFFFFF
let reservedBuildVersionId: UInt64 = 0xFFFFFFFFFFFFFFFE
let reservedVocabularyId: UInt64 = 0xFFFFFFFFFFFFFFFD
let mapKeyId = fnv1a64("key")
let mapValueId = fnv1a64("value")
let blobBytesId = fnv1a64("bytes")
let blobStringId = fnv1a64("string")

func newRec(_ d: TableDecl) -> Rec {
    Rec(d, d.fields.map { fieldDefault($0) })
}

func fieldDefault(_ f: Field) -> Value {
    if f.optional { return .absent }
    if let d = f.def { return d }
    return typeDefault(f.ft)
}

func typeDefault(_ t: FT) -> Value {
    switch t {
    case .bool: return .bool(false)
    case .int, .bits, .fixed, .timestamp, .enm, .flags: return .int(.zero)
    case let .float(w, _, _): return w == 32 ? .f32(0) : .f64(0)
    case .str, .bytes: return .bytes([])
    case let .table(d): return .rec(newRec(d))
    case .union: return .union(-1, nil)
    case .ptr: return .ptr(nil)
    case .blob: return .blob(nil)
    case let .array(e, k, eopt):
        switch k {
        case let .fixed(n): return .arr((0..<n).map { _ in eopt ? .absent : typeDefault(e) })
        case let .counted(mn, _): return .arr((0..<mn).map { _ in eopt ? .absent : typeDefault(e) })
        case let .unbounded(mn): return .arr((0..<mn).map { _ in eopt ? .absent : typeDefault(e) })
        }
    case let .keyed(en, e): return .keyed((0..<en.max).map { _ in typeDefault(e) })
    case .map: return .map([])
    }
}

/// The value a specified default spells (SPEC.md §4.2).
func defaultValue(_ u: Unit, _ t: FT, _ d: DefaultAST) throws -> Value {
    switch (t, d) {
    case (.bool, .expr(.name(let n))): return .bool(n == "true")
    case (.enm(let en), .expr(.name(let n))):
        guard let k = en.variants.firstIndex(of: n) else { throw Refusal("default \(n) is no variant") }
        return .int(Big(k + 1))
    case (.flags(let fl), .list(let names)):
        var m: UInt128 = 0
        for n in names { guard let k = fl.variants.firstIndex(of: n) else { throw Refusal("bad flags default") }; m |= UInt128(1) << UInt128(k) }
        return .int(Big(m))
    case (.str, .str(let b)), (.bytes, .str(let b)): return .bytes(b)
    case (.float(let w, _, _), .expr(let e)):
        let v = try u.evalFloat(e)
        return w == 32 ? .f32(Float(v)) : .f64(v)
    case (.fixed(_, _, let f, _, _), .expr(let e)):
        if let b = try? u.evalInt(e) { return .int(try b.shl(f)) }
        let v = try u.evalFloat(e)
        let raw = v * Double(sign: .plus, exponent: f, significand: 1)
        guard raw == raw.rounded() else { throw Refusal("fixed default not exact") }
        return .int(Big(neg: raw < 0, mag: UInt128(raw.magnitude)))
    case (.int, .expr(let e)), (.bits, .expr(let e)), (.timestamp, .expr(let e)): return .int(try u.evalInt(e))
    default: throw Refusal("default does not fit its field")
    }
}

// MARK: equality used by elision

func scalarEq(_ a: Value, _ b: Value, _ t: FT) -> Bool {
    switch (a, b) {
    case let (.bool(x), .bool(y)): return x == y
    case let (.int(x), .int(y)): return x == y
    case let (.f32(x), .f32(y)):
        if case let .float(_, lo, hi) = t, lo != nil || hi != nil { return x == y }
        return x.bitPattern == y.bitPattern
    case let (.f64(x), .f64(y)):
        if case let .float(_, lo, hi) = t, lo != nil || hi != nil { return x == y }
        return x.bitPattern == y.bitPattern
    case let (.bytes(x), .bytes(y)): return x == y
    default: return false
    }
}

func isCompressed(_ t: FT) -> Bool { if case let .float(_, lo, hi) = t { return lo != nil || hi != nil }; return false }

/// A field's guard holds (SPEC.md §4.4; SPEC-TABLES.md §3).
func guardHolds(_ f: Field, _ r: Rec) -> Bool {
    for (gi, neg) in f.guardIdx {
        var v = false
        if case let .bool(b) = r.vals[gi] { v = b }
        if v == neg { return false }
    }
    return true
}

/// A table body whose every field elides (§22.2): written as `L = 1` and
/// its terminator, so a by-value nesting of it elides.
func bodyIsEmpty(_ r: Rec) -> Bool {
    for (i, f) in r.decl.fields.enumerated() {
        if !guardHolds(f, r) { continue }
        if !fieldElides(f, r.vals[i]) { return false }
    }
    return true
}

func fieldElides(_ f: Field, _ v: Value) -> Bool {
    if f.optional { if case .absent = v { return true }; return false }
    if f.required { return false }
    return valueElides(f.ft, v, f.def)
}

/// §22.2's elision table for a non-optional, non-required slot.
func valueElides(_ t: FT, _ v: Value, _ def: Value?) -> Bool {
    switch t {
    case .bool, .int, .bits, .float, .fixed, .timestamp, .enm, .flags, .str, .bytes:
        return scalarEq(v, def ?? typeDefault(t), t)
    case .table:
        if case let .rec(r) = v { return bodyIsEmpty(r) }
        return true
    case .union: if case let .union(a, _) = v { return a < 0 }; return true
    case .ptr: if case let .ptr(r) = v { return r == nil }; return true
    case .blob: if case let .blob(b) = v { return b == nil }; return true
    case let .array(e, k, eopt):
        guard case let .arr(xs) = v else { return true }
        switch k {
        case .fixed:
            if e.kind == 13 && !eopt { return false }
            return xs.allSatisfy { elemIsDefault(e, $0, eopt) }
        case let .counted(mn, _):
            if mn == 0 { return xs.isEmpty }
            return xs.count == mn && xs.allSatisfy { elemIsDefault(e, $0, eopt) }
        case let .unbounded(mn):
            if mn == 0 { return xs.isEmpty }
            return xs.count == mn && xs.allSatisfy { elemIsDefault(e, $0, eopt) }
        }
    case let .keyed(_, e):
        guard case let .keyed(xs) = v else { return true }
        return xs.allSatisfy { slotIsDefault(e, $0) }
    case .map:
        if case let .map(es) = v { return es.isEmpty }
        return true
    }
}

func elemIsDefault(_ e: FT, _ v: Value, _ eopt: Bool) -> Bool {
    if eopt { if case .absent = v { return true }; return false }
    switch e {
    case .table: if case let .rec(r) = v { return bodyIsEmpty(r) }; return true
    case .union: if case let .union(a, _) = v { return a < 0 }; return true
    case .ptr: if case let .ptr(r) = v { return r == nil }; return true
    case .blob: if case let .blob(b) = v { return b == nil }; return true
    default: return scalarEq(v, typeDefault(e), e)
    }
}

func slotIsDefault(_ e: FT, _ v: Value) -> Bool { elemIsDefault(e, v, false) }

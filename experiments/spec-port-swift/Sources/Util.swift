// Util.swift — the small exact pieces every form needs: a signed 129-bit
// integer for constants and values, LEB128, FNV-1a 64, CRC-32C, UTF-8.
// Written from docs/SPEC.md and docs/SPEC-TABLES.md alone.

struct Refusal: Error, CustomStringConvertible {
    let reason: String
    init(_ r: String) { reason = r }
    var description: String { reason }
}

/// Big: sign and a 128-bit magnitude. Enough for every value a schema
/// declares (int128 and uint128 both fit) and for constant arithmetic,
/// which refuses on overflow rather than trapping.
struct Big: Equatable, Hashable, Comparable, CustomStringConvertible {
    var neg: Bool
    var mag: UInt128
    init(neg: Bool, mag: UInt128) { self.neg = mag == 0 ? false : neg; self.mag = mag }
    init(_ v: Int) { self.init(neg: v < 0, mag: UInt128(v.magnitude)) }
    init(_ v: Int128) { self.init(neg: v < 0, mag: v.magnitude) }
    init(_ v: UInt128) { self.init(neg: false, mag: v) }
    init(_ v: UInt64) { self.init(neg: false, mag: UInt128(v)) }
    static let zero = Big(neg: false, mag: 0)
    var isZero: Bool { mag == 0 }

    static func < (a: Big, b: Big) -> Bool {
        if a.neg != b.neg { return a.neg }
        return a.neg ? a.mag > b.mag : a.mag < b.mag
    }
    static prefix func - (a: Big) -> Big { Big(neg: !a.neg, mag: a.mag) }

    static func add(_ a: Big, _ b: Big) throws -> Big {
        if a.neg == b.neg {
            let (s, o) = a.mag.addingReportingOverflow(b.mag)
            if o { throw Refusal("constant overflow") }
            return Big(neg: a.neg, mag: s)
        }
        if a.mag >= b.mag { return Big(neg: a.neg, mag: a.mag - b.mag) }
        return Big(neg: b.neg, mag: b.mag - a.mag)
    }
    static func sub(_ a: Big, _ b: Big) throws -> Big { try add(a, -b) }
    static func mul(_ a: Big, _ b: Big) throws -> Big {
        let (p, o) = a.mag.multipliedReportingOverflow(by: b.mag)
        if o { throw Refusal("constant overflow") }
        return Big(neg: a.neg != b.neg, mag: p)
    }
    static func div(_ a: Big, _ b: Big) throws -> Big {
        if b.mag == 0 { throw Refusal("division by zero") }
        return Big(neg: a.neg != b.neg, mag: a.mag / b.mag) // truncates toward zero
    }
    static func rem(_ a: Big, _ b: Big) throws -> Big {
        if b.mag == 0 { throw Refusal("division by zero") }
        return Big(neg: a.neg, mag: a.mag % b.mag)
    }
    func shl(_ n: Int) throws -> Big {
        if n == 0 || mag == 0 { return self }
        if mag.leadingZeroBitCount < n { throw Refusal("constant overflow") }
        return Big(neg: neg, mag: mag << UInt128(n))
    }

    var description: String { (neg ? "-" : "") + String(mag) }

    /// Two's complement bits at a width (8..128), low bits.
    func bits(_ width: Int) -> UInt128 {
        var b: UInt128 = neg ? (0 &- mag) : mag
        if width < 128 { b &= (UInt128(1) << UInt128(width)) - 1 }
        return b
    }
    /// Read `width` bits as signed or unsigned.
    static func fromBits(_ raw: UInt128, width: Int, signed: Bool) -> Big {
        var b = raw
        if width < 128 { b &= (UInt128(1) << UInt128(width)) - 1 }
        if signed {
            let top = UInt128(1) << UInt128(width - 1)
            if b & top != 0 {
                // negative: magnitude = 2^width - b
                let m: UInt128 = width == 128 ? (0 &- b) : ((UInt128(1) << UInt128(width)) - b)
                return Big(neg: true, mag: m)
            }
        }
        return Big(neg: false, mag: b)
    }
    static func minOf(width: Int, signed: Bool) -> Big {
        signed ? Big(neg: true, mag: UInt128(1) << UInt128(width - 1)) : .zero
    }
    static func maxOf(width: Int, signed: Bool) -> Big {
        if signed { return Big(neg: false, mag: (UInt128(1) << UInt128(width - 1)) - 1) }
        return Big(neg: false, mag: width == 128 ? UInt128.max : (UInt128(1) << UInt128(width)) - 1)
    }
    /// Parse decimal digits (no sign).
    static func parseDecimal(_ s: Substring) -> Big? {
        var m: UInt128 = 0
        if s.isEmpty { return nil }
        for c in s.utf8 {
            guard c >= 48 && c <= 57 else { return nil }
            let (a, o1) = m.multipliedReportingOverflow(by: 10)
            if o1 { return nil }
            let (b, o2) = a.addingReportingOverflow(UInt128(c - 48))
            if o2 { return nil }
            m = b
        }
        return Big(neg: false, mag: m)
    }
}

// MARK: LEB128

@inline(__always) func putLEB(_ out: inout [UInt8], _ v: UInt64) {
    var v = v
    while v >= 0x80 { out.append(UInt8(v & 0x7f) | 0x80); v >>= 7 }
    out.append(UInt8(v))
}
func lebLen(_ v: UInt64) -> Int { var n = 1; var v = v; while v >= 0x80 { v >>= 7; n += 1 }; return n }

// MARK: FNV-1a 64

func fnv1a64(_ s: String) -> UInt64 {
    var h: UInt64 = 0xcbf29ce484222325
    for b in s.utf8 { h ^= UInt64(b); h = h &* 0x100000001b3 }
    return h
}

// MARK: CRC-32C (Castagnoli, reflected 0x82F63B78, init and xorout 0xFFFFFFFF)

let crcTable: [UInt32] = {
    var t = [UInt32](repeating: 0, count: 256)
    for i in 0..<256 {
        var c = UInt32(i)
        for _ in 0..<8 { c = (c & 1) != 0 ? (0x82F63B78 ^ (c >> 1)) : (c >> 1) }
        t[i] = c
    }
    return t
}()
func crc32c<C: Collection>(_ d: C) -> UInt32 where C.Element == UInt8 {
    var c: UInt32 = 0xFFFFFFFF
    for b in d { c = crcTable[Int((c ^ UInt32(b)) & 0xff)] ^ (c >> 8) }
    return c ^ 0xFFFFFFFF
}

// MARK: UTF-8 (well-formed per Unicode Table 3-7; no surrogates, no overlongs)

func utf8WellFormed<C: Collection>(_ b: C, allowZero: Bool = false) -> Bool where C.Element == UInt8, C.Index == Int {
    var i = b.startIndex
    let e = b.endIndex
    while i < e {
        let c = b[i]
        if c < 0x80 {
            if c == 0 && !allowZero { return false }
            i += 1; continue
        }
        var need = 0
        var lo: UInt8 = 0x80, hi: UInt8 = 0xBF
        switch c {
        case 0xC2...0xDF: need = 1
        case 0xE0: need = 2; lo = 0xA0
        case 0xE1...0xEC, 0xEE...0xEF: need = 2
        case 0xED: need = 2; hi = 0x9F
        case 0xF0: need = 3; lo = 0x90
        case 0xF1...0xF3: need = 3
        case 0xF4: need = 3; hi = 0x8F
        default: return false
        }
        if i + need >= e { return false }
        let c1 = b[i + 1]
        if c1 < lo || c1 > hi { return false }
        var k = 2
        while k <= need {
            let ck = b[i + k]
            if ck < 0x80 || ck > 0xBF { return false }
            k += 1
        }
        i += need + 1
    }
    return true
}

func le<T: FixedWidthInteger>(_ v: T, _ out: inout [UInt8]) {
    var x = v.littleEndian
    withUnsafeBytes(of: &x) { out.append(contentsOf: $0) }
}

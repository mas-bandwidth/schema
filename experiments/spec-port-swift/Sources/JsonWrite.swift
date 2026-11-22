// JsonWrite.swift — the canonical text writer, docs/SPEC-TABLES.md §16.14
// W0–W14, step by step.

import Foundation

final class JsonWriter {
    var out: [UInt8] = []
    var namings: [ObjectIdentifier: Int] = [:]
    var labels: [ObjectIdentifier: Int] = [:]
    var nextLabel = 1

    func write(_ root: Rec) throws -> [UInt8] {
        try graphPass(root)
        if root.decl.jsonArray {
            try value(root.decl.fields[0].ft, root.vals[0], 0, root.decl.fields[0])
        } else {
            try table(root, 0, isRootOrValue: true)
        }
        out.append(0x0A)
        return out
    }

    // W1: count namings, refuse a cycle and a shared blob.
    func graphPass(_ root: Rec) throws {
        var open = Set<ObjectIdentifier>([ObjectIdentifier(root)])
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
                namings[id, default: 0] += 1
                if namings[id]! > 1 { return }
                open.insert(id)
                try walkRec(r)
                open.remove(id)
            case let (.blob, .blob(b)):
                guard let b = b else { return }
                let id = ObjectIdentifier(b)
                namings[id, default: 0] += 1
                if namings[id]! > 1 { throw Refusal("blob_shared") }
            case let (.union(un), .union(a, x)):
                if a >= 0, a < un.arms.count, let at = un.arms[a].ft, let x = x { try walkVal(at, x) }
            case let (.array(et, _, _), .arr(xs)):
                for x in xs { if case .absent = x { continue }; try walkVal(et, x) }
            case let (.keyed(_, et), .keyed(xs)):
                for x in xs { try walkVal(et, x) }
            case let (.map(kt, vt, _), .map(es)):
                for (_, x) in sortedEntries(es, kt) { if case .absent = x { continue }; try walkVal(vt, x) }
            default: break
            }
        }
        try walkRec(root)
    }

    func nl(_ d: Int) { out.append(0x0A); for _ in 0..<(2 * d) { out.append(0x20) } }
    func raw(_ s: String) { out += Array(s.utf8) }

    // W3
    func table(_ r: Rec, _ d: Int, isRootOrValue: Bool, label: Int? = nil) throws {
        if d > maxDepth { throw Refusal("json depth past 128") }
        if r.decl.jsonArray && label == nil {
            try value(r.decl.fields[0].ft, r.vals[0], d, r.decl.fields[0])
            return
        }
        var keep: [Int] = []
        for (i, f) in r.decl.fields.enumerated() {
            if !guardHolds(f, r) { continue }
            if case .absent = r.vals[i] { continue }
            try writeCheck(f.ft, r.vals[i], f)
            keep.append(i)
        }
        if let l = label {
            if keep.isEmpty { throw Refusal("a &node definition holding no field") }
            out.append(0x7B)
            nl(d + 1); raw("\"&node\": \(l)")
            for i in keep {
                out.append(0x2C); nl(d + 1)
                str(Array(r.decl.fields[i].jsonKey.utf8)); raw(": ")
                try value(r.decl.fields[i].ft, r.vals[i], d + 1, r.decl.fields[i])
            }
            nl(d); out.append(0x7D)
            return
        }
        if keep.isEmpty { raw("{}"); return }
        out.append(0x7B)
        for (n, i) in keep.enumerated() {
            if n > 0 { out.append(0x2C) }
            nl(d + 1)
            str(Array(r.decl.fields[i].jsonKey.utf8)); raw(": ")
            try value(r.decl.fields[i].ft, r.vals[i], d + 1, r.decl.fields[i])
        }
        nl(d); out.append(0x7D)
    }

    func value(_ t: FT, _ v: Value, _ d: Int, _ f: Field?) throws {
        try writeCheck(t, v, f)
        switch (t, v) {
        case let (.bool, .bool(x)): raw(x ? "true" : "false")
        case let (.int(w, _, _, _), .int(x)):
            if w > 32 { raw("\"\(x)\"") } else { raw(x.description) }
        case let (.bits(n), .int(x)):
            if n > 32 { raw("\"\(x)\"") } else { raw(x.description) }
        case let (.fixed(_, i, fr, _, _), .int(x)):
            let s = fixedText(x, fr)
            if i + fr > 32 { raw("\"\(s)\"") } else { raw(s) }
        case let (.timestamp, .int(x)): raw("\"" + timestampText(x) + "\"")
        case let (.enm(en), .int(x)):
            let k = Int(x.mag)
            if k == 0 { raw("\"None\"") } else { str(Array(en.variants[k - 1].utf8)) }
        case let (.flags(fl), .int(x)):
            var names: [String] = []
            var m = x.mag
            var bit = 0
            while m != 0 {
                if m & 1 != 0 {
                    guard bit < fl.variants.count else { throw Refusal("invalid_value: flags bit no variant names") }
                    names.append(fl.variants[bit])
                }
                m >>= 1; bit += 1
            }
            if names.isEmpty { raw("[]"); return }
            out.append(0x5B)
            for (n, s) in names.enumerated() { if n > 0 { out.append(0x2C) }; nl(d + 1); str(Array(s.utf8)) }
            nl(d); out.append(0x5D)
        case let (.float(w, _, _), .f32(x)): raw(try floatText(Double(x), w == 32 ? 32 : 64))
        case let (.float(w, _, _), .f64(x)): raw(try floatText(x, w))
        case let (.str, .bytes(bb)): try strChecked(bb)
        case let (.bytes, .bytes(bb)): raw("\"" + base64(bb) + "\"")
        case let (.table, .rec(r)): try table(r, d, isRootOrValue: true)
        case let (.union(un), .union(a, x)):
            if a < 0 { raw("{}"); return }
            let arm = un.arms[a]
            out.append(0x7B); nl(d + 1)
            str(Array(arm.name.utf8)); raw(": ")
            if let at = arm.ft { try value(at, x ?? typeDefault(at), d + 1, nil) } else { raw("null") }
            nl(d); out.append(0x7D)
        case let (.ptr, .ptr(r)):
            guard let r = r else { raw("null"); return }
            let id = ObjectIdentifier(r)
            if (namings[id] ?? 1) > 1 {
                if let l = labels[id] {
                    if d > maxDepth { throw Refusal("json depth past 128") }
                    out.append(0x7B); nl(d + 1); raw("\"&node\": \(l)"); nl(d); out.append(0x7D)
                    return
                }
                let l = nextLabel; nextLabel += 1
                labels[id] = l
                try table(r, d, isRootOrValue: false, label: l)
            } else {
                try table(r, d, isRootOrValue: false)
            }
        case let (.blob, .blob(b)):
            guard let b = b else { raw("null"); return }
            if b.isString { try strChecked(b.bytes) } else { raw("\"" + base64(b.bytes) + "\"") }
        case let (.array(et, _, eopt), .arr(xs)):
            if xs.isEmpty { raw("[]"); return }
            out.append(0x5B)
            for (n, x) in xs.enumerated() {
                if n > 0 { out.append(0x2C) }
                nl(d + 1)
                if eopt, case .absent = x { raw("null") } else { try value(et, x, d + 1, nil) }
            }
            nl(d); out.append(0x5D)
        case let (.keyed(en, et), .keyed(xs)):
            if xs.isEmpty { raw("{}"); return }
            out.append(0x7B)
            for (n, x) in xs.enumerated() {
                if n > 0 { out.append(0x2C) }
                nl(d + 1); str(Array(en.variants[n].utf8)); raw(": ")
                try value(et, x, d + 1, nil)
            }
            nl(d); out.append(0x7D)
        case let (.map(kt, vt, vopt), .map(es)):
            if es.isEmpty { raw("{}"); return }
            out.append(0x7B)
            for (n, (k, x)) in sortedEntries(es, kt).enumerated() {
                if n > 0 { out.append(0x2C) }
                nl(d + 1)
                switch k {
                case let .int(b): raw("\"\(b)\"")
                case let .bytes(bb): try strChecked(bb)
                default: throw Refusal("invalid_value: map key")
                }
                raw(": ")
                if vopt, case .absent = x { raw("null") } else { try value(vt, x, d + 1, nil) }
            }
            nl(d); out.append(0x7D)
        default:
            throw Refusal("invalid_value: value does not fit its type")
        }
    }

    // W8
    func strChecked(_ b: [UInt8]) throws {
        guard utf8WellFormed(b) else { throw Refusal("invalid_value: storage that is not text") }
        str(b)
    }
    func str(_ b: [UInt8]) {
        out.append(0x22)
        let hex: [UInt8] = Array("0123456789abcdef".utf8)
        for c in b {
            switch c {
            case 0x22: out += [0x5C, 0x22]
            case 0x5C: out += [0x5C, 0x5C]
            case 0x08: out += [0x5C, 0x62]
            case 0x0C: out += [0x5C, 0x66]
            case 0x0A: out += [0x5C, 0x6E]
            case 0x0D: out += [0x5C, 0x72]
            case 0x09: out += [0x5C, 0x74]
            case 0x00..<0x20: out += [0x5C, 0x75, 0x30, 0x30, hex[Int(c >> 4)], hex[Int(c & 15)]]
            default: out.append(c)
            }
        }
        out.append(0x22)
    }
}

// W9
let b64: [UInt8] = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/".utf8)
func base64(_ d: [UInt8]) -> String {
    var o: [UInt8] = []
    var i = 0
    while i + 3 <= d.count {
        let n = UInt32(d[i]) << 16 | UInt32(d[i + 1]) << 8 | UInt32(d[i + 2])
        o += [b64[Int(n >> 18)], b64[Int(n >> 12 & 63)], b64[Int(n >> 6 & 63)], b64[Int(n & 63)]]
        i += 3
    }
    let r = d.count - i
    if r == 1 {
        let n = UInt32(d[i]) << 16
        o += [b64[Int(n >> 18)], b64[Int(n >> 12 & 63)], 0x3D, 0x3D]
    } else if r == 2 {
        let n = UInt32(d[i]) << 16 | UInt32(d[i + 1]) << 8
        o += [b64[Int(n >> 18)], b64[Int(n >> 12 & 63)], b64[Int(n >> 6 & 63)], 0x3D]
    }
    return String(decoding: o, as: UTF8.self)
}

// W11
func fixedText(_ x: Big, _ f: Int) -> String {
    var s = x.neg ? "-" : ""
    let whole = f >= 128 ? 0 : x.mag >> UInt128(f)
    s += String(whole)
    var frac = f == 0 ? UInt128(0) : x.mag & ((UInt128(1) << UInt128(f)) - 1)
    s += "."
    if frac == 0 { return s + "0" }
    // digits: frac / 2^f, multiply by ten each step (fits: f <= 64 here, frac < 2^f)
    var digits = ""
    var steps = 0
    while frac != 0 && steps < 200 {
        // frac * 10 may overflow for f near 128; f in the corpus is at most 16
        frac = frac * 10
        let dgt = frac >> UInt128(f)
        digits += String(dgt)
        frac &= (UInt128(1) << UInt128(f)) - 1
        steps += 1
    }
    return s + digits
}

// W13
func timestampText(_ x: Big) -> String {
    let ns = x.neg ? -Int64(x.mag) : Int64(x.mag)
    var secs = ns / 1_000_000_000
    var frac = ns % 1_000_000_000
    if frac < 0 { frac += 1_000_000_000; secs -= 1 }
    var days = secs / 86400
    var rem = secs % 86400
    if rem < 0 { rem += 86400; days -= 1 }
    // civil from days (Howard Hinnant)
    let z = days + 719468
    let era = (z >= 0 ? z : z - 146096) / 146097
    let doe = z - era * 146097
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365
    var y = yoe + era * 400
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100)
    let mp = (5 * doy + 2) / 153
    let dd = doy - (153 * mp + 2) / 5 + 1
    let mm = mp < 10 ? mp + 3 : mp - 9
    if mm <= 2 { y += 1 }
    func pad(_ v: Int64, _ w: Int) -> String { let s = String(v); return String(repeating: "0", count: max(0, w - s.count)) + s }
    var s = "\(pad(y, 4))-\(pad(mm, 2))-\(pad(dd, 2))T\(pad(rem / 3600, 2)):\(pad(rem % 3600 / 60, 2)):\(pad(rem % 60, 2))"
    if frac != 0 {
        var f = pad(frac, 9)
        while f.hasSuffix("0") { f.removeLast() }
        s += "." + f
    }
    return s + "Z"
}

// W12: the %.*g loop, C's own conversion, under the C locale.
func cfmt(_ v: Double, _ p: Int) -> String {
    var buf = [CChar](repeating: 0, count: 64)
    _ = withVaList([Int32(p), v]) { vsnprintf(&buf, 64, "%.*g", $0) }
    return String(cString: buf)
}
func floatText(_ v: Double, _ width: Int) throws -> String {
    guard v.isFinite else { throw Refusal("non_finite_float") }
    let (lo, hi) = width == 32 ? (6, 9) : (15, 17)
    for p in lo...hi {
        let t = cfmt(v, p)
        if p == hi { return t }
        if width == 32 {
            if strtof(t, nil).bitPattern == Float(v).bitPattern { return t }
        } else {
            if strtod(t, nil).bitPattern == v.bitPattern { return t }
        }
    }
    fatalError()
}

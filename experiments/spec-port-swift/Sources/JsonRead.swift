// JsonRead.swift — the text read, docs/SPEC-TABLES.md §16.1–§16.3, §16.7,
// §16.8, §16.10. One read: the whole value or a refusal.

import Foundation

func malformed(_ why: String) -> Refusal { Refusal("malformed: " + why) }
func kindMismatch(_ why: String) -> Refusal { Refusal("kind_mismatch: " + why) }

final class JsonReader {
    let s: [UInt8]
    var p = 0
    var unknown = 0
    // labels: value is the node (Rec or Blob) or nil for one defined in a skipped value
    var labels: [UInt64: Rec?] = [:]
    var openLabels = Set<UInt64>()

    init(_ text: [UInt8]) { s = text }

    // MARK: lexical

    func ws() throws {
        while p < s.count {
            let c = s[p]
            if c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D { p += 1; continue }
            if c == 0x2F && p + 1 < s.count && s[p + 1] == 0x2F {
                let st = p + 2
                p = st
                while p < s.count && s[p] != 0x0A { p += 1 }
                guard utf8WellFormed(s[st..<p]) else { throw malformed("string_not_utf8 in comment") }
                continue
            }
            if c == 0x2F && p + 1 < s.count && s[p + 1] == 0x2A {
                let st = p + 2
                var q = st
                while q + 1 < s.count && !(s[q] == 0x2A && s[q + 1] == 0x2F) { q += 1 }
                guard q + 1 < s.count else { throw malformed("unclosed comment") }
                guard utf8WellFormed(s[st..<q]) else { throw malformed("string_not_utf8 in comment") }
                p = q + 2
                continue
            }
            return
        }
    }
    func peek() throws -> UInt8? { try ws(); return p < s.count ? s[p] : nil }
    func expect(_ c: UInt8) throws {
        guard try peek() == c else { throw malformed("expected \(Character(UnicodeScalar(c)))") }
        p += 1
    }

    /// A JSON string's decoded bytes: UTF-8 checked, escapes undone, a lone
    /// surrogate refused.
    func string() throws -> [UInt8] {
        guard try peek() == 0x22 else { throw kindMismatch("expected a string") }
        p += 1
        var out: [UInt8] = []
        while true {
            guard p < s.count else { throw malformed("unclosed string") }
            let c = s[p]
            if c == 0x22 { p += 1; return out }
            if c < 0x20 { throw malformed("raw control byte in string") }
            if c == 0x5C {
                p += 1
                guard p < s.count else { throw malformed("unclosed string") }
                let e = s[p]; p += 1
                switch e {
                case 0x22: out.append(0x22)
                case 0x5C: out.append(0x5C)
                case 0x2F: out.append(0x2F)
                case 0x62: out.append(0x08)
                case 0x66: out.append(0x0C)
                case 0x6E: out.append(0x0A)
                case 0x72: out.append(0x0D)
                case 0x74: out.append(0x09)
                case 0x75:
                    var u = try hex4()
                    if u >= 0xD800 && u <= 0xDBFF {
                        guard p + 1 < s.count && s[p] == 0x5C && s[p + 1] == 0x75 else { throw malformed("string_lone_surrogate") }
                        p += 2
                        let lo = try hex4()
                        guard lo >= 0xDC00 && lo <= 0xDFFF else { throw malformed("string_lone_surrogate") }
                        u = 0x10000 + ((u - 0xD800) << 10) + (lo - 0xDC00)
                    } else if u >= 0xDC00 && u <= 0xDFFF { throw malformed("string_lone_surrogate") }
                    out += Array(String(UnicodeScalar(u)!).utf8)
                default: throw malformed("bad escape")
                }
                continue
            }
            if c < 0x80 { out.append(c); p += 1; continue }
            // a raw multi-byte sequence: take it whole and check it
            var n = 0
            if c >= 0xC2 && c <= 0xDF { n = 2 } else if c >= 0xE0 && c <= 0xEF { n = 3 } else if c >= 0xF0 && c <= 0xF4 { n = 4 }
            guard n > 0, p + n <= s.count, utf8WellFormed(s[p..<(p + n)]) else { throw malformed("string_not_utf8") }
            out += s[p..<(p + n)]
            p += n
        }
    }
    func hex4() throws -> UInt32 {
        guard p + 4 <= s.count else { throw malformed("short \\u escape") }
        var v: UInt32 = 0
        for _ in 0..<4 {
            guard let d = Character(UnicodeScalar(s[p])).hexDigitValue else { throw malformed("bad \\u escape") }
            v = v * 16 + UInt32(d); p += 1
        }
        return v
    }

    /// An RFC 8259 number token, returned as its spelling.
    func numberToken() throws -> String {
        try ws()
        let st = p
        if p < s.count && s[p] == 0x2D { p += 1 }
        guard p < s.count, isDigit(s[p]) else { throw malformed("bad number") }
        if s[p] == 0x30 { p += 1 } else { while p < s.count && isDigit(s[p]) { p += 1 } }
        if p < s.count && s[p] == 0x2E {
            p += 1
            guard p < s.count, isDigit(s[p]) else { throw malformed("bad number") }
            while p < s.count && isDigit(s[p]) { p += 1 }
        }
        if p < s.count && (s[p] == 0x65 || s[p] == 0x45) {
            p += 1
            if p < s.count && (s[p] == 0x2B || s[p] == 0x2D) { p += 1 }
            guard p < s.count, isDigit(s[p]) else { throw malformed("bad number") }
            while p < s.count && isDigit(s[p]) { p += 1 }
        }
        // a token runs into the next structural byte; anything else glued on is malformed
        if p < s.count, !([0x20, 0x09, 0x0A, 0x0D, 0x2C, 0x5D, 0x7D, 0x2F] as [UInt8]).contains(s[p]) { throw malformed("bad number") }
        return String(decoding: s[st..<p], as: UTF8.self)
    }
    func isDigit(_ c: UInt8) -> Bool { c >= 0x30 && c <= 0x39 }

    func literal(_ w: String) -> Bool {
        let b = Array(w.utf8)
        if p + b.count <= s.count && Array(s[p..<(p + b.count)]) == b {
            let q = p + b.count
            if q < s.count && (isDigit(s[q]) || (s[q] >= 0x61 && s[q] <= 0x7A) || (s[q] >= 0x41 && s[q] <= 0x5A)) { return false }
            p = q
            return true
        }
        return false
    }
    func isNull() throws -> Bool { _ = try peek(); return literal("null") }

    // MARK: objects

    /// Iterate an object's members, refusing a repeated key. The body reads
    /// the value. Returns the key list.
    func object(_ depth: Int, _ member: ([UInt8], Int) throws -> Void) throws {
        if depth > maxDepth { throw malformed("depth past 128") }
        guard try peek() == 0x7B else { throw kindMismatch("expected an object") }
        p += 1
        var keys = Set<[UInt8]>()
        var idx = 0
        while true {
            let c = try peek()
            if c == 0x7D { p += 1; return }
            guard c == 0x22 else { throw malformed("expected a key") }
            let k = try string()
            guard keys.insert(k).inserted else { throw malformed("repeated key") }
            try expect(0x3A)
            try member(k, idx)
            idx += 1
            let n = try peek()
            if n == 0x2C { p += 1; continue }
            if n == 0x7D { p += 1; return }
            throw malformed("expected , or }")
        }
    }
    func array(_ depth: Int, _ elem: (Int) throws -> Void) throws {
        if depth > maxDepth { throw malformed("depth past 128") }
        guard try peek() == 0x5B else { throw kindMismatch("expected an array") }
        p += 1
        var i = 0
        while true {
            let c = try peek()
            if c == 0x5D { p += 1; return }
            try elem(i); i += 1
            let n = try peek()
            if n == 0x2C { p += 1; continue }
            if n == 0x5D { p += 1; return }
            throw malformed("expected , or ]")
        }
    }

    func checkName(_ k: [UInt8]) throws { if k.count > 255 { throw Refusal("out_of_range: name past 255 bytes") } }
    func noZero(_ b: [UInt8]) throws { if b.contains(0) { throw Refusal("text_holds_zero") } }

    // MARK: skipping, with the label rules held at every depth

    func skipValue(_ depth: Int) throws {
        guard let c = try peek() else { throw malformed("missing value") }
        switch c {
        case 0x7B:
            var labelDefined: UInt64? = nil
            var isRef = false
            try object(depth) { k, idx in
                if k.first == 0x26 {
                    guard k == Array("&node".utf8), idx == 0 else { throw malformed("& key") }
                    let l = try label()
                    if labels[l] != nil || openLabels.contains(l) {
                        if openLabels.contains(l) { throw malformed("label named inside its own definition") }
                        isRef = true
                    } else {
                        labelDefined = l
                        openLabels.insert(l)
                    }
                    return
                }
                try checkName(k)
                if isRef { throw malformed("field after a reference: second definition") }
                try skipValue(depth + 1)
            }
            if let l = labelDefined {
                openLabels.remove(l)
                labels[l] = .some(nil)
            }
            if labelDefined == nil && !isRef { return }
        case 0x5B: try array(depth) { _ in try skipValue(depth + 1) }
        case 0x22: _ = try string()
        case 0x74, 0x66, 0x6E:
            guard literal("true") || literal("false") || literal("null") else { throw malformed("bad literal") }
        default: _ = try numberToken()
        }
    }

    func label() throws -> UInt64 {
        let t = try numberToken()
        guard !t.hasPrefix("-"), !t.contains("."), !t.contains("e"), !t.contains("E"), !(t.count > 1 && t.hasPrefix("0")), t != "0" else { throw malformed("bad label") }
        guard let v = UInt64(t) else { throw malformed("label past 2^64-1") }
        return v
    }

    // MARK: the walk

    func readRoot(_ d: TableDecl) throws -> Rec {
        if p + 3 <= s.count && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF { p = 3 }
        let r = newRec(d)
        if d.jsonArray {
            r.vals[0] = try value(d.fields[0].ft, 0, d.fields[0], r.vals[0])
        } else {
            try readTable(r, 0, labelOK: false)
        }
        if try peek() != nil { throw malformed("data after the root") }
        return r
    }

    /// A table's object. `labelOK` allows a leading `&node` (a pointer's object).
    func readTable(_ r: Rec, _ depth: Int, labelOK: Bool, onLabel: ((UInt64) throws -> Bool)? = nil) throws {
        let d = r.decl
        var got = [Bool](repeating: false, count: d.fields.count)
        var defining: UInt64? = nil
        var count = 0
        try object(depth) { k, idx in
            count += 1
            if k.first == 0x26 {
                guard labelOK, k == Array("&node".utf8), idx == 0, let cb = onLabel else { throw malformed("& key where no label may stand") }
                let l = try label()
                if try cb(l) { defining = l }
                return
            }
            try checkName(k)
            guard let fi = d.byKey[String(decoding: k, as: UTF8.self)] else {
                unknown += 1
                try skipValue(depth + 1)
                return
            }
            let f = d.fields[fi]
            got[fi] = true
            if f.optional {
                if try isNull() { r.vals[fi] = .absent; return }
                r.vals[fi] = try value(f.ft, depth + 1, f, typeDefault(f.ft))
                return
            }
            r.vals[fi] = try value(f.ft, depth + 1, f, fieldDefault(f))
        }
        if let l = defining {
            if count == 1 { throw malformed("label alone that the text has not defined") }
            openLabels.remove(l)
        }
        for (i, f) in d.fields.enumerated() where f.required && !got[i] {
            throw Refusal("missing_required: \(d.name).\(f.name)")
        }
        if let tot = d.fields.first(where: { $0.total != nil }) { _ = tot }
        for (i, f) in d.fields.enumerated() {
            if let tot = f.total, case let .arr(xs) = r.vals[i] {
                var sum = 0
                for x in xs { if case let .bytes(bb) = x { sum += bb.count } }
                if sum > tot { throw Refusal("length_over_bound: total") }
            }
        }
    }

    func value(_ t: FT, _ depth: Int, _ f: Field?, _ cur: Value) throws -> Value {
        let name = f?.name ?? "element"
        guard let c = try peek() else { throw malformed("missing value") }
        switch t {
        case .bool:
            if literal("true") { return .bool(true) }
            if literal("false") { return .bool(false) }
            if c == 0x7B || c == 0x5B { try skipValue(depth) }
            throw kindMismatch(name)
        case let .int(w, sg, lo, hi):
            let v: Big
            if w > 32 { v = try wideInt(try stringOrMismatch(depth, name), name) } else { v = try intToken(try numberOrMismatch(depth, name), name) }
            if v < Big.minOf(width: w, signed: sg) || Big.maxOf(width: w, signed: sg) < v { throw Refusal("out_of_range: \(name)") }
            if let lo = lo, v < lo { throw Refusal("out_of_range: \(name)") }
            if let hi = hi, hi < v { throw Refusal("out_of_range: \(name)") }
            return .int(v)
        case let .bits(n):
            let v: Big
            if n > 32 { v = try wideInt(try stringOrMismatch(depth, name), name) } else { v = try intToken(try numberOrMismatch(depth, name), name) }
            if v.neg || (n < 128 && v.mag >= UInt128(1) << UInt128(n)) { throw Refusal("out_of_range: \(name)") }
            return .int(v)
        case let .fixed(sg, i, fr, lo, hi):
            let tok: String
            if i + fr > 32 {
                let str = try stringOrMismatch(depth, name)
                tok = String(decoding: str, as: UTF8.self)
                guard isNumberToken(tok) else { throw kindMismatch(name) }
            } else { tok = try numberOrMismatch(depth, name) }
            let v = try fixedFromToken(tok, fr, name)
            if v < Big.minOf(width: i + fr, signed: sg) || Big.maxOf(width: i + fr, signed: sg) < v { throw Refusal("out_of_range: \(name)") }
            if let lo = lo, v < lo { throw Refusal("out_of_range: \(name)") }
            if let hi = hi, hi < v { throw Refusal("out_of_range: \(name)") }
            return .int(v)
        case let .float(w, lo, hi):
            let tok = try numberOrMismatch(depth, name)
            var d: Double
            var out: Value
            if w == 32 {
                let x = strtof(tok, nil)
                guard x.isFinite else { throw Refusal("out_of_range: \(name)") }
                d = Double(x); out = .f32(x)
            } else {
                let x = strtod(tok, nil)
                guard x.isFinite else { throw Refusal("out_of_range: \(name)") }
                d = x; out = .f64(x)
            }
            if lo != nil || hi != nil {
                if let lo = lo, d < lo { throw Refusal("out_of_range: \(name)") }
                if let hi = hi, d > hi { throw Refusal("out_of_range: \(name)") }
                if d == 0 { out = w == 32 ? .f32(0) : .f64(0) }
            }
            return out
        case let .str(n):
            let b = try stringOrMismatch(depth, name)
            try noZero(b)
            if let n = n, b.count > n { throw Refusal("length_over_bound: \(name)") }
            return .bytes(b)
        case let .bytes(n):
            let b = try stringOrMismatch(depth, name)
            let d = try base64Decode(b, name)
            if d.count > n { throw Refusal("length_over_bound: \(name)") }
            return .bytes(d)
        case .timestamp:
            let b = try stringOrMismatch(depth, name)
            guard let ns = parseRFC3339(b) else { throw Refusal("timestamp_not_rfc3339: \(name)") }
            return .int(ns)
        case let .enm(en):
            let b = try stringOrMismatch(depth, name)
            try checkName(b)
            let nm = String(decoding: b, as: UTF8.self)
            if nm == "None" { return .int(.zero) }
            if let k = en.variants.firstIndex(of: nm) {
                if en.deprecated[k] { return f.map { fieldDefault($0) } ?? .int(.zero) }
                return .int(Big(k + 1))
            }
            unknown += 1
            return .int(.zero)
        case let .flags(fl):
            var m: UInt128 = 0
            var seen = Set<Int>()
            try array(depth) { _ in
                guard try peek() == 0x22 else { try skipValue(depth + 1); throw kindMismatch(name) }
                let b = try string()
                try checkName(b)
                let nm = String(decoding: b, as: UTF8.self)
                if let k = fl.variants.firstIndex(of: nm) {
                    guard seen.insert(k).inserted else { throw malformed("flags variant twice") }
                    m |= UInt128(1) << UInt128(k)
                } else { unknown += 1 }
            }
            return .int(Big(m))
        case let .table(d):
            if c != 0x7B && !(d.jsonArray) { try skipValue(depth); throw kindMismatch(name) }
            let r = newRec(d)
            if d.jsonArray { r.vals[0] = try value(d.fields[0].ft, depth, d.fields[0], r.vals[0]); return .rec(r) }
            try readTable(r, depth, labelOK: false)
            return .rec(r)
        case let .union(un):
            guard c == 0x7B else { try skipValue(depth); throw kindMismatch(name) }
            var out: Value = .union(-1, nil)
            var n = 0
            try object(depth) { k, _ in
                n += 1
                if n > 1 { throw malformed("union with two keys") }
                try checkName(k)
                let nm = String(decoding: k, as: UTF8.self)
                guard nm != "None", let ai = un.arms.firstIndex(where: { $0.name == nm }) else {
                    if k.first == 0x26 { throw malformed("& key in a union") }
                    unknown += 1; try skipValue(depth + 1); return
                }
                guard let at = un.arms[ai].ft else {
                    guard try isNull() else { try skipValue(depth + 1); throw kindMismatch("payload-free arm") }
                    out = .union(ai, nil); return
                }
                out = .union(ai, try value(at, depth + 1, nil, typeDefault(at)))
            }
            return out
        case let .ptr(d):
            if try isNull() { return .ptr(nil) }
            guard c == 0x7B else { try skipValue(depth); throw kindMismatch(name) }
            return .ptr(try readPointee(d, depth))
        case let .blob(isString):
            if try isNull() { return .blob(nil) }
            let b = try stringOrMismatch(depth, name)
            if isString { try noZero(b); return .blob(Blob(b, isString: true)) }
            return .blob(Blob(try base64Decode(b, name), isString: false))
        case let .array(et, ak, eopt):
            var xs: [Value] = []
            if c != 0x5B { try skipValue(depth); throw kindMismatch(name) }
            try array(depth) { _ in
                if eopt {
                    if try isNull() { xs.append(.absent); return }
                    xs.append(try value(et, depth + 1, nil, typeDefault(et)))
                    return
                }
                xs.append(try value(et, depth + 1, nil, typeDefault(et)))
                if case let .fixed(cap) = ak, xs.count > cap { throw Refusal("count_over_bound: \(name)") }
                if case let .counted(_, mx) = ak, xs.count > mx { throw Refusal("out_of_range: \(name) count") }
            }
            switch ak {
            case let .fixed(cap):
                if xs.count > cap { throw Refusal("count_over_bound: \(name)") }
                while xs.count < cap { xs.append(eopt ? .absent : typeDefault(et)) }
            case let .counted(mn, mx):
                if xs.count > mx { throw Refusal("count_over_bound: \(name)") }
                if xs.count < mn { throw Refusal("out_of_range: \(name) below min") }
            case let .unbounded(mn): if xs.count < mn { throw Refusal("out_of_range: below min") }
            }
            return .arr(xs)
        case let .keyed(en, et):
            guard c == 0x7B else { try skipValue(depth); throw kindMismatch(name) }
            guard case var .keyed(slots) = typeDefault(t) else { fatalError() }
            try object(depth) { k, _ in
                try checkName(k)
                let nm = String(decoding: k, as: UTF8.self)
                guard nm != "None", let vi = en.variants.firstIndex(of: nm) else {
                    if k.first == 0x26 { throw malformed("& key in a keyed array") }
                    unknown += 1; try skipValue(depth + 1); return
                }
                slots[vi] = try value(et, depth + 1, nil, typeDefault(et))
            }
            return .keyed(slots)
        case let .map(kt, vt, vopt):
            guard c == 0x7B else { try skipValue(depth); throw kindMismatch(name) }
            var es: [(Value, Value)] = []
            try object(depth) { k, _ in
                try noZero(k)
                let kv: Value
                switch kt {
                case let .str(n):
                    if let n = n, k.count > n { throw Refusal("length_over_bound: \(name) key") }
                    kv = .bytes(k)
                case let .int(w, sg, _, _):
                    let ks = String(decoding: k, as: UTF8.self)
                    guard isNumberToken(ks) else { throw malformed("map key") }
                    let v = try intToken(ks, name)
                    if v < Big.minOf(width: w, signed: sg) || Big.maxOf(width: w, signed: sg) < v { throw Refusal("out_of_range: \(name) key") }
                    kv = .int(v)
                default: throw kindMismatch("map key kind")
                }
                if vopt, try isNull() { es.append((kv, .absent)); return }
                es.append((kv, try value(vt, depth + 1, nil, typeDefault(vt))))
            }
            return .map(sortedEntries(es, kt))
        }
    }

    /// A pointer's object: a definition, a reference, or an unlabelled node.
    func readPointee(_ d: TableDecl, _ depth: Int) throws -> Rec? {
        // look ahead for a leading &node key
        let save = p
        var refLabel: UInt64? = nil
        p += 1
        if try peek() == 0x22 {
            let k = try string()
            if k == Array("&node".utf8) {
                try expect(0x3A)
                let l = try label()
                if try peek() == 0x7D {
                    if openLabels.contains(l) { throw malformed("label named inside its own definition") }
                    guard labels[l] != nil else { throw malformed("label alone that the text has not defined") }
                    refLabel = l
                    p += 1
                }
            }
        }
        if let l = refLabel {
            if depth > maxDepth { throw malformed("depth past 128") }
            guard let node = labels[l]! else { return nil }
            guard node.decl === d else { throw kindMismatch("reference to a node of another table") }
            return node
        }
        p = save
        let r = newRec(d)
        var defined: UInt64? = nil
        try readTable(r, depth, labelOK: true) { l in
            if self.openLabels.contains(l) { throw malformed("label named inside its own definition") }
            if self.labels[l] != nil { throw malformed("label defined twice") }
            self.openLabels.insert(l)
            defined = l
            return true
        }
        if let l = defined { labels[l] = .some(r) }
        return r
    }

    func stringOrMismatch(_ depth: Int, _ name: String) throws -> [UInt8] {
        let c = try peek()
        guard c == 0x22 else {
            if c != nil { try skipValue(depth) }
            throw kindMismatch(name)
        }
        return try string()
    }
    func numberOrMismatch(_ depth: Int, _ name: String) throws -> String {
        let c = try peek()
        guard let cc = c, cc == 0x2D || isDigit(cc) else {
            if c != nil { try skipValue(depth) }
            throw kindMismatch(name)
        }
        return try numberToken()
    }

    // MARK: number rules

    func intToken(_ t: String, _ name: String) throws -> Big {
        if t.contains(".") || t.contains("e") || t.contains("E") || t == "-0" { throw kindMismatch(name) }
        let neg = t.hasPrefix("-")
        guard let m = Big.parseDecimal(Substring(neg ? String(t.dropFirst()) : t)) else {
            // a valid integer token too large for any kind
            throw Refusal("out_of_range: \(name)")
        }
        return Big(neg: neg, mag: m.mag)
    }
    func wideInt(_ b: [UInt8], _ name: String) throws -> Big {
        let t = String(decoding: b, as: UTF8.self)
        var body = Substring(t)
        let neg = body.hasPrefix("-")
        if neg { body = body.dropFirst() }
        guard !body.isEmpty, body.allSatisfy({ $0.isASCII && $0.isNumber }), !(body.count > 1 && body.hasPrefix("0")), !(neg && body == "0") else { throw kindMismatch(name) }
        guard let m = Big.parseDecimal(body) else { throw Refusal("out_of_range: \(name)") }
        return Big(neg: neg, mag: m.mag)
    }
    func isNumberToken(_ t: String) -> Bool {
        let r = JsonReader(Array(t.utf8))
        guard let c = t.utf8.first, c == 0x2D || (c >= 0x30 && c <= 0x39) else { return false }
        do { _ = try r.numberToken() } catch { return false }
        return r.p == t.utf8.count
    }
    /// A number token in whole units, scaled by 2^F, exactly.
    func fixedFromToken(_ t: String, _ f: Int, _ name: String) throws -> Big {
        var s = Substring(t)
        let neg = s.hasPrefix("-")
        if neg { s = s.dropFirst() }
        var exp = 0
        if let ei = s.firstIndex(where: { $0 == "e" || $0 == "E" }) {
            guard let e = Int(s[s.index(after: ei)...]) else { throw Refusal("out_of_range: \(name)") }
            exp = e
            s = s[..<ei]
        }
        var digits = String(s)
        if let dot = digits.firstIndex(of: ".") {
            let frac = digits[digits.index(after: dot)...]
            exp -= frac.count
            digits = String(digits[..<dot]) + frac
        }
        // strip leading zeros and trailing zeros
        while digits.count > 1 && digits.hasPrefix("0") { digits.removeFirst() }
        while digits.count > 1 && digits.hasSuffix("0") { digits.removeLast(); exp += 1 }
        if digits == "0" { return .zero }
        guard let m = Big.parseDecimal(Substring(digits)) else { throw Refusal("out_of_range: \(name)") }
        var v = m.mag
        // multiply by 2^F
        guard v.leadingZeroBitCount > f else { throw Refusal("out_of_range: \(name)") }
        v <<= UInt128(f)
        if exp >= 0 {
            for _ in 0..<exp {
                let (x, o) = v.multipliedReportingOverflow(by: 10)
                if o { throw Refusal("out_of_range: \(name)") }
                v = x
            }
        } else {
            for _ in 0..<(-exp) {
                if v % 10 != 0 { throw kindMismatch("\(name): finer than the fixed resolution") }
                v /= 10
            }
        }
        return Big(neg: neg, mag: v)
    }
}

// §16.8 canonical base64
func base64Decode(_ b: [UInt8], _ name: String) throws -> [UInt8] {
    if b.isEmpty { return [] }
    guard b.count % 4 == 0 else { throw kindMismatch("\(name): not base64") }
    var val = [Int](repeating: -1, count: 256)
    for (i, c) in b64.enumerated() { val[Int(c)] = i }
    var out: [UInt8] = []
    var i = 0
    while i < b.count {
        let q = Array(b[i..<(i + 4)])
        let last = i + 4 == b.count
        var pad = 0
        if q[3] == 0x3D { pad = 1; if q[2] == 0x3D { pad = 2 } }
        if pad > 0 && !last { throw kindMismatch("\(name): misplaced padding") }
        if q[0] == 0x3D || q[1] == 0x3D || (q[2] == 0x3D && q[3] != 0x3D) { throw kindMismatch("\(name): misplaced padding") }
        var n: UInt32 = 0
        for k in 0..<(4 - pad) {
            let v = val[Int(q[k])]
            if v < 0 { throw kindMismatch("\(name): not base64") }
            n |= UInt32(v) << UInt32(18 - 6 * k)
        }
        out.append(UInt8(n >> 16))
        if pad < 2 { out.append(UInt8(n >> 8 & 0xff)) }
        if pad < 1 { out.append(UInt8(n & 0xff)) }
        if pad == 2 && n & 0xFFFF != 0 { throw Refusal("base64_not_canonical: \(name)") }
        if pad == 1 && n & 0xFF != 0 { throw Refusal("base64_not_canonical: \(name)") }
        i += 4
    }
    return out
}

// §16.2 timestamp: strict RFC 3339 §5.6 date-time, to UTC nanoseconds.
func parseRFC3339(_ b: [UInt8]) -> Big? {
    func num(_ s: Int, _ n: Int) -> Int? {
        guard s + n <= b.count else { return nil }
        var v = 0
        for i in s..<(s + n) { guard b[i] >= 0x30 && b[i] <= 0x39 else { return nil }; v = v * 10 + Int(b[i] - 0x30) }
        return v
    }
    guard b.count >= 20, let y = num(0, 4), b[4] == 0x2D, let mo = num(5, 2), b[7] == 0x2D, let d = num(8, 2),
          b[10] == 0x54 || b[10] == 0x74, let h = num(11, 2), b[13] == 0x3A, let mi = num(14, 2), b[16] == 0x3A, let se = num(17, 2)
    else { return nil }
    guard (1...12).contains(mo), h <= 23, mi <= 59, se <= 59 else { return nil }
    let leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0
    let mdays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
    guard d >= 1 && d <= mdays[mo - 1] else { return nil }
    var p = 19
    var frac: Int64 = 0
    if p < b.count && b[p] == 0x2E {
        p += 1
        let st = p
        var digits = 0
        while p < b.count && b[p] >= 0x30 && b[p] <= 0x39 {
            if digits < 9 { frac = frac * 10 + Int64(b[p] - 0x30); digits += 1 }
            p += 1
        }
        guard p > st else { return nil }
        while digits < 9 { frac *= 10; digits += 1 }
    }
    guard p < b.count else { return nil }
    var off = 0
    if b[p] == 0x5A || b[p] == 0x7A { p += 1 }
    else if b[p] == 0x2B || b[p] == 0x2D {
        let sign = b[p] == 0x2D ? -1 : 1
        guard let oh = num(p + 1, 2), p + 3 < b.count, b[p + 3] == 0x3A, let om = num(p + 4, 2), oh <= 23, om <= 59 else { return nil }
        off = sign * (oh * 3600 + om * 60)
        p += 6
    } else { return nil }
    guard p == b.count else { return nil }
    // days from civil
    let yy = mo <= 2 ? y - 1 : y
    let era = (yy >= 0 ? yy : yy - 399) / 400
    let yoe = yy - era * 400
    let mp = mo > 2 ? mo - 3 : mo + 9
    let doy = (153 * mp + 2) / 5 + d - 1
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy
    let days = Int64(era * 146097 + doe - 719468)
    let secs = days * 86400 + Int64(h * 3600 + mi * 60 + se) - Int64(off)
    let (ns, o1) = secs.multipliedReportingOverflow(by: 1_000_000_000)
    if o1 { return nil }
    let (t, o2) = ns.addingReportingOverflow(frac)
    if o2 { return nil }
    return Big(neg: t < 0, mag: UInt128(t.magnitude))
}

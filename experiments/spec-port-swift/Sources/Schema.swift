// Schema.swift — schema intake. docs/SPEC.md §4.1 (lexical), §4.2 (grammar),
// docs/SPEC-TABLES.md §2 (table declarations). The spec defines no serialized
// IR a port can receive (SPEC-GAP 1), so this port parses `.schema` text.
// It is a reader of checked units, not a checker: the compile-time refusals
// of §4.6 and SPEC-TABLES.md §11 belong to `schema check` (SPEC-GAP 2).

import Foundation

// MARK: tokens

enum Tok: Equatable {
    case ident(String)
    case int(String)      // literal spelling
    case float(String)
    case str([UInt8])
    case punct(String)
    case nl
    case eof
}

struct Lexer {
    let s: [UInt8]
    var i = 0
    var toks: [Tok] = []
    init(_ text: [UInt8]) { s = text }

    mutating func run() throws -> [Tok] {
        while i < s.count {
            let c = s[i]
            if c == 0x0A { toks.append(.nl); i += 1; continue }
            if c == 0x20 || c == 0x09 || c == 0x0D { i += 1; continue }
            if c == 0x2F && i + 1 < s.count && s[i + 1] == 0x2F {
                while i < s.count && s[i] != 0x0A { i += 1 }
                continue
            }
            if c == 0x2F && i + 1 < s.count && s[i + 1] == 0x2A {
                i += 2
                var sawNL = false
                while i + 1 < s.count && !(s[i] == 0x2A && s[i + 1] == 0x2F) { if s[i] == 0x0A { sawNL = true }; i += 1 }
                if i + 1 >= s.count { throw Refusal("unclosed block comment") }
                i += 2
                if sawNL { toks.append(.nl) }
                continue
            }
            if isIdentStart(c) {
                let st = i
                while i < s.count && (isIdentStart(s[i]) || (s[i] >= 0x30 && s[i] <= 0x39)) { i += 1 }
                toks.append(.ident(String(decoding: s[st..<i], as: UTF8.self)))
                continue
            }
            if c >= 0x30 && c <= 0x39 { try number(); continue }
            if c == 0x22 {
                i += 1
                let st = i
                while i < s.count && s[i] != 0x22 && s[i] != 0x0A { i += 1 }
                if i >= s.count || s[i] != 0x22 { throw Refusal("unclosed string literal") }
                toks.append(.str(Array(s[st..<i])))
                i += 1
                continue
            }
            // punctuation, maximal munch for `..` and `::`
            if c == 0x2E && i + 1 < s.count && s[i + 1] == 0x2E { toks.append(.punct("..")); i += 2; continue }
            if c == 0x3A && i + 1 < s.count && s[i + 1] == 0x3A { toks.append(.punct("::")); i += 2; continue }
            let p = String(UnicodeScalar(c))
            if "{}()[],:=!?.|+-*/%".contains(p) { toks.append(.punct(p)); i += 1; continue }
            throw Refusal("unexpected byte \(c) in schema")
        }
        toks.append(.nl)
        toks.append(.eof)
        return toks
    }
    func isIdentStart(_ c: UInt8) -> Bool { (c >= 0x41 && c <= 0x5A) || (c >= 0x61 && c <= 0x7A) || c == 0x5F }

    mutating func number() throws {
        let st = i
        if s[i] == 0x30 && i + 1 < s.count && (s[i + 1] == 0x78 || s[i + 1] == 0x58 || s[i + 1] == 0x62 || s[i + 1] == 0x42) {
            i += 2
            while i < s.count && (isHex(s[i]) || s[i] == 0x5F) { i += 1 }
            toks.append(.int(String(decoding: s[st..<i], as: UTF8.self)))
            return
        }
        while i < s.count && s[i] >= 0x30 && s[i] <= 0x39 { i += 1 }
        var isFloat = false
        if i < s.count && s[i] == 0x2E && !(i + 1 < s.count && s[i + 1] == 0x2E) {
            isFloat = true
            i += 1
            while i < s.count && s[i] >= 0x30 && s[i] <= 0x39 { i += 1 }
        }
        if i < s.count && (s[i] == 0x65 || s[i] == 0x45) {
            isFloat = true
            i += 1
            if i < s.count && (s[i] == 0x2B || s[i] == 0x2D) { i += 1 }
            while i < s.count && s[i] >= 0x30 && s[i] <= 0x39 { i += 1 }
        }
        let t = String(decoding: s[st..<i], as: UTF8.self)
        toks.append(isFloat ? .float(t) : .int(t))
    }
    func isHex(_ c: UInt8) -> Bool { (c >= 0x30 && c <= 0x39) || (c >= 0x41 && c <= 0x46) || (c >= 0x61 && c <= 0x66) }
}

// MARK: AST

indirect enum Expr {
    case int(Big)
    case float(Double)
    case name(String)
    case member(String, String)  // E.Max, F.Count
    case neg(Expr)
    case bin(String, Expr, Expr)
}

enum AttrVal { case expr(Expr), str([UInt8]), word(String), none }
struct Attr { let key: String; let val: AttrVal }

indirect enum TypeAST {
    case scalar(String, [Expr])     // int32, bits(e), string(e), string (no args), fixed(i,f) ...
    case named(String)
    case pointer(String)            // *T, *bytes, *string
    case array(BoundAST, TypeAST, elemOpt: Bool)
    case map(TypeAST, TypeAST, valOpt: Bool)
}
enum BoundAST { case exact(Expr), upTo(Expr), range(Expr, Expr), none }

enum DefaultAST { case expr(Expr), str([UInt8]), list([String]) }

struct Guard { let field: String; let negate: Bool }

final class FieldAST {
    let name: String; let type: TypeAST; let optional: Bool; let def: DefaultAST?; let attrs: [Attr]; let guards: [Guard]
    init(name: String, type: TypeAST, optional: Bool, def: DefaultAST?, attrs: [Attr], guards: [Guard]) {
        self.name = name; self.type = type; self.optional = optional; self.def = def; self.attrs = attrs; self.guards = guards
    }
}

// MARK: resolved model

final class EnumDecl {
    let name: String
    var variants: [String] = []       // ordinal k is variants[k-1]
    var wireIds: [UInt64] = []
    var deprecated: [Bool] = []
    init(name: String) { self.name = name }
    var max: Int { variants.count }
}
final class FlagsDecl {
    let name: String
    var variants: [String] = []
    var width = 0
    init(name: String) { self.name = name }
}
final class Field {
    let name: String
    var wireId: UInt64
    var jsonKey: String
    var ft: FT = .bool
    var optional = false
    var required = false
    var deprecated = false
    var guards: [Guard] = []
    var guardIdx: [(Int, Bool)] = []
    var def: Value? = nil           // declared default for scalars/strings/flags
    var total: Int? = nil
    init(name: String) { self.name = name; wireId = fnv1a64(name); jsonKey = name }
}
final class TableDecl {
    let name: String
    var wireId: UInt64
    var isType = false
    var isFixed = false
    var fields: [Field] = []
    var integrity = false
    var jsonArray = false
    var byWireId: [UInt64: Int] = [:]
    var byKey: [String: Int] = [:]
    init(name: String) { self.name = name; wireId = fnv1a64(name) }
}
final class Arm {
    let name: String
    var wireId: UInt64
    var ft: FT? = nil   // nil: payload-free
    init(name: String) { self.name = name; wireId = fnv1a64(name) }
}
final class UnionDecl {
    let name: String
    var arms: [Arm] = []
    init(name: String) { self.name = name }
}

enum ArrKind: Equatable { case fixed(Int), counted(Int, Int), unbounded(min: Int) }

indirect enum FT {
    case bool
    case int(width: Int, signed: Bool, lo: Big?, hi: Big?)
    case bits(Int)
    case float(width: Int, lo: Double?, hi: Double?)
    case fixed(signed: Bool, i: Int, f: Int, lo: Big?, hi: Big?)   // lo/hi raw scale
    case str(Int?)
    case bytes(Int)
    case timestamp
    case enm(EnumDecl)
    case flags(FlagsDecl)
    case table(TableDecl)
    case union(UnionDecl)
    case ptr(TableDecl)
    case blob(isString: Bool)
    case array(FT, ArrKind, elemOpt: Bool)
    case keyed(EnumDecl, FT)
    case map(FT, FT, valOpt: Bool)

    var kind: UInt8 {
        switch self {
        case .bool: return 1
        case let .int(w, s, _, _):
            switch (w, s) {
            case (8, true): return 2; case (16, true): return 3; case (32, true): return 4; case (64, true): return 5; case (128, true): return 18
            case (8, false): return 6; case (16, false): return 7; case (32, false): return 8; case (64, false): return 9; default: return 19
            }
        case let .bits(n): return n <= 8 ? 6 : n <= 16 ? 7 : n <= 32 ? 8 : 9
        case let .float(w, _, _): return w == 32 ? 10 : 11
        case let .fixed(s, i, f, _, _):
            let w = i + f
            let base: UInt8 = s ? 20 : 25
            switch w { case 8: return base; case 16: return base + 1; case 32: return base + 2; case 64: return base + 3; default: return base + 4 }
        case .str: return 12
        case .bytes: return 14
        case .timestamp: return 36
        case .enm: return 30
        case .flags: return 9
        case .table: return 13
        case .union: return 15
        case .ptr, .blob: return 17
        case .array: return 14
        case .keyed: return 16
        case .map: return 14
        }
    }
    /// The element kind when this type is an array element.
    var elemKind: UInt8 { kind }
    /// Fixed payload width for scalar kinds.
    var width: Int? { kindWidth(kind) }
    var isScalarFixedWidth: Bool { width != nil }
}

func kindWidth(_ k: UInt8) -> Int? {
    switch k {
    case 1, 2, 6, 20, 25: return 1
    case 3, 7, 21, 26: return 2
    case 4, 8, 10, 22, 27: return 4
    case 5, 9, 11, 23, 28, 36: return 8
    case 18, 19, 24, 29: return 16
    default: return nil
    }
}

final class Unit {
    var consts: [String: (Expr, String?)] = [:]
    var enums: [String: EnumDecl] = [:]
    var flags: [String: FlagsDecl] = [:]
    var tables: [String: TableDecl] = [:]
    var unions: [String: UnionDecl] = [:]
    var fieldASTs: [String: [FieldAST]] = [:]
    var tableAttrs: [String: [Attr]] = [:]
    var armASTs: [String: [(String, TypeAST?, [Attr])]] = [:]
    var enumAttrs: [String: [[Attr]]] = [:]
    var flagsAttrs: [String: [Attr]] = [:]
    var tableOrder: [String] = []

    // MARK: constant evaluation

    func evalInt(_ e: Expr, _ depth: Int = 0) throws -> Big {
        if depth > 64 { throw Refusal("constant cycle") }
        switch e {
        case let .int(b): return b
        case .float: throw Refusal("float where integer required")
        case let .name(n):
            if let (ce, _) = consts[n] { return try evalInt(ce, depth + 1) }
            throw Refusal("unknown constant \(n)")
        case let .member(a, m):
            if m == "Max", let en = enums[a] { return Big(en.max) }
            if m == "Count", let fl = flags[a] { return Big(fl.variants.count) }
            if m == "Max", let un = unions[a] { return Big(un.arms.count) }
            throw Refusal("unknown member \(a).\(m)")
        case let .neg(x): return -(try evalInt(x, depth + 1))
        case let .bin(op, a, b):
            let x = try evalInt(a, depth + 1), y = try evalInt(b, depth + 1)
            switch op {
            case "+": return try Big.add(x, y)
            case "-": return try Big.sub(x, y)
            case "*": return try Big.mul(x, y)
            case "/": return try Big.div(x, y)
            default: return try Big.rem(x, y)
            }
        }
    }
    func evalFloat(_ e: Expr, _ depth: Int = 0) throws -> Double {
        if depth > 64 { throw Refusal("constant cycle") }
        switch e {
        case let .int(b): return (b.neg ? -1 : 1) * Double(b.mag)
        case let .float(d): return d
        case let .name(n):
            if let (ce, _) = consts[n] { return try evalFloat(ce, depth + 1) }
            throw Refusal("unknown constant \(n)")
        case .member: let b = try evalInt(e, depth + 1); return Double(b.mag) * (b.neg ? -1 : 1)
        case let .neg(x): return -(try evalFloat(x, depth + 1))
        case let .bin(op, a, b):
            let x = try evalFloat(a, depth + 1), y = try evalFloat(b, depth + 1)
            switch op { case "+": return x + y; case "-": return x - y; case "*": return x * y; case "/": return x / y; default: throw Refusal("% on float") }
        }
    }
    func intOf(_ e: Expr) throws -> Int {
        let b = try evalInt(e)
        guard !b.neg, b.mag <= UInt128(Int32.max) else { throw Refusal("bound out of range") }
        return Int(b.mag)
    }
}

// MARK: parser

struct Parser {
    var t: [Tok]
    var p = 0
    let unit: Unit
    init(_ toks: [Tok], _ u: Unit) { t = toks; unit = u }

    var cur: Tok { t[p] }
    mutating func next() -> Tok { let x = t[p]; if p < t.count - 1 { p += 1 }; return x }
    func isP(_ s: String) -> Bool { if case let .punct(x) = cur { return x == s }; return false }
    func isW(_ s: String) -> Bool { if case let .ident(x) = cur { return x == s }; return false }
    mutating func skipNL() { while cur == .nl { p += 1 } }
    mutating func expectP(_ s: String) throws { guard isP(s) else { throw Refusal("expected \(s), got \(cur)") }; p += 1 }
    mutating func ident() throws -> String {
        if case let .ident(x) = cur { p += 1; return x }
        throw Refusal("expected identifier, got \(cur)")
    }
    mutating func endItem() throws {
        if cur == .nl { skipNL(); return }
        if isP("}") || cur == .eof { return }
        throw Refusal("expected end of line, got \(cur)")
    }

    mutating func file() throws {
        while true {
            skipNL()
            if cur == .eof { return }
            let w = try ident()
            switch w {
            case "package": _ = try ident(); try endItem()
            case "const":
                let n = try ident()
                var ty: String? = nil
                if case let .ident(x) = cur { ty = x; p += 1 }
                try expectP("=")
                let e = try expr()
                if isP("|") { _ = try attrs() }
                unit.consts[n] = (e, ty)
                try endItem()
            case "enum":
                let n = try ident()
                let d = EnumDecl(name: n)
                if isP("|") { _ = try attrs() }
                skipNL()
                var vattrs: [[Attr]] = []
                for (v, a) in try variantList() { d.variants.append(v); vattrs.append(a) }
                unit.enums[n] = d
                unit.enumAttrs[n] = vattrs
                try endItem()
            case "flags":
                let n = try ident()
                let d = FlagsDecl(name: n)
                var a: [Attr] = []
                if isP("|") { a = try attrs() }
                skipNL()
                for (v, _) in try variantList() { d.variants.append(v) }
                unit.flags[n] = d
                unit.flagsAttrs[n] = a
                try endItem()
            case "type", "table", "fixed":
                var isFixed = false
                var kw = w
                if w == "fixed" { isFixed = true; kw = try ident(); guard kw == "table" else { throw Refusal("fixed must precede table") } }
                let n = try ident()
                let d = TableDecl(name: n)
                d.isType = kw == "type"
                d.isFixed = isFixed
                var a: [Attr] = []
                if isP("|") { a = try attrs() }
                skipNL()
                let fs = try block([])
                unit.tables[n] = d
                unit.tableOrder.append(n)
                unit.fieldASTs[n] = fs
                unit.tableAttrs[n] = a
                try endItem()
            case "union":
                let n = try ident()
                let d = UnionDecl(name: n)
                if isP("|") { _ = try attrs() }
                skipNL()
                try expectP("{")
                var arms: [(String, TypeAST?, [Attr])] = []
                skipNL()
                while !isP("}") {
                    let an = try ident()
                    var ty: TypeAST? = nil
                    if !isP("|") && cur != .nl && !isP("}") { ty = try typeAST().0 }
                    var a: [Attr] = []
                    if isP("|") { a = try attrs() }
                    arms.append((an, ty, a))
                    try endItem()
                }
                p += 1
                unit.unions[n] = d
                unit.armASTs[n] = arms
                try endItem()
            default:
                throw Refusal("unknown declaration \(w)")
            }
        }
    }

    mutating func variantList() throws -> [(String, [Attr])] {
        try expectP("{")
        var out: [(String, [Attr])] = []
        skipNL()
        while !isP("}") {
            let v = try ident()
            var a: [Attr] = []
            if isP("|") { a = try attrs() }
            out.append((v, a))
            skipNL()
            if isP(",") { p += 1; skipNL() }
        }
        p += 1
        return out
    }

    /// The qualification section: runs to the end of the line. A `{` that
    /// follows it on the same line opens the block (SPEC-GAP 3).
    mutating func attrs() throws -> [Attr] {
        try expectP("|")
        var out: [Attr] = []
        while true {
            let k = try ident()
            if isP("=") {
                p += 1
                if case let .str(b) = cur { p += 1; out.append(Attr(key: k, val: .str(b))) }
                else {
                    // a word (QualIdent) or an expression
                    let save = p
                    if case let .ident(w) = cur {
                        p += 1
                        if isP("::") {
                            var q = w
                            while isP("::") { p += 1; q += "::" + (try ident()) }
                            out.append(Attr(key: k, val: .word(q)))
                        } else { p = save; out.append(Attr(key: k, val: .expr(try expr()))) }
                    } else { out.append(Attr(key: k, val: .expr(try expr()))) }
                }
            } else { out.append(Attr(key: k, val: .none)) }
            if isP(",") { p += 1; continue }
            break
        }
        if cur != .nl && !isP("{") && !isP("}") && cur != .eof { throw Refusal("qualification runs to end of line, got \(cur)") }
        return out
    }

    mutating func block(_ guards: [Guard]) throws -> [FieldAST] {
        try expectP("{")
        var out: [FieldAST] = []
        skipNL()
        while !isP("}") {
            if isW("if") {
                p += 1
                var neg = false
                if isP("!") { neg = true; p += 1 }
                let g = try ident()
                skipNL()
                out += try block(guards + [Guard(field: g, negate: neg)])
                let afterBlock = p
                skipNL()
                if isW("else") {
                    p += 1
                    skipNL()
                    out += try block(guards + [Guard(field: g, negate: !neg)])
                } else { p = afterBlock }
                try endItem()
                continue
            }
            let n = try ident()
            let (ty, opt) = try typeAST()
            var def: DefaultAST? = nil
            if isP("=") {
                p += 1
                if case let .str(b) = cur { p += 1; def = .str(b) }
                else if isP("{") {
                    p += 1
                    var names: [String] = []
                    while !isP("}") { names.append(try ident()); if isP(",") { p += 1 } }
                    p += 1
                    def = .list(names)
                } else { def = .expr(try expr()) }
            }
            var a: [Attr] = []
            if isP("|") { a = try attrs() }
            out.append(FieldAST(name: n, type: ty, optional: opt, def: def, attrs: a, guards: guards))
            try endItem()
        }
        p += 1
        return out
    }

    mutating func typeAST() throws -> (TypeAST, Bool) {
        var opt = false
        if isP("?") { opt = true; p += 1 }
        return (try typeInner(), opt)
    }
    mutating func typeInner() throws -> TypeAST {
        if isP("[") {
            p += 1
            var b: BoundAST = .none
            if isP("]") { b = .none }
            else if isP("..") { p += 1; b = .upTo(try expr()) }
            else {
                let a = try expr()
                if isP("..") { p += 1; b = .range(a, try expr()) } else { b = .exact(a) }
            }
            try expectP("]")
            var eopt = false
            if isP("?") { eopt = true; p += 1 }
            return .array(b, try typeInner(), elemOpt: eopt)
        }
        if isW("map") {
            p += 1
            try expectP("[")
            let k = try typeInner()
            try expectP("]")
            var vopt = false
            if isP("?") { vopt = true; p += 1 }
            return .map(k, try typeInner(), valOpt: vopt)
        }
        if isP("*") { p += 1; return .pointer(try ident()) }
        let w = try ident()
        switch w {
        case "bits", "bytes":
            try expectP("("); let e = try expr(); try expectP(")"); return .scalar(w, [e])
        case "string":
            if isP("(") { p += 1; let e = try expr(); try expectP(")"); return .scalar(w, [e]) }
            return .scalar(w, [])
        case "fixed", "ufixed":
            try expectP("("); let a = try expr(); try expectP(","); let b = try expr(); try expectP(")")
            return .scalar(w, [a, b])
        case "bool", "float32", "float64", "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "int128", "uint128", "timestamp":
            return .scalar(w, [])
        default: return .named(w)
        }
    }

    // expressions
    mutating func expr() throws -> Expr {
        var l = try term()
        while isP("+") || isP("-") { let op = (next() == .punct("+")) ? "+" : "-"; l = .bin(op, l, try term()) }
        return l
    }
    mutating func term() throws -> Expr {
        var l = try unary()
        while isP("*") || isP("/") || isP("%") {
            guard case let .punct(op) = next() else { break }
            l = .bin(op, l, try unary())
        }
        return l
    }
    mutating func unary() throws -> Expr {
        if isP("-") { p += 1; return .neg(try unary()) }
        if isP("(") { p += 1; let e = try expr(); try expectP(")"); return e }
        switch next() {
        case let .int(s): return .int(try parseIntLit(s))
        case let .float(s): guard let d = Double(s) else { throw Refusal("bad float \(s)") }; return .float(d)
        case let .ident(n):
            if isP(".") { p += 1; return .member(n, try ident()) }
            return .name(n)
        case let x: throw Refusal("unexpected \(x) in expression")
        }
    }
    func parseIntLit(_ s: String) throws -> Big {
        let lower = s.lowercased().replacingOccurrences(of: "_", with: "")
        var radix: UInt128 = 10
        var body = Substring(lower)
        if lower.hasPrefix("0x") { radix = 16; body = body.dropFirst(2) }
        else if lower.hasPrefix("0b") { radix = 2; body = body.dropFirst(2) }
        else if lower.count > 1 && lower.hasPrefix("0") { throw Refusal("leading zero literal \(s)") }
        var m: UInt128 = 0
        for c in body {
            guard let d = c.hexDigitValue, UInt128(d) < radix else { throw Refusal("bad literal \(s)") }
            let (a, o1) = m.multipliedReportingOverflow(by: radix)
            let (b, o2) = a.addingReportingOverflow(UInt128(d))
            if o1 || o2 { throw Refusal("literal overflow \(s)") }
            m = b
        }
        return Big(neg: false, mag: m)
    }
}

// MARK: loading and resolving a unit

func loadUnit(paths: [String]) throws -> Unit {
    let u = Unit()
    var files: [String] = []
    for p in paths {
        var isDir: ObjCBool = false
        if FileManager.default.fileExists(atPath: p, isDirectory: &isDir), isDir.boolValue {
            let names = try FileManager.default.contentsOfDirectory(atPath: p).filter { $0.hasSuffix(".schema") }.sorted()
            files += names.map { p + "/" + $0 }
        } else { files.append(p) }
    }
    for f in files {
        guard let d = FileManager.default.contents(atPath: f) else { throw Refusal("cannot read \(f)") }
        var lx = Lexer([UInt8](d))
        let toks = try lx.run()
        var ps = Parser(toks, u)
        try ps.file()
    }
    try resolve(u)
    return u
}

func attr(_ a: [Attr], _ k: String) -> AttrVal? { a.first { $0.key == k }?.val }
func hasTag(_ a: [Attr], _ k: String) -> Bool { a.contains { $0.key == k } }
func attrStr(_ a: [Attr], _ k: String) -> String? {
    if case let .str(b)? = attr(a, k) { return String(decoding: b, as: UTF8.self) }
    return nil
}

func resolve(_ u: Unit) throws {
    for (n, d) in u.enums {
        let va = u.enumAttrs[n] ?? []
        for (k, v) in d.variants.enumerated() {
            let a = k < va.count ? va[k] : []
            d.wireIds.append(fnv1a64(attrStr(a, "was") ?? v))
            d.deprecated.append(hasTag(a, "deprecated"))
        }
    }
    for (n, d) in u.flags {
        d.width = d.variants.count
        if case let .expr(e)? = attr(u.flagsAttrs[n] ?? [], "max") { d.width = try u.intOf(e) }
    }
    for (n, d) in u.tables {
        let a = u.tableAttrs[n] ?? []
        if let w = attrStr(a, "was") { d.wireId = fnv1a64(w) }
        if attr(a, "integrity") != nil { d.integrity = true }
        if attrStr(a, "json") == "array" { d.jsonArray = true }
    }
    for (n, d) in u.unions {
        for (an, ty, a) in u.armASTs[n] ?? [] {
            let arm = Arm(name: an)
            if let w = attrStr(a, "was") { arm.wireId = fnv1a64(w) }
            if let ty = ty { arm.ft = try resolveType(u, ty, a) }
            d.arms.append(arm)
        }
    }
    for n in u.tableOrder {
        let d = u.tables[n]!
        for fa in u.fieldASTs[n] ?? [] {
            let f = Field(name: fa.name)
            if let w = attrStr(fa.attrs, "was") { f.wireId = fnv1a64(w) }
            if let j = attrStr(fa.attrs, "json") { f.jsonKey = j }
            f.optional = fa.optional
            f.required = hasTag(fa.attrs, "required")
            f.deprecated = hasTag(fa.attrs, "deprecated")
            f.guards = fa.guards
            if case let .expr(e)? = attr(fa.attrs, "total") { f.total = try u.intOf(e) }
            f.ft = try resolveType(u, fa.type, fa.attrs)
            if let def = fa.def { f.def = try defaultValue(u, f.ft, def) }
            d.fields.append(f)
        }
        for (i, f) in d.fields.enumerated() {
            d.byWireId[f.wireId] = i
            d.byKey[f.jsonKey] = i
        }
        for f in d.fields {
            f.guardIdx = try f.guards.map { g in
                guard let gi = d.fields.firstIndex(where: { $0.name == g.field }) else { throw Refusal("guard \(g.field) names no field") }
                return (gi, g.negate)
            }
        }
    }
}

func rangeBig(_ u: Unit, _ a: [Attr]) throws -> (Big?, Big?) {
    var lo: Big? = nil, hi: Big? = nil
    if case let .expr(e)? = attr(a, "min") { lo = try u.evalInt(e) }
    if case let .expr(e)? = attr(a, "max") { hi = try u.evalInt(e) }
    return (lo, hi)
}

func resolveType(_ u: Unit, _ t: TypeAST, _ a: [Attr]) throws -> FT {
    switch t {
    case let .scalar(w, args):
        switch w {
        case "bool": return .bool
        case "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "int128", "uint128":
            let signed = !w.hasPrefix("u")
            let width = Int(w.drop(while: { !$0.isNumber }))!
            let (lo, hi) = try rangeBig(u, a)
            return .int(width: width, signed: signed, lo: lo, hi: hi)
        case "bits": return .bits(try u.intOf(args[0]))
        case "float32", "float64":
            var lo: Double? = nil, hi: Double? = nil
            if case let .expr(e)? = attr(a, "min") { lo = try u.evalFloat(e) }
            if case let .expr(e)? = attr(a, "max") { hi = try u.evalFloat(e) }
            return .float(width: w == "float32" ? 32 : 64, lo: lo, hi: hi)
        case "string": return .str(args.isEmpty ? nil : try u.intOf(args[0]))
        case "bytes": return .bytes(try u.intOf(args[0]))
        case "timestamp": return .timestamp
        case "fixed", "ufixed":
            let i = try u.intOf(args[0]), f = try u.intOf(args[1])
            var (lo, hi) = try rangeBig(u, a)
            lo = try lo?.shl(f); hi = try hi?.shl(f)
            return .fixed(signed: w == "fixed", i: i, f: f, lo: lo, hi: hi)
        default: throw Refusal("unknown scalar \(w)")
        }
    case let .named(n):
        if let e = u.enums[n] { return .enm(e) }
        if let f = u.flags[n] { return .flags(f) }
        if let d = u.tables[n] { return .table(d) }
        if let un = u.unions[n] { return .union(un) }
        throw Refusal("unknown type \(n)")
    case let .pointer(n):
        if n == "bytes" { return .blob(isString: false) }
        if n == "string" { return .blob(isString: true) }
        guard let d = u.tables[n] else { throw Refusal("pointer to unknown table \(n)") }
        return .ptr(d)
    case let .array(b, et, eopt):
        let el = try resolveType(u, et, a)
        switch b {
        case .none: return .array(el, .unbounded(min: 0), elemOpt: eopt)
        case let .upTo(e): return .array(el, .counted(0, try u.intOf(e)), elemOpt: eopt)
        case let .range(x, y): return .array(el, .counted(try u.intOf(x), try u.intOf(y)), elemOpt: eopt)
        case let .exact(e):
            if case let .name(n) = e, let en = u.enums[n] { return .keyed(en, el) }
            return .array(el, .fixed(try u.intOf(e)), elemOpt: eopt)
        }
    case let .map(k, v, vopt):
        return .map(try resolveType(u, k, []), try resolveType(u, v, a), valOpt: vopt)
    }
}

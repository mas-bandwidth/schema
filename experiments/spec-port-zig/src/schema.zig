// Schema intake: parse a unit's `.schema` text and resolve it into the
// declaration model the codecs walk (docs/SPEC.md §3.2, §4; SPEC-TABLES.md §2).
//
// SPEC-GAP 1: the spec describes an IR the Go compiler hands its backends
// (SPEC.md §7.1) but defines no serialized IR a separate port could read, so
// this port parses the `.schema` text itself.
const std = @import("std");
const lex = @import("lex.zig");
const Token = lex.Token;
const Allocator = std.mem.Allocator;

pub const Int = i129; // every value an integer field can hold, int128 and uint128 included
pub const Big = i256; // constant folding (SPEC.md §4.2: arbitrary precision, fit-checked at use)

pub const ParseError = error{ ParseError, LexError, OutOfMemory, Unresolved };

/// fnv1a64 (SPEC-TABLES.md §5): the one hash every vocabulary takes.
pub fn fnv1a64(s: []const u8) u64 {
    var h: u64 = 0xcbf29ce484222325;
    for (s) |b| {
        h ^= b;
        h *%= 0x100000001b3;
    }
    return h;
}

pub const node_table_id: u64 = 0xFFFFFFFFFFFFFFFF;
pub const build_version_id: u64 = 0xFFFFFFFFFFFFFFFE;
pub const vocabulary_id: u64 = 0xFFFFFFFFFFFFFFFD;
pub const map_key_id: u64 = 0x3DC94A19365B10EC;
pub const map_value_id: u64 = 0x7CE4FD9430E80CEA;

// ---------------------------------------------------------------- model

pub const IntT = struct {
    width: u8, // storage bits: 8, 16, 32, 64, 128
    signed: bool,
    min: Int,
    max: Int,
    bits_n: u8 = 0, // nonzero for bits(N)
};

pub const FixedT = struct {
    width: u8,
    signed: bool,
    i_bits: u8,
    f_bits: u8,
    min_raw: Int,
    max_raw: Int,
};

pub const Compressed = struct { min: f64, max: f64, res: f64 };

pub const FloatT = struct {
    wide: bool,
    compressed: ?Compressed = null,
};

pub const ArrayT = struct {
    elem: *Type,
    elem_optional: bool = false,
    min: u64, // born count ([A..B]); [N] has min = max = N
    max: ?u64, // null: unbounded []T
    exact: bool, // [N]
    total: ?u64 = null, // text-list `total` (SPEC-TABLES.md §2.13)
};

pub const KeyedT = struct { key: *EnumDecl, elem: *Type };

pub const MapT = struct { key: *Type, val: *Type, val_optional: bool };

pub const Type = union(enum) {
    boolean,
    int: IntT,
    fixed: FixedT,
    float: FloatT,
    timestamp,
    string: ?u64, // string(N), or null for unbounded `string`
    bytes: u64, // bytes(N)
    enum_: *EnumDecl,
    flags: *FlagsDecl,
    struct_: *StructDecl,
    union_: *UnionDecl,
    array: ArrayT,
    keyed: KeyedT,
    map: MapT,
    pointer: *StructDecl,
    blob_bytes,
    blob_string,
};

pub const Variant = struct {
    name: []const u8,
    id: u64,
    deprecated: bool = false,
};

pub const EnumDecl = struct {
    name: []const u8,
    variants: []Variant, // ordinal k is variants[k-1]
    pub fn byId(self: *const EnumDecl, id: u64) ?u32 {
        for (self.variants, 0..) |v, i| if (v.id == id) return @intCast(i + 1);
        return null;
    }
    pub fn byName(self: *const EnumDecl, name: []const u8) ?u32 {
        for (self.variants, 0..) |v, i| if (std.mem.eql(u8, v.name, name)) return @intCast(i + 1);
        return null;
    }
};

pub const FlagsDecl = struct {
    name: []const u8,
    names: [][]const u8, // bit k is names[k]
    pub fn byName(self: *const FlagsDecl, name: []const u8) ?u6 {
        for (self.names, 0..) |n, i| if (std.mem.eql(u8, n, name)) return @intCast(i);
        return null;
    }
};

pub const Guard = struct { field: u32, want: bool };

pub const Field = struct {
    name: []const u8,
    id: u64,
    key: []const u8, // JSON key
    ty: *Type,
    optional: bool,
    required: bool,
    deprecated: bool,
    default: ?DefaultSpec, // specified default, unresolved to a value here
    guards: []Guard,
};

pub const DefaultSpec = union(enum) {
    expr: *Expr,
    str: []const u8,
    names: [][]const u8,
};

pub const StructDecl = struct {
    name: []const u8,
    id: u64, // table type id: fnv1a64(was or name)
    is_table: bool,
    is_fixed: bool,
    json_array: bool = false,
    integrity: bool = false,
    fields: []Field,
    unit: ?*const Unit = null,
    pub fn unit_ptr(self: *const StructDecl) ?*const Unit {
        return self.unit;
    }
    pub fn fieldById(self: *const StructDecl, id: u64) ?u32 {
        for (self.fields, 0..) |f, i| if (f.id == id) return @intCast(i);
        return null;
    }
    pub fn fieldByKey(self: *const StructDecl, key: []const u8) ?u32 {
        for (self.fields, 0..) |f, i| if (std.mem.eql(u8, f.key, key)) return @intCast(i);
        return null;
    }
};

pub const Arm = struct {
    name: []const u8,
    id: u64,
    ty: ?*Type, // null: payload-free arm
};

pub const UnionDecl = struct {
    name: []const u8,
    arms: []Arm, // tag k is arms[k-1]
    pub fn byId(self: *const UnionDecl, id: u64) ?u32 {
        for (self.arms, 0..) |a, i| if (a.id == id) return @intCast(i + 1);
        return null;
    }
    pub fn byName(self: *const UnionDecl, name: []const u8) ?u32 {
        for (self.arms, 0..) |a, i| if (std.mem.eql(u8, a.name, name)) return @intCast(i + 1);
        return null;
    }
};

pub const Unit = struct {
    arena: Allocator,
    structs: std.StringHashMapUnmanaged(*StructDecl) = .empty,
    enums: std.StringHashMapUnmanaged(*EnumDecl) = .empty,
    flagss: std.StringHashMapUnmanaged(*FlagsDecl) = .empty,
    unions: std.StringHashMapUnmanaged(*UnionDecl) = .empty,
    consts: std.StringHashMapUnmanaged(*Expr) = .empty,
    /// every table by type id, for node records
    tables_by_id: std.AutoHashMapUnmanaged(u64, *StructDecl) = .empty,

    pub fn findStruct(self: *const Unit, name: []const u8) ?*StructDecl {
        return self.structs.get(name);
    }
};

// ---------------------------------------------------------------- expressions

pub const Expr = union(enum) {
    int: Big,
    float: f64,
    float_text: []const u8, // the literal, kept exact for fixed-point defaults
    name: []const u8,
    member: struct { base: []const u8, member: []const u8 },
    neg: *Expr,
    bin: struct { op: u8, a: *Expr, b: *Expr },
};

pub const Num = union(enum) { int: Big, float: f64 };

pub fn evalNum(u: *const Unit, e: *const Expr) ParseError!Num {
    switch (e.*) {
        .int => |v| return .{ .int = v },
        .float => |v| return .{ .float = v },
        .float_text => |t| return .{ .float = std.fmt.parseFloat(f64, t) catch return error.ParseError },
        .name => |n| {
            const c = u.consts.get(n) orelse return error.Unresolved;
            return evalNum(u, c);
        },
        .member => |m| {
            if (std.mem.eql(u8, m.member, "Max")) {
                if (u.enums.get(m.base)) |en| return .{ .int = @intCast(en.variants.len) };
                if (u.unions.get(m.base)) |un| return .{ .int = @intCast(un.arms.len) };
            }
            if (std.mem.eql(u8, m.member, "Count")) {
                if (u.flagss.get(m.base)) |fl| return .{ .int = @intCast(fl.names.len) };
            }
            return error.Unresolved;
        },
        .neg => |x| {
            const v = try evalNum(u, x);
            return switch (v) {
                .int => |i| .{ .int = -i },
                .float => |f| .{ .float = -f },
            };
        },
        .bin => |b| {
            const x = try evalNum(u, b.a);
            const y = try evalNum(u, b.b);
            if (x == .int and y == .int) {
                const a = x.int;
                const c = y.int;
                return .{ .int = switch (b.op) {
                    '+' => a + c,
                    '-' => a - c,
                    '*' => a * c,
                    '/' => if (c == 0) return error.ParseError else @divTrunc(a, c),
                    '%' => if (c == 0) return error.ParseError else @rem(a, c),
                    else => return error.ParseError,
                } };
            }
            const a = toF(x);
            const c = toF(y);
            return .{ .float = switch (b.op) {
                '+' => a + c,
                '-' => a - c,
                '*' => a * c,
                '/' => a / c,
                else => return error.ParseError,
            } };
        },
    }
}

pub fn toF(n: Num) f64 {
    return switch (n) {
        .int => |i| @floatFromInt(i),
        .float => |f| f,
    };
}

pub fn evalInt(u: *const Unit, e: *const Expr) ParseError!Big {
    const n = try evalNum(u, e);
    return switch (n) {
        .int => |i| i,
        .float => error.ParseError,
    };
}

// ---------------------------------------------------------------- raw AST

const Attr = struct {
    key: []const u8,
    expr: ?*Expr = null,
    str: ?[]const u8 = null,
    word: ?[]const u8 = null,
};

const BoundExpr = union(enum) {
    exact: *Expr, // [N] or [E] (a bare name may be an enum: keyed)
    upto: *Expr, // [..N]
    range: struct { lo: *Expr, hi: *Expr }, // [A..B]
    unbounded, // []
};

const BaseExpr = union(enum) {
    named: []const u8,
    int: struct { width: u8, signed: bool },
    int128: bool, // signed?
    bits: *Expr,
    boolean,
    f32,
    f64,
    string: ?*Expr,
    bytes: *Expr,
    timestamp,
    fixed: struct { signed: bool, i: *Expr, f: *Expr },
    pointer: []const u8,
    map: struct { key: *BaseExpr, val: *TypeExpr },
};

const TypeExpr = struct {
    optional: bool,
    bound: ?BoundExpr,
    elem_optional: bool, // `[]?string(8)`
    base: *BaseExpr,
};

const RawField = struct {
    name: []const u8,
    ty: *TypeExpr,
    default: ?DefaultSpec,
    attrs: []Attr,
    guards: []RawGuard,
};

const RawGuard = struct { name: []const u8, want: bool };

const RawStruct = struct {
    decl: *StructDecl,
    fields: []RawField,
    attrs: []Attr,
};

const RawArm = struct { name: []const u8, ty: ?*TypeExpr, attrs: []Attr };
const RawUnion = struct { decl: *UnionDecl, arms: []RawArm };

// ---------------------------------------------------------------- parser

const Parser = struct {
    a: Allocator,
    toks: []const Token,
    pos: usize = 0,
    unit: *Unit,
    raw_structs: *std.ArrayList(RawStruct),
    raw_unions: *std.ArrayList(RawUnion),

    fn peek(p: *Parser) Token {
        return p.toks[p.pos];
    }
    fn peekAt(p: *Parser, k: usize) Token {
        const i = @min(p.pos + k, p.toks.len - 1);
        return p.toks[i];
    }
    fn next(p: *Parser) Token {
        const t = p.toks[p.pos];
        if (p.pos < p.toks.len - 1) p.pos += 1;
        return t;
    }
    fn isWord(p: *Parser, w: []const u8) bool {
        const t = p.peek();
        return t.tag == .ident and std.mem.eql(u8, t.text, w);
    }
    fn expect(p: *Parser, tag: lex.Tag) ParseError!Token {
        const t = p.next();
        if (t.tag != tag) return error.ParseError;
        return t;
    }
    fn skipNL(p: *Parser) void {
        while (p.peek().tag == .nl) _ = p.next();
    }
    fn endItem(p: *Parser) ParseError!void {
        // a trailing NL is satisfied by a newline or the following `}`
        const t = p.peek();
        if (t.tag == .nl) {
            _ = p.next();
            return;
        }
        if (t.tag == .rbrace or t.tag == .eof) return;
        return error.ParseError;
    }

    fn file(p: *Parser) ParseError!void {
        while (true) {
            p.skipNL();
            const t = p.peek();
            if (t.tag == .eof) return;
            if (t.tag != .ident) return error.ParseError;
            if (std.mem.eql(u8, t.text, "package")) {
                _ = p.next();
                _ = try p.expect(.ident);
                try p.endItem();
            } else if (std.mem.eql(u8, t.text, "const")) {
                _ = p.next();
                const name = (try p.expect(.ident)).text;
                if (p.peek().tag == .ident) _ = p.next(); // ConstType
                _ = try p.expect(.eq);
                const e = try p.expr();
                if (p.peek().tag == .pipe) _ = try p.attrs();
                try p.endItem();
                try p.unit.consts.put(p.a, name, e);
            } else if (std.mem.eql(u8, t.text, "enum")) {
                _ = p.next();
                try p.enumDecl();
            } else if (std.mem.eql(u8, t.text, "flags")) {
                _ = p.next();
                try p.flagsDecl();
            } else if (std.mem.eql(u8, t.text, "union")) {
                _ = p.next();
                try p.unionDecl();
            } else if (std.mem.eql(u8, t.text, "type")) {
                _ = p.next();
                try p.structDecl(false, false);
            } else if (std.mem.eql(u8, t.text, "table")) {
                _ = p.next();
                try p.structDecl(true, false);
            } else if (std.mem.eql(u8, t.text, "fixed") and p.peekAt(1).tag == .ident and std.mem.eql(u8, p.peekAt(1).text, "table")) {
                _ = p.next();
                _ = p.next();
                try p.structDecl(true, true);
            } else return error.ParseError;
        }
    }

    /// `| a, b = expr, c = "s"` up to the end of the line (SPEC.md §4.2).
    /// SPEC-GAP 2: the grammar says a qualified declaration's body opens on
    /// the NEXT line, but the corpus writes `table Root | forms="..." {`; a
    /// `{` also ends the section here.
    fn attrs(p: *Parser) ParseError![]Attr {
        _ = try p.expect(.pipe);
        var list: std.ArrayList(Attr) = .empty;
        while (true) {
            const k = try p.expect(.ident);
            var at: Attr = .{ .key = k.text };
            if (p.peek().tag == .eq) {
                _ = p.next();
                const t = p.peek();
                if (t.tag == .string) {
                    at.str = p.next().text;
                } else if (t.tag == .ident and p.peekAt(1).tag != .dot and p.unit.consts.get(t.text) == null and isWordValued(k.text)) {
                    at.word = p.next().text;
                    while (p.peek().tag == .coloncolon) {
                        _ = p.next();
                        _ = try p.expect(.ident);
                    }
                } else {
                    at.expr = try p.expr();
                }
            }
            try list.append(p.a, at);
            if (p.peek().tag == .comma) {
                _ = p.next();
                continue;
            }
            break;
        }
        const t = p.peek();
        if (t.tag != .nl and t.tag != .lbrace and t.tag != .rbrace and t.tag != .eof) return error.ParseError;
        return list.items;
    }

    fn isWordValued(key: []const u8) bool {
        return std.mem.eql(u8, key, "cpp_native");
    }

    fn variantList(p: *Parser, names: *std.ArrayList(Variant)) ParseError!void {
        p.skipNL();
        _ = try p.expect(.lbrace);
        while (true) {
            p.skipNL();
            if (p.peek().tag == .rbrace) {
                _ = p.next();
                return;
            }
            const n = try p.expect(.ident);
            var v: Variant = .{ .name = n.text, .id = fnv1a64(n.text) };
            if (p.peek().tag == .pipe) {
                const as = try p.attrs();
                for (as) |a| {
                    if (std.mem.eql(u8, a.key, "was")) {
                        if (a.str) |s| v.id = fnv1a64(s);
                    } else if (std.mem.eql(u8, a.key, "deprecated")) v.deprecated = true;
                }
            }
            try names.append(p.a, v);
            if (p.peek().tag == .comma) _ = p.next();
        }
    }

    fn enumDecl(p: *Parser) ParseError!void {
        const name = (try p.expect(.ident)).text;
        if (p.peek().tag == .pipe) _ = try p.attrs();
        var vs: std.ArrayList(Variant) = .empty;
        try p.variantList(&vs);
        try p.endItem();
        const d = try p.a.create(EnumDecl);
        d.* = .{ .name = name, .variants = vs.items };
        try p.unit.enums.put(p.a, name, d);
    }

    fn flagsDecl(p: *Parser) ParseError!void {
        const name = (try p.expect(.ident)).text;
        if (p.peek().tag == .pipe) _ = try p.attrs();
        var vs: std.ArrayList(Variant) = .empty;
        try p.variantList(&vs);
        try p.endItem();
        var ns = try p.a.alloc([]const u8, vs.items.len);
        for (vs.items, 0..) |v, i| ns[i] = v.name;
        const d = try p.a.create(FlagsDecl);
        d.* = .{ .name = name, .names = ns };
        try p.unit.flagss.put(p.a, name, d);
    }

    fn unionDecl(p: *Parser) ParseError!void {
        const name = (try p.expect(.ident)).text;
        if (p.peek().tag == .pipe) _ = try p.attrs();
        p.skipNL();
        _ = try p.expect(.lbrace);
        var arms: std.ArrayList(RawArm) = .empty;
        while (true) {
            p.skipNL();
            if (p.peek().tag == .rbrace) {
                _ = p.next();
                break;
            }
            const an = (try p.expect(.ident)).text;
            var ra: RawArm = .{ .name = an, .ty = null, .attrs = &.{} };
            const t = p.peek();
            if (t.tag != .nl and t.tag != .pipe and t.tag != .rbrace) ra.ty = try p.typeExpr();
            if (p.peek().tag == .pipe) ra.attrs = try p.attrs();
            try p.endItem();
            try arms.append(p.a, ra);
        }
        try p.endItem();
        const d = try p.a.create(UnionDecl);
        d.* = .{ .name = name, .arms = &.{} };
        try p.unit.unions.put(p.a, name, d);
        try p.raw_unions.append(p.a, .{ .decl = d, .arms = arms.items });
    }

    fn structDecl(p: *Parser, is_table: bool, is_fixed: bool) ParseError!void {
        const name = (try p.expect(.ident)).text;
        var as: []Attr = &.{};
        if (p.peek().tag == .pipe) as = try p.attrs();
        p.skipNL();
        var fields: std.ArrayList(RawField) = .empty;
        var guards: std.ArrayList(RawGuard) = .empty;
        try p.block(&fields, &guards);
        try p.endItem();
        const d = try p.a.create(StructDecl);
        d.* = .{ .name = name, .id = fnv1a64(name), .is_table = is_table, .is_fixed = is_fixed, .fields = &.{}, .unit = p.unit };
        for (as) |a| {
            if (std.mem.eql(u8, a.key, "was")) {
                if (a.str) |s| d.id = fnv1a64(s);
            } else if (std.mem.eql(u8, a.key, "json")) {
                if (a.str) |s| d.json_array = std.mem.eql(u8, s, "array");
            } else if (std.mem.eql(u8, a.key, "integrity")) {
                d.integrity = true;
            }
        }
        try p.unit.structs.put(p.a, name, d);
        try p.raw_structs.append(p.a, .{ .decl = d, .fields = fields.items, .attrs = as });
    }

    fn block(p: *Parser, fields: *std.ArrayList(RawField), guards: *std.ArrayList(RawGuard)) ParseError!void {
        _ = try p.expect(.lbrace);
        while (true) {
            p.skipNL();
            const t = p.peek();
            if (t.tag == .rbrace) {
                _ = p.next();
                return;
            }
            if (t.tag != .ident) return error.ParseError;
            if (std.mem.eql(u8, t.text, "if")) {
                _ = p.next();
                var want = true;
                if (p.peek().tag == .bang) {
                    _ = p.next();
                    want = false;
                }
                const g = (try p.expect(.ident)).text;
                p.skipNL();
                try guards.append(p.a, .{ .name = g, .want = want });
                try p.block(fields, guards);
                _ = guards.pop();
                // optional else, possibly after newlines
                var save = p.pos;
                p.skipNL();
                if (p.isWord("else")) {
                    _ = p.next();
                    p.skipNL();
                    try guards.append(p.a, .{ .name = g, .want = !want });
                    try p.block(fields, guards);
                    _ = guards.pop();
                    save = p.pos;
                } else p.pos = save;
                try p.endItem();
                continue;
            }
            const fname = p.next().text;
            const ty = try p.typeExpr();
            var def: ?DefaultSpec = null;
            if (p.peek().tag == .eq) {
                _ = p.next();
                if (p.peek().tag == .string) {
                    def = .{ .str = p.next().text };
                } else if (p.peek().tag == .lbrace) {
                    _ = p.next();
                    var ns: std.ArrayList([]const u8) = .empty;
                    while (p.peek().tag != .rbrace) {
                        p.skipNL();
                        if (p.peek().tag == .rbrace) break;
                        try ns.append(p.a, (try p.expect(.ident)).text);
                        p.skipNL();
                        if (p.peek().tag == .comma) _ = p.next();
                    }
                    _ = try p.expect(.rbrace);
                    def = .{ .names = ns.items };
                } else {
                    def = .{ .expr = try p.expr() };
                }
            }
            var as: []Attr = &.{};
            if (p.peek().tag == .pipe) as = try p.attrs();
            try p.endItem();
            try fields.append(p.a, .{
                .name = fname,
                .ty = ty,
                .default = def,
                .attrs = as,
                .guards = try p.a.dupe(RawGuard, guards.items),
            });
        }
    }

    fn typeExpr(p: *Parser) ParseError!*TypeExpr {
        const te = try p.a.create(TypeExpr);
        te.* = .{ .optional = false, .bound = null, .elem_optional = false, .base = undefined };
        if (p.peek().tag == .question) {
            _ = p.next();
            te.optional = true;
        }
        if (p.peek().tag == .lbrack) {
            _ = p.next();
            if (p.peek().tag == .rbrack) {
                te.bound = .unbounded;
            } else if (p.peek().tag == .dotdot) {
                _ = p.next();
                te.bound = .{ .upto = try p.expr() };
            } else {
                const lo = try p.expr();
                if (p.peek().tag == .dotdot) {
                    _ = p.next();
                    te.bound = .{ .range = .{ .lo = lo, .hi = try p.expr() } };
                } else te.bound = .{ .exact = lo };
            }
            _ = try p.expect(.rbrack);
            if (p.peek().tag == .question) {
                _ = p.next();
                te.elem_optional = true;
            }
        }
        te.base = try p.baseExpr();
        return te;
    }

    fn baseExpr(p: *Parser) ParseError!*BaseExpr {
        const b = try p.a.create(BaseExpr);
        if (p.peek().tag == .star) {
            _ = p.next();
            b.* = .{ .pointer = (try p.expect(.ident)).text };
            return b;
        }
        const t = try p.expect(.ident);
        const w = t.text;
        const eq = std.mem.eql;
        if (eq(u8, w, "map") and p.peek().tag == .lbrack) {
            _ = p.next();
            const k = try p.baseExpr();
            _ = try p.expect(.rbrack);
            const v = try p.typeExpr();
            b.* = .{ .map = .{ .key = k, .val = v } };
        } else if (eq(u8, w, "bool")) {
            b.* = .boolean;
        } else if (eq(u8, w, "float32")) {
            b.* = .f32;
        } else if (eq(u8, w, "float64")) {
            b.* = .f64;
        } else if (eq(u8, w, "timestamp")) {
            b.* = .timestamp;
        } else if (eq(u8, w, "int128")) {
            b.* = .{ .int128 = true };
        } else if (eq(u8, w, "uint128")) {
            b.* = .{ .int128 = false };
        } else if (eq(u8, w, "bits")) {
            _ = try p.expect(.lparen);
            const e = try p.expr();
            _ = try p.expect(.rparen);
            b.* = .{ .bits = e };
        } else if (eq(u8, w, "string")) {
            if (p.peek().tag == .lparen) {
                _ = p.next();
                const e = try p.expr();
                _ = try p.expect(.rparen);
                b.* = .{ .string = e };
            } else b.* = .{ .string = null };
        } else if (eq(u8, w, "bytes")) {
            _ = try p.expect(.lparen);
            const e = try p.expr();
            _ = try p.expect(.rparen);
            b.* = .{ .bytes = e };
        } else if (eq(u8, w, "fixed") or eq(u8, w, "ufixed")) {
            _ = try p.expect(.lparen);
            const i = try p.expr();
            _ = try p.expect(.comma);
            const f = try p.expr();
            _ = try p.expect(.rparen);
            b.* = .{ .fixed = .{ .signed = eq(u8, w, "fixed"), .i = i, .f = f } };
        } else if (intKind(w)) |ik| {
            b.* = .{ .int = .{ .width = ik.width, .signed = ik.signed } };
        } else {
            b.* = .{ .named = w };
        }
        return b;
    }

    // expressions: + - over * / % over unary over primary
    fn expr(p: *Parser) ParseError!*Expr {
        var a = try p.term();
        while (p.peek().tag == .plus or p.peek().tag == .minus) {
            const op = p.next().text[0];
            const b = try p.term();
            const e = try p.a.create(Expr);
            e.* = .{ .bin = .{ .op = op, .a = a, .b = b } };
            a = e;
        }
        return a;
    }
    fn term(p: *Parser) ParseError!*Expr {
        var a = try p.unary();
        while (p.peek().tag == .star or p.peek().tag == .slash or p.peek().tag == .percent) {
            const op = p.next().text[0];
            const b = try p.unary();
            const e = try p.a.create(Expr);
            e.* = .{ .bin = .{ .op = op, .a = a, .b = b } };
            a = e;
        }
        return a;
    }
    fn unary(p: *Parser) ParseError!*Expr {
        if (p.peek().tag == .minus) {
            _ = p.next();
            const x = try p.unary();
            const e = try p.a.create(Expr);
            // keep a negated float literal's text exact
            if (x.* == .float_text) {
                const s = try std.mem.concat(p.a, u8, &.{ "-", x.float_text });
                e.* = .{ .float_text = s };
                return e;
            }
            e.* = .{ .neg = x };
            return e;
        }
        if (p.peek().tag == .plus) {
            _ = p.next();
            return p.unary();
        }
        return p.primary();
    }
    fn primary(p: *Parser) ParseError!*Expr {
        const t = p.next();
        const e = try p.a.create(Expr);
        switch (t.tag) {
            .int => e.* = .{ .int = try parseIntLit(t.text) },
            .float => e.* = .{ .float_text = t.text },
            .ident => {
                if (p.peek().tag == .dot and p.peekAt(1).tag == .ident) {
                    _ = p.next();
                    const m = p.next().text;
                    e.* = .{ .member = .{ .base = t.text, .member = m } };
                } else e.* = .{ .name = t.text };
            },
            .lparen => {
                const x = try p.expr();
                _ = try p.expect(.rparen);
                return x;
            },
            else => return error.ParseError,
        }
        return e;
    }
};

fn parseIntLit(s: []const u8) ParseError!Big {
    var base: u8 = 10;
    var digits = s;
    if (s.len > 2 and s[0] == '0' and (s[1] == 'x' or s[1] == 'X')) {
        base = 16;
        digits = s[2..];
    } else if (s.len > 2 and s[0] == '0' and (s[1] == 'b' or s[1] == 'B')) {
        base = 2;
        digits = s[2..];
    } else if (s.len > 1 and s[0] == '0') return error.ParseError; // SPEC.md §4.1: no octal-looking decimal
    var v: Big = 0;
    for (digits) |c| {
        if (c == '_') continue;
        const d = std.fmt.charToDigit(c, base) catch return error.ParseError;
        v = v * base + d;
    }
    return v;
}

const IK = struct { width: u8, signed: bool };
fn intKind(w: []const u8) ?IK {
    const tbl = [_]struct { []const u8, IK }{
        .{ "int8", .{ .width = 8, .signed = true } },
        .{ "int16", .{ .width = 16, .signed = true } },
        .{ "int32", .{ .width = 32, .signed = true } },
        .{ "int64", .{ .width = 64, .signed = true } },
        .{ "uint8", .{ .width = 8, .signed = false } },
        .{ "uint16", .{ .width = 16, .signed = false } },
        .{ "uint32", .{ .width = 32, .signed = false } },
        .{ "uint64", .{ .width = 64, .signed = false } },
    };
    for (tbl) |e| if (std.mem.eql(u8, e[0], w)) return e[1];
    return null;
}

pub fn intLimits(width: u8, signed: bool) struct { min: Int, max: Int } {
    if (signed) {
        const one: Int = 1;
        return .{ .min = -(one << @intCast(width - 1)), .max = (one << @intCast(width - 1)) - 1 };
    }
    const one: Int = 1;
    return .{ .min = 0, .max = (one << @intCast(width)) - 1 };
}

// ---------------------------------------------------------------- resolver

const Resolver = struct {
    a: Allocator,
    u: *Unit,

    fn fitInt(v: Big) ParseError!Int {
        const lo: Big = std.math.minInt(Int);
        const hi: Big = std.math.maxInt(Int);
        if (v < lo or v > hi) return error.ParseError;
        return @intCast(v);
    }

    fn attr(as: []Attr, key: []const u8) ?Attr {
        for (as) |a| if (std.mem.eql(u8, a.key, key)) return a;
        return null;
    }

    fn baseType(r: *Resolver, b: *BaseExpr, as: []Attr) ParseError!*Type {
        const t = try r.a.create(Type);
        switch (b.*) {
            .boolean => t.* = .boolean,
            .timestamp => t.* = .timestamp,
            .f64 => t.* = .{ .float = .{ .wide = true } },
            .f32 => {
                var ft: FloatT = .{ .wide = false };
                if (attr(as, "resolution")) |res| {
                    const mn = attr(as, "min") orelse return error.ParseError;
                    const mx = attr(as, "max") orelse return error.ParseError;
                    ft.compressed = .{
                        .min = toF(try evalNum(r.u, mn.expr.?)),
                        .max = toF(try evalNum(r.u, mx.expr.?)),
                        .res = toF(try evalNum(r.u, res.expr.?)),
                    };
                }
                t.* = .{ .float = ft };
            },
            .int => |ik| {
                const lim = intLimits(ik.width, ik.signed);
                var it: IntT = .{ .width = ik.width, .signed = ik.signed, .min = lim.min, .max = lim.max };
                if (attr(as, "min")) |mn| it.min = try fitInt(try evalInt(r.u, mn.expr.?));
                if (attr(as, "max")) |mx| it.max = try fitInt(try evalInt(r.u, mx.expr.?));
                t.* = .{ .int = it };
            },
            .int128 => |signed| {
                const lim = intLimits(128, signed);
                var it: IntT = .{ .width = 128, .signed = signed, .min = lim.min, .max = lim.max };
                if (attr(as, "min")) |mn| it.min = try fitInt(try evalInt(r.u, mn.expr.?));
                if (attr(as, "max")) |mx| it.max = try fitInt(try evalInt(r.u, mx.expr.?));
                t.* = .{ .int = it };
            },
            .bits => |e| {
                const n: u8 = @intCast(try evalInt(r.u, e));
                const w: u8 = if (n <= 8) 8 else if (n <= 16) 16 else if (n <= 32) 32 else 64;
                const one: Int = 1;
                t.* = .{ .int = .{ .width = w, .signed = false, .min = 0, .max = (one << @intCast(n)) - 1, .bits_n = n } };
            },
            .string => |e| t.* = .{ .string = if (e) |x| @intCast(try evalInt(r.u, x)) else null },
            .bytes => |e| t.* = .{ .bytes = @intCast(try evalInt(r.u, e)) },
            .fixed => |fx| {
                const ib: u8 = @intCast(try evalInt(r.u, fx.i));
                const fb: u8 = @intCast(try evalInt(r.u, fx.f));
                const mn = attr(as, "min") orelse return error.ParseError;
                const mx = attr(as, "max") orelse return error.ParseError;
                const lo = try evalInt(r.u, mn.expr.?);
                const hi = try evalInt(r.u, mx.expr.?);
                t.* = .{ .fixed = .{
                    .width = ib + fb,
                    .signed = fx.signed,
                    .i_bits = ib,
                    .f_bits = fb,
                    .min_raw = try fitInt(lo << @intCast(fb)),
                    .max_raw = try fitInt(hi << @intCast(fb)),
                } };
            },
            .pointer => |n| {
                if (std.mem.eql(u8, n, "bytes")) {
                    t.* = .blob_bytes;
                } else if (std.mem.eql(u8, n, "string")) {
                    t.* = .blob_string;
                } else {
                    t.* = .{ .pointer = r.u.structs.get(n) orelse return error.Unresolved };
                }
            },
            .named => |n| {
                if (r.u.structs.get(n)) |s| {
                    t.* = .{ .struct_ = s };
                } else if (r.u.enums.get(n)) |e| {
                    t.* = .{ .enum_ = e };
                } else if (r.u.flagss.get(n)) |f| {
                    t.* = .{ .flags = f };
                } else if (r.u.unions.get(n)) |un| {
                    t.* = .{ .union_ = un };
                } else return error.Unresolved;
            },
            .map => |m| {
                const k = try r.baseType(m.key, &.{});
                const v = try r.typeOf(m.val, &.{});
                t.* = .{ .map = .{ .key = k, .val = v, .val_optional = m.val.optional } };
            },
        }
        return t;
    }

    /// The type a field line declares, without its `?`.
    fn typeOf(r: *Resolver, te: *TypeExpr, as: []Attr) ParseError!*Type {
        const base = try r.baseType(te.base, as);
        const bound = te.bound orelse return base;
        const t = try r.a.create(Type);
        switch (bound) {
            .unbounded => t.* = .{ .array = .{ .elem = base, .elem_optional = te.elem_optional, .min = 0, .max = null, .exact = false } },
            .upto => |e| t.* = .{ .array = .{ .elem = base, .elem_optional = te.elem_optional, .min = 0, .max = @intCast(try evalInt(r.u, e)), .exact = false } },
            .range => |rg| t.* = .{ .array = .{
                .elem = base,
                .elem_optional = te.elem_optional,
                .min = @intCast(try evalInt(r.u, rg.lo)),
                .max = @intCast(try evalInt(r.u, rg.hi)),
                .exact = false,
            } },
            .exact => |e| {
                if (e.* == .name) {
                    if (r.u.enums.get(e.name)) |en| {
                        t.* = .{ .keyed = .{ .key = en, .elem = base } };
                        return t;
                    }
                }
                const n: u64 = @intCast(try evalInt(r.u, e));
                t.* = .{ .array = .{ .elem = base, .elem_optional = te.elem_optional, .min = n, .max = n, .exact = true } };
            },
        }
        if (t.* == .array) {
            if (attr(as, "total")) |tot| t.array.total = @intCast(try evalInt(r.u, tot.expr.?));
        }
        return t;
    }
};

/// Load a unit: one `.schema` file, or every `.schema` file in a directory.
pub fn loadUnit(a: Allocator, sources: []const []const u8) ParseError!*Unit {
    const u = try a.create(Unit);
    u.* = .{ .arena = a };
    var raw_structs: std.ArrayList(RawStruct) = .empty;
    var raw_unions: std.ArrayList(RawUnion) = .empty;
    for (sources) |src| {
        var toks: std.ArrayList(Token) = .empty;
        try lex.lex(a, src, &toks);
        var p: Parser = .{ .a = a, .toks = toks.items, .unit = u, .raw_structs = &raw_structs, .raw_unions = &raw_unions };
        try p.file();
    }
    var r: Resolver = .{ .a = a, .u = u };
    for (raw_unions.items) |ru| {
        var arms = try a.alloc(Arm, ru.arms.len);
        for (ru.arms, 0..) |ra, i| {
            arms[i] = .{ .name = ra.name, .id = fnv1a64(ra.name), .ty = null };
            if (Resolver.attr(ra.attrs, "was")) |w| arms[i].id = fnv1a64(w.str.?);
            if (ra.ty) |te| arms[i].ty = try r.typeOf(te, ra.attrs);
        }
        ru.decl.arms = arms;
    }
    for (raw_structs.items) |rs| {
        var fields = try a.alloc(Field, rs.fields.len);
        for (rs.fields, 0..) |rf, i| {
            var f: Field = .{
                .name = rf.name,
                .id = fnv1a64(rf.name),
                .key = rf.name,
                .ty = try r.typeOf(rf.ty, rf.attrs),
                .optional = rf.ty.optional,
                .required = Resolver.attr(rf.attrs, "required") != null,
                .deprecated = Resolver.attr(rf.attrs, "deprecated") != null,
                .default = rf.default,
                .guards = &.{},
            };
            if (Resolver.attr(rf.attrs, "was")) |w| f.id = fnv1a64(w.str.?);
            if (Resolver.attr(rf.attrs, "json")) |j| f.key = j.str.?;
            fields[i] = f;
        }
        // guards name a bool field declared earlier in the same body
        for (rs.fields, 0..) |rf, i| {
            var gs = try a.alloc(Guard, rf.guards.len);
            for (rf.guards, 0..) |g, k| {
                var idx: ?u32 = null;
                for (rs.fields[0..i], 0..) |prev, j| {
                    if (std.mem.eql(u8, prev.name, g.name)) idx = @intCast(j);
                }
                gs[k] = .{ .field = idx orelse return error.Unresolved, .want = g.want };
            }
            fields[i].guards = gs;
        }
        rs.decl.fields = fields;
        rs.decl.unit = u;
        if (rs.decl.is_table) try u.tables_by_id.put(a, rs.decl.id, rs.decl);
    }
    return u;
}

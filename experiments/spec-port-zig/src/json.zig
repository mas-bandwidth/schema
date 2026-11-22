// The text form (SPEC-TABLES.md §16): the one read (§16.2, §16.3, §16.7)
// and the canonical writer (§16.14, W0 - W14).
//
// The read is two steps: a strict RFC 8259 parse into a generic tree that
// holds the dialect's own rules (comments, trailing commas, a leading BOM,
// the depth cap, repeated keys, UTF-8 and surrogates, `\u0000`), then a
// typed walk that places the tree against the declarations.
const std = @import("std");
const schema = @import("schema.zig");
const value = @import("value.zig");
const wire = @import("wire.zig");
const Type = schema.Type;
const Int = schema.Int;
const Value = value.Value;
const Allocator = std.mem.Allocator;

pub const Error = wire.Error;
const refuse = wireRefuse;
fn wireRefuse(reason: []const u8) Error {
    wire.last_reason = reason;
    return error.Refused;
}

pub const max_depth: u32 = 128;
pub const name_cap: usize = 255;

// ------------------------------------------------------------ the generic tree

pub const Member = struct { key: []const u8, val: JV };

pub const JV = union(enum) {
    null_,
    boolean: bool,
    num: []const u8, // the token, validated against RFC 8259's grammar
    str: []const u8, // decoded UTF-8
    arr: []JV,
    obj: []Member,
};

const Parser = struct {
    a: Allocator,
    s: []const u8,
    p: usize = 0,

    fn ws(ps: *Parser) Error!void {
        while (ps.p < ps.s.len) {
            const c = ps.s[ps.p];
            if (c == ' ' or c == '\t' or c == '\n' or c == '\r') {
                ps.p += 1;
                continue;
            }
            if (c == '/' and ps.p + 1 < ps.s.len and ps.s[ps.p + 1] == '/') {
                const st = ps.p + 2;
                var e = st;
                while (e < ps.s.len and ps.s[e] != '\n') e += 1;
                try commentText(ps.s[st..e]);
                ps.p = e;
                continue;
            }
            if (c == '/' and ps.p + 1 < ps.s.len and ps.s[ps.p + 1] == '*') {
                const st = ps.p + 2;
                const rel = std.mem.indexOf(u8, ps.s[st..], "*/") orelse return refuse("malformed: unclosed comment");
                try commentText(ps.s[st .. st + rel]);
                ps.p = st + rel + 2;
                continue;
            }
            return;
        }
    }

    fn value_(ps: *Parser, depth: u32) Error!JV {
        try ps.ws();
        if (ps.p >= ps.s.len) return refuse("malformed: end of input");
        const c = ps.s[ps.p];
        switch (c) {
            '{' => {
                if (depth > max_depth) return refuse("malformed: depth");
                ps.p += 1;
                var ms: std.ArrayList(Member) = .empty;
                var seen: std.StringHashMapUnmanaged(void) = .empty;
                while (true) {
                    try ps.ws();
                    if (ps.p >= ps.s.len) return refuse("malformed: open object");
                    if (ps.s[ps.p] == '}') {
                        ps.p += 1;
                        break;
                    }
                    if (ps.s[ps.p] != '"') return refuse("malformed: key");
                    const k = try ps.string();
                    const gop = try seen.getOrPut(ps.a, k);
                    if (gop.found_existing) return refuse("malformed: repeated key");
                    try ps.ws();
                    if (ps.p >= ps.s.len or ps.s[ps.p] != ':') return refuse("malformed: colon");
                    ps.p += 1;
                    const v = try ps.value_(depth + 1);
                    try ms.append(ps.a, .{ .key = k, .val = v });
                    try ps.ws();
                    if (ps.p >= ps.s.len) return refuse("malformed: open object");
                    if (ps.s[ps.p] == ',') {
                        ps.p += 1;
                        continue;
                    }
                    if (ps.s[ps.p] == '}') {
                        ps.p += 1;
                        break;
                    }
                    return refuse("malformed: object");
                }
                return .{ .obj = ms.items };
            },
            '[' => {
                if (depth > max_depth) return refuse("malformed: depth");
                ps.p += 1;
                var xs: std.ArrayList(JV) = .empty;
                while (true) {
                    try ps.ws();
                    if (ps.p >= ps.s.len) return refuse("malformed: open array");
                    if (ps.s[ps.p] == ']') {
                        ps.p += 1;
                        break;
                    }
                    try xs.append(ps.a, try ps.value_(depth + 1));
                    try ps.ws();
                    if (ps.p >= ps.s.len) return refuse("malformed: open array");
                    if (ps.s[ps.p] == ',') {
                        ps.p += 1;
                        continue;
                    }
                    if (ps.s[ps.p] == ']') {
                        ps.p += 1;
                        break;
                    }
                    return refuse("malformed: array");
                }
                return .{ .arr = xs.items };
            },
            '"' => return .{ .str = try ps.string() },
            't' => return ps.lit("true", .{ .boolean = true }),
            'f' => return ps.lit("false", .{ .boolean = false }),
            'n' => return ps.lit("null", .null_),
            else => return .{ .num = try ps.number() },
        }
    }

    fn lit(ps: *Parser, word: []const u8, v: JV) Error!JV {
        if (!std.mem.startsWith(u8, ps.s[ps.p..], word)) return refuse("malformed: literal");
        ps.p += word.len;
        return v;
    }

    fn number(ps: *Parser) Error![]const u8 {
        const st = ps.p;
        const s = ps.s;
        if (ps.p < s.len and s[ps.p] == '-') ps.p += 1;
        if (ps.p >= s.len) return refuse("malformed: number");
        if (s[ps.p] == '0') {
            ps.p += 1;
        } else if (s[ps.p] >= '1' and s[ps.p] <= '9') {
            while (ps.p < s.len and std.ascii.isDigit(s[ps.p])) ps.p += 1;
        } else return refuse("malformed: token");
        if (ps.p < s.len and s[ps.p] == '.') {
            ps.p += 1;
            const d = ps.p;
            while (ps.p < s.len and std.ascii.isDigit(s[ps.p])) ps.p += 1;
            if (ps.p == d) return refuse("malformed: number");
        }
        if (ps.p < s.len and (s[ps.p] == 'e' or s[ps.p] == 'E')) {
            ps.p += 1;
            if (ps.p < s.len and (s[ps.p] == '+' or s[ps.p] == '-')) ps.p += 1;
            const d = ps.p;
            while (ps.p < s.len and std.ascii.isDigit(s[ps.p])) ps.p += 1;
            if (ps.p == d) return refuse("malformed: number");
        }
        // a token runs to a delimiter: `1-2`, `5+`, `1.2.3` are not numbers
        if (ps.p < s.len) {
            const c = s[ps.p];
            const delim = c == ' ' or c == '\t' or c == '\n' or c == '\r' or c == ',' or c == ']' or c == '}' or c == '/';
            if (!delim) return refuse("malformed: number");
        }
        return s[st..ps.p];
    }

    fn hex4(ps: *Parser) Error!u21 {
        if (ps.p + 4 > ps.s.len) return refuse("malformed: escape");
        const v = std.fmt.parseInt(u16, ps.s[ps.p .. ps.p + 4], 16) catch return refuse("malformed: escape");
        for (ps.s[ps.p .. ps.p + 4]) |c| if (!std.ascii.isHex(c)) return refuse("malformed: escape");
        ps.p += 4;
        return v;
    }

    fn string(ps: *Parser) Error![]const u8 {
        ps.p += 1; // the opening quote
        var out: std.ArrayList(u8) = .empty;
        const s = ps.s;
        while (true) {
            if (ps.p >= s.len) return refuse("malformed: open string");
            const c = s[ps.p];
            if (c == '"') {
                ps.p += 1;
                return out.items;
            }
            if (c < 0x20) return refuse("malformed: control byte in string");
            if (c == '\\') {
                ps.p += 1;
                if (ps.p >= s.len) return refuse("malformed: escape");
                const e = s[ps.p];
                ps.p += 1;
                switch (e) {
                    '"' => try out.append(ps.a, '"'),
                    '\\' => try out.append(ps.a, '\\'),
                    '/' => try out.append(ps.a, '/'),
                    'b' => try out.append(ps.a, 8),
                    'f' => try out.append(ps.a, 12),
                    'n' => try out.append(ps.a, '\n'),
                    'r' => try out.append(ps.a, '\r'),
                    't' => try out.append(ps.a, '\t'),
                    'u' => {
                        var cp = try ps.hex4();
                        if (cp >= 0xDC00 and cp <= 0xDFFF) return refuse("string_lone_surrogate");
                        if (cp >= 0xD800 and cp <= 0xDBFF) {
                            if (ps.p + 2 > s.len or s[ps.p] != '\\' or s[ps.p + 1] != 'u') return refuse("string_lone_surrogate");
                            ps.p += 2;
                            const lo = try ps.hex4();
                            if (lo < 0xDC00 or lo > 0xDFFF) return refuse("string_lone_surrogate");
                            cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                        }
                        if (cp == 0) return refuse("text_holds_zero");
                        var buf: [4]u8 = undefined;
                        const n = std.unicode.utf8Encode(cp, &buf) catch return refuse("malformed");
                        try out.appendSlice(ps.a, buf[0..n]);
                    },
                    else => return refuse("malformed: escape"),
                }
                continue;
            }
            if (c < 0x80) {
                try out.append(ps.a, c);
                ps.p += 1;
                continue;
            }
            const n = std.unicode.utf8ByteSequenceLength(c) catch return refuse("string_not_utf8");
            if (ps.p + n > s.len) return refuse("string_not_utf8");
            _ = std.unicode.utf8Decode(s[ps.p .. ps.p + n]) catch return refuse("string_not_utf8");
            try out.appendSlice(ps.a, s[ps.p .. ps.p + n]);
            ps.p += n;
        }
    }
};

fn commentText(b: []const u8) Error!void {
    if (std.mem.indexOfScalar(u8, b, 0) != null) return refuse("malformed: NUL in comment");
    if (!std.unicode.utf8ValidateSlice(b)) return refuse("string_not_utf8: comment");
}

pub fn parse(a: Allocator, text: []const u8) Error!JV {
    var s = text;
    if (s.len >= 3 and s[0] == 0xEF and s[1] == 0xBB and s[2] == 0xBF) s = s[3..];
    var ps: Parser = .{ .a = a, .s = s };
    const v = try ps.value_(0);
    try ps.ws();
    if (ps.p != s.len) return refuse("malformed: bytes after the value");
    return v;
}

// ------------------------------------------------------------ numbers

/// `-?(0|[1-9][0-9]*)`, not `-0`: the integer grammar (§16.2, Numbers).
fn integerText(t: []const u8) ?Int {
    var s = t;
    var neg = false;
    if (s.len > 0 and s[0] == '-') {
        neg = true;
        s = s[1..];
    }
    if (s.len == 0) return null;
    if (s[0] == '0' and s.len > 1) return null;
    for (s) |c| if (!std.ascii.isDigit(c)) return null;
    if (neg and s.len == 1 and s[0] == '0') return null;
    return 0; // grammar ok; the value is read by `integerValue`
}

const IntRead = union(enum) { ok: Int, over };

fn integerValue(t: []const u8) IntRead {
    var s = t;
    var neg = false;
    if (s[0] == '-') {
        neg = true;
        s = s[1..];
    }
    if (s.len > 40) return .over;
    var v: i256 = 0;
    for (s) |c| v = v * 10 + (c - '0');
    if (neg) v = -v;
    if (v < std.math.minInt(Int) or v > std.math.maxInt(Int)) return .over;
    return .{ .ok = @intCast(v) };
}

/// A number token as mantissa digits and a decimal exponent, exactly.
const Dec = struct { neg: bool, digits: []const u8, exp: i64 };

fn decompose(a: Allocator, t: []const u8) Error!Dec {
    var s = t;
    var neg = false;
    if (s.len > 0 and s[0] == '-') {
        neg = true;
        s = s[1..];
    }
    var ds: std.ArrayList(u8) = .empty;
    var exp: i64 = 0;
    var i: usize = 0;
    while (i < s.len and std.ascii.isDigit(s[i])) : (i += 1) try ds.append(a, s[i]);
    if (i < s.len and s[i] == '.') {
        i += 1;
        while (i < s.len and std.ascii.isDigit(s[i])) : (i += 1) {
            try ds.append(a, s[i]);
            exp -= 1;
        }
    }
    if (i < s.len and (s[i] == 'e' or s[i] == 'E')) {
        i += 1;
        var eneg = false;
        if (s[i] == '+' or s[i] == '-') {
            eneg = s[i] == '-';
            i += 1;
        }
        var e: i64 = 0;
        while (i < s.len) : (i += 1) {
            if (e < 1_000_000_000) e = e * 10 + (s[i] - '0');
        }
        exp += if (eneg) -e else e;
    }
    // strip leading zeros and fold trailing zeros into the exponent
    var d = ds.items;
    while (d.len > 0 and d[0] == '0') d = d[1..];
    while (d.len > 0 and d[d.len - 1] == '0') {
        d = d[0 .. d.len - 1];
        exp += 1;
    }
    return .{ .neg = neg, .digits = d, .exp = exp };
}

const FixRead = union(enum) { ok: Int, inexact, over };

/// The token's exact value scaled by 2^F, placed only when it is an integer.
fn fixedFromToken(a: Allocator, t: []const u8, f_bits: u8) Error!FixRead {
    const d = try decompose(a, t);
    if (d.digits.len == 0) return .{ .ok = 0 };
    const mag: i64 = @as(i64, @intCast(d.digits.len)) + d.exp; // digits before the point
    if (mag > 41) return .over;
    if (d.exp < -400 or d.digits.len > 450) return .inexact;
    const Wide = u2048;
    var m: Wide = 0;
    for (d.digits) |c| m = m * 10 + (c - '0');
    m <<= @intCast(f_bits);
    if (d.exp >= 0) {
        var k: i64 = 0;
        while (k < d.exp) : (k += 1) m *= 10;
    } else {
        var k: i64 = 0;
        while (k < -d.exp) : (k += 1) {
            if (m % 10 != 0) return .inexact;
            m /= 10;
        }
    }
    if (m > (@as(Wide, 1) << 129)) return .over;
    var v: i256 = @intCast(m);
    if (d.neg) v = -v;
    if (v < std.math.minInt(Int) or v > std.math.maxInt(Int)) return .over;
    return .{ .ok = @intCast(v) };
}

fn validNumberToken(t: []const u8) bool {
    var ps: Parser = .{ .a = std.heap.page_allocator, .s = t };
    const n = ps.number() catch return false;
    return n.len == t.len;
}

// ------------------------------------------------------------ the typed read

const Label = struct { open: bool, node: ?*value.Node };

pub const Reader = struct {
    a: Allocator,
    unit: *const schema.Unit,
    labels: std.AutoHashMapUnmanaged(u64, Label) = .empty,
    unknown: u32 = 0,

    fn table(r: *Reader, sd: *schema.StructDecl, jv: JV, as_node: ?*value.Node) Error!*value.Struct {
        if (sd.json_array) {
            const s = try value.defaultStruct(r.a, sd);
            const f = &sd.fields[0];
            s.fields[0] = try r.place(f.ty, jv, f, f.optional);
            return s;
        }
        if (jv != .obj) return refuse("kind_mismatch: want an object");
        const s = value.defaultStruct(r.a, sd) catch return refuse("bad default");
        const seen = try r.a.alloc(bool, sd.fields.len);
        @memset(seen, false);
        for (jv.obj, 0..) |m, mi| {
            if (m.key.len > name_cap) return refuse("out_of_range: name past 255 bytes");
            if (m.key.len > 0 and m.key[0] == '&') {
                if (!(mi == 0 and as_node != null and std.mem.eql(u8, m.key, "&node")))
                    return refuse("malformed: & key out of place");
                continue;
            }
            const fi = sd.fieldByKey(m.key) orelse {
                r.unknown += 1;
                try r.skip(m.val);
                continue;
            };
            const f = &sd.fields[fi];
            seen[fi] = true;
            s.fields[fi] = try r.place(f.ty, m.val, f, f.optional);
        }
        for (sd.fields, 0..) |*f, i| {
            if (!seen[i] and f.required) return refuse("missing_required");
            if (seen[i] and f.required and f.optional and s.fields[i].opt == null) return refuse("missing_required");
        }
        return s;
    }

    /// A field's value, by its row (§16.2).
    fn place(r: *Reader, ty: *const Type, jv: JV, f: ?*const schema.Field, optional: bool) Error!Value {
        if (optional) {
            if (jv == .null_) return .{ .opt = null };
            const p = try r.a.create(Value);
            p.* = try r.place(ty, jv, f, false);
            return .{ .opt = p };
        }
        switch (ty.*) {
            .boolean => {
                if (jv != .boolean) return refuse("kind_mismatch");
                return .{ .b = jv.boolean };
            },
            .int => |it| {
                const t = if (it.width > 32) blk: {
                    if (jv != .str) return refuse("kind_mismatch: wide integer is a string");
                    break :blk jv.str;
                } else blk: {
                    if (jv != .num) return refuse("kind_mismatch");
                    break :blk jv.num;
                };
                if (integerText(t) == null) return refuse("kind_mismatch: not an integer");
                const v = switch (integerValue(t)) {
                    .ok => |x| x,
                    .over => return refuse("out_of_range"),
                };
                if (v < it.min or v > it.max) return refuse("out_of_range");
                return .{ .i = v };
            },
            .fixed => |fx| {
                const t = if (fx.width > 32) blk: {
                    if (jv != .str) return refuse("kind_mismatch: wide fixed is a string");
                    if (!validNumberToken(jv.str)) return refuse("kind_mismatch");
                    break :blk jv.str;
                } else blk: {
                    if (jv != .num) return refuse("kind_mismatch");
                    break :blk jv.num;
                };
                const v = switch (try fixedFromToken(r.a, t, fx.f_bits)) {
                    .ok => |x| x,
                    .inexact => return refuse("kind_mismatch: finer than 2^-F"),
                    .over => return refuse("out_of_range"),
                };
                if (v < fx.min_raw or v > fx.max_raw) return refuse("out_of_range");
                return .{ .i = v };
            },
            .float => |ft| {
                if (jv != .num) return refuse("kind_mismatch");
                if (ft.wide) {
                    const x = std.fmt.parseFloat(f64, jv.num) catch return refuse("malformed");
                    if (!std.math.isFinite(x)) return refuse("out_of_range");
                    return .{ .f64 = x };
                }
                var x = std.fmt.parseFloat(f32, jv.num) catch return refuse("malformed");
                if (!std.math.isFinite(x)) return refuse("out_of_range");
                if (ft.compressed) |c| {
                    if (x < c.min or x > c.max) return refuse("out_of_range");
                    if (x == 0) x = 0;
                }
                return .{ .f32 = x };
            },
            .timestamp => {
                if (jv != .str) return refuse("kind_mismatch");
                return .{ .i = try parseTimestamp(jv.str) };
            },
            .string => |cap| {
                if (jv != .str) return refuse("kind_mismatch");
                if (cap) |c| if (jv.str.len > c) return refuse("length_over_bound");
                return .{ .str = jv.str };
            },
            .bytes => |cap| {
                if (jv != .str) return refuse("kind_mismatch");
                const b = try base64Read(r.a, jv.str);
                if (b.len > cap) return refuse("length_over_bound");
                return .{ .str = b };
            },
            .enum_ => |ed| {
                if (jv != .str) return refuse("kind_mismatch");
                return .{ .en = try r.enumName(ed, jv.str, f) };
            },
            .flags => |fd| {
                if (jv != .arr) return refuse("kind_mismatch");
                var m: u64 = 0;
                for (jv.arr) |x| {
                    if (x != .str) return refuse("kind_mismatch");
                    if (x.str.len > name_cap) return refuse("out_of_range: name past 255 bytes");
                    const bit = fd.byName(x.str) orelse {
                        r.unknown += 1;
                        continue;
                    };
                    const mask = @as(u64, 1) << bit;
                    if (m & mask != 0) return refuse("malformed: flag named twice");
                    m |= mask;
                }
                return .{ .fl = m };
            },
            .struct_ => |sd| {
                if (!sd.json_array and jv == .obj and jv.obj.len > 0 and jv.obj[0].key.len > 0 and jv.obj[0].key[0] == '&')
                    return refuse("malformed: & key in a by-value table");
                return .{ .st = try r.table(sd, jv, null) };
            },
            .union_ => |ud| {
                if (jv != .obj) return refuse("kind_mismatch");
                if (jv.obj.len == 0) return .{ .un = .{ .arm = 0, .payload = null } };
                if (jv.obj.len > 1) return refuse("malformed: union with two keys");
                const m = jv.obj[0];
                if (m.key.len > name_cap) return refuse("out_of_range: name past 255 bytes");
                const ai = ud.byName(m.key) orelse {
                    r.unknown += 1;
                    try r.skip(m.val);
                    return .{ .un = .{ .arm = 0, .payload = null } };
                };
                const arm = ud.arms[ai - 1];
                const at = arm.ty orelse {
                    if (m.val != .null_) return refuse("kind_mismatch: payload-free arm");
                    return .{ .un = .{ .arm = ai, .payload = null } };
                };
                const p = try r.a.create(Value);
                p.* = try r.place(at, m.val, null, false);
                return .{ .un = .{ .arm = ai, .payload = p } };
            },
            .array => |*at| return .{ .arr = try r.array(at, jv) },
            .keyed => |kt| {
                if (jv != .obj) return refuse("kind_mismatch");
                var xs = try r.a.alloc(Value, kt.key.variants.len);
                for (xs) |*x| x.* = value.zeroOf(r.a, kt.elem) catch return error.OutOfMemory;
                for (jv.obj) |m| {
                    if (m.key.len > name_cap) return refuse("out_of_range: name past 255 bytes");
                    const ord = kt.key.byName(m.key) orelse {
                        r.unknown += 1;
                        try r.skip(m.val);
                        continue;
                    };
                    xs[ord - 1] = try r.place(kt.elem, m.val, null, false);
                }
                return .{ .keyed = xs };
            },
            .map => |*mt| {
                if (jv != .obj) return refuse("kind_mismatch");
                var xs = try r.a.alloc(value.MapEntry, jv.obj.len);
                for (jv.obj, 0..) |m, i| {
                    xs[i] = .{ .key = try r.mapKey(mt.key, m.key), .val = try r.place(mt.val, m.val, null, mt.val_optional) };
                }
                wire.sortEntries(mt.key, xs);
                for (1..xs.len) |i| if (wire.keyOrder(mt.key, xs[i - 1].key, xs[i].key) != .lt) return refuse("malformed: repeated map key");
                return .{ .map = xs };
            },
            .pointer => |sd| return r.pointer(sd, jv),
            .blob_bytes => {
                if (jv == .null_) return .{ .ptr = null };
                if (jv != .str) return refuse("kind_mismatch");
                const nd = try r.a.create(value.Node);
                nd.* = .{ .kind = .blob_bytes, .bytes = try base64Read(r.a, jv.str) };
                return .{ .ptr = nd };
            },
            .blob_string => {
                if (jv == .null_) return .{ .ptr = null };
                if (jv != .str) return refuse("kind_mismatch");
                const nd = try r.a.create(value.Node);
                nd.* = .{ .kind = .blob_string, .bytes = jv.str };
                return .{ .ptr = nd };
            },
        }
    }

    fn enumName(r: *Reader, ed: *schema.EnumDecl, name: []const u8, f: ?*const schema.Field) Error!u32 {
        if (name.len > name_cap) return refuse("out_of_range: name past 255 bytes");
        if (std.mem.eql(u8, name, "None")) return 0;
        const ord = ed.byName(name) orelse {
            r.unknown += 1;
            return 0;
        };
        if (ed.variants[ord - 1].deprecated) {
            if (f) |fl| if (fl.default != null) {
                const dv = value.fieldDefault(r.a, r.unit, fl) catch return 0;
                if (dv == .en) return dv.en;
            };
            return 0;
        }
        return ord;
    }

    fn mapKey(r: *Reader, kt: *const Type, key: []const u8) Error!Value {
        _ = r;
        switch (kt.*) {
            .string => |cap| {
                if (cap) |c| if (key.len > c) return refuse("length_over_bound");
                return .{ .str = key };
            },
            .int => |it| {
                if (key.len == 0) return refuse("malformed: map key");
                if (!validNumberToken(key)) return refuse("malformed: map key");
                if (integerText(key) == null) return refuse("kind_mismatch: map key");
                const v = switch (integerValue(key)) {
                    .ok => |x| x,
                    .over => return refuse("out_of_range"),
                };
                if (v < it.min or v > it.max) return refuse("out_of_range");
                return .{ .i = v };
            },
            else => return refuse("kind_mismatch: map key kind"),
        }
    }

    fn array(r: *Reader, at: *const schema.ArrayT, jv: JV) Error![]Value {
        if (jv != .arr) return refuse("kind_mismatch");
        const n = jv.arr.len;
        if (at.max) |mx| if (n > mx) return refuse("count_over_bound");
        if (!at.exact and n < at.min) return refuse("out_of_range: below the minimum");
        const len: usize = if (at.exact) @intCast(at.max.?) else n;
        var xs = try r.a.alloc(Value, len);
        var total: u64 = 0;
        for (jv.arr, 0..) |x, i| {
            if (at.elem_optional) {
                xs[i] = try r.place(at.elem, x, null, true);
                if (xs[i].opt) |p| total += p.str.len;
            } else {
                if (x == .null_ and !isPointer(at.elem)) return refuse("kind_mismatch: null element");
                xs[i] = try r.place(at.elem, x, null, false);
                if (at.elem.* == .string) total += xs[i].str.len;
            }
        }
        var i = n;
        while (i < len) : (i += 1) xs[i] = value.elemDefault(r.a, at) catch return error.OutOfMemory;
        if (at.total) |t| if (total > t) return refuse("length_over_bound: total");
        return xs;
    }

    fn parseLabel(jv: JV) Error!u64 {
        if (jv != .num) return refuse("malformed: label");
        const t = jv.num;
        if (t.len == 0 or t[0] < '1' or t[0] > '9') return refuse("malformed: label");
        var v: u64 = 0;
        for (t) |c| {
            if (!std.ascii.isDigit(c)) return refuse("malformed: label");
            v = std.math.mul(u64, v, 10) catch return refuse("malformed: label past 2^64");
            v = std.math.add(u64, v, c - '0') catch return refuse("malformed: label past 2^64");
        }
        return v;
    }

    fn pointer(r: *Reader, sd: *schema.StructDecl, jv: JV) Error!Value {
        if (jv == .null_) return .{ .ptr = null };
        if (jv != .obj) return refuse("kind_mismatch");
        const o = jv.obj;
        for (o[@min(o.len, 1)..]) |m| if (std.mem.eql(u8, m.key, "&node")) return refuse("malformed: &node not first");
        if (o.len > 0 and std.mem.eql(u8, o[0].key, "&node")) {
            const label = try parseLabel(o[0].val);
            if (o.len == 1) {
                const l = r.labels.get(label) orelse return refuse("malformed: reference before definition");
                if (l.open) return refuse("malformed: reference inside its own definition");
                const nd = l.node orelse return .{ .ptr = null };
                if (nd.st.?.decl != sd) return refuse("wire_damaged: label of another table");
                return .{ .ptr = nd };
            }
            if (r.labels.contains(label)) return refuse("malformed: label defined twice");
            try r.labels.put(r.a, label, .{ .open = true, .node = null });
            const nd = try r.a.create(value.Node);
            nd.* = .{ .kind = .table };
            nd.st = try r.table(sd, jv, nd);
            try r.labels.put(r.a, label, .{ .open = false, .node = nd });
            return .{ .ptr = nd };
        }
        const nd = try r.a.create(value.Node);
        nd.* = .{ .kind = .table };
        nd.st = try r.table(sd, jv, nd);
        return .{ .ptr = nd };
    }

    /// A skipped value is still read for its labels at every depth (§16.7).
    fn skip(r: *Reader, jv: JV) Error!void {
        switch (jv) {
            .arr => |xs| for (xs) |x| try r.skip(x),
            .obj => |o| {
                for (o, 0..) |m, i| {
                    if (m.key.len > name_cap) return refuse("out_of_range: name past 255 bytes");
                    if (i > 0 and std.mem.eql(u8, m.key, "&node")) return refuse("malformed: &node not first");
                }
                if (o.len > 0 and std.mem.eql(u8, o[0].key, "&node")) {
                    const label = try parseLabel(o[0].val);
                    if (o.len == 1) {
                        const l = r.labels.get(label) orelse return refuse("malformed: reference before definition");
                        if (l.open) return refuse("malformed: reference inside its own definition");
                        return;
                    }
                    if (r.labels.contains(label)) return refuse("malformed: label defined twice");
                    try r.labels.put(r.a, label, .{ .open = true, .node = null });
                    for (o[1..]) |m| try r.skip(m.val);
                    try r.labels.put(r.a, label, .{ .open = false, .node = null });
                    return;
                }
                for (o) |m| try r.skip(m.val);
            },
            else => {},
        }
    }
};

fn isPointer(ty: *const Type) bool {
    return switch (ty.*) {
        .pointer, .blob_bytes, .blob_string => true,
        else => false,
    };
}

/// FromJson: the whole value or a refusal.
pub fn read(a: Allocator, unit: *const schema.Unit, root: *schema.StructDecl, text: []const u8) Error!*value.Struct {
    const jv = try parse(a, text);
    var r: Reader = .{ .a = a, .unit = unit };
    if (!root.json_array and jv == .obj and jv.obj.len > 0 and jv.obj[0].key.len > 0 and jv.obj[0].key[0] == '&')
        return refuse("malformed: & key at the root");
    return r.table(root, jv, null);
}

// ------------------------------------------------------------ base64 (§16.8)

const b64 = std.base64.standard;

fn base64Read(a: Allocator, s: []const u8) Error![]const u8 {
    if (s.len % 4 != 0) return refuse("kind_mismatch: base64");
    const n = b64.Decoder.calcSizeForSlice(s) catch return refuse("kind_mismatch: base64");
    const out = try a.alloc(u8, n);
    b64.Decoder.decode(out, s) catch return refuse("kind_mismatch: base64");
    // canonical: exactly the spelling the writer writes for those bytes
    const back = try a.alloc(u8, b64.Encoder.calcSize(n));
    _ = b64.Encoder.encode(back, out);
    if (!std.mem.eql(u8, back, s)) return refuse("base64_not_canonical");
    return out;
}

// ------------------------------------------------------------ timestamps (§2.11, W13)

fn daysFromCivil(y0: i64, m: i64, d: i64) i64 {
    const y = if (m <= 2) y0 - 1 else y0;
    const era = @divFloor(y, 400);
    const yoe = y - era * 400;
    const mp = @mod(m + 9, 12);
    const doy = @divFloor(153 * mp + 2, 5) + d - 1;
    const doe = yoe * 365 + @divFloor(yoe, 4) - @divFloor(yoe, 100) + doy;
    return era * 146097 + doe - 719468;
}

fn civilFromDays(z0: i64) struct { y: i64, m: i64, d: i64 } {
    const z = z0 + 719468;
    const era = @divFloor(z, 146097);
    const doe = z - era * 146097;
    const yoe = @divFloor(doe - @divFloor(doe, 1460) + @divFloor(doe, 36524) - @divFloor(doe, 146096), 365);
    const y = yoe + era * 400;
    const doy = doe - (365 * yoe + @divFloor(yoe, 4) - @divFloor(yoe, 100));
    const mp = @divFloor(5 * doy + 2, 153);
    const d = doy - @divFloor(153 * mp + 2, 5) + 1;
    const m = if (mp < 10) mp + 3 else mp - 9;
    return .{ .y = if (m <= 2) y + 1 else y, .m = m, .d = d };
}

fn digitsAt(s: []const u8, i: usize, n: usize) ?i64 {
    if (i + n > s.len) return null;
    var v: i64 = 0;
    for (s[i .. i + n]) |c| {
        if (!std.ascii.isDigit(c)) return null;
        v = v * 10 + (c - '0');
    }
    return v;
}

fn isLeap(y: i64) bool {
    return (@mod(y, 4) == 0 and @mod(y, 100) != 0) or @mod(y, 400) == 0;
}

fn parseTimestamp(s: []const u8) Error!Int {
    const bad = "timestamp_not_rfc3339";
    const y = digitsAt(s, 0, 4) orelse return refuse(bad);
    if (s.len < 20 or s[4] != '-' or s[7] != '-') return refuse(bad);
    const mo = digitsAt(s, 5, 2) orelse return refuse(bad);
    const d = digitsAt(s, 8, 2) orelse return refuse(bad);
    if (s[10] != 'T' and s[10] != 't') return refuse(bad);
    const hh = digitsAt(s, 11, 2) orelse return refuse(bad);
    if (s[13] != ':' or s[16] != ':') return refuse(bad);
    const mi = digitsAt(s, 14, 2) orelse return refuse(bad);
    const ss = digitsAt(s, 17, 2) orelse return refuse(bad);
    if (mo < 1 or mo > 12 or hh > 23 or mi > 59 or ss > 59) return refuse(bad);
    const mdays = [_]i64{ 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31 };
    var dim = mdays[@intCast(mo - 1)];
    if (mo == 2 and isLeap(y)) dim = 29;
    if (d < 1 or d > dim) return refuse(bad);
    var i: usize = 19;
    var frac: i64 = 0;
    if (i < s.len and s[i] == '.') {
        i += 1;
        const st = i;
        var k: u32 = 0;
        while (i < s.len and std.ascii.isDigit(s[i])) : (i += 1) {
            if (k < 9) {
                frac = frac * 10 + (s[i] - '0');
                k += 1;
            }
        }
        if (i == st) return refuse(bad);
        while (k < 9) : (k += 1) frac *= 10;
    }
    if (i >= s.len) return refuse(bad);
    var off: i64 = 0;
    if (s[i] == 'Z' or s[i] == 'z') {
        i += 1;
    } else if (s[i] == '+' or s[i] == '-') {
        const sign: i64 = if (s[i] == '-') -1 else 1;
        const oh = digitsAt(s, i + 1, 2) orelse return refuse(bad);
        if (i + 3 >= s.len or s[i + 3] != ':') return refuse(bad);
        const om = digitsAt(s, i + 4, 2) orelse return refuse(bad);
        if (oh > 23 or om > 59) return refuse(bad);
        off = sign * (oh * 3600 + om * 60);
        i += 6;
    } else return refuse(bad);
    if (i != s.len) return refuse(bad);
    const secs: i128 = @as(i128, daysFromCivil(y, mo, d)) * 86400 + hh * 3600 + mi * 60 + ss - off;
    const ns: i128 = secs * 1_000_000_000 + frac;
    if (ns < std.math.minInt(i64) or ns > std.math.maxInt(i64)) return refuse(bad);
    return @intCast(ns);
}

fn writeTimestamp(a: Allocator, out: *std.ArrayList(u8), ns: i64) Error!void {
    const secs = @divFloor(ns, 1_000_000_000);
    const frac = @mod(ns, 1_000_000_000);
    const days = @divFloor(secs, 86400);
    const sod = @mod(secs, 86400);
    const c = civilFromDays(days);
    var buf: [64]u8 = undefined;
    const head = std.fmt.bufPrint(&buf, "\"{d:0>4}-{d:0>2}-{d:0>2}T{d:0>2}:{d:0>2}:{d:0>2}", .{
        @as(u64, @intCast(c.y)), @as(u64, @intCast(c.m)), @as(u64, @intCast(c.d)),
        @as(u64, @intCast(@divFloor(sod, 3600))), @as(u64, @intCast(@mod(@divFloor(sod, 60), 60))), @as(u64, @intCast(@mod(sod, 60))),
    }) catch unreachable;
    try out.appendSlice(a, head);
    if (frac != 0) {
        var fb: [9]u8 = undefined;
        var f: i64 = frac;
        var k: usize = 9;
        while (k > 0) {
            k -= 1;
            fb[k] = @intCast('0' + @mod(f, 10));
            f = @divFloor(f, 10);
        }
        var n: usize = 9;
        while (fb[n - 1] == '0') n -= 1;
        try out.append(a, '.');
        try out.appendSlice(a, fb[0..n]);
    }
    try out.appendSlice(a, "Z\"");
}

// ------------------------------------------------------------ floats (W12)

/// The exact decimal digits of a finite nonzero |v| = digits × 10^shift.
const Limbs = std.ArrayList(u32); // base 1e9, least significant first

fn mulSmall(a: Allocator, x: *Limbs, k: u32) Error!void {
    var carry: u64 = 0;
    for (x.items) |*l| {
        const t = @as(u64, l.*) * k + carry;
        l.* = @intCast(t % 1_000_000_000);
        carry = t / 1_000_000_000;
    }
    while (carry > 0) {
        try x.append(a, @intCast(carry % 1_000_000_000));
        carry /= 1_000_000_000;
    }
}

fn exactDigits(a: Allocator, mant: u64, e2: i32) Error!struct { d: []u8, shift: i32 } {
    var x: Limbs = .empty;
    var m = mant;
    while (m > 0) {
        try x.append(a, @intCast(m % 1_000_000_000));
        m /= 1_000_000_000;
    }
    var shift: i32 = 0;
    if (e2 >= 0) {
        var k = e2;
        while (k >= 29) : (k -= 29) try mulSmall(a, &x, 1 << 29);
        if (k > 0) try mulSmall(a, &x, @as(u32, 1) << @intCast(k));
    } else {
        var k = -e2;
        shift = e2;
        while (k >= 13) : (k -= 13) try mulSmall(a, &x, 1220703125);
        var p: u32 = 1;
        while (k > 0) : (k -= 1) p *= 5;
        try mulSmall(a, &x, p);
    }
    var s: std.ArrayList(u8) = .empty;
    var i = x.items.len;
    var first = true;
    while (i > 0) {
        i -= 1;
        var buf: [9]u8 = undefined;
        const t = if (first) std.fmt.bufPrint(&buf, "{d}", .{x.items[i]}) catch unreachable else std.fmt.bufPrint(&buf, "{d:0>9}", .{x.items[i]}) catch unreachable;
        first = false;
        try s.appendSlice(a, t);
    }
    return .{ .d = s.items, .shift = shift };
}

/// C's `%.*g` with precision p over the exact value (W12's G).
fn formatG(a: Allocator, neg: bool, mant: u64, e2: i32, p: usize) Error![]u8 {
    var out: std.ArrayList(u8) = .empty;
    if (neg) try out.append(a, '-');
    if (mant == 0) {
        try out.append(a, '0');
        return out.items;
    }
    const ex = try exactDigits(a, mant, e2);
    const D = ex.d;
    var X: i64 = @as(i64, @intCast(D.len)) - 1 + ex.shift;
    var digs = try a.alloc(u8, p);
    if (D.len <= p) {
        @memcpy(digs[0..D.len], D);
        @memset(digs[D.len..], '0');
    } else {
        @memcpy(digs, D[0..p]);
        const rest = D[p..];
        var up = false;
        if (rest[0] > '5') up = true else if (rest[0] == '5') {
            var tail_nonzero = false;
            for (rest[1..]) |c| if (c != '0') {
                tail_nonzero = true;
                break;
            };
            up = tail_nonzero or ((digs[p - 1] - '0') % 2 == 1);
        }
        if (up) {
            var i = p;
            var carry = true;
            while (i > 0 and carry) {
                i -= 1;
                if (digs[i] == '9') {
                    digs[i] = '0';
                } else {
                    digs[i] += 1;
                    carry = false;
                }
            }
            if (carry) {
                digs[0] = '1';
                @memset(digs[1..], '0');
                X += 1;
            }
        }
    }
    var n = p;
    while (n > 1 and digs[n - 1] == '0') n -= 1;
    const sig = digs[0..n];
    if (X < -4 or X >= @as(i64, @intCast(p))) {
        try out.append(a, sig[0]);
        if (n > 1) {
            try out.append(a, '.');
            try out.appendSlice(a, sig[1..]);
        }
        try out.append(a, 'e');
        try out.append(a, if (X < 0) '-' else '+');
        const ax: u64 = @intCast(if (X < 0) -X else X);
        var buf: [24]u8 = undefined;
        try out.appendSlice(a, std.fmt.bufPrint(&buf, "{d:0>2}", .{ax}) catch unreachable);
    } else if (X < 0) {
        try out.appendSlice(a, "0.");
        var z: i64 = 0;
        while (z < -X - 1) : (z += 1) try out.append(a, '0');
        try out.appendSlice(a, sig);
    } else {
        const ip: usize = @intCast(X + 1);
        if (n <= ip) {
            try out.appendSlice(a, sig);
            var z = n;
            while (z < ip) : (z += 1) try out.append(a, '0');
        } else {
            try out.appendSlice(a, sig[0..ip]);
            try out.append(a, '.');
            try out.appendSlice(a, sig[ip..]);
        }
    }
    return out.items;
}

pub fn formatF64(a: Allocator, v: f64) Error![]u8 {
    const bits: u64 = @bitCast(v);
    const neg = bits >> 63 != 0;
    const be: i32 = @intCast((bits >> 52) & 0x7ff);
    const frac = bits & ((1 << 52) - 1);
    const mant: u64 = if (be == 0) frac else frac | (1 << 52);
    const e2: i32 = if (be == 0) -1074 else be - 1075;
    var p: usize = 15;
    while (true) : (p += 1) {
        const t = try formatG(a, neg, mant, e2, p);
        if (p == 17) return t;
        const back = std.fmt.parseFloat(f64, t) catch unreachable;
        if (@as(u64, @bitCast(back)) == bits) return t;
    }
}

pub fn formatF32(a: Allocator, v: f32) Error![]u8 {
    const bits: u32 = @bitCast(v);
    const neg = bits >> 31 != 0;
    const be: i32 = @intCast((bits >> 23) & 0xff);
    const frac: u64 = bits & ((1 << 23) - 1);
    const mant: u64 = if (be == 0) frac else frac | (1 << 23);
    const e2: i32 = if (be == 0) -149 else be - 150;
    var p: usize = 6;
    while (true) : (p += 1) {
        const t = try formatG(a, neg, mant, e2, p);
        if (p == 9) return t;
        const back = std.fmt.parseFloat(f32, t) catch unreachable;
        if (@as(u32, @bitCast(back)) == bits) return t;
    }
}

// ------------------------------------------------------------ the writer (§16.14)

const Count = struct { n: u32 = 0, open: bool = false, label: u64 = 0 };

pub const Writer = struct {
    a: Allocator,
    out: std.ArrayList(u8) = .empty,
    counts: std.AutoHashMapUnmanaged(*value.Node, Count) = .empty,
    next_label: u64 = 1,

    // W1: count the namings, refuse a cycle and a shared blob
    fn graphStruct(w: *Writer, s: *value.Struct) Error!void {
        for (s.decl.fields, 0..) |*f, i| {
            if (!value.guardOn(s, f)) continue;
            const v = s.fields[i];
            if (f.optional) {
                if (v.opt) |p| try w.graphValue(f.ty, p.*);
            } else try w.graphValue(f.ty, v);
        }
    }

    fn graphValue(w: *Writer, ty: *const Type, v: Value) Error!void {
        switch (ty.*) {
            .pointer, .blob_bytes, .blob_string => {
                const nd = v.ptr orelse return;
                const gop = try w.counts.getOrPut(w.a, nd);
                if (gop.found_existing) {
                    if (gop.value_ptr.open) return refuse("data_cycle");
                    gop.value_ptr.n += 1;
                    if (nd.kind != .table) return refuse("blob_shared");
                    return;
                }
                gop.value_ptr.* = .{ .n = 1, .open = true };
                if (nd.kind == .table) try w.graphStruct(nd.st.?);
                w.counts.getPtr(nd).?.open = false;
            },
            .struct_ => try w.graphStruct(v.st),
            .union_ => |ud| if (v.un.arm != 0 and v.un.arm <= ud.arms.len) {
                if (ud.arms[v.un.arm - 1].ty) |at| try w.graphValue(at, v.un.payload.?.*);
            },
            .array => |at| for (v.arr) |x| {
                if (at.elem_optional) {
                    if (x.opt) |p| try w.graphValue(at.elem, p.*);
                } else try w.graphValue(at.elem, x);
            },
            .keyed => |kt| for (v.keyed) |x| try w.graphValue(kt.elem, x),
            .map => |mt| for (v.map) |e| {
                if (mt.val_optional) {
                    if (e.val.opt) |p| try w.graphValue(mt.val, p.*);
                } else try w.graphValue(mt.val, e.val);
            },
            else => {},
        }
    }

    fn nl(w: *Writer, d: u32) Error!void {
        try w.out.append(w.a, '\n');
        var i: u32 = 0;
        while (i < d) : (i += 1) try w.out.appendSlice(w.a, "  ");
    }

    fn str(w: *Writer, s: []const u8) Error!void {
        if (!wire.validUtf8NoNul(s)) return refuse("invalid_value: not text");
        try w.out.append(w.a, '"');
        for (s) |c| {
            switch (c) {
                '"' => try w.out.appendSlice(w.a, "\\\""),
                '\\' => try w.out.appendSlice(w.a, "\\\\"),
                8 => try w.out.appendSlice(w.a, "\\b"),
                12 => try w.out.appendSlice(w.a, "\\f"),
                '\n' => try w.out.appendSlice(w.a, "\\n"),
                '\r' => try w.out.appendSlice(w.a, "\\r"),
                '\t' => try w.out.appendSlice(w.a, "\\t"),
                else => if (c < 0x20) {
                    var buf: [6]u8 = undefined;
                    try w.out.appendSlice(w.a, std.fmt.bufPrint(&buf, "\\u{x:0>4}", .{c}) catch unreachable);
                } else try w.out.append(w.a, c),
            }
        }
        try w.out.append(w.a, '"');
    }

    fn int(w: *Writer, v: Int, quoted: bool) Error!void {
        var buf: [48]u8 = undefined;
        const t = std.fmt.bufPrint(&buf, "{d}", .{v}) catch unreachable;
        if (quoted) try w.out.append(w.a, '"');
        try w.out.appendSlice(w.a, t);
        if (quoted) try w.out.append(w.a, '"');
    }

    fn fixed(w: *Writer, raw: Int, f_bits: u8, quoted: bool) Error!void {
        if (quoted) try w.out.append(w.a, '"');
        if (raw < 0) try w.out.append(w.a, '-');
        const mag: u256 = @intCast(if (raw < 0) -@as(i256, raw) else @as(i256, raw));
        const whole = mag >> @intCast(f_bits);
        var buf: [48]u8 = undefined;
        try w.out.appendSlice(w.a, std.fmt.bufPrint(&buf, "{d}", .{whole}) catch unreachable);
        try w.out.append(w.a, '.');
        var fr: u256 = mag & ((@as(u256, 1) << @intCast(f_bits)) - 1);
        if (fr == 0) {
            try w.out.append(w.a, '0');
        } else {
            while (fr != 0) {
                fr *= 10;
                const dgt: u8 = @intCast(fr >> @intCast(f_bits));
                try w.out.append(w.a, '0' + dgt);
                fr &= (@as(u256, 1) << @intCast(f_bits)) - 1;
            }
        }
        if (quoted) try w.out.append(w.a, '"');
    }

    /// W3: a table or type value at depth d.
    fn table(w: *Writer, s: *value.Struct, d: u32, label: ?u64) Error!void {
        if (d > max_depth) return refuse("depth_exceeded");
        const sd = s.decl;
        if (sd.json_array) return w.value_(sd.fields[0].ty, s.fields[0], d, sd.fields[0].optional);
        var any = false;
        if (label) |l| {
            try w.out.append(w.a, '{');
            try w.nl(d + 1);
            try w.out.appendSlice(w.a, "\"&node\": ");
            try w.int(l, false);
            any = true;
        }
        var wrote_field = false;
        for (sd.fields, 0..) |*f, i| {
            if (!value.guardOn(s, f)) continue;
            const v = s.fields[i];
            if (f.optional and v.opt == null) continue;
            if (any) try w.out.append(w.a, ',') else try w.out.append(w.a, '{');
            any = true;
            wrote_field = true;
            try w.nl(d + 1);
            try w.str(f.key);
            try w.out.appendSlice(w.a, ": ");
            try w.value_(f.ty, v, d + 1, f.optional);
        }
        if (label != null and !wrote_field) return refuse("invalid_value: &node definition with no field");
        if (!any) return w.out.appendSlice(w.a, "{}");
        try w.nl(d);
        try w.out.append(w.a, '}');
    }

    fn value_(w: *Writer, ty: *const Type, v0: Value, d: u32, optional: bool) Error!void {
        var v = v0;
        if (optional) {
            v = (v0.opt orelse return w.out.appendSlice(w.a, "null")).*;
        }
        switch (ty.*) {
            .boolean => try w.out.appendSlice(w.a, if (v.b) "true" else "false"),
            .int => |it| {
                if (v.i < it.min or v.i > it.max) return refuse("invalid_value: out of range");
                try w.int(v.i, it.width > 32);
            },
            .fixed => |fx| {
                if (v.i < fx.min_raw or v.i > fx.max_raw) return refuse("invalid_value: out of range");
                try w.fixed(v.i, fx.f_bits, fx.width > 32);
            },
            .float => |ft| {
                if (ft.wide) {
                    if (!std.math.isFinite(v.f64)) return refuse("non_finite_float");
                    try w.out.appendSlice(w.a, try formatF64(w.a, v.f64));
                } else {
                    if (!std.math.isFinite(v.f32)) return refuse("non_finite_float");
                    if (ft.compressed) |c| if (v.f32 < c.min or v.f32 > c.max) return refuse("invalid_value: out of range");
                    try w.out.appendSlice(w.a, try formatF32(w.a, v.f32));
                }
            },
            .timestamp => try writeTimestamp(w.a, &w.out, @intCast(v.i)),
            .string => |cap| {
                if (cap) |c| if (v.str.len > c) return refuse("invalid_value: length");
                try w.str(v.str);
            },
            .bytes => |cap| {
                if (v.str.len > cap) return refuse("invalid_value: length");
                try w.base64(v.str);
            },
            .enum_ => |ed| {
                if (v.en == 0) return w.str("None");
                if (v.en > ed.variants.len) return refuse("invalid_value: enum ordinal");
                const vr = ed.variants[v.en - 1];
                if (vr.deprecated) return refuse("invalid_value: deprecated variant");
                try w.str(vr.name);
            },
            .flags => |fd| {
                if (fd.names.len < 64 and (v.fl >> @intCast(fd.names.len)) != 0) return refuse("invalid_value: flags bit");
                if (v.fl == 0) return w.out.appendSlice(w.a, "[]");
                try w.out.append(w.a, '[');
                var first = true;
                for (fd.names, 0..) |n, i| {
                    if (v.fl & (@as(u64, 1) << @intCast(i)) == 0) continue;
                    if (!first) try w.out.append(w.a, ',');
                    first = false;
                    try w.nl(d + 1);
                    try w.str(n);
                }
                try w.nl(d);
                try w.out.append(w.a, ']');
            },
            .struct_ => try w.table(v.st, d, null),
            .union_ => |ud| {
                if (v.un.arm == 0) return w.out.appendSlice(w.a, "{}");
                if (v.un.arm > ud.arms.len) return refuse("invalid_value: union tag");
                const arm = ud.arms[v.un.arm - 1];
                try w.out.append(w.a, '{');
                try w.nl(d + 1);
                try w.str(arm.name);
                try w.out.appendSlice(w.a, ": ");
                if (arm.ty) |at| try w.value_(at, v.un.payload.?.*, d + 1, false) else try w.out.appendSlice(w.a, "null");
                try w.nl(d);
                try w.out.append(w.a, '}');
            },
            .array => |at| {
                if (at.max) |mx| if (v.arr.len > mx) return refuse("invalid_value: count");
                if (v.arr.len < at.min) return refuse("invalid_value: count");
                if (at.total) |tot| {
                    var sum: u64 = 0;
                    for (v.arr) |x| sum += if (at.elem_optional) (if (x.opt) |p| p.str.len else 0) else x.str.len;
                    if (sum > tot) return refuse("invalid_value: total");
                }
                if (v.arr.len == 0) return w.out.appendSlice(w.a, "[]");
                try w.out.append(w.a, '[');
                for (v.arr, 0..) |x, i| {
                    if (i > 0) try w.out.append(w.a, ',');
                    try w.nl(d + 1);
                    try w.value_(at.elem, x, d + 1, at.elem_optional);
                }
                try w.nl(d);
                try w.out.append(w.a, ']');
            },
            .keyed => |kt| {
                if (kt.key.variants.len == 0) return w.out.appendSlice(w.a, "{}");
                try w.out.append(w.a, '{');
                for (v.keyed, 0..) |x, i| {
                    if (i > 0) try w.out.append(w.a, ',');
                    try w.nl(d + 1);
                    try w.str(kt.key.variants[i].name);
                    try w.out.appendSlice(w.a, ": ");
                    try w.value_(kt.elem, x, d + 1, false);
                }
                try w.nl(d);
                try w.out.append(w.a, '}');
            },
            .map => |mt| {
                if (v.map.len == 0) return w.out.appendSlice(w.a, "{}");
                wire.sortEntries(mt.key, v.map);
                try w.out.append(w.a, '{');
                for (v.map, 0..) |e, i| {
                    if (i > 0) try w.out.append(w.a, ',');
                    try w.nl(d + 1);
                    switch (mt.key.*) {
                        .string => |cap| {
                            if (cap) |c| if (e.key.str.len > c) return refuse("invalid_value: length");
                            try w.str(e.key.str);
                        },
                        .int => |it| {
                            if (e.key.i < it.min or e.key.i > it.max) return refuse("invalid_value: out of range");
                            try w.int(e.key.i, true);
                        },
                        else => return refuse("invalid_value: map key kind"),
                    }
                    try w.out.appendSlice(w.a, ": ");
                    try w.value_(mt.val, e.val, d + 1, mt.val_optional);
                }
                try w.nl(d);
                try w.out.append(w.a, '}');
            },
            .pointer => {
                const nd = v.ptr orelse return w.out.appendSlice(w.a, "null");
                const c = w.counts.getPtr(nd).?;
                if (c.n > 1) {
                    if (c.label != 0) {
                        if (d > max_depth) return refuse("depth_exceeded");
                        try w.out.append(w.a, '{');
                        try w.nl(d + 1);
                        try w.out.appendSlice(w.a, "\"&node\": ");
                        try w.int(c.label, false);
                        try w.nl(d);
                        try w.out.append(w.a, '}');
                        return;
                    }
                    c.label = w.next_label;
                    w.next_label += 1;
                    return w.table(nd.st.?, d, c.label);
                }
                try w.table(nd.st.?, d, null);
            },
            .blob_bytes => {
                const nd = v.ptr orelse return w.out.appendSlice(w.a, "null");
                try w.base64(nd.bytes);
            },
            .blob_string => {
                const nd = v.ptr orelse return w.out.appendSlice(w.a, "null");
                try w.str(nd.bytes);
            },
        }
    }

    fn base64(w: *Writer, b: []const u8) Error!void {
        const n = b64.Encoder.calcSize(b.len);
        const buf = try w.a.alloc(u8, n);
        _ = b64.Encoder.encode(buf, b);
        try w.out.append(w.a, '"');
        try w.out.appendSlice(w.a, buf);
        try w.out.append(w.a, '"');
    }
};

/// ToJson: the whole canonical text or a refusal (W0).
pub fn write(a: Allocator, s: *value.Struct) Error![]u8 {
    var w: Writer = .{ .a = a };
    try w.graphStruct(s);
    try w.table(s, 0, null);
    try w.out.append(a, '\n');
    return w.out.items;
}

test "formatG pins" {
    const a = std.testing.allocator;
    var arena = std.heap.ArenaAllocator.init(a);
    defer arena.deinit();
    const aa = arena.allocator();
    try std.testing.expectEqualStrings("4.94065645841247e-324", try formatF64(aa, 4.9406564584124654e-324));
    try std.testing.expectEqualStrings("1.4013e-45", try formatF32(aa, 1.401298464324817e-45));
    try std.testing.expectEqualStrings("0.30000000000000004", try formatF64(aa, 0.1 + 0.2));
    try std.testing.expectEqualStrings("-0", try formatF64(aa, -0.0));
    try std.testing.expectEqualStrings("1e+21", try formatF64(aa, 1e21));
    try std.testing.expectEqualStrings("2.5e-05", try formatF64(aa, 2.5e-05));
    try std.testing.expectEqualStrings("100", try formatF64(aa, 100));
    try std.testing.expectEqualStrings("7.0385313e-26", try formatF32(aa, @bitCast(@as(u32, 0x15AE43FE))));
}

test "timestamps" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const aa = arena.allocator();
    var out: std.ArrayList(u8) = .empty;
    try writeTimestamp(aa, &out, -1);
    try std.testing.expectEqualStrings("\"1969-12-31T23:59:59.999999999Z\"", out.items);
    try std.testing.expectEqual(@as(Int, 0), try parseTimestamp("1970-01-01T00:00:00Z"));
}

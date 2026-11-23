// The in-memory value of a table instance, its defaults, and the elision
// predicates of SPEC-TABLES.md §22.2.
//
// Values live in a caller's arena: nothing here frees, and every allocation
// is bounded by the input it was decoded from (see SPEC-GAPS.md on memory).
const std = @import("std");
const schema = @import("schema.zig");
const Type = schema.Type;
const Int = schema.Int;
const Allocator = std.mem.Allocator;

pub const Struct = struct {
    decl: *schema.StructDecl,
    fields: []Value,
};

pub const UnionVal = struct {
    arm: u32, // 0: None
    payload: ?*Value,
};

pub const MapEntry = struct {
    key: Value,
    val: Value, // `.opt` when the map's value is `?T`
};

pub const NodeKind = enum { table, blob_bytes, blob_string };

pub const Node = struct {
    kind: NodeKind,
    st: ?*Struct = null,
    bytes: []const u8 = &.{},
    // writer state (SPEC-TABLES.md §22.3)
    index: u32 = 0,
    state: u8 = 0, // 0 unvisited, 1 open, 2 closed
    refs: u32 = 0,
    label: u32 = 0,
};

pub const Value = union(enum) {
    b: bool,
    i: Int, // integers, bits(N), fixed raw values, timestamps
    f32: f32,
    f64: f64,
    str: []const u8, // string(N), string, bytes(N)
    en: u32, // enum ordinal, 0 = None
    fl: u64, // flags mask
    st: *Struct,
    un: UnionVal,
    arr: []Value,
    keyed: []Value, // slot k-1 for variant ordinal k
    map: []MapEntry,
    ptr: ?*Node,
    opt: ?*Value, // an optional: null = absent
};

pub const Error = error{ OutOfMemory, BadDefault };

// ------------------------------------------------------------ defaults

pub fn zeroOf(a: Allocator, ty: *const Type) Error!Value {
    return switch (ty.*) {
        .boolean => .{ .b = false },
        .int, .fixed, .timestamp => .{ .i = 0 },
        .float => |f| if (f.wide) .{ .f64 = 0 } else .{ .f32 = 0 },
        .string, .bytes => .{ .str = "" },
        .enum_ => .{ .en = 0 },
        .flags => .{ .fl = 0 },
        .struct_ => |sd| .{ .st = try defaultStruct(a, sd) },
        .union_ => .{ .un = .{ .arm = 0, .payload = null } },
        .array => |at| blk: {
            const n: usize = @intCast(at.min);
            const xs = try a.alloc(Value, n);
            for (xs) |*x| x.* = try elemDefault(a, &at);
            break :blk .{ .arr = xs };
        },
        .keyed => |kt| blk: {
            const xs = try a.alloc(Value, kt.key.variants.len);
            for (xs) |*x| x.* = try zeroOf(a, kt.elem);
            break :blk .{ .keyed = xs };
        },
        .map => .{ .map = &.{} },
        .pointer, .blob_bytes, .blob_string => .{ .ptr = null },
    };
}

pub fn elemDefault(a: Allocator, at: *const schema.ArrayT) Error!Value {
    if (at.elem_optional) return .{ .opt = null };
    return zeroOf(a, at.elem);
}

pub fn defaultStruct(a: Allocator, sd: *schema.StructDecl) Error!*Struct {
    const s = try a.create(Struct);
    s.* = .{ .decl = sd, .fields = try a.alloc(Value, sd.fields.len) };
    for (sd.fields, 0..) |*f, i| s.fields[i] = try fieldDefault(a, sd.unit_ptr(), f);
    return s;
}

/// The value an absent field reads as (SPEC-TABLES.md §4).
pub fn fieldDefault(a: Allocator, u: ?*const schema.Unit, f: *const schema.Field) Error!Value {
    if (f.optional) return .{ .opt = null };
    const spec = f.default orelse return zeroOf(a, f.ty);
    return specValue(a, u.?, f.ty, spec) catch return error.BadDefault;
}

fn specValue(_: Allocator, u: *const schema.Unit, ty: *const Type, spec: schema.DefaultSpec) !Value {
    switch (ty.*) {
        .boolean => {
            const e = spec.expr;
            if (e.* == .name) return .{ .b = std.mem.eql(u8, e.name, "true") };
            return error.BadDefault;
        },
        .int, .timestamp => return .{ .i = @intCast(try schema.evalInt(u, spec.expr)) },
        .fixed => |fx| return .{ .i = try fixedFromExpr(u, spec.expr, fx.f_bits) },
        .float => |ft| {
            const e = spec.expr;
            if (e.* == .float_text) {
                if (ft.wide) return .{ .f64 = try std.fmt.parseFloat(f64, e.float_text) };
                return .{ .f32 = try std.fmt.parseFloat(f32, e.float_text) };
            }
            const n = try schema.evalNum(u, e);
            const v = schema.toF(n);
            if (ft.wide) return .{ .f64 = v };
            return .{ .f32 = @floatCast(v) };
        },
        .string, .bytes => return .{ .str = spec.str },
        .enum_ => |ed| {
            const e = spec.expr;
            if (e.* == .name) return .{ .en = ed.byName(e.name) orelse return error.BadDefault };
            return error.BadDefault;
        },
        .flags => |fd| {
            var m: u64 = 0;
            for (spec.names) |n| m |= @as(u64, 1) << (fd.byName(n) orelse return error.BadDefault);
            return .{ .fl = m };
        },
        else => return error.BadDefault,
    }
}

/// A fixed default is whole units (SPEC.md §4.6): raw = value × 2^F, exactly.
fn fixedFromExpr(u: *const schema.Unit, e: *const schema.Expr, f_bits: u8) !Int {
    switch (e.*) {
        .float_text => |t| return decimalToRaw(t, f_bits) orelse error.BadDefault,
        else => {
            const v = try schema.evalInt(u, e);
            return @intCast(v << @intCast(f_bits));
        },
    }
}

/// Exact decimal text (optionally signed, optional fraction, no exponent) to
/// raw = value × 2^F; null when the value is not representable exactly.
pub fn decimalToRaw(t: []const u8, f_bits: u8) ?Int {
    var s = t;
    var neg = false;
    if (s.len > 0 and (s[0] == '-' or s[0] == '+')) {
        neg = s[0] == '-';
        s = s[1..];
    }
    var num: i256 = 0;
    var den_pow: u32 = 0;
    var seen_dot = false;
    for (s) |c| {
        if (c == '.') {
            seen_dot = true;
            continue;
        }
        if (c < '0' or c > '9') return null;
        num = num * 10 + (c - '0');
        if (seen_dot) den_pow += 1;
        if (num > (@as(i256, 1) << 200)) return null;
    }
    const scaled = num << @intCast(f_bits);
    var den: i256 = 1;
    var k: u32 = 0;
    while (k < den_pow) : (k += 1) den *= 10;
    if (@rem(scaled, den) != 0) return null;
    const raw = @divTrunc(scaled, den);
    const r = if (neg) -raw else raw;
    if (r < std.math.minInt(Int) or r > std.math.maxInt(Int)) return null;
    return @intCast(r);
}

// ------------------------------------------------------------ equality

pub fn eql(a: Value, b: Value, ty: *const Type) bool {
    return switch (ty.*) {
        .boolean => a.b == b.b,
        .int, .fixed, .timestamp => a.i == b.i,
        .float => |ft| if (ft.compressed != null)
            (if (ft.wide) a.f64 == b.f64 else a.f32 == b.f32)
        else if (ft.wide)
            @as(u64, @bitCast(a.f64)) == @as(u64, @bitCast(b.f64))
        else
            @as(u32, @bitCast(a.f32)) == @as(u32, @bitCast(b.f32)),
        .string, .bytes => std.mem.eql(u8, a.str, b.str),
        .enum_ => a.en == b.en,
        .flags => a.fl == b.fl,
        .struct_ => structEql(a.st, b.st),
        .union_ => |ud| blk: {
            if (a.un.arm != b.un.arm) break :blk false;
            if (a.un.arm == 0) break :blk true;
            const arm = ud.arms[a.un.arm - 1];
            const at = arm.ty orelse break :blk true;
            break :blk eql(a.un.payload.?.*, b.un.payload.?.*, at);
        },
        .array => |at| blk: {
            if (a.arr.len != b.arr.len) break :blk false;
            for (a.arr, b.arr) |x, y| if (!elemEql(x, y, &at)) break :blk false;
            break :blk true;
        },
        .keyed => |kt| blk: {
            for (a.keyed, b.keyed) |x, y| if (!eql(x, y, kt.elem)) break :blk false;
            break :blk true;
        },
        .map => a.map.len == 0 and b.map.len == 0,
        .pointer, .blob_bytes, .blob_string => a.ptr == b.ptr,
    };
}

fn elemEql(x: Value, y: Value, at: *const schema.ArrayT) bool {
    if (at.elem_optional) {
        if (x.opt == null or y.opt == null) return x.opt == null and y.opt == null;
        return eql(x.opt.?.*, y.opt.?.*, at.elem);
    }
    return eql(x, y, at.elem);
}

fn structEql(a: *Struct, b: *Struct) bool {
    for (a.decl.fields, 0..) |*f, i| {
        if (f.optional) {
            const x = a.fields[i].opt;
            const y = b.fields[i].opt;
            if (x == null or y == null) {
                if (x != null or y != null) return false;
                continue;
            }
            if (!eql(x.?.*, y.?.*, f.ty)) return false;
        } else if (!eql(a.fields[i], b.fields[i], f.ty)) return false;
    }
    return true;
}

// ------------------------------------------------------------ elision (SPEC-TABLES.md §22.2)

pub fn guardOn(s: *const Struct, f: *const schema.Field) bool {
    for (f.guards) |g| {
        const gv = s.fields[g.field];
        if (gv != .b) return false;
        if (gv.b != g.want) return false;
    }
    return true;
}

/// A body is its terminator alone: every field elided.
pub fn bodyEmpty(a: Allocator, s: *const Struct) Error!bool {
    for (s.decl.fields, 0..) |*f, i| {
        if (!try fieldElided(a, s, f, s.fields[i])) return false;
    }
    return true;
}

pub fn fieldElided(a: Allocator, s: *const Struct, f: *const schema.Field, v: Value) Error!bool {
    if (!guardOn(s, f)) return true;
    if (f.optional) return v.opt == null;
    if (f.required) return false;
    const unit: ?*const schema.Unit = s.decl.unit_ptr();
    switch (f.ty.*) {
        .struct_ => return bodyEmpty(a, v.st),
        .pointer, .blob_bytes, .blob_string => return v.ptr == null,
        .union_ => return v.un.arm == 0,
        .map => return v.map.len == 0,
        .keyed => |kt| {
            for (v.keyed) |x| if (!try slotDefault(a, x, kt.elem)) return false;
            return true;
        },
        .array => |at| {
            if (at.exact) {
                if (!at.elem_optional and at.elem.* == .struct_) return false;
                for (v.arr) |x| if (!try elemAtDefault(a, x, &at)) return false;
                return true;
            }
            if (at.min == 0) return v.arr.len == 0;
            if (v.arr.len != at.min) return false;
            for (v.arr) |x| if (!try elemAtDefault(a, x, &at)) return false;
            return true;
        },
        else => {
            const d = try fieldDefault(a, unit, f);
            return eql(v, d, f.ty);
        },
    }
}

/// An array element at its default: a table element's body is its
/// terminator alone; an enum or union element at the zero reference.
pub fn elemAtDefault(a: Allocator, x: Value, at: *const schema.ArrayT) Error!bool {
    if (at.elem_optional) return x.opt == null;
    return slotDefault(a, x, at.elem);
}

pub fn slotDefault(a: Allocator, x: Value, ty: *const Type) Error!bool {
    switch (ty.*) {
        .struct_ => return bodyEmpty(a, x.st),
        .union_ => return x.un.arm == 0,
        .pointer, .blob_bytes, .blob_string => return x.ptr == null,
        else => {
            const z = try zeroOf(a, ty);
            return eql(x, z, ty);
        },
    }
}

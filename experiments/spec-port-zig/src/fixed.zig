// Form 3, the fixed form (SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md):
// the layout, its hash with the definitions digest, the record write and the
// identity read. A file under any other hash is refused `layout_unknown`: this
// port carries no lock lineage (SPEC-GAPS.md).
const std = @import("std");
const schema = @import("schema.zig");
const value = @import("value.zig");
const wire = @import("wire.zig");
const Type = schema.Type;
const Int = schema.Int;
const Value = value.Value;
const Allocator = std.mem.Allocator;
const Error = wire.Error;

fn refuse(reason: []const u8) Error {
    wire.last_reason = reason;
    return error.Refused;
}

const List = std.ArrayList(u8);

fn put(a: Allocator, out: *List, v: u128, w: usize) Error!void {
    var x = v;
    for (0..w) |_| {
        try out.append(a, @intCast(x & 0xff));
        x >>= 8;
    }
}

fn get(b: []const u8) u128 {
    var v: u128 = 0;
    var i = b.len;
    while (i > 0) {
        i -= 1;
        v = (v << 8) | b[i];
    }
    return v;
}

/// An ordinal's storage width from the top value it must hold (note a).
pub fn ordinalWidth(top: usize) usize {
    if (top <= 0xff) return 1;
    if (top <= 0xffff) return 2;
    if (top <= 0xffffffff) return 4;
    return 8;
}

fn isCounted(at: schema.ArrayT) bool {
    return !at.exact;
}

/// C(T): the constant size (§3.4, THE CONSTANT SIZE, KIND BY KIND).
pub fn size(ty: *const Type) usize {
    return switch (ty.*) {
        .boolean => 1,
        .int => |it| if (it.bits_n != 0) (if (it.bits_n <= 32) 4 else 8) else it.width / 8,
        .fixed => |fx| fx.width / 8,
        .float => |ft| if (ft.wide) 8 else 4,
        .timestamp => 8,
        .string => |cap| 4 + @as(usize, @intCast(cap.?)),
        .bytes => |cap| 4 + @as(usize, @intCast(cap)),
        .enum_ => |ed| ordinalWidth(ed.variants.len),
        .flags => 8,
        .struct_ => |sd| structSize(sd),
        .union_ => |ud| blk: {
            var w: usize = 0;
            for (ud.arms) |arm| if (arm.ty) |t| {
                w = @max(w, size(t));
            };
            break :blk ordinalWidth(ud.arms.len) + w;
        },
        .array => |at| blk: {
            const n: usize = @intCast(at.max.?);
            const e = size(at.elem);
            break :blk if (at.exact) n * e else 4 + n * e;
        },
        .keyed => |kt| kt.key.variants.len * size(kt.elem),
        .map, .pointer, .blob_bytes, .blob_string => 0,
    };
}

fn fieldSize(f: *const schema.Field) usize {
    return size(f.ty) + @as(usize, if (f.optional) 1 else 0);
}

pub fn structSize(sd: *const schema.StructDecl) usize {
    var n: usize = 0;
    for (sd.fields) |*f| n += fieldSize(f);
    return n;
}

fn leafKind(ty: *const Type) u8 {
    return switch (ty.*) {
        .enum_ => 30,
        .bytes => 14,
        .struct_ => 13,
        .union_ => 15,
        .array => 14,
        .keyed => 16,
        else => wire.kindOf(ty),
    };
}

// ------------------------------------------------------------ the layout

const Lay = struct {
    a: Allocator,
    out: List = .empty,
    count: u32 = 0,

    fn entry(l: *Lay, id: u64, kind: u8, sz: usize, children: usize) Error!void {
        try put(l.a, &l.out, id, 8);
        try l.out.append(l.a, kind);
        try put(l.a, &l.out, sz, 4);
        try put(l.a, &l.out, children, 4);
        l.count += 1;
    }

    fn field(l: *Lay, id: u64, ty: *const Type, optional: bool) Error!void {
        if (optional) try l.entry(id, 35, 1 + size(ty), 1);
        try l.ty(id, ty);
    }

    fn ty(l: *Lay, id: u64, t: *const Type) Error!void {
        switch (t.*) {
            .enum_ => |ed| {
                try l.entry(id, 30, ordinalWidth(ed.variants.len), ed.variants.len);
                for (ed.variants) |v| try l.entry(v.id, 32, 0, 0);
            },
            .bytes => {
                try l.entry(id, 14, size(t), 1);
                try l.entry(0, 6, 1, 0);
            },
            .struct_ => |sd| {
                try l.entry(id, 13, size(t), sd.fields.len);
                for (sd.fields) |*f| try l.field(f.id, f.ty, f.optional);
            },
            .union_ => |ud| {
                try l.entry(id, 15, size(t), ud.arms.len);
                for (ud.arms) |arm| {
                    if (arm.ty) |at| try l.ty(arm.id, at) else try l.entry(arm.id, 32, 0, 0);
                }
            },
            .array => |at| {
                try l.entry(id, 14, size(t), 1);
                try l.ty(0, at.elem);
            },
            .keyed => |kt| {
                try l.entry(id, 16, size(t), 2);
                try l.entry(schema.fnv1a64(kt.key.name), 30, ordinalWidth(kt.key.variants.len), kt.key.variants.len);
                for (kt.key.variants) |v| try l.entry(v.id, 32, 0, 0);
                try l.ty(0, kt.elem);
            },
            else => try l.entry(id, leafKind(t), size(t), 0),
        }
    }
};

// ------------------------------------------------------------ the definitions digest (§3.4, current scheme)

const prefix = "\x00schema-fixed-definitions\x02";

/// The kind a digest path segment names (§3.4, the digest, step 1).
fn digestKind(t: *const Type) u32 {
    return switch (t.*) {
        .keyed => 16,
        .array, .bytes => 14,
        .struct_ => 13,
        .union_ => 15,
        .string => 12,
        .enum_ => 7, // SPEC-GAP 15: SPEC-TABLES.md says 7; the layout says 30
        .flags => 9,
        .timestamp => 5,
        else => wire.kindOf(t),
    };
}

const Dig = struct {
    a: Allocator,
    out: List = .empty,

    fn seg(d: *Dig, path: *List, role: u8, kind: u32, id: u64) Error!void {
        try path.append(d.a, role);
        try put(d.a, path, kind, 4);
        try put(d.a, path, id, 8);
    }

    fn row(d: *Dig, tag: u8, path: []const u8, payload: []const u8) Error!void {
        try d.out.append(d.a, tag);
        try put(d.a, &d.out, path.len, 4);
        try d.out.appendSlice(d.a, path);
        try put(d.a, &d.out, payload.len, 4);
        try d.out.appendSlice(d.a, payload);
    }

    fn canon(d: *Dig, p: *List, v: Int) Error!void {
        try p.append(d.a, if (v < 0) 1 else 0);
        var mag: u128 = @intCast(if (v < 0) -v else v);
        var be: [16]u8 = undefined;
        var n: usize = 0;
        while (mag != 0) : (n += 1) {
            be[15 - n] = @intCast(mag & 0xff);
            mag >>= 8;
        }
        try put(d.a, p, n, 4);
        try p.appendSlice(d.a, be[16 - n ..]);
    }

    fn field(d: *Dig, parent: []const u8, role: u8, id: u64, ty: *const Type, optional: bool) Error!void {
        var path: List = .empty;
        try path.appendSlice(d.a, parent);
        try d.seg(&path, role, digestKind(ty), id);
        if (optional) try d.seg(&path, '?', 35, 0);
        var t = ty;
        switch (ty.*) {
            .array => |at| {
                if (!at.exact) {
                    var p: List = .empty;
                    try put(d.a, &p, at.min, 8);
                    try d.row('C', path.items, p.items);
                }
                try d.seg(&path, 'e', digestKind(at.elem), 0);
                t = at.elem;
            },
            .keyed => |kt| {
                try d.seg(&path, 'e', digestKind(kt.elem), 0);
                t = kt.elem;
            },
            else => {},
        }
        try d.leaf(path.items, t);
    }

    fn leaf(d: *Dig, path: []const u8, t: *const Type) Error!void {
        switch (t.*) {
            .int => |it| {
                if (it.bits_n != 0) {
                    var p: List = .empty;
                    try put(d.a, &p, it.bits_n, 4);
                    return d.row('B', path, p.items);
                }
                const lim = schema.intLimits(it.width, it.signed);
                if (it.min == lim.min and it.max == lim.max) return;
                var p: List = .empty;
                try put(d.a, &p, wire.kindOf(t), 4);
                try p.append(d.a, @intFromBool(it.signed));
                try put(d.a, &p, 0, 4);
                try d.canon(&p, it.min);
                try d.canon(&p, it.max);
                try d.row('R', path, p.items);
            },
            .fixed => |fx| {
                var p: List = .empty;
                try put(d.a, &p, wire.kindOf(t), 4);
                try p.append(d.a, @intFromBool(fx.signed));
                try put(d.a, &p, fx.f_bits, 4);
                try d.canon(&p, fx.min_raw >> @intCast(fx.f_bits));
                try d.canon(&p, fx.max_raw >> @intCast(fx.f_bits));
                try d.row('R', path, p.items);
                var x: List = .empty;
                try put(d.a, &x, fx.i_bits, 4);
                try put(d.a, &x, fx.f_bits, 4);
                try x.append(d.a, @intFromBool(fx.signed));
                try d.row('X', path, x.items);
            },
            .float => |ft| if (ft.compressed) |c| {
                var p: List = .empty;
                try put(d.a, &p, wire.kindOf(t), 4);
                try put(d.a, &p, @as(u64, @bitCast(c.min)), 8);
                try put(d.a, &p, @as(u64, @bitCast(c.max)), 8);
                try d.row('D', path, p.items);
                var q: List = .empty;
                try put(d.a, &q, @as(u64, @bitCast(c.res)), 8);
                try d.row('Q', path, q.items);
            },
            .flags => |fd| {
                var p: List = .empty;
                try put(d.a, &p, fd.names.len, 4);
                try put(d.a, &p, fd.names.len, 4);
                for (fd.names) |n| try put(d.a, &p, schema.fnv1a64(n), 8);
                try d.row('F', path, p.items);
            },
            .struct_ => |sd| for (sd.fields) |*f| try d.field(path, 'f', f.id, f.ty, f.optional),
            .union_ => |ud| for (ud.arms) |arm| if (arm.ty) |at| try d.field(path, 'a', arm.id, at, false),
            else => {},
        }
    }
};

pub const Identity = struct { layout: []u8, hash: u64, record: usize };

pub fn identity(a: Allocator, root: *const schema.StructDecl) Error!Identity {
    var l: Lay = .{ .a = a };
    try l.entry(root.id, 13, structSize(root), root.fields.len);
    for (root.fields) |*f| try l.field(f.id, f.ty, f.optional);
    var lay: List = .empty;
    try put(a, &lay, l.count, 4);
    try lay.appendSlice(a, l.out.items);

    var d: Dig = .{ .a = a };
    try d.out.appendSlice(a, prefix);
    var rootp: List = .empty;
    try rootp.append(a, 't');
    try put(a, &rootp, 13, 4);
    try put(a, &rootp, root.id, 8);
    for (root.fields) |*f| try d.field(rootp.items, 'f', f.id, f.ty, f.optional);

    var h: u64 = 0xcbf29ce484222325;
    for (lay.items) |b| h = (h ^ b) *% 0x100000001b3;
    for (d.out.items) |b| h = (h ^ b) *% 0x100000001b3;
    return .{ .layout = lay.items, .hash = h, .record = 8 + structSize(root) };
}

// ------------------------------------------------------------ the write (§3.1)

const W = struct {
    a: Allocator,
    out: *List,

    fn zero(w: *W, n: usize) Error!void {
        try w.out.appendNTimes(w.a, 0, n);
    }

    fn st(w: *W, s: *const value.Struct) Error!void {
        for (s.decl.fields, 0..) |*f, i| {
            const v = s.fields[i];
            if (f.optional) {
                if (v.opt) |p| {
                    try w.out.append(w.a, 1);
                    try w.val(f.ty, p.*);
                } else {
                    try w.out.append(w.a, 0);
                    try w.zero(size(f.ty));
                }
            } else try w.val(f.ty, v);
        }
    }

    fn val(w: *W, ty: *const Type, v: Value) Error!void {
        const a = w.a;
        switch (ty.*) {
            .boolean => try w.out.append(a, @intFromBool(v.b)),
            .int => |it| {
                if (v.i < it.min or v.i > it.max) return refuse("invalid_value: out of range");
                try put(a, w.out, @bitCast(@as(i128, @truncate(v.i))), size(ty));
            },
            .fixed => |fx| {
                if (v.i < fx.min_raw or v.i > fx.max_raw) return refuse("invalid_value: out of range");
                try put(a, w.out, @bitCast(@as(i128, @truncate(v.i))), size(ty));
            },
            .timestamp => try put(a, w.out, @bitCast(@as(i128, @truncate(v.i))), 8),
            .float => |ft| {
                if (ft.wide) {
                    if (!std.math.isFinite(v.f64)) return refuse("non_finite_float");
                    try put(a, w.out, @as(u64, @bitCast(v.f64)), 8);
                } else {
                    var f = v.f32;
                    if (!std.math.isFinite(f)) return refuse("non_finite_float");
                    if (ft.compressed) |c| {
                        if (f < c.min or f > c.max) return refuse("invalid_value: out of range");
                        if (f == 0) f = 0;
                    }
                    try put(a, w.out, @as(u32, @bitCast(f)), 4);
                }
            },
            .string => |cap| {
                const n: usize = @intCast(cap.?);
                if (v.str.len > n) return refuse("invalid_value: length");
                if (!wire.validUtf8NoNul(v.str)) return refuse("invalid_value: not text");
                try put(a, w.out, v.str.len, 4);
                try w.out.appendSlice(a, v.str);
                try w.zero(n - v.str.len);
            },
            .bytes => |cap| {
                const n: usize = @intCast(cap);
                if (v.str.len > n) return refuse("invalid_value: length");
                try put(a, w.out, v.str.len, 4);
                try w.out.appendSlice(a, v.str);
                try w.zero(n - v.str.len);
            },
            .enum_ => |ed| {
                if (v.en > ed.variants.len) return refuse("invalid_value: enum ordinal");
                if (v.en != 0 and ed.variants[v.en - 1].deprecated) return refuse("invalid_value: deprecated variant");
                try put(a, w.out, v.en, size(ty));
            },
            .flags => |fd| {
                if (fd.names.len < 64 and (v.fl >> @intCast(fd.names.len)) != 0) return refuse("invalid_value: flags bit");
                try put(a, w.out, v.fl, 8);
            },
            .struct_ => try w.st(v.st),
            .union_ => |ud| {
                const tw = ordinalWidth(ud.arms.len);
                const span = size(ty) - tw;
                if (v.un.arm > ud.arms.len) return refuse("invalid_value: union tag");
                try put(a, w.out, v.un.arm, tw);
                if (v.un.arm == 0) return w.zero(span);
                const arm = ud.arms[v.un.arm - 1];
                const at = arm.ty orelse return w.zero(span);
                const before = w.out.items.len;
                try w.val(at, v.un.payload.?.*);
                try w.zero(span - (w.out.items.len - before));
            },
            .array => |at| {
                const mx: usize = @intCast(at.max.?);
                if (v.arr.len > mx or v.arr.len < at.min) return refuse("invalid_value: count");
                if (!at.exact) try put(a, w.out, v.arr.len, 4);
                for (v.arr) |x| try w.val(at.elem, x);
                try w.zero((mx - v.arr.len) * size(at.elem));
            },
            .keyed => |kt| for (v.keyed) |x| try w.val(kt.elem, x),
            else => return refuse("invalid_value: not a fixed construct"),
        }
    }
};

/// FixedSave of one record: the whole file, or a refusal.
pub fn save(a: Allocator, s: *const value.Struct) Error![]u8 {
    const id = try identity(a, s.decl);
    var out: List = .empty;
    try out.append(a, 3);
    try out.appendNTimes(a, 0, 7);
    try put(a, &out, id.hash, 8);
    try put(a, &out, id.layout.len, 4);
    try out.appendSlice(a, id.layout);
    try put(a, &out, id.hash, 8);
    var w: W = .{ .a = a, .out = &out };
    try w.st(s);
    return out.items;
}

// ------------------------------------------------------------ the read (§4, identity plan)

const R = struct {
    a: Allocator,
    unit: *const schema.Unit,

    fn dead(b: []const u8) Error!void {
        for (b) |x| if (x != 0) return refuse("wire_damaged: non-zero dead byte");
    }

    fn st(r: *R, sd: *schema.StructDecl, b: []const u8) Error!*value.Struct {
        const s = try r.a.create(value.Struct);
        s.* = .{ .decl = sd, .fields = try r.a.alloc(Value, sd.fields.len) };
        var off: usize = 0;
        for (sd.fields, 0..) |*f, i| {
            const n = size(f.ty);
            if (f.optional) {
                const flag = b[off];
                if (flag > 1) return refuse("wire_damaged: present byte");
                const body = b[off + 1 .. off + 1 + n];
                if (flag == 0) {
                    try dead(body);
                    s.fields[i] = .{ .opt = null };
                } else {
                    const p = try r.a.create(Value);
                    p.* = try r.val(f.ty, body, f);
                    s.fields[i] = .{ .opt = p };
                }
                off += 1 + n;
            } else {
                s.fields[i] = try r.val(f.ty, b[off .. off + n], f);
                off += n;
            }
        }
        return s;
    }

    fn val(r: *R, ty: *const Type, b: []const u8, f: ?*const schema.Field) Error!Value {
        switch (ty.*) {
            .boolean => {
                if (b[0] > 1) return refuse("wire_damaged: bool byte");
                return .{ .b = b[0] == 1 };
            },
            .int => |it| {
                const raw = get(b);
                const v: Int = if (it.signed) signExtend(raw, b.len) else @intCast(raw);
                if (v < it.min or v > it.max) return refuse("out_of_range");
                return .{ .i = v };
            },
            .fixed => |fx| {
                const raw = get(b);
                const v: Int = if (fx.signed) signExtend(raw, b.len) else @intCast(raw);
                if (v < fx.min_raw or v > fx.max_raw) return refuse("out_of_range");
                return .{ .i = v };
            },
            .timestamp => return .{ .i = signExtend(get(b), 8) },
            .float => |ft| {
                if (ft.wide) {
                    const x: f64 = @bitCast(@as(u64, @intCast(get(b))));
                    if (!std.math.isFinite(x)) return refuse("non_finite_float");
                    return .{ .f64 = x };
                }
                var x: f32 = @bitCast(@as(u32, @intCast(get(b))));
                if (!std.math.isFinite(x)) return refuse("non_finite_float");
                if (ft.compressed) |c| {
                    if (x < c.min or x > c.max) return refuse("out_of_range");
                    if (x == 0) x = 0;
                }
                return .{ .f32 = x };
            },
            .string, .bytes => {
                const n = b.len - 4;
                const len: i32 = @bitCast(@as(u32, @intCast(get(b[0..4]))));
                if (len < 0 or len > n) return refuse("length_over_bound");
                const used = b[4 .. 4 + @as(usize, @intCast(len))];
                try dead(b[4 + used.len ..]);
                if (ty.* == .string and !wire.validUtf8NoNul(used)) return refuse("wire_damaged: ill-formed text");
                return .{ .str = used };
            },
            .enum_ => |ed| {
                const raw = get(b);
                if (raw > ed.variants.len) return refuse("out_of_range: ordinal");
                const ord: u32 = @intCast(raw);
                if (ord != 0 and ed.variants[ord - 1].deprecated) {
                    if (f) |fl| if (fl.default != null) {
                        const dv = value.fieldDefault(r.a, r.unit, fl) catch return .{ .en = 0 };
                        if (dv == .en) return dv;
                    };
                    return .{ .en = 0 };
                }
                return .{ .en = ord };
            },
            .flags => return .{ .fl = @intCast(get(b)) },
            .struct_ => |sd| return .{ .st = try r.st(sd, b) },
            .union_ => |ud| {
                const tw = ordinalWidth(ud.arms.len);
                const tag = get(b[0..tw]);
                if (tag > ud.arms.len) return refuse("out_of_range: union tag");
                const span = b[tw..];
                if (tag == 0) {
                    try dead(span);
                    return .{ .un = .{ .arm = 0, .payload = null } };
                }
                const ai: u32 = @intCast(tag);
                const at = ud.arms[ai - 1].ty orelse {
                    try dead(span);
                    return .{ .un = .{ .arm = ai, .payload = null } };
                };
                const n = size(at);
                try dead(span[n..]);
                const p = try r.a.create(Value);
                p.* = try r.val(at, span[0..n], null);
                return .{ .un = .{ .arm = ai, .payload = p } };
            },
            .array => |at| {
                const e = size(at.elem);
                const mx: usize = @intCast(at.max.?);
                var body = b;
                var count = mx;
                if (!at.exact) {
                    const c: i32 = @bitCast(@as(u32, @intCast(get(b[0..4]))));
                    if (c < 0 or c > mx) return refuse("count_over_bound");
                    if (c < at.min) return refuse("count_over_bound: below the minimum");
                    count = @intCast(c);
                    body = b[4..];
                }
                try dead(body[count * e ..]);
                const xs = try r.a.alloc(Value, count);
                for (xs, 0..) |*x, i| x.* = try r.val(at.elem, body[i * e .. (i + 1) * e], null);
                return .{ .arr = xs };
            },
            .keyed => |kt| {
                const e = size(kt.elem);
                const xs = try r.a.alloc(Value, kt.key.variants.len);
                for (xs, 0..) |*x, i| x.* = try r.val(kt.elem, b[i * e .. (i + 1) * e], null);
                return .{ .keyed = xs };
            },
            else => return refuse("kind_mismatch"),
        }
    }
};

fn signExtend(raw: u128, width: usize) Int {
    if (width == 16) return @as(i128, @bitCast(raw));
    const sh: u7 = @intCast(128 - width * 8);
    return @as(i128, @bitCast(raw << sh)) >> sh;
}

/// FixedLoad of one record (§2 of the algorithm, the load in order).
pub fn load(a: Allocator, unit: *const schema.Unit, root: *schema.StructDecl, b: []const u8) Error!*value.Struct {
    if (b.len == 0) return refuse("truncated");
    if (b[0] != 3) return refuse(switch (b[0]) {
        1 => "previous_form",
        2 => "message_form_as_file",
        else => "newer_form",
    });
    if (b.len < 20) return refuse("truncated");
    for (b[1..8]) |x| if (x != 0) return refuse("reserved_not_zero");
    const L: usize = @intCast(get(b[16..20]));
    if (20 + L > b.len) return refuse("layout_malformed");
    const h: u64 = @intCast(get(b[8..16]));
    const id = try identity(a, root);
    if (h != id.hash) return refuse("layout_unknown");
    if (!std.mem.eql(u8, b[20 .. 20 + L], id.layout)) return refuse("layout_malformed");
    const rest = b.len - 20 - L;
    if (id.record <= 8 or rest % id.record != 0) return refuse("truncated");
    if (rest / id.record != 1) return refuse(if (rest == 0) "truncated: no record" else "batch_too_large");
    const rec = b[20 + L ..];
    if (get(rec[0..8]) != h) return refuse("no_layout");
    var r: R = .{ .a = a, .unit = unit };
    return r.st(root, rec[8..]);
}

// Form 1, the variable form: the one read (SPEC-TABLES.md §3, §3.1, §3.2,
// §4, §6.5) and the canonical write (§22.1 - §22.5).
//
// The read is two passes over the bytes. Pass one is declaration-free: it
// walks every body by its kind bytes alone and refuses everything the wire
// itself shows to be damage (framing, canonical LEB128, the id table and its
// first-use order, the closed kind lists, text content, non-finite floats,
// depth, the node table and the pointer graph). Pass two decodes against the
// reader's declarations (kinds, widening, ranges, bounds, names).
const std = @import("std");
const schema = @import("schema.zig");
const value = @import("value.zig");
const Type = schema.Type;
const Int = schema.Int;
const Value = value.Value;
const Allocator = std.mem.Allocator;

pub const Error = error{ Refused, OutOfMemory };

pub const max_ids: u64 = 4096; // SPEC-TABLES.md §3: the id-table maximum
pub const max_depth: u32 = 128; // §4: ONE NESTING BOUND
pub const extent_cap: u64 = 1 << 31; // §2.2: int32 extent storage cap
pub const blob_cap: u64 = (1 << 32) - 1; // §3.1

pub const bytes_id = schema.fnv1a64("bytes");
pub const string_id = schema.fnv1a64("string");
pub const value_id = schema.fnv1a64("value");

pub var last_reason: []const u8 = "";

fn refuse(reason: []const u8) Error {
    last_reason = reason;
    return error.Refused;
}

// ------------------------------------------------------------ bytes

const Rd = struct {
    b: []const u8,
    p: usize = 0,

    fn left(r: *const Rd) usize {
        return r.b.len - r.p;
    }
    fn byte(r: *Rd) Error!u8 {
        if (r.p >= r.b.len) return refuse("wire_damaged: truncated");
        r.p += 1;
        return r.b[r.p - 1];
    }
    fn take(r: *Rd, n: u64) Error![]const u8 {
        if (n > r.left()) return refuse("wire_damaged: length past body");
        const s = r.b[r.p .. r.p + @as(usize, @intCast(n))];
        r.p += @intCast(n);
        return s;
    }
    /// canonical unsigned LEB128, at most ten bytes (§3)
    fn leb(r: *Rd) Error!u64 {
        var v: u64 = 0;
        var i: u32 = 0;
        while (true) : (i += 1) {
            const c = try r.byte();
            if (i == 9) {
                if (c > 1) return refuse("wire_damaged: leb128 past 64 bits");
            }
            v |= @as(u64, c & 0x7f) << @intCast(7 * i);
            if (c & 0x80 == 0) {
                if (i > 0 and c == 0) return refuse("wire_damaged: non-minimal leb128");
                return v;
            }
            if (i == 9) return refuse("wire_damaged: leb128 too long");
        }
    }
};

pub fn putLeb(a: Allocator, out: *std.ArrayList(u8), v0: u64) Error!void {
    var v = v0;
    while (true) {
        const c: u8 = @intCast(v & 0x7f);
        v >>= 7;
        if (v == 0) {
            try out.append(a, c);
            return;
        }
        try out.append(a, c | 0x80);
    }
}

fn putLE(a: Allocator, out: *std.ArrayList(u8), v: u128, width_bytes: usize) Error!void {
    var x = v;
    var i: usize = 0;
    while (i < width_bytes) : (i += 1) {
        try out.append(a, @intCast(x & 0xff));
        x >>= 8;
    }
}

fn getLE(b: []const u8) u128 {
    var v: u128 = 0;
    var i: usize = b.len;
    while (i > 0) {
        i -= 1;
        v = (v << 8) | b[i];
    }
    return v;
}

// ------------------------------------------------------------ kinds

pub fn fixedWidth(kind: u8) ?usize {
    return switch (kind) {
        1, 2, 6, 20, 25 => 1,
        3, 7, 21, 26 => 2,
        4, 8, 10, 22, 27 => 4,
        5, 9, 11, 23, 28, 36 => 8,
        18, 19, 24, 29 => 16,
        else => null,
    };
}

fn arrayElemOk(k: u8) bool {
    return (k >= 1 and k <= 13) or k == 15 or k == 17 or (k >= 18 and k <= 29) or k == 30;
}
fn keyedElemOk(k: u8) bool {
    return (k >= 1 and k <= 11) or k == 13 or (k >= 18 and k <= 29) or k == 30;
}

pub fn kindOf(ty: *const Type) u8 {
    return switch (ty.*) {
        .boolean => 1,
        .int => |it| intKind(it.width, it.signed),
        .fixed => |fx| (if (fx.signed) @as(u8, 20) else 25) + widthIndex(fx.width),
        .float => |ft| if (ft.wide) 11 else 10,
        .timestamp => 36,
        .string => 12,
        .bytes => 14,
        .enum_ => 30,
        .flags => 9,
        .struct_ => 13,
        .union_ => 15,
        .array, .map => 14,
        .keyed => 16,
        .pointer, .blob_bytes, .blob_string => 17,
    };
}

fn widthIndex(w: u8) u8 {
    return switch (w) {
        8 => 0,
        16 => 1,
        32 => 2,
        64 => 3,
        else => 4,
    };
}

fn intKind(width: u8, signed: bool) u8 {
    const base: u8 = if (signed) 2 else 6;
    return switch (width) {
        8 => base,
        16 => base + 1,
        32 => base + 2,
        64 => base + 3,
        else => if (signed) 18 else 19,
    };
}

pub fn elemKindOf(at: *const schema.ArrayT) u8 {
    if (at.elem_optional) return 13;
    return kindOf(at.elem);
}

fn ladderPos(k: u8) ?struct { signed: bool, pos: u8 } {
    return switch (k) {
        2, 3, 4, 5 => .{ .signed = true, .pos = k - 2 },
        18 => .{ .signed = true, .pos = 4 },
        6, 7, 8, 9 => .{ .signed = false, .pos = k - 6 },
        19 => .{ .signed = false, .pos = 4 },
        else => null,
    };
}

/// §4: the kind pair decides; returns true when the read widens.
fn kindCheck(declared: u8, wire_kind: u8) Error!bool {
    if (declared == wire_kind) return false;
    if (declared == 11 and wire_kind == 10) return true;
    if (ladderPos(declared)) |d| if (ladderPos(wire_kind)) |w| {
        if (d.signed == w.signed and w.pos < d.pos) return true;
    };
    return refuse("kind_mismatch");
}

// ------------------------------------------------------------ text

pub fn validUtf8NoNul(s: []const u8) bool {
    if (std.mem.indexOfScalar(u8, s, 0) != null) return false;
    return std.unicode.utf8ValidateSlice(s);
}

// ------------------------------------------------------------ pass one: the declaration-free scan

const Scan = struct {
    a: Allocator,
    ids: []const u64,
    next_ref: u64 = 1,
    owner: u64 = 1,
    edges: std.ArrayList([2]u64) = .empty,
    node_count: u64 = 0,
    records: std.ArrayList(Record) = .empty,

    fn useRef(s: *Scan, ref: u64) Error!void {
        if (ref == 0) return;
        if (ref > s.ids.len) return refuse("wire_damaged: reference past the id table");
        if (ref == s.next_ref) {
            s.next_ref += 1;
        } else if (ref > s.next_ref) return refuse("non_canonical: id table not in first-use order");
    }

    fn idOf(s: *Scan, ref: u64) u64 {
        return s.ids[@intCast(ref - 1)];
    }

    fn body(s: *Scan, b: []const u8, depth: u32, is_root: bool) Error!void {
        if (depth > max_depth) return refuse("depth_exceeded");
        var r: Rd = .{ .b = b };
        var seen: std.ArrayList(u64) = .empty;
        while (true) {
            const ref = try r.leb();
            if (ref == 0) {
                if (r.left() != 0) return refuse("wire_damaged: bytes after terminator");
                return;
            }
            try s.useRef(ref);
            for (seen.items) |x| if (x == ref) return refuse("wire_damaged: repeated field");
            try seen.append(s.a, ref);
            const kind = try r.byte();
            const id = s.idOf(ref);
            if (id == schema.node_table_id) {
                if (!is_root) return refuse("wire_damaged: node table in a nested body");
                if (kind != 12) return refuse("wire_damaged: node table kind");
                const l = try r.leb();
                const payload = try r.take(l);
                // the node-table field is the root body's last field
                if (r.left() != 1 or r.b[r.p] != 0) return refuse("non_canonical: node table not last");
                try s.nodeTable(payload);
                continue;
            }
            if (id == schema.build_version_id or id == schema.vocabulary_id)
                return refuse("wire_damaged: reserved id out of place");
            try s.payload(kind, &r, depth);
        }
    }

    fn nodeTable(s: *Scan, payload: []const u8) Error!void {
        var r: Rd = .{ .b = payload };
        const count = try r.leb();
        if (count == 0) return refuse("non_canonical: empty node table");
        var n: u64 = 0;
        while (r.left() > 0) {
            const tref = try r.leb();
            if (tref == 0) return refuse("wire_damaged: node type id 0");
            try s.useRef(tref);
            const l = try r.leb();
            const b = try r.take(l);
            const tid = s.idOf(tref);
            n += 1;
            const saved = s.owner;
            s.owner = n + 1;
            if (tid == bytes_id) {
                if (l > blob_cap) return refuse("blob_over_size_cap");
            } else if (tid == string_id) {
                if (l > blob_cap) return refuse("blob_over_size_cap");
                if (!validUtf8NoNul(b)) return refuse("wire_damaged: ill-formed text blob");
            } else {
                try s.body(b, 0, false);
            }
            s.owner = saved;
            try s.records.append(s.a, .{ .type_id = tid, .body = b });
        }
        if (n != count) return refuse("wire_damaged: node count disagrees with the scan");
        s.node_count = count;
    }

    /// One payload after its kind byte, at a body of `depth`.
    fn payload(s: *Scan, kind: u8, r: *Rd, depth: u32) Error!void {
        if (fixedWidth(kind)) |w| {
            const b = try r.take(w);
            try scalarCheck(kind, b);
            return;
        }
        switch (kind) {
            12 => {
                const l = try r.leb();
                const b = try r.take(l);
                if (!validUtf8NoNul(b)) return refuse("wire_damaged: ill-formed text");
            },
            13 => {
                const l = try r.leb();
                try s.body(try r.take(l), depth + 1, false);
            },
            14 => {
                const l = try r.leb();
                try s.arrayBody(try r.take(l), depth + 1);
            },
            15 => try s.unionPayload(r, depth),
            16 => {
                const l = try r.leb();
                try s.keyedBody(try r.take(l), depth + 1);
            },
            17 => {
                const idx = try r.leb();
                if (idx != 0) try s.edges.append(s.a, .{ s.owner, idx });
            },
            30 => try s.useRef(try r.leb()),
            31 => {
                const l = try r.leb();
                _ = try r.take(l);
            },
            32 => {
                const l = try r.leb();
                if (l != 0) return refuse("wire_damaged: kind 32 with a payload");
            },
            else => return refuse("wire_damaged: unknown kind"),
        }
    }

    fn arrayBody(s: *Scan, b: []const u8, depth: u32) Error!void {
        if (depth > max_depth) return refuse("depth_exceeded");
        if (b.len < 2) return refuse("wire_damaged: array body too short");
        var r: Rd = .{ .b = b };
        const ek = try r.byte();
        if (!arrayElemOk(ek)) return refuse("wire_damaged: element kind");
        const n = try r.leb();
        if (n >= extent_cap) return refuse("count_over_extent_cap");
        if (fixedWidth(ek)) |w| {
            if (n * w != r.left()) return refuse("wire_damaged: array count against its body");
            var i: u64 = 0;
            while (i < n) : (i += 1) try scalarCheck(ek, try r.take(w));
            return;
        }
        if (n > r.left()) return refuse("wire_damaged: count the body cannot cover");
        var i: u64 = 0;
        while (i < n) : (i += 1) try s.element(ek, &r, depth);
        if (r.left() != 0) return refuse("wire_damaged: bytes after elements");
    }

    /// An array element in its place (§3, array elements).
    fn element(s: *Scan, ek: u8, r: *Rd, depth: u32) Error!void {
        switch (ek) {
            12 => {
                const l = try r.leb();
                if (!validUtf8NoNul(try r.take(l))) return refuse("wire_damaged: ill-formed text");
            },
            13 => {
                const l = try r.leb();
                try s.body(try r.take(l), depth + 1, false);
            },
            15 => try s.unionPayload(r, depth),
            17 => {
                const idx = try r.leb();
                if (idx != 0) try s.edges.append(s.a, .{ s.owner, idx });
            },
            30 => try s.useRef(try r.leb()),
            else => unreachable,
        }
    }

    fn keyedBody(s: *Scan, b: []const u8, depth: u32) Error!void {
        if (depth > max_depth) return refuse("depth_exceeded");
        if (b.len < 2) return refuse("wire_damaged: keyed body too short");
        var r: Rd = .{ .b = b };
        const ek = try r.byte();
        if (!keyedElemOk(ek)) return refuse("wire_damaged: keyed element kind");
        const n = try r.leb();
        if (n >= extent_cap) return refuse("count_over_extent_cap");
        if (n > r.left()) return refuse("wire_damaged: count the body cannot cover");
        var keys: std.ArrayList(u64) = .empty;
        var i: u64 = 0;
        while (i < n) : (i += 1) {
            const kref = try r.leb();
            if (kref == 0) return refuse("wire_damaged: keyed slot key 0");
            try s.useRef(kref);
            for (keys.items) |k| if (k == kref) return refuse("wire_damaged: repeated keyed slot");
            try keys.append(s.a, kref);
            const l = try r.leb();
            const e = try r.take(l);
            try s.framedElem(ek, e, depth);
        }
        if (r.left() != 0) return refuse("wire_damaged: bytes after keyed slots");
    }

    /// A payload framed by an outer `L`: a keyed element or an arm.
    fn framedElem(s: *Scan, k: u8, e: []const u8, depth: u32) Error!void {
        if (fixedWidth(k)) |w| {
            if (e.len != w) return refuse("wire_damaged: L does not frame the kind");
            try scalarCheck(k, e);
            return;
        }
        var r: Rd = .{ .b = e };
        switch (k) {
            12 => if (!validUtf8NoNul(e)) return refuse("wire_damaged: ill-formed text"),
            13 => try s.body(e, depth + 1, false),
            14 => try s.arrayBody(e, depth + 1),
            15 => {
                try s.unionPayload(&r, depth);
                if (r.left() != 0) return refuse("wire_damaged: bytes after an inner union");
            },
            16 => try s.keyedBody(e, depth + 1),
            17 => {
                const idx = try r.leb();
                if (r.left() != 0) return refuse("wire_damaged: L does not frame the index");
                if (idx != 0) try s.edges.append(s.a, .{ s.owner, idx });
            },
            30 => {
                try s.useRef(try r.leb());
                if (r.left() != 0) return refuse("wire_damaged: L does not frame the reference");
            },
            31 => {},
            32 => if (e.len != 0) return refuse("wire_damaged: kind 32 with a payload"),
            else => return refuse("wire_damaged: unknown kind"),
        }
    }

    fn unionPayload(s: *Scan, r: *Rd, depth: u32) Error!void {
        const aref = try r.leb();
        if (aref == 0) return;
        try s.useRef(aref);
        const k = try r.byte();
        const l = try r.leb();
        const e = try r.take(l);
        // a union whose arm holds a structural payload is one level (§4)
        const structural = k == 13 or k == 14 or k == 15 or k == 16;
        const d = if (structural) depth + 1 else depth;
        if (d > max_depth) return refuse("depth_exceeded");
        try s.framedElem(k, e, d);
    }
};

fn scalarCheck(kind: u8, b: []const u8) Error!void {
    switch (kind) {
        1 => if (b[0] > 1) return refuse("wire_damaged: bool byte"),
        10 => {
            const f: f32 = @bitCast(@as(u32, @intCast(getLE(b))));
            if (!std.math.isFinite(f)) return refuse("non_finite_float");
        },
        11 => {
            const f: f64 = @bitCast(@as(u64, @intCast(getLE(b))));
            if (!std.math.isFinite(f)) return refuse("non_finite_float");
        },
        else => {},
    }
}

const Record = struct { type_id: u64, body: []const u8 };

/// The pointer graph (§3.1, §6.5): every index in range, no index names the
/// root, every record reached, no cycle, no blob named twice.
fn graphCheck(s: *Scan) Error!void {
    const n = s.node_count;
    const total: usize = @intCast(n + 2);
    var refs = try s.a.alloc(u32, total);
    @memset(refs, 0);
    var adj_count = try s.a.alloc(u32, total + 1);
    @memset(adj_count, 0);
    for (s.edges.items) |e| {
        if (e[1] > n + 1) return refuse("wire_damaged: index past the node table");
        if (e[1] == 1) return refuse("wire_damaged: pointer to the root");
        refs[@intCast(e[1])] += 1;
        adj_count[@intCast(e[0])] += 1;
    }
    for (s.records.items, 0..) |rec, i| {
        if ((rec.type_id == bytes_id or rec.type_id == string_id) and refs[i + 2] > 1)
            return refuse("blob_shared");
    }
    if (n == 0) return;
    // adjacency (CSR)
    var start = try s.a.alloc(u32, total + 1);
    start[0] = 0;
    for (0..total) |i| start[i + 1] = start[i] + adj_count[i];
    var fill = try s.a.dupe(u32, start[0..total]);
    var adj = try s.a.alloc(u32, s.edges.items.len);
    for (s.edges.items) |e| {
        const o: usize = @intCast(e[0]);
        adj[fill[o]] = @intCast(e[1]);
        fill[o] += 1;
    }
    // iterative DFS with three marks, entered once (linear, §3.1)
    var mark = try s.a.alloc(u8, total);
    @memset(mark, 0);
    var stack: std.ArrayList([2]u32) = .empty;
    try stack.append(s.a, .{ 1, start[1] });
    mark[1] = 1;
    while (stack.items.len > 0) {
        const top = &stack.items[stack.items.len - 1];
        const v = top[0];
        if (top[1] < start[v + 1]) {
            const w = adj[top[1]];
            top[1] += 1;
            if (mark[w] == 1) return refuse("wire_damaged: cycle");
            if (mark[w] == 0) {
                mark[w] = 1;
                try stack.append(s.a, .{ w, start[w] });
            }
        } else {
            mark[v] = 2;
            _ = stack.pop();
        }
    }
    var i: usize = 2;
    while (i < total) : (i += 1) {
        if (mark[i] == 0) return refuse("wire_damaged: unreachable record");
    }
}

// ------------------------------------------------------------ the read

pub const Report = struct { unknown: u32 = 0, widened: u32 = 0 };

const Fixup = struct {
    slot: *Value,
    index: u64,
    want: union(enum) { table: *schema.StructDecl, blob_bytes, blob_string },
};

const Dec = struct {
    a: Allocator,
    unit: *const schema.Unit,
    ids: []const u64,
    report: *Report,
    fixups: std.ArrayList(Fixup) = .empty,

    fn idOf(d: *Dec, ref: u64) u64 {
        return d.ids[@intCast(ref - 1)];
    }

    fn structBody(d: *Dec, sd: *schema.StructDecl, b: []const u8, is_root: bool) Error!*value.Struct {
        _ = is_root;
        const s = try d.a.create(value.Struct);
        s.* = .{ .decl = sd, .fields = try d.a.alloc(Value, sd.fields.len) };
        const seen = try d.a.alloc(bool, sd.fields.len);
        @memset(seen, false);
        var r: Rd = .{ .b = b };
        while (true) {
            const ref = try r.leb();
            if (ref == 0) break;
            const kind = try r.byte();
            const id = d.idOf(ref);
            const fb = try framed(kind, &r);
            if (id == schema.node_table_id) continue;
            const fi = sd.fieldById(id) orelse {
                d.report.unknown += 1;
                continue;
            };
            const f = &sd.fields[fi];
            seen[fi] = true;
            if (f.optional) {
                const p = try d.a.create(Value);
                p.* = try d.decode(f.ty, kind, fb, f);
                s.fields[fi] = .{ .opt = p };
            } else {
                s.fields[fi] = try d.decode(f.ty, kind, fb, f);
            }
        }
        for (sd.fields, 0..) |*f, i| {
            if (seen[i]) continue;
            if (f.required and value.guardOn(s, f)) return refuse("missing_required");
            s.fields[i] = value.fieldDefault(d.a, d.unit, f) catch return refuse("bad default");
        }
        return s;
    }

    fn decode(d: *Dec, ty: *const Type, kind: u8, b: []const u8, field: ?*const schema.Field) Error!Value {
        const dk = kindOf(ty);
        if (try kindCheck(dk, kind)) d.report.widened += 1;
        switch (ty.*) {
            .boolean => return .{ .b = b[0] == 1 },
            .int => |it| {
                const v = readInt(kind, b);
                if (v < it.min or v > it.max) return refuse("out_of_range");
                return .{ .i = v };
            },
            .fixed => |fx| {
                const v = readInt(kind, b);
                if (v < fx.min_raw or v > fx.max_raw) return refuse("out_of_range");
                return .{ .i = v };
            },
            .timestamp => return .{ .i = readInt(5, b) },
            .float => |ft| {
                var x: f64 = undefined;
                if (kind == 10) {
                    const f: f32 = @bitCast(@as(u32, @intCast(getLE(b))));
                    x = f;
                    if (!ft.wide) {
                        if (ft.compressed) |c| {
                            if (f < c.min or f > c.max) return refuse("out_of_range");
                            return .{ .f32 = if (f == 0) 0 else f };
                        }
                        return .{ .f32 = f };
                    }
                } else x = @bitCast(@as(u64, @intCast(getLE(b))));
                return .{ .f64 = x };
            },
            .string => |cap| {
                if (cap) |c| if (b.len > c) return refuse("length_over_bound");
                return .{ .str = b };
            },
            .bytes => |cap| {
                var r: Rd = .{ .b = b };
                const ek = try r.byte();
                if (try kindCheck(6, ek)) unreachable;
                const n = try r.leb();
                if (n > cap) return refuse("length_over_bound");
                return .{ .str = try r.take(n) };
            },
            .enum_ => |ed| {
                var r: Rd = .{ .b = b };
                const ref = try r.leb();
                return .{ .en = try d.enumRef(ed, ref, field) };
            },
            .flags => return .{ .fl = @intCast(getLE(b)) },
            .struct_ => |sd| return .{ .st = try d.structBody(sd, b, false) },
            .union_ => |ud| {
                var r: Rd = .{ .b = b };
                return .{ .un = try d.unionPayload(ud, &r) };
            },
            .array => |*at| return .{ .arr = try d.array(at, b) },
            .keyed => |kt| return .{ .keyed = try d.keyed(kt, b) },
            .map => |*mt| return .{ .map = try d.map(mt, b) },
            .pointer => |sd| {
                var r: Rd = .{ .b = b };
                return try d.pointerSlot(try r.leb(), .{ .table = sd });
            },
            .blob_bytes => {
                var r: Rd = .{ .b = b };
                return try d.pointerSlot(try r.leb(), .blob_bytes);
            },
            .blob_string => {
                var r: Rd = .{ .b = b };
                return try d.pointerSlot(try r.leb(), .blob_string);
            },
        }
    }

    fn pointerSlot(d: *Dec, idx: u64, want: @FieldType(Fixup, "want")) Error!Value {
        if (idx == 0) return .{ .ptr = null };
        // the slot is patched once the records are decoded
        const holder = try d.a.create(Value);
        holder.* = .{ .ptr = null };
        try d.fixups.append(d.a, .{ .slot = holder, .index = idx, .want = want });
        return .{ .opt = holder }; // unwrapped by `resolve`
    }

    fn enumRef(d: *Dec, ed: *schema.EnumDecl, ref: u64, field: ?*const schema.Field) Error!u32 {
        if (ref == 0) return 0;
        const id = d.idOf(ref);
        const ord = ed.byId(id) orelse {
            d.report.unknown += 1;
            return 0;
        };
        if (ed.variants[ord - 1].deprecated) {
            // §2.15: a deprecated variant reads as the FIELD's declared default
            if (field) |f| {
                if (f.default) |_| {
                    const dv = value.fieldDefault(d.a, d.unit, f) catch return 0;
                    if (dv == .en) return dv.en;
                }
            }
            return 0;
        }
        return ord;
    }

    fn unionPayload(d: *Dec, ud: *schema.UnionDecl, r: *Rd) Error!value.UnionVal {
        const aref = try r.leb();
        if (aref == 0) return .{ .arm = 0, .payload = null };
        const k = try r.byte();
        const l = try r.leb();
        const e = try r.take(l);
        const id = d.idOf(aref);
        const ai = ud.byId(id) orelse {
            d.report.unknown += 1;
            return .{ .arm = 0, .payload = null };
        };
        const arm = ud.arms[ai - 1];
        const at = arm.ty orelse {
            if (k != 32) return refuse("kind_mismatch: payload-free arm");
            return .{ .arm = ai, .payload = null };
        };
        if (k == 32) return refuse("kind_mismatch: arm without payload");
        const p = try d.a.create(Value);
        p.* = try d.decode(at, k, e, null);
        return .{ .arm = ai, .payload = p };
    }

    fn array(d: *Dec, at: *const schema.ArrayT, b: []const u8) Error![]Value {
        var r: Rd = .{ .b = b };
        const ek = try r.byte();
        const dek = elemKindOf(at);
        const widened = try kindCheck(dek, ek);
        const n = try r.leb();
        if (at.max) |mx| if (n > mx) return refuse("count_over_bound");
        if (!at.exact and n < at.min) return refuse("count_over_bound");
        const len: usize = if (at.exact) @intCast(at.max.?) else @intCast(n);
        var xs = try d.a.alloc(Value, len);
        var i: usize = 0;
        while (i < n) : (i += 1) xs[i] = try d.element(at, ek, &r);
        while (i < len) : (i += 1) xs[i] = value.elemDefault(d.a, at) catch return error.OutOfMemory;
        if (widened and n > 0) d.report.widened += 1;
        return xs;
    }

    fn element(d: *Dec, at: *const schema.ArrayT, ek: u8, r: *Rd) Error!Value {
        if (at.elem_optional) {
            const l = try r.leb();
            const body = try r.take(l);
            return try d.optionalElem(at.elem, body);
        }
        const ty = at.elem;
        if (fixedWidth(ek)) |w| return d.decodeNoCount(ty, ek, try r.take(w));
        switch (ek) {
            12, 13 => {
                const l = try r.leb();
                return d.decodeNoCount(ty, ek, try r.take(l));
            },
            15 => {
                const ud = ty.union_;
                return .{ .un = try d.unionPayload(ud, r) };
            },
            17 => {
                const idx = try r.leb();
                return switch (ty.*) {
                    .pointer => |sd| try d.pointerSlot(idx, .{ .table = sd }),
                    .blob_bytes => try d.pointerSlot(idx, .blob_bytes),
                    .blob_string => try d.pointerSlot(idx, .blob_string),
                    else => refuse("kind_mismatch"),
                };
            },
            30 => {
                const ref = try r.leb();
                return .{ .en = try d.enumRef(ty.enum_, ref, null) };
            },
            else => return refuse("wire_damaged"),
        }
    }

    /// decode an element without counting a second widening for it
    fn decodeNoCount(d: *Dec, ty: *const Type, k: u8, b: []const u8) Error!Value {
        const before = d.report.widened;
        const v = try d.decode(ty, k, b, null);
        d.report.widened = before;
        return v;
    }

    /// `[..N]?string(M)`: a body holding `value` when present, only the
    /// terminator when absent (§3).
    fn optionalElem(d: *Dec, ty: *const Type, body: []const u8) Error!Value {
        var r: Rd = .{ .b = body };
        var got: ?*Value = null;
        while (true) {
            const ref = try r.leb();
            if (ref == 0) break;
            const k = try r.byte();
            const fb = try framed(k, &r);
            if (d.idOf(ref) != value_id) {
                d.report.unknown += 1;
                continue;
            }
            const p = try d.a.create(Value);
            p.* = try d.decodeNoCount(ty, k, fb);
            got = p;
        }
        return .{ .opt = got };
    }

    fn keyed(d: *Dec, kt: schema.KeyedT, b: []const u8) Error![]Value {
        var r: Rd = .{ .b = b };
        const ek = try r.byte();
        if (try kindCheck(kindOf(kt.elem), ek)) d.report.widened += 1;
        const n = try r.leb();
        var xs = try d.a.alloc(Value, kt.key.variants.len);
        for (xs) |*x| x.* = value.zeroOf(d.a, kt.elem) catch return error.OutOfMemory;
        var i: u64 = 0;
        while (i < n) : (i += 1) {
            const kref = try r.leb();
            const l = try r.leb();
            const e = try r.take(l);
            const ord = kt.key.byId(d.idOf(kref)) orelse {
                d.report.unknown += 1;
                continue;
            };
            xs[ord - 1] = try d.decodeNoCount(kt.elem, ek, e);
        }
        return xs;
    }

    fn map(d: *Dec, mt: *const schema.MapT, b: []const u8) Error![]value.MapEntry {
        var r: Rd = .{ .b = b };
        const ek = try r.byte();
        _ = try kindCheck(13, ek);
        const n = try r.leb();
        var xs = try d.a.alloc(value.MapEntry, @intCast(n));
        const key_kind = kindOf(mt.key);
        const val_kind = if (mt.val_optional) kindOf(mt.val) else kindOf(mt.val);
        _ = val_kind;
        var widened = false;
        var i: usize = 0;
        while (i < n) : (i += 1) {
            const l = try r.leb();
            const body = try r.take(l);
            var er: Rd = .{ .b = body };
            var key: ?Value = null;
            var val: ?Value = null;
            while (true) {
                const ref = try er.leb();
                if (ref == 0) break;
                const k = try er.byte();
                const fb = try framed(k, &er);
                const id = d.idOf(ref);
                if (id == schema.map_key_id) {
                    if (try kindCheck(key_kind, k)) widened = true;
                    key = try d.decodeNoCount(mt.key, k, fb);
                } else if (id == schema.map_value_id) {
                    const before = d.report.widened;
                    const v = try d.decode(mt.val, k, fb, null);
                    if (d.report.widened != before) widened = true;
                    d.report.widened = before;
                    if (mt.val_optional) {
                        const p = try d.a.create(Value);
                        p.* = v;
                        val = .{ .opt = p };
                    } else val = v;
                } else d.report.unknown += 1;
            }
            xs[i] = .{
                .key = key orelse (value.zeroOf(d.a, mt.key) catch return error.OutOfMemory),
                .val = val orelse if (mt.val_optional) Value{ .opt = null } else (value.zeroOf(d.a, mt.val) catch return error.OutOfMemory),
            };
            if (i > 0 and keyOrder(mt.key, xs[i - 1].key, xs[i].key) != .lt)
                return refuse("wire_damaged: map keys not ascending");
        }
        if (widened) d.report.widened += 1;
        return xs;
    }
};

/// The bytes of one payload after its kind byte, as `decode` takes them.
fn framed(kind: u8, r: *Rd) Error![]const u8 {
    if (fixedWidth(kind)) |w| return r.take(w);
    switch (kind) {
        12, 13, 14, 16, 31, 32 => {
            const l = try r.leb();
            return r.take(l);
        },
        15 => {
            const s = r.p;
            const aref = try r.leb();
            if (aref != 0) {
                _ = try r.byte();
                const l = try r.leb();
                _ = try r.take(l);
            }
            return r.b[s..r.p];
        },
        17, 30 => {
            const s = r.p;
            _ = try r.leb();
            return r.b[s..r.p];
        },
        else => return refuse("wire_damaged: unknown kind"),
    }
}

fn readInt(kind: u8, b: []const u8) Int {
    const raw = getLE(b);
    const signed = switch (kind) {
        2, 3, 4, 5, 18, 20, 21, 22, 23, 24, 36 => true,
        else => false,
    };
    if (!signed) return @intCast(raw);
    const bits: u8 = @intCast(b.len * 8);
    if (bits == 128) return @as(i128, @bitCast(raw));
    const sh: u7 = @intCast(128 - bits);
    const v: i128 = @as(i128, @bitCast(raw << sh)) >> sh;
    return v;
}

/// §22.2.1: the key order per key kind.
pub fn keyOrder(kt: *const Type, x: Value, y: Value) std.math.Order {
    return switch (kt.*) {
        .string => std.mem.order(u8, x.str, y.str),
        else => std.math.order(x.i, y.i),
    };
}

/// After the records decode, patch every pointer slot (§3.1).
fn resolve(d: *Dec, v: *Value, nodes: []?*value.Node, rec_ids: []const u64) Error!void {
    _ = v;
    for (d.fixups.items) |fx| {
        const i: usize = @intCast(fx.index - 2);
        const tid = rec_ids[i];
        switch (fx.want) {
            .table => |sd| {
                if (tid == sd.id) {
                    fx.slot.* = .{ .ptr = nodes[i] };
                } else if (tid == bytes_id or tid == string_id or d.unit.tables_by_id.get(tid) != null) {
                    return refuse("wire_damaged: wrong-type pointer target");
                } else {
                    fx.slot.* = .{ .ptr = null };
                }
            },
            .blob_bytes => {
                if (tid != bytes_id) return refuse("wire_damaged: wrong-type blob target");
                fx.slot.* = .{ .ptr = nodes[i] };
            },
            .blob_string => {
                if (tid != string_id) return refuse("wire_damaged: wrong-type blob target");
                fx.slot.* = .{ .ptr = nodes[i] };
            },
        }
    }
}

/// Replace every `.opt` holder a pointer slot left behind by its `.ptr`.
fn unwrapPointers(v: *Value, ty: *const Type, optional: bool) void {
    if (optional) {
        if (v.opt) |p| unwrapPointers(p, ty, false);
        return;
    }
    switch (ty.*) {
        .pointer, .blob_bytes, .blob_string => {
            if (v.* == .opt) v.* = v.opt.?.*;
        },
        .struct_ => unwrapStruct(v.st),
        .union_ => |ud| if (v.un.arm != 0) {
            if (ud.arms[v.un.arm - 1].ty) |at| unwrapPointers(v.un.payload.?, at, false);
        },
        .array => |at| for (v.arr) |*x| unwrapPointers(x, at.elem, at.elem_optional),
        .keyed => |kt| for (v.keyed) |*x| unwrapPointers(x, kt.elem, false),
        .map => |mt| for (v.map) |*e| unwrapPointers(&e.val, mt.val, mt.val_optional),
        else => {},
    }
}

pub fn unwrapStruct(s: *value.Struct) void {
    for (s.decl.fields, 0..) |*f, i| unwrapPointers(&s.fields[i], f.ty, f.optional);
}

const Crc32c = std.hash.crc.Crc32Iscsi;

/// Load: the whole value or a refusal (§6.5).
pub fn load(a: Allocator, unit: *const schema.Unit, root: *schema.StructDecl, input: []const u8, report: *Report) Error!*value.Struct {
    if (input.len == 0) return refuse("wire_damaged: empty");
    if (input[0] != 1) return refuse(if (input[0] == 2) "message_form_as_file" else "newer_form");
    var buf = input;
    if (root.integrity) {
        if (buf.len < 13) return refuse("wire_damaged: short");
        const want: u32 = @intCast(getLE(buf[buf.len - 4 ..]));
        if (Crc32c.hash(buf[0 .. buf.len - 4]) != want) return refuse("integrity_mismatch");
        buf = buf[0 .. buf.len - 4];
    }
    if (buf.len < 9) return refuse("wire_damaged: short");
    const count = getLE(buf[buf.len - 8 ..]);
    if (count > max_ids) return refuse("wire_damaged: id table past 4096");
    const tail: u64 = @as(u64, @intCast(count)) * 8 + 8;
    if (tail > buf.len - 1) return refuse("wire_damaged: id table past the front");
    const n: usize = @intCast(count);
    const ids = try a.alloc(u64, n);
    const ent = buf[buf.len - 8 - 8 * n .. buf.len - 8];
    for (0..n) |i| ids[i] = @intCast(getLE(ent[i * 8 .. i * 8 + 8]));
    // distinct: sort a copy, compare neighbours (bounded by the 4096 cap)
    const sorted = try a.dupe(u64, ids);
    std.mem.sort(u64, sorted, {}, std.sort.asc(u64));
    for (1..n) |i| if (sorted[i] == sorted[i - 1]) return refuse("wire_damaged: id twice");
    const body = buf[1 .. buf.len - 8 - 8 * n];

    var sc: Scan = .{ .a = a, .ids = ids };
    try sc.body(body, 0, true);
    if (sc.next_ref - 1 != n) return refuse("non_canonical: unused id-table entry");
    try graphCheck(&sc);

    var d: Dec = .{ .a = a, .unit = unit, .ids = ids, .report = report };
    const s = try d.structBody(root, body, true);
    // the records
    const recs = sc.records.items;
    var nodes = try a.alloc(?*value.Node, recs.len);
    var rec_ids = try a.alloc(u64, recs.len);
    for (recs, 0..) |rec, i| {
        rec_ids[i] = rec.type_id;
        if (rec.type_id == bytes_id or rec.type_id == string_id) {
            const nd = try a.create(value.Node);
            nd.* = .{ .kind = if (rec.type_id == bytes_id) .blob_bytes else .blob_string, .bytes = rec.body };
            nodes[i] = nd;
        } else if (unit.tables_by_id.get(rec.type_id)) |sd| {
            const nd = try a.create(value.Node);
            nd.* = .{ .kind = .table, .st = try d.structBody(sd, rec.body, false) };
            nodes[i] = nd;
        } else {
            report.unknown += 1;
            nodes[i] = null;
        }
    }
    var dummy: Value = .{ .b = false };
    try resolve(&d, &dummy, nodes, rec_ids);
    unwrapStruct(s);
    for (nodes) |nd| if (nd) |x| if (x.st) |st| unwrapStruct(st);
    return s;
}

// ------------------------------------------------------------ the write (§22)

const NodeInfo = struct { index: u32, state: u8 };

pub const Writer = struct {
    a: Allocator,
    ids: std.ArrayList(u64) = .empty,
    id_ref: std.AutoHashMapUnmanaged(u64, u64) = .empty,
    order: std.ArrayList(*value.Node) = .empty,
    info: std.AutoHashMapUnmanaged(*value.Node, NodeInfo) = .empty,

    fn intern(w: *Writer, id: u64) Error!u64 {
        if (w.id_ref.get(id)) |r| return r;
        if (w.ids.items.len >= max_ids) return refuse("invalid_value: more than 4096 ids");
        try w.ids.append(w.a, id);
        const r: u64 = w.ids.items.len;
        try w.id_ref.put(w.a, id, r);
        return r;
    }

    // ---- numbering (§22.3)

    fn numberStruct(w: *Writer, s: *value.Struct) Error!void {
        for (s.decl.fields, 0..) |*f, i| {
            if (!value.guardOn(s, f)) continue;
            if (f.optional) {
                if (s.fields[i].opt) |p| try w.numberValue(f.ty, p.*);
                continue;
            }
            try w.numberValue(f.ty, s.fields[i]);
        }
    }

    fn numberValue(w: *Writer, ty: *const Type, v: Value) Error!void {
        switch (ty.*) {
            .pointer, .blob_bytes, .blob_string => {
                const nd = v.ptr orelse return;
                if (w.info.get(nd)) |inf| {
                    if (inf.state == 1) return refuse("data_cycle");
                    if (nd.kind != .table) return refuse("blob_shared");
                    return;
                }
                try w.order.append(w.a, nd);
                const idx: u32 = @intCast(w.order.items.len + 1);
                try w.info.put(w.a, nd, .{ .index = idx, .state = 1 });
                if (nd.kind == .table) try w.numberStruct(nd.st.?);
                try w.info.put(w.a, nd, .{ .index = idx, .state = 2 });
            },
            .struct_ => try w.numberStruct(v.st),
            .union_ => |ud| if (v.un.arm != 0 and v.un.arm <= ud.arms.len) {
                if (ud.arms[v.un.arm - 1].ty) |at| try w.numberValue(at, v.un.payload.?.*);
            },
            .array => |at| for (v.arr) |x| {
                if (at.elem_optional) {
                    if (x.opt) |p| try w.numberValue(at.elem, p.*);
                } else try w.numberValue(at.elem, x);
            },
            .keyed => |kt| for (v.keyed) |x| try w.numberValue(kt.elem, x),
            .map => |mt| for (v.map) |e| {
                if (mt.val_optional) {
                    if (e.val.opt) |p| try w.numberValue(mt.val, p.*);
                } else try w.numberValue(mt.val, e.val);
            },
            else => {},
        }
    }

    // ---- bodies

    fn body(w: *Writer, s: *value.Struct, out: *std.ArrayList(u8)) Error!void {
        for (s.decl.fields, 0..) |*f, i| {
            const v = s.fields[i];
            if (try value.fieldElided(w.a, s, f, v)) continue;
            const inner = if (f.optional) v.opt.?.* else v;
            try w.field(f.id, f.ty, inner, out);
        }
    }

    fn field(w: *Writer, id: u64, ty: *const Type, v: Value, out: *std.ArrayList(u8)) Error!void {
        const ref = try w.intern(id);
        const kind = kindOf(ty);
        var tmp: std.ArrayList(u8) = .empty;
        try w.bodyOnly(ty, v, &tmp);
        try putLeb(w.a, out, ref);
        try out.append(w.a, kind);
        switch (kind) {
            12, 13, 14, 16 => try putLeb(w.a, out, tmp.items.len),
            else => {},
        }
        try out.appendSlice(w.a, tmp.items);
    }

    /// A payload without its own `L` (an arm's, a keyed element's).
    fn bodyOnly(w: *Writer, ty: *const Type, v: Value, out: *std.ArrayList(u8)) Error!void {
        const a = w.a;
        switch (ty.*) {
            .boolean => try out.append(a, @intFromBool(v.b)),
            .int => |it| {
                if (v.i < it.min or v.i > it.max) return refuse("invalid_value: out of range");
                try putLE(a, out, @bitCast(@as(i128, @truncate(v.i))), it.width / 8);
            },
            .fixed => |fx| {
                if (v.i < fx.min_raw or v.i > fx.max_raw) return refuse("invalid_value: out of range");
                try putLE(a, out, @bitCast(@as(i128, @truncate(v.i))), fx.width / 8);
            },
            .timestamp => {
                if (v.i < std.math.minInt(i64) or v.i > std.math.maxInt(i64)) return refuse("invalid_value");
                try putLE(a, out, @bitCast(@as(i128, @truncate(v.i))), 8);
            },
            .float => |ft| {
                if (ft.wide) {
                    if (!std.math.isFinite(v.f64)) return refuse("non_finite_float");
                    try putLE(a, out, @as(u64, @bitCast(v.f64)), 8);
                } else {
                    var f = v.f32;
                    if (!std.math.isFinite(f)) return refuse("non_finite_float");
                    if (ft.compressed) |c| {
                        if (f < c.min or f > c.max) return refuse("invalid_value: out of range");
                        if (f == 0) f = 0; // -0 stored as +0
                    }
                    try putLE(a, out, @as(u32, @bitCast(f)), 4);
                }
            },
            .string => |cap| {
                if (cap) |c| if (v.str.len > c) return refuse("invalid_value: length");
                if (!validUtf8NoNul(v.str)) return refuse("invalid_value: not text");
                try out.appendSlice(a, v.str);
            },
            .bytes => |cap| {
                if (v.str.len > cap) return refuse("invalid_value: length");
                try out.append(a, 6);
                try putLeb(a, out, v.str.len);
                try out.appendSlice(a, v.str);
            },
            .enum_ => |ed| {
                if (v.en == 0) return out.append(a, 0);
                if (v.en > ed.variants.len) return refuse("invalid_value: enum ordinal");
                const vr = ed.variants[v.en - 1];
                if (vr.deprecated) return refuse("invalid_value: deprecated variant");
                try putLeb(a, out, try w.intern(vr.id));
            },
            .flags => try putLE(a, out, v.fl, 8),
            .struct_ => {
                try w.body(v.st, out);
                try out.append(a, 0);
            },
            .union_ => |ud| try w.unionPayload(ud, v.un, out),
            .array => |*at| try w.array(at, v.arr, out),
            .keyed => |kt| try w.keyed(kt, v.keyed, out),
            .map => |*mt| try w.map(mt, v.map, out),
            .pointer, .blob_bytes, .blob_string => {
                const nd = v.ptr orelse return out.append(a, 0);
                const inf = w.info.get(nd) orelse return refuse("unnumbered node");
                try putLeb(a, out, inf.index);
            },
        }
    }

    fn unionPayload(w: *Writer, ud: *schema.UnionDecl, u: value.UnionVal, out: *std.ArrayList(u8)) Error!void {
        if (u.arm == 0) return out.append(w.a, 0);
        if (u.arm > ud.arms.len) return refuse("invalid_value: union tag");
        const arm = ud.arms[u.arm - 1];
        try putLeb(w.a, out, try w.intern(arm.id));
        const at = arm.ty orelse {
            try out.append(w.a, 32);
            try out.append(w.a, 0);
            return;
        };
        var tmp: std.ArrayList(u8) = .empty;
        try w.bodyOnly(at, u.payload.?.*, &tmp);
        try out.append(w.a, kindOf(at));
        try putLeb(w.a, out, tmp.items.len);
        try out.appendSlice(w.a, tmp.items);
    }

    fn array(w: *Writer, at: *const schema.ArrayT, xs: []const Value, out: *std.ArrayList(u8)) Error!void {
        if (at.max) |mx| if (xs.len > mx) return refuse("invalid_value: count");
        if (xs.len < at.min) return refuse("invalid_value: count");
        if (at.total) |tot| {
            var sum: u64 = 0;
            for (xs) |x| sum += if (at.elem_optional) (if (x.opt) |p| p.str.len else 0) else x.str.len;
            if (sum > tot) return refuse("invalid_value: total");
        }
        const ek = elemKindOf(at);
        try out.append(w.a, ek);
        try putLeb(w.a, out, xs.len);
        for (xs) |x| {
            if (at.elem_optional) {
                var tmp: std.ArrayList(u8) = .empty;
                if (x.opt) |p| try w.field(value_id, at.elem, p.*, &tmp);
                try tmp.append(w.a, 0);
                try putLeb(w.a, out, tmp.items.len);
                try out.appendSlice(w.a, tmp.items);
                continue;
            }
            switch (ek) {
                12, 13 => {
                    var tmp: std.ArrayList(u8) = .empty;
                    try w.bodyOnly(at.elem, x, &tmp);
                    try putLeb(w.a, out, tmp.items.len);
                    try out.appendSlice(w.a, tmp.items);
                },
                else => try w.bodyOnly(at.elem, x, out),
            }
        }
    }

    fn keyed(w: *Writer, kt: schema.KeyedT, xs: []const Value, out: *std.ArrayList(u8)) Error!void {
        try out.append(w.a, kindOf(kt.elem));
        var present: usize = 0;
        for (xs) |x| {
            if (!(value.slotDefault(w.a, x, kt.elem) catch return error.OutOfMemory)) present += 1;
        }
        try putLeb(w.a, out, present);
        for (xs, 0..) |x, i| {
            if (value.slotDefault(w.a, x, kt.elem) catch return error.OutOfMemory) continue;
            try putLeb(w.a, out, try w.intern(kt.key.variants[i].id));
            var tmp: std.ArrayList(u8) = .empty;
            try w.bodyOnly(kt.elem, x, &tmp);
            try putLeb(w.a, out, tmp.items.len);
            try out.appendSlice(w.a, tmp.items);
        }
    }

    fn map(w: *Writer, mt: *const schema.MapT, entries: []value.MapEntry, out: *std.ArrayList(u8)) Error!void {
        sortEntries(mt.key, entries);
        try out.append(w.a, 13);
        try putLeb(w.a, out, entries.len);
        for (entries) |e| {
            var tmp: std.ArrayList(u8) = .empty;
            // the key, elided at its default
            if (!(value.slotDefault(w.a, e.key, mt.key) catch return error.OutOfMemory))
                try w.field(schema.map_key_id, mt.key, e.key, &tmp);
            if (mt.val_optional) {
                if (e.val.opt) |p| try w.field(schema.map_value_id, mt.val, p.*, &tmp);
            } else if (!try mapValueElided(w.a, mt.val, e.val)) {
                try w.field(schema.map_value_id, mt.val, e.val, &tmp);
            }
            try tmp.append(w.a, 0);
            try putLeb(w.a, out, tmp.items.len);
            try out.appendSlice(w.a, tmp.items);
        }
    }
};

/// A map value is a plain field with no declared default (§22.2.1).
pub fn mapValueElided(a: Allocator, ty: *const Type, v: Value) Error!bool {
    switch (ty.*) {
        .struct_ => return value.bodyEmpty(a, v.st) catch return error.OutOfMemory,
        .union_ => return v.un.arm == 0,
        .pointer, .blob_bytes, .blob_string => return v.ptr == null,
        .map => return v.map.len == 0,
        .keyed => |kt| {
            for (v.keyed) |x| if (!(value.slotDefault(a, x, kt.elem) catch return error.OutOfMemory)) return false;
            return true;
        },
        .array => |at| {
            if (at.exact) {
                if (!at.elem_optional and at.elem.* == .struct_) return false;
                for (v.arr) |x| if (!(value.elemAtDefault(a, x, &at) catch return error.OutOfMemory)) return false;
                return true;
            }
            if (at.min == 0) return v.arr.len == 0;
            if (v.arr.len != at.min) return false;
            for (v.arr) |x| if (!(value.elemAtDefault(a, x, &at) catch return error.OutOfMemory)) return false;
            return true;
        },
        else => return value.slotDefault(a, v, ty) catch return error.OutOfMemory,
    }
}

pub fn sortEntries(kt: *const Type, entries: []value.MapEntry) void {
    const Ctx = struct {
        kt: *const Type,
        fn lt(c: @This(), x: value.MapEntry, y: value.MapEntry) bool {
            return keyOrder(c.kt, x.key, y.key) == .lt;
        }
    };
    std.mem.sort(value.MapEntry, entries, Ctx{ .kt = kt }, Ctx.lt);
}

/// Save (§22.1): the canonical bytes, or a refusal.
pub fn save(a: Allocator, s: *value.Struct) Error![]u8 {
    var w: Writer = .{ .a = a };
    try w.numberStruct(s);
    var out: std.ArrayList(u8) = .empty;
    try out.append(a, 1);
    var rb: std.ArrayList(u8) = .empty;
    try w.body(s, &rb);
    try out.appendSlice(a, rb.items);
    if (w.order.items.len > 0) {
        const ref = try w.intern(schema.node_table_id);
        var nt: std.ArrayList(u8) = .empty;
        try putLeb(a, &nt, w.order.items.len);
        for (w.order.items) |nd| {
            const tid: u64 = switch (nd.kind) {
                .table => nd.st.?.decl.id,
                .blob_bytes => bytes_id,
                .blob_string => string_id,
            };
            const tref = try w.intern(tid);
            var b: std.ArrayList(u8) = .empty;
            switch (nd.kind) {
                .table => {
                    try w.body(nd.st.?, &b);
                    try b.append(a, 0);
                },
                .blob_bytes => try b.appendSlice(a, nd.bytes),
                .blob_string => {
                    if (!validUtf8NoNul(nd.bytes)) return refuse("invalid_value: not text");
                    try b.appendSlice(a, nd.bytes);
                },
            }
            try putLeb(a, &nt, tref);
            try putLeb(a, &nt, b.items.len);
            try nt.appendSlice(a, b.items);
        }
        try putLeb(a, &out, ref);
        try out.append(a, 12);
        try putLeb(a, &out, nt.items.len);
        try out.appendSlice(a, nt.items);
    }
    try out.append(a, 0);
    for (w.ids.items) |id| try putLE(a, &out, id, 8);
    try putLE(a, &out, w.ids.items.len, 8);
    if (s.decl.integrity) {
        const c = Crc32c.hash(out.items);
        try putLE(a, &out, c, 4);
    }
    return out.items;
}

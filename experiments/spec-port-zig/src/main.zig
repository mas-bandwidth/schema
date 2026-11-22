// The conformance driver (test/conformance/README.md, "The shape"):
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// The schema is read at run time from the manifest's unit paths: this port
// interprets the schema rather than generating code per schema (SPEC-GAPS.md).
const std = @import("std");
const schema = @import("schema.zig");
const value = @import("value.zig");
const wire = @import("wire.zig");
const json = @import("json.zig");
const Io = std.Io;
const Allocator = std.mem.Allocator;
const Value = value.Value;

const surfaces = [_][]const u8{
    "wire",       "json-read",     "json-write",         "report",
    "framing",    "json-hostile",  "write-refusal",      "write-refusal-json",
};

const Ctx = struct {
    a: Allocator, // process lifetime: units
    io: Io,
    cwd: Io.Dir,
    out: Io.Dir,
    unit_paths: std.StringHashMapUnmanaged([]const []const u8) = .empty,
    units: std.StringHashMapUnmanaged(?*schema.Unit) = .empty,
    refusal: []const u8 = "refused\n",
    // SPEC-GAP 8: the wire surface scores a refusal only as `<case>.absent`
    wire_absent: bool = false,

    fn readFile(c: *Ctx, a: Allocator, path: []const u8) ?[]u8 {
        return c.cwd.readFileAlloc(c.io, path, a, .unlimited) catch null;
    }

    fn put(c: *Ctx, name: []const u8, data: []const u8) !void {
        try c.out.writeFile(c.io, .{ .sub_path = name, .data = data });
    }

    fn absent(c: *Ctx, a: Allocator, case: []const u8, why: []const u8) !void {
        try c.put(try std.fmt.allocPrint(a, "{s}.absent", .{case}), "");
        std.debug.print("absent {s}: {s}\n", .{ case, why });
    }

    fn unit(c: *Ctx, key: []const u8) ?*schema.Unit {
        if (c.units.get(key)) |u| return u;
        const u = c.loadUnit(key) catch |e| blk: {
            std.debug.print("unit {s}: {s} {s}\n", .{ key, @errorName(e), wire.last_reason });
            break :blk null;
        };
        c.units.put(c.a, key, u) catch {};
        return u;
    }

    fn loadUnit(c: *Ctx, key: []const u8) !*schema.Unit {
        const paths = c.unit_paths.get(key) orelse return error.NoUnit;
        var srcs: std.ArrayList([]const u8) = .empty;
        for (paths) |p| {
            if (std.mem.endsWith(u8, p, ".schema")) {
                try srcs.append(c.a, c.readFile(c.a, p) orelse return error.NoFile);
                continue;
            }
            var dir = try c.cwd.openDir(c.io, p, .{ .iterate = true });
            defer dir.close(c.io);
            var names: std.ArrayList([]const u8) = .empty;
            var it = dir.iterate();
            while (try it.next(c.io)) |e| {
                if (e.kind == .file and std.mem.endsWith(u8, e.name, ".schema"))
                    try names.append(c.a, try c.a.dupe(u8, e.name));
            }
            std.mem.sort([]const u8, names.items, {}, lessStr);
            for (names.items) |n| {
                const full = try std.fmt.allocPrint(c.a, "{s}/{s}", .{ p, n });
                try srcs.append(c.a, c.readFile(c.a, full) orelse return error.NoFile);
            }
        }
        return schema.loadUnit(c.a, srcs.items);
    }
};

fn lessStr(_: void, x: []const u8, y: []const u8) bool {
    return std.mem.order(u8, x, y) == .lt;
}

fn fields(a: Allocator, line: []const u8) ![]const []const u8 {
    var xs: std.ArrayList([]const u8) = .empty;
    var it = std.mem.tokenizeAny(u8, line, " \t\r");
    while (it.next()) |t| try xs.append(a, t);
    return xs.items;
}

pub fn main(init: std.process.Init) !u8 {
    const a = init.arena.allocator();
    const io = init.io;
    const args = try init.minimal.args.toSlice(a);
    if (args.len < 3) return 2;
    const surface = args[2];
    if (std.mem.eql(u8, surface, "list")) {
        var buf: std.ArrayList(u8) = .empty;
        for (surfaces) |s| {
            try buf.appendSlice(a, s);
            try buf.append(a, '\n');
        }
        try Io.File.stdout().writeStreamingAll(io, buf.items);
        return 0;
    }
    var known = false;
    for (surfaces) |s| if (std.mem.eql(u8, s, surface)) {
        known = true;
    };
    if (!known) return 2;
    if (args.len < 4) return 3;
    const cwd = Io.Dir.cwd();
    try cwd.createDirPath(io, args[3]);
    var c: Ctx = .{ .a = a, .io = io, .cwd = cwd, .out = try cwd.openDir(io, args[3], .{}) };
    if (std.mem.eql(u8, surface, "wire")) c.wire_absent = true;
    const manifest = c.readFile(a, args[1]) orelse return 3;

    var lines: std.ArrayList([]const []const u8) = .empty;
    var it = std.mem.splitScalar(u8, manifest, '\n');
    while (it.next()) |line| {
        if (line.len == 0 or line[0] == '#') continue;
        const f = try fields(a, line);
        if (f.len == 0) continue;
        if (std.mem.eql(u8, f[0], "unit")) try c.unit_paths.put(a, f[1], f[2..]);
        try lines.append(a, f);
    }

    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    for (lines.items) |f| {
        _ = arena.reset(.retain_capacity);
        const ca = arena.allocator();
        const kind = f[0];
        if (std.mem.eql(u8, surface, "wire") and eq(kind, "instance")) try surfWire(&c, ca, f);
        if (std.mem.eql(u8, surface, "json-read") and eq(kind, "instance")) try surfJsonRead(&c, ca, f);
        if (std.mem.eql(u8, surface, "json-write") and eq(kind, "instance")) try surfJsonWrite(&c, ca, f, null);
        if (std.mem.eql(u8, surface, "json-write") and eq(kind, "json-write-refusal")) try surfJsonWrite(&c, ca, f, f[5]);
        if ((std.mem.eql(u8, surface, "report") and eq(kind, "report")) or
            (std.mem.eql(u8, surface, "framing") and eq(kind, "framing"))) try surfReport(&c, ca, f);
        if (std.mem.eql(u8, surface, "json-hostile") and eq(kind, "json-hostile")) try surfHostile(&c, ca, f);
        if (std.mem.eql(u8, surface, "write-refusal") and eq(kind, "write-refusal")) try surfWriteRefusal(&c, ca, f, false);
        if (std.mem.eql(u8, surface, "write-refusal-json") and eq(kind, "write-refusal-json")) try surfWriteRefusal(&c, ca, f, true);
    }
    return 0;
}

fn eq(x: []const u8, y: []const u8) bool {
    return std.mem.eql(u8, x, y);
}

const Root = struct { unit: *schema.Unit, decl: *schema.StructDecl };

fn root(c: *Ctx, a: Allocator, case: []const u8, unit_key: []const u8, name: []const u8) !?Root {
    const u = c.unit(unit_key) orelse {
        try c.absent(a, case, "unit did not load");
        return null;
    };
    const d = u.findStruct(name) orelse {
        try c.absent(a, case, "no such root");
        return null;
    };
    return .{ .unit = u, .decl = d };
}

/// Load a file at a root. A declared fixed root reads form 3 alone and refuses
/// every other form by name (SPEC-TABLES.md "NEITHER CLASS BORROWS THE OTHER'S
/// FORM"); this port has no form-3 codec, so a form-3 file is an honest absence.
fn loadAt(c: *Ctx, a: Allocator, case: []const u8, r: Root, bytes: []const u8) !?*value.Struct {
    if (r.decl.is_fixed) {
        if (bytes.len > 0 and bytes[0] == 3) {
            try c.absent(a, case, "fixed form (form 3) not implemented");
            return null;
        }
        // SPEC-GAP 6: the framing battery reads form 1 at the fixed root `Cfg`
        // and owes values; the report battery's previous_form rows refuse it.
        // This port follows the rule text: form 1 at a fixed root refuses.
        wire.last_reason = "previous_form";
        try refused(c, case, error.Refused);
        return null;
    }
    var rep: wire.Report = .{};
    return wire.load(a, r.unit, r.decl, bytes, &rep) catch |e| {
        try refused(c, case, e);
        return null;
    };
}

fn hasMarker(f: []const []const u8, m: []const u8) bool {
    for (f[5..]) |x| if (std.mem.eql(u8, x, m)) return true;
    return false;
}

/// instance <name> <unit> <root> <wire> [markers]
fn surfWire(c: *Ctx, a: Allocator, f: []const []const u8) !void {
    const r = (try root(c, a, f[1], f[2], f[3])) orelse return;
    const bytes = c.readFile(a, f[4]) orelse return c.absent(a, f[1], "no wire file");
    const s = (try loadAt(c, a, f[1], r, bytes)) orelse return;
    const out = wire.save(a, s) catch |e| return refused(c, f[1], e);
    try c.put(f[1], out);
}

fn refused(c: *Ctx, name: []const u8, e: anyerror) !void {
    if (e != error.Refused) return e;
    if (c.wire_absent) {
        std.debug.print("refused {s}: {s}\n", .{ name, wire.last_reason });
        var buf: [512]u8 = undefined;
        return c.put(std.fmt.bufPrint(&buf, "{s}.absent", .{name}) catch name, "");
    }
    try c.put(name, c.refusal);
}

fn surfJsonRead(c: *Ctx, a: Allocator, f: []const []const u8) !void {
    if (hasMarker(f, "no-text")) return;
    const r = (try root(c, a, f[1], f[2], f[3])) orelse return;
    const path = try std.fmt.allocPrint(a, "testdata/conformance/tables/json/{s}.json", .{f[1]});
    const text = c.readFile(a, path) orelse return;
    const s = json.read(a, r.unit, r.decl, text) catch |e| return refused(c, f[1], e);
    if (r.decl.is_fixed) return c.absent(a, f[1], "accepted; the form-3 save is not implemented");
    const out = wire.save(a, s) catch |e| return refused(c, f[1], e);
    try c.put(f[1], out);
}

/// instance rows, and json-write-refusal <case> <unit> <root> <wire> <damage>
fn surfJsonWrite(c: *Ctx, a: Allocator, f: []const []const u8, damage: ?[]const u8) !void {
    if (damage == null and hasMarker(f, "no-text")) return;
    const name = try std.fmt.allocPrint(a, "{s}.json", .{f[1]});
    const r = (try root(c, a, name, f[2], f[3])) orelse return;
    const bytes = c.readFile(a, f[4]) orelse return c.absent(a, name, "no wire file");
    const s = (try loadAt(c, a, name, r, bytes)) orelse return;
    if (damage) |d| {
        if (std.mem.startsWith(u8, d, "narrow:")) {
            const fi = r.decl.fieldByKey(d[7..]) orelse r.decl.fieldByKey(d[7..]) orelse return c.absent(a, name, "no such field");
            s.fields[fi] = .{ .str = "a\xffb" };
        } else if (!std.mem.eql(u8, d, "none")) return c.absent(a, name, "damage this leg cannot build");
    }
    const text = json.write(a, s) catch |e| return refused(c, name, e);
    try c.put(name, text);
}

/// report|framing <case> <unit> <root> <wire>
fn surfReport(c: *Ctx, a: Allocator, f: []const []const u8) !void {
    const r = (try root(c, a, f[1], f[2], f[3])) orelse return;
    const bytes = c.readFile(a, f[4]) orelse return c.absent(a, f[1], "no wire file");
    const s = (try loadAt(c, a, f[1], r, bytes)) orelse return;
    try answer(c, a, f[1], s);
}

fn answer(c: *Ctx, a: Allocator, case: []const u8, s: *value.Struct) !void {
    const w = wire.save(a, s) catch |e| {
        std.debug.print("{s}: accepted, then the save refused: {s}\n", .{ case, wire.last_reason });
        return refused(c, case, e);
    };
    const text = json.write(a, s) catch |e| {
        std.debug.print("{s}: accepted, then the text write refused: {s}\n", .{ case, wire.last_reason });
        return refused(c, case, e);
    };
    try c.put(case, try std.mem.concat(a, u8, &.{ "read\n", text }));
    try c.put(try std.fmt.allocPrint(a, "{s}.wire", .{case}), w);
}

/// json-hostile <case> <unit> <root> <tree>
fn surfHostile(c: *Ctx, a: Allocator, f: []const []const u8) !void {
    const r = (try root(c, a, f[1], f[2], f[3])) orelse return;
    const path = try std.fmt.allocPrint(a, "{s}/{s}.json", .{ f[4], f[3] });
    const text = c.readFile(a, path) orelse return c.absent(a, f[1], "no tree");
    const s = json.read(a, r.unit, r.decl, text) catch |e| return refused(c, f[1], e);
    if (r.decl.is_fixed) return c.absent(a, f[1], "accepted; the form-3 save is not implemented");
    try answer(c, a, f[1], s);
}

// ------------------------------------------------------------ the write contract

const Unrepresentable = error{Unrepresentable};

/// write-refusal[-json] <case> <unit> <root> <wire> <path> <kind> <value>
fn surfWriteRefusal(c: *Ctx, a: Allocator, f: []const []const u8, text: bool) !void {
    const r = (try root(c, a, f[1], f[2], f[3])) orelse return;
    if (r.decl.is_fixed) return c.absent(a, f[1], "fixed form not implemented");
    const bytes = c.readFile(a, f[4]) orelse return c.absent(a, f[1], "no wire file");
    var rep: wire.Report = .{};
    const s = wire.load(a, r.unit, r.decl, bytes, &rep) catch return c.absent(a, f[1], "the row's wire did not load");
    setPath(a, s, f[5], f[6], f[7]) catch |e| switch (e) {
        error.Unrepresentable => return c.put(f[1], "unrepresentable\n"),
        error.NoPath => return c.absent(a, f[1], "path not found"),
        else => return e,
    };
    if (text) {
        _ = json.write(a, s) catch |e| return refused(c, f[1], e);
    } else {
        _ = wire.save(a, s) catch |e| return refused(c, f[1], e);
    }
    try c.put(f[1], "wrote\n");
}

const PathError = error{ Unrepresentable, NoPath, OutOfMemory, BadDefault };

fn setPath(a: Allocator, s: *value.Struct, path: []const u8, kind: []const u8, val: []const u8) PathError!void {
    var cur_s = s;
    var segs = std.mem.splitScalar(u8, path, '.');
    var slot: *Value = undefined;
    var ty: *const schema.Type = undefined;
    var first = true;
    while (segs.next()) |seg0| {
        var seg = seg0;
        var index: ?usize = null;
        if (std.mem.indexOfScalar(u8, seg, '[')) |b| {
            index = std.fmt.parseInt(usize, seg[b + 1 .. seg.len - 1], 10) catch return error.NoPath;
            seg = seg[0..b];
        }
        if (!first) {
            // descend from the previous slot into a table value
            cur_s = try enterTable(a, slot, ty, seg) orelse {
                // a union arm by name
                if (ty.* == .union_) {
                    const ud = ty.union_;
                    const ai = ud.byName(seg) orelse return error.NoPath;
                    if (slot.un.arm != ai) {
                        const p = try a.create(Value);
                        p.* = try value.zeroOf(a, ud.arms[ai - 1].ty.?);
                        slot.un = .{ .arm = ai, .payload = p };
                    }
                    slot = slot.un.payload.?;
                    ty = ud.arms[ai - 1].ty.?;
                    if (index) |i| {
                        slot = &slot.arr[i];
                        ty = ty.array.elem;
                    }
                    continue;
                }
                return error.NoPath;
            };
        }
        first = false;
        const fi = cur_s.decl.fieldByKey(seg) orelse fieldByName(cur_s.decl, seg) orelse return error.NoPath;
        const fd = &cur_s.decl.fields[fi];
        slot = &cur_s.fields[fi];
        ty = fd.ty;
        if (fd.optional) {
            if (slot.opt == null) {
                const p = try a.create(Value);
                p.* = try value.zeroOf(a, ty);
                slot.* = .{ .opt = p };
            }
            slot = slot.opt.?;
        }
        if (index) |i| {
            switch (ty.*) {
                .array => |at| {
                    if (i >= slot.arr.len) return error.NoPath;
                    slot = &slot.arr[i];
                    ty = at.elem;
                    if (at.elem_optional) {
                        if (slot.opt == null) return error.NoPath;
                        slot = slot.opt.?;
                    }
                },
                else => return error.NoPath,
            }
        }
    }
    try setLeaf(a, slot, ty, kind, val);
}

fn fieldByName(sd: *schema.StructDecl, name: []const u8) ?u32 {
    for (sd.fields, 0..) |fl, i| if (std.mem.eql(u8, fl.name, name)) return @intCast(i);
    return null;
}

fn enterTable(a: Allocator, slot: *Value, ty: *const schema.Type, seg: []const u8) PathError!?*value.Struct {
    _ = a;
    switch (ty.*) {
        .struct_ => |sd| {
            if (sd.fieldByKey(seg) == null and fieldByName(sd, seg) == null) return null;
            return slot.st;
        },
        .pointer => |sd| {
            if (sd.fieldByKey(seg) == null and fieldByName(sd, seg) == null) return null;
            const nd = slot.ptr orelse return error.NoPath;
            return nd.st.?;
        },
        else => return null,
    }
}

fn setLeaf(a: Allocator, slot: *Value, ty: *const schema.Type, kind: []const u8, val: []const u8) PathError!void {
    if (eq(kind, "int")) {
        const v = std.fmt.parseInt(schema.Int, val, 10) catch return error.Unrepresentable;
        switch (ty.*) {
            .int => |it| {
                const lim = schema.intLimits(it.width, it.signed);
                if (v < lim.min or v > lim.max) return error.Unrepresentable;
                slot.* = .{ .i = v };
            },
            .fixed => |fx| {
                const lim = schema.intLimits(fx.width, fx.signed);
                if (v < lim.min or v > lim.max) return error.Unrepresentable;
                slot.* = .{ .i = v };
            },
            .flags => {
                if (v < 0 or v > std.math.maxInt(u64)) return error.Unrepresentable;
                slot.* = .{ .fl = @intCast(v) };
            },
            .timestamp => slot.* = .{ .i = v },
            else => return error.NoPath,
        }
        return;
    }
    if (eq(kind, "float")) {
        const x: f64 = if (eq(val, "nan")) std.math.nan(f64) else if (eq(val, "inf")) std.math.inf(f64) else if (eq(val, "-inf")) -std.math.inf(f64) else std.fmt.parseFloat(f64, val) catch return error.NoPath;
        switch (ty.*) {
            .float => |ft| {
                if (ft.wide) slot.* = .{ .f64 = x } else slot.* = .{ .f32 = @floatCast(x) };
            },
            else => return error.NoPath,
        }
        return;
    }
    if (eq(kind, "count")) {
        const n = std.fmt.parseInt(u64, val, 10) catch return error.Unrepresentable;
        if (n > 1 << 24) return error.Unrepresentable;
        const at = switch (ty.*) {
            .array => |at| at,
            else => return error.NoPath,
        };
        const old = slot.arr;
        const xs = try a.alloc(Value, @intCast(n));
        for (xs, 0..) |*x, i| {
            x.* = if (i < old.len) old[i] else if (old.len > 0) old[0] else try value.elemDefault(a, &at);
        }
        slot.* = .{ .arr = xs };
        return;
    }
    if (eq(kind, "length")) {
        const n = std.fmt.parseInt(u64, val, 10) catch return error.Unrepresentable;
        if (n > 1 << 24) return error.Unrepresentable;
        const b = try a.alloc(u8, @intCast(n));
        @memset(b, 'a');
        slot.* = .{ .str = b };
        return;
    }
    if (eq(kind, "bytes")) {
        if (val.len % 2 != 0) return error.NoPath;
        const b = try a.alloc(u8, val.len / 2);
        _ = std.fmt.hexToBytes(b, val) catch return error.NoPath;
        slot.* = .{ .str = b };
        return;
    }
    if (eq(kind, "ordinal")) {
        const n = std.fmt.parseInt(u32, val, 10) catch return error.Unrepresentable;
        slot.* = .{ .en = n };
        return;
    }
    if (eq(kind, "tag")) {
        const n = std.fmt.parseInt(u32, val, 10) catch return error.Unrepresentable;
        const ud = switch (ty.*) {
            .union_ => |u| u,
            else => return error.NoPath,
        };
        var p: ?*Value = null;
        if (n >= 1 and n <= ud.arms.len) {
            if (ud.arms[n - 1].ty) |at| {
                p = try a.create(Value);
                p.?.* = try value.zeroOf(a, at);
            }
        }
        slot.* = .{ .un = .{ .arm = n, .payload = p } };
        return;
    }
    return error.NoPath;
}

test {
    _ = json;
    _ = wire;
}

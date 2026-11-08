// Typed fixed records <-> the flat value table used by form-1 load/save.
// Generated accessors select storage; these helpers own buffers and indices.
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;
import .schema_tables;
import .wire_load;
import .wire_save;
import .fixed;

fn value_reserve(vals: LoadVal[>..]&, n: i64) -> i64 {
    let first = vals.len;
    for _ in n { vals.push(LoadVal { .. }); }
    return first;
}
fn value_table(unit: LoadUnit, name: const u8[:]) -> i64 {
    for t, i in unit.tables { if t.name == name { return i; } }
    return -1;
}
fn value_union(unit: LoadUnit, name: const u8[:]) -> i64 {
    for u, i in unit.unions { if u.name == name { return i; } }
    return -1;
}
fn value_child(vals: const LoadVal[:], rec: i64, i: i64) -> i64 {
    if rec < 0 || rec >= vals.len || !vals[rec].made || i < 0 || i >= vals[rec].count { return -1; }
    let at = vals[rec].first + i;
    return if at >= 0 && at < vals.len { at } else { -1 };
}
fn value_rode(vals: const LoadVal[:], at: i64) -> bool {
    return at >= 0 && at < vals.len && vals[at].state != ValAbsent;
}
fn value_put_text(data: u8[>..]&, vals: LoadVal[>..]&, at: i64, src: const u8[:], n: i32) -> bool {
    if n < 0 || n > src.len { return false; }
    vals[at].off = data.len;
    vals[at].len = n;
    data.append(src[0..n as i64]);
    return true;
}
fn value_get_text(data: const u8[:], vals: const LoadVal[:], at: i64, dst: u8[:]) -> i32 {
    if !value_rode(vals, at) { return 0; }
    var n = vals[at].len;
    if n < 0 { n = 0; }
    if n > dst.len { n = dst.len; }
    let off = vals[at].off;
    if off < 0 || off > data.len || n > data.len - off { return 0; }
    for i in n { dst[i] = data[off + i]; }
    return n as! i32;
}

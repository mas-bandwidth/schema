// driver.goose — the Goose card conformance driver
// (tables/cards/conformance/FORMAT.md, test/conformance/README.md "The shape").
//
// It answers the driver contract — `<manifest> list` and `<manifest>
// <surface> <outdir>` — over the nine surfaces this leg implements (load,
// save, render, parse, bounds, upgrade, edit, taskhash, view) through the
// facade in cards.goose. The shell driver beside it runs it with `goose run`
// from the generated Card unit's directory, so every module of the unit is
// beside it; a fourth argument names the one surface a negative control
// damages (render, upgrade, hash).
//
// The working directory is the repository root, which is the contract: every
// path in the manifest is repo-relative, so this driver never resolves one.
import os;
import .cards;
import .wire_load;

fn fail(message: const u8[:]) {
    write_stderr(str("cards-driver: ", message, "\n"));
    exit(1);
}

fn input(path: const u8[:], out: u8[>..]&) {
    if !read_file(path, out) { fail(str("cannot read ", path)); }
}

fn put(outdir: const u8[:], name: const u8[:], ext: const u8[:], data: const u8[:]) {
    if !write_file(str(outdir, "/", name, ext), data) { fail(str("cannot write ", name, ext)); }
}

// refuse writes a case's refusal class, one line (FORMAT.md, Refusals).
fn refuse(outdir: const u8[:], name: const u8[:], why: const u8[:]) {
    var line: u8[>..] = [];
    line.append(why);
    line.push(10);
    put(outdir, name, ".refused", line);
}

// one answers one case of the surface; c is the manifest line's words.
fn one(surface: const u8[:], c: const u8[:][:], outdir: const u8[:], sabotage: const u8[:]) {
    let name = c[1];
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var wire: u8[>..] = [];
    var r = card::record(data, vals, nodes, wire);
    var why: u8[>..] = [];
    var text: u8[>..] = [];
    if surface != "view" { input(c[2], text); }
    if surface == "load" {
        if !card::Load(r, text, why) { fail(why); }
        var json: u8[>..] = [];
        if !card::ToJSON(r, json, why) { fail(why); }
        put(outdir, name, ".json", json);
        var rep: u8[>..] = [];
        card::ReportText(r.report, rep);
        put(outdir, name, ".report", rep);
    } else if surface == "save" || surface == "bounds" {
        if !card::FromJSON(r, text, why) { refuse(outdir, name, why); return; }
        put(outdir, name, ".wire", card::Wire(r));
    } else if surface == "render" {
        if !card::Load(r, text, why) { fail(why); }
        var out: u8[>..] = [];
        if !card::Render(r, out, why) { fail(why); }
        if sabotage == "render" && out.len > 0 { out[0] = 0; }
        put(outdir, name, ".txt", out);
    } else if surface == "parse" {
        if !card::Parse(r, text, why) { refuse(outdir, name, why); return; }
        put(outdir, name, ".wire", card::Wire(r));
    } else if surface == "upgrade" {
        var out: u8[>..] = [];
        if !card::Upgrade(r, text, out, why) { refuse(outdir, name, why); return; }
        if sabotage == "upgrade" && out.len > 0 { out[0] ^= 0xFF; }
        put(outdir, name, ".wire", out);
        var rep: u8[>..] = [];
        card::ReportText(r.report, rep);
        put(outdir, name, ".report", rep);
    } else if surface == "edit" {
        if !card::Load(r, text, why) { fail(why); }
        if c.len != 4 { fail("an edit line carries exactly one edit"); }
        let eq = find(c[3], "=");
        if eq < 0 { fail(str("edit ", c[3], " has no =")); }
        let key = c[3][..eq];
        let value = c[3][eq + 1..];
        var data2: u8[>..] = [];
        var vals2: schema_rt::LoadVal[>..] = [];
        var nodes2: schema_rt::LoadNode[>..] = [];
        var wire2: u8[>..] = [];
        var e = card::record(data2, vals2, nodes2, wire2);
        var items: (const u8[:])[>..] = [];
        if value.len > 0 { for p in split(value, ',') { items.push(p); } }
        var ok = false;
        if key == "needs" { ok = card::SetList(r, e, "needs", items, 64, why); }
        else if key == "paths" { ok = card::SetList(r, e, "paths", items, 192, why); }
        else if key == "tier" || key == "priority" || key == "column" { ok = card::SetVariant(r, e, key, value, why); }
        else if key == "holder" { ok = card::SetHolder(r, e, value, why); }
        else { fail(str("unknown edit ", key)); }
        if !ok { refuse(outdir, name, why); return; }
        put(outdir, name, ".wire", card::Wire(e));
    } else if surface == "taskhash" {
        if !card::Load(r, text, why) { fail(why); }
        var hex: u8[>..] = [];
        if !card::TaskHash(r, hex, why) { fail(why); }
        if sabotage == "hash" {
            for i in hex.len { hex[i] = '0'; }
        }
        put(outdir, name, ".hex", hex);
    } else if surface == "view" {
        var out: u8[>..] = [];
        card::View(out);
        put(outdir, "view", ".txt", out);
    }
}

fn main() {
    let argv = args();
    if argv.len < 3 {
        write_stderr("usage: driver <manifest> list | <manifest> <surface> <outdir> [sabotage]\n");
        exit(2);
    }
    let surfaces = ["load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"];
    if argv[2] == "list" {
        for s in surfaces { write_stdout(str(s, "\n")); }
        return;
    }
    if argv.len < 4 {
        write_stderr("usage: driver <manifest> <surface> <outdir> [sabotage]\n");
        exit(2);
    }
    var known = false;
    for s in surfaces { if s == argv[2] { known = true; } }
    if !known { exit(2); }
    let sabotage = if argv.len > 4 { argv[4] } else { "" };
    var manifest: u8[>..] = [];
    input(argv[1], manifest);
    for raw in split(manifest, '\n') {
        let line = trim(raw);
        if line.len == 0 || line[0] == '#' { continue; }
        let c = words(line);
        if c[0] == argv[2] { one(argv[2], c, argv[3], sabotage); }
    }
}

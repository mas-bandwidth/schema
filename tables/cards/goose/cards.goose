// cards.goose — the Goose card facade (tables/cards/cards.go, parse.go,
// render.go, edit.go, upgrade.go), the owning layer a customer imports.
//
// A Card is a variable table, and a variable table lives in Goose as the FLAT
// VALUE TABLE the table wire loads into and saves from (docs/PORT-GOOSE.md,
// THE LOADED VALUE): the generated unit (TableUnit, TextUnit) and the runtime
// beside it (wire_load, wire_save, wire_retain, json_value) answer the wire
// and text forms, and this facade is the thin owning layer over one card's
// stores — the bytes, the value slots and the node table, which the caller
// owns, so a Record is a context of borrowed tables (the Goose spec, A.6). It
// keeps its own copy of the wire, refuses before it publishes, and enforces
// the two customer ceilings the schema deliberately leaves unbounded
// (docs/SPEC-TABLES.md §2.5, §2.9). It answers the nine surfaces of
// tables/cards/conformance byte for byte with the Go leg (tla/CardHops.tla:
// ImplementationsAgree, HopOrderIrrelevant).
//
// THE ERROR IDIOM. A call that can refuse answers false and appends its
// refusal CLASS to `why` — one of the closed classes
// tables/cards/conformance/FORMAT.md fixes (body-bound 65536, list-bound
// <field> 512, stranded-key <KEY>, retain-lost, unknown-present, malformed) —
// never free text.
namespace card;

import os;
import .schema_rt;
import .schema_tables;
import .schema_json;
import .json_value;
import .wire_load;
import .wire_save;
import .wire_retain;
import .cards_unit;
import .cards_json;

// MaxBodyBytes is the customer ceiling on one card's opaque body; the schema
// declares `body *string` with no bound (docs/SPEC-TABLES.md §2.5).
let MaxBodyBytes: i64 = 65536;

// MaxListEntries is the customer ceiling on each collection field, which the
// schema leaves unbounded (docs/SPEC-TABLES.md §2.9).
let MaxListEntries: i64 = 512;

// MaxRetainBytes and MaxRetainIDs are the capacities of the retained byte
// store and the retained-id store (docs/SPEC-TABLES.md §6.6).
let MaxRetainBytes: i64 = 1048576;
let MaxRetainIDs: i64 = 4096;

let Root: const u8[:] = "Card";

// Record is one card: the caller's four stores — the bytes its values view,
// its value slots, its node table and its own wire — the root's slot, and
// the read report of the load that produced it. A Record starts from empty
// stores and is filled once (Load, FromJSON, Parse); an edit decodes the
// record's wire into a second, fresh Record and publishes from there, so the
// original is never touched.
struct Record {
    data: u8[>..]&,
    vals: schema_rt::LoadVal[>..]&,
    nodes: schema_rt::LoadNode[>..]&,
    wire: u8[>..]&,
    root: i64,
    report: schema_rt::LoadReport,
}

fn record(data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, wire: u8[>..]&) -> Record {
    return Record { data: data, vals: vals, nodes: nodes, wire: wire, root: -1, report: schema_rt::LoadReport { .. } };
}

// Wire is the record's wire bytes.
fn Wire(r: const Record&) -> const u8[:] { return r.wire[..]; }

// ---- reading the flat value table ----

fn table_of(name: const u8[:]) -> i64 { return schema_rt::jv_table(cards::TableUnit, name); }

fn field_of(tab: i64, name: const u8[:]) -> i64 {
    for f, i in cards::TableUnit.tables[tab].fields { if f.name == name { return i; } }
    return -1;
}

fn desc(tab: i64, i: i64) -> schema_rt::FieldDesc { return cards::TableUnit.tables[tab].fields[i]; }

// slot is field `name` of record rec of table tab, or -1 (a record at its
// defaults).
fn slot(r: const Record&, tab: i64, rec: i64, name: const u8[:]) -> i64 {
    return schema_rt::jv_slot(r.vals, rec, field_of(tab, name));
}

// text is a string field's bytes ("" at its default).
fn text(r: const Record&, s: i64) -> const u8[:] {
    if schema_rt::jv_state(r.vals, s) == schema_rt::ValAbsent { return ""; }
    return r.data[r.vals[s].off..r.vals[s].off + r.vals[s].len];
}

// blob is a `*string` field's bytes, and whether it is non-null.
fn blob(r: const Record&, s: i64) -> const u8[:], bool {
    if s < 0 || r.vals[s].state != schema_rt::ValPresent { return "", false; }
    let w = r.vals[s].bits.lo as! i64;
    if w < 2 || w - 2 >= r.nodes.len { return "", false; }
    let n = r.nodes[w - 2];
    return r.data[n.off..n.off + n.len], true;
}

fn int_of(r: const Record&, s: i64) -> i64 {
    if schema_rt::jv_state(r.vals, s) == schema_rt::ValAbsent { return 0; }
    return r.vals[s].bits.lo as! i64;
}

// count is a list field's live count.
fn count(r: const Record&, s: i64) -> i64 {
    if s < 0 || !r.vals[s].made { return 0; }
    return r.vals[s].len;
}

// elem is element k of list slot s: a record slot.
fn elem(r: const Record&, s: i64, k: i64) -> i64 { return r.vals[s].first + k; }

// enum_name is an enum field's variant name, "" for None.
fn enum_name(r: const Record&, tab: i64, name: const u8[:]) -> const u8[:] {
    let i = field_of(tab, name);
    let v = int_of(r, schema_rt::jv_slot(r.vals, r.root, i));
    let d = desc(tab, i);
    if v <= 0 || v > d.variants.len { return ""; }
    return d.variants[v - 1].name;
}

// ---- UTF-8 and whitespace (Go's unicode.IsSpace, strings.TrimSpace) ----

// well_formed: valid UTF-8 with no NUL.
fn well_formed(s: const u8[:]) -> bool {
    var i: i64 = 0;
    while i < s.len {
        if s[i] == 0 { return false; }
        let code, w = schema_rt::json_utf8(s, i);
        if w < 0 { return false; }
        i += w;
    }
    return true;
}

fn space_at(s: const u8[:], i: i64) -> i64 {
    if i < 0 || i >= s.len { return 0; }
    let b = s[i];
    if b == 0x20 || (b >= 0x09 && b <= 0x0D) { return 1; }
    if b < 0x80 { return 0; }
    if i + 1 < s.len && b == 0xC2 && (s[i + 1] == 0x85 || s[i + 1] == 0xA0) { return 2; }
    if i + 2 < s.len {
        let c = s[i + 1];
        let d = s[i + 2];
        if b == 0xE1 && c == 0x9A && d == 0x80 { return 3; }
        if b == 0xE2 && c == 0x80 && ((d >= 0x80 && d <= 0x8A) || d == 0xA8 || d == 0xA9 || d == 0xAF) { return 3; }
        if b == 0xE2 && c == 0x81 && d == 0x9F { return 3; }
        if b == 0xE3 && c == 0x80 && d == 0x80 { return 3; }
    }
    return 0;
}

fn trim(s: const u8[:]) -> const u8[:] {
    var i: i64 = 0;
    var j = s.len;
    while i < j {
        let n = space_at(s, i);
        if n == 0 { break; }
        i += n;
    }
    while j > i {
        var back: i64 = 0;
        var n: i64 = 1;
        while n <= 3 {
            if j - n >= i && space_at(s, j - n) == n {
                back = n;
                break;
            }
            n += 1;
        }
        if back == 0 { break; }
        j -= back;
    }
    return s[i..j];
}

fn has_crlf(s: const u8[:]) -> bool {
    for b in s { if b == 13 || b == 10 { return true; } }
    return false;
}

// header_key: one letter, then letters, digits and hyphens.
fn header_key(s: const u8[:]) -> bool {
    if s.len == 0 { return false; }
    for b, i in s {
        let letter = (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z');
        let digit = b >= '0' && b <= '9';
        if !letter && !(i > 0 && (digit || b == '-')) { return false; }
    }
    return true;
}

fn reserved(k: const u8[:]) -> bool {
    return k == "RESULT" || k == "tier" || k == "REPO" || k == "BASE" || k == "KIND" || k == "PRIORITY" || k == "PATHS" || k == "NEW" || k == "SHARED" || k == "DEPENDS-ON" || k == "TEST" || k == "DONE-WHEN" || k == "DEADLINE" || k == "SOURCE";
}

fn stranded(k: const u8[:]) -> bool {
    return k == "KIND" || k == "PATHS" || k == "TEST" || k == "LEGS" || k == "SOURCE" || k == "START" || k == "STOP" || k == "DEPENDS-ON" || k == "PRIORITY" || k == "NEW" || k == "SHARED" || k == "REPO" || k == "BASE" || k == "tier" || k == "RESULT";
}

fn is_tier(s: const u8[:]) -> bool { s == "flash" || s == "pro" || s == "heavy" || s == "frontier" }
fn is_priority(s: const u8[:]) -> bool { s == "normal" || s == "high" || s == "critical" || s == "blocker" }
fn is_column(s: const u8[:]) -> bool { s == "waiting" || s == "ready" || s == "working" || s == "review" || s == "merging" || s == "landed" || s == "plan" || s == "check" }

// ---- the checked write (cards.go checkCard) ----

fn list_bound(why: u8[>..]&, name: const u8[:]) -> bool {
    format(why, "list-bound ", name, " 512");
    return false;
}

fn malformed(why: u8[>..]&) -> bool {
    why.append("malformed");
    return false;
}

fn check_strings(r: const Record&, tab: i64, name: const u8[:], why: u8[>..]&) -> bool {
    let s = slot(r, tab, r.root, name);
    let n = count(r, s);
    if n > MaxListEntries { return list_bound(why, name); }
    for k in n {
        let e = elem(r, s, k);
        let et = r.vals[e].tab;
        if !well_formed(text(r, slot(r, et, e, "s"))) { return malformed(why); }
    }
    return true;
}

fn check_extras(r: const Record&, tab: i64, why: u8[>..]&) -> bool {
    let s = slot(r, tab, r.root, "extra");
    let n = count(r, s);
    if n > MaxListEntries { return list_bound(why, "extra"); }
    for k in n {
        let e = elem(r, s, k);
        let et = r.vals[e].tab;
        let key = text(r, slot(r, et, e, "key"));
        if !well_formed(key) || !header_key(key) || has_crlf(key) || reserved(key) { return malformed(why); }
        let value, present = blob(r, slot(r, et, e, "value"));
        if present && (!well_formed(value) || has_crlf(value)) { return malformed(why); }
    }
    return true;
}

fn check_consumers(r: const Record&, tab: i64, why: u8[>..]&) -> bool {
    let s = slot(r, tab, r.root, "consumers");
    let n = count(r, s);
    if n > MaxListEntries { return list_bound(why, "consumers"); }
    for k in n {
        let e = elem(r, s, k);
        let et = r.vals[e].tab;
        for name in ["kind", "who", "route", "model", "outcome", "line"] {
            if !well_formed(text(r, slot(r, et, e, name))) { return malformed(why); }
        }
    }
    return true;
}

// check_card enforces the customer bounds and the well-formedness of the
// opaque body and of every collection element before bytes are published
// (docs/SPEC-TABLES.md §2.5, §2.9).
fn check_card(r: const Record&, why: u8[>..]&) -> bool {
    let tab = table_of(Root);
    if r.root < 0 { return malformed(why); }
    let body, present = blob(r, slot(r, tab, r.root, "body"));
    if present {
        if body.len > MaxBodyBytes {
            why.append("body-bound 65536");
            return false;
        }
        if !well_formed(body) { return malformed(why); }
    }
    return check_strings(r, tab, "paths", why) && check_strings(r, tab, "_new", why) && check_strings(r, tab, "shared", why) && check_strings(r, tab, "needs", why) && check_extras(r, tab, why) && check_consumers(r, tab, why);
}

// save publishes the record's values as its wire, only once every check has
// passed.
fn save(r: Record&, why: u8[>..]&) -> bool {
    var out: u8[>..<] = [];
    if schema_rt::save_table(cards::TableUnit, Root, r.data, r.vals, r.nodes, r.root, out) != 0 { return malformed(why); }
    r.wire.append(out);
    return true;
}

// ---- the owning facade (cards.go) ----

// Load reads wire into a Record (docs/SPEC-TABLES.md §6.2): a malformed wire
// refuses; tolerant events ride in r.report. The ORIGINAL wire is kept, never
// rewritten.
fn Load(r: Record&, wire: const u8[:], why: u8[>..]&) -> bool {
    r.wire.append(wire);
    r.data.append(wire);
    r.root = schema_rt::load_table(cards::TableUnit, Root, r.data, r.vals, r.nodes, r.report);
    if r.root < 0 { return malformed(why); }
    return check_card(r, why);
}

// FromJSON builds a Record from JSON text (docs/SPEC-TABLES.md §16): unknown,
// mismatched, clamped and malformed inputs refuse, as do the customer bounds;
// bytes are published only on complete success.
fn FromJSON(r: Record&, json: const u8[:], why: u8[>..]&) -> bool {
    var jr = schema_rt::json_report();
    r.root = schema_rt::json_load_table(cards::TableUnit, cards::TextUnit, Root, json, r.data, r.vals, r.nodes, jr);
    if r.root < 0 || jr.damaged { return malformed(why); }
    if jr.unknown > 0 { return malformed(why); }
    if !check_card(r, why) { return false; }
    return save(r, why);
}

// ToJSON appends the record's known-field JSON text (docs/SPEC-TABLES.md §16).
fn ToJSON(r: const Record&, out: u8[>..]&, why: u8[>..]&) -> bool {
    if schema_rt::json_save_table(cards::TableUnit, cards::TextUnit, Root, r.data, r.vals, r.nodes, r.root, out) < 0 { return malformed(why); }
    return true;
}

// ---- render (render.go) ----

fn line(out: u8[>..]&, key: const u8[:], value: const u8[:]) {
    out.append(key);
    out.append(": ");
    out.append(value);
    out.push(10);
}

fn joined(r: const Record&, s: i64, out: u8[>..]&) {
    for k in count(r, s) {
        if k > 0 { out.append(", "); }
        let e = elem(r, s, k);
        out.append(text(r, slot(r, r.vals[e].tab, e, "s")));
    }
}

fn list_line(r: const Record&, tab: i64, out: u8[>..]&, key: const u8[:], name: const u8[:]) {
    let s = slot(r, tab, r.root, name);
    if count(r, s) == 0 { return; }
    out.append(key);
    out.append(": ");
    joined(r, s, out);
    out.push(10);
}

// Render appends the card's informational brief text (JOB.md).
fn Render(r: const Record&, out: u8[>..]&, why: u8[>..]&) -> bool {
    let tab = table_of(Root);
    let tier = enum_name(r, tab, "tier");
    if tier.len == 0 { return malformed(why); }
    out.append("RESULT: ");
    out.append(text(r, slot(r, tab, r.root, "id")));
    out.append(" sha=");
    out.append(text(r, slot(r, tab, r.root, "sha")));
    out.append(" tier: ");
    out.append(tier);
    out.push(10);
    for f in ["repo", "_base", "kind"] {
        let v = text(r, slot(r, tab, r.root, f));
        if v.len > 0 { line(out, if f == "repo" { "REPO" } else if f == "_base" { "BASE" } else { "KIND" }, v); }
    }
    let pr = slot(r, tab, r.root, "priority");
    if int_of(r, pr) != 0 {
        let p = enum_name(r, tab, "priority");
        if p.len == 0 { return malformed(why); }
        line(out, "PRIORITY", p);
    }
    list_line(r, tab, out, "PATHS", "paths");
    list_line(r, tab, out, "NEW", "_new");
    list_line(r, tab, out, "SHARED", "shared");
    list_line(r, tab, out, "DEPENDS-ON", "needs");
    let test = text(r, slot(r, tab, r.root, "test"));
    if test.len > 0 { line(out, "TEST", test); }
    let done, has_done = blob(r, slot(r, tab, r.root, "done"));
    if has_done { line(out, "DONE-WHEN", done); }
    let minutes = int_of(r, slot(r, tab, r.root, "deadline_minutes"));
    if minutes != 0 { format(out, "DEADLINE: finish within ", minutes, " minutes.\n"); }
    let source, has_source = blob(r, slot(r, tab, r.root, "source"));
    if has_source { line(out, "SOURCE", source); }
    let xs = slot(r, tab, r.root, "extra");
    for k in count(r, xs) {
        let e = elem(r, xs, k);
        let et = r.vals[e].tab;
        let value, present = blob(r, slot(r, et, e, "value"));
        line(out, text(r, slot(r, et, e, "key")), if present { value } else { "" });
    }
    let body, has_body = blob(r, slot(r, tab, r.root, "body"));
    if has_body { out.append(body); }
    return true;
}

// ---- parse (parse.go): the brief, assembled as the card's JSON text and
// read through the same checked path FromJSON takes ----

fn last_index(s: const u8[:], needle: const u8[:]) -> i64 { return rfind(s, needle); }

fn is_hex(s: const u8[:]) -> bool {
    if s.len == 0 { return false; }
    for b in s {
        if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')) { return false; }
    }
    return true;
}

fn is_blank(s: const u8[:]) -> bool {
    for b in s { if b != ' ' && b != 9 && b != 13 { return false; } }
    return true;
}

// match_header splits a header line `Key:<ws>value`: the key, the value and
// whether the line is one.
fn match_header(content: const u8[:]) -> const u8[:], const u8[:], bool {
    let colon = find(content, ":");
    if colon <= 0 || !header_key(content[..colon]) { return "", "", false; }
    var v = colon + 1;
    while v < content.len && (content[v] == ' ' || content[v] == 9 || content[v] == 10 || content[v] == 12 || content[v] == 13) { v += 1; }
    return content[..colon], content[v..], true;
}

// The brief's fields as JSON members, in the order build writes them.
struct Brief {
    id: const u8[:],
    sha: const u8[:],
    tier: const u8[:],
    repo: const u8[:],
    base: const u8[:],
    kind: const u8[:],
    priority: const u8[:],
    test: const u8[:],
    done: const u8[:],
    source: const u8[:],
    deadline: i64,
}

fn put_member(json: u8[>..]&, key: const u8[:], value: const u8[:]) {
    json.append(",\n");
    schema_rt::json_put_string(json, key);
    json.append(": ");
    schema_rt::json_put_string(json, value);
}

// put_list splits a comma-separated list, trims the elements and drops empty
// ones, holding each to its bound and the list to the entry ceiling (§2.9);
// an element is its one-field record, `{"s": ...}`.
// A whole value of "-" or empty is an empty list; PATHS alone also takes the
// sentinel "none".
fn put_list(json: u8[>..]&, key: const u8[:], value: const u8[:], bound: i64, none: bool) -> bool {
    let v = trim(value);
    json.append(",\n");
    schema_rt::json_put_string(json, key);
    json.append(": [");
    if v.len == 0 || v == "-" || (none && v == "none") {
        json.push(']');
        return true;
    }
    var n: i64 = 0;
    var ok = true;
    each_split(v, ',') { part =>
        let p = trim(part);
        if p.len > 0 {
            if p.len > bound { ok = false; }
            if n > 0 { json.push(','); }
            json.append("{\"s\": ");
            schema_rt::json_put_string(json, p);
            json.push('}');
            n += 1;
        }
    };
    json.push(']');
    return ok && n <= MaxListEntries;
}

// Parse reads admitted brief text and publishes a Record only on complete
// checked success (docs/SPEC-TABLES.md §2, §6.2). The body is the exact suffix
// after the last header line's terminator.
fn Parse(r: Record&, brief: const u8[:], why: u8[>..]&) -> bool {
    if !well_formed(brief) { return malformed(why); }
    // every member is a view of the brief, so each starts as its empty view
    let none = brief[..0];
    var b = Brief { id: none, sha: none, tier: none, repo: none, base: none, kind: none, priority: none, test: none, done: none, source: none, deadline: 0 };
    var lists: u8[>..] = [];
    var extras: u8[>..] = [];
    var seen_tier = false;
    var seen: u8[>..] = [];   // the mapped keys met, each followed by a NUL
    var line_no: i64 = 0;
    var at: i64 = 0;
    var last_end: i64 = 0;
    var prose = false;
    var ok = true;
    var stranded_key = none;
    while at < brief.len && ok {
        var end = at;
        while end < brief.len && brief[end] != 10 { end += 1; }
        let term = if end < brief.len { end + 1 } else { end };
        var content = brief[at..end];
        if content.len > 0 && content[content.len - 1] == 13 { content = content[..content.len - 1]; }
        line_no += 1;
        at = term;
        if line_no == 1 {
            let key, value, is = match_header(content);
            if !is || key != "RESULT" {
                ok = false;
                break;
            }
            let i = last_index(value, " tier:");
            if i < 0 { ok = false; break; }
            let tier = trim(value[i + 6..]);
            let rest = value[..i];
            let j = last_index(rest, " sha=");
            if j < 0 { ok = false; break; }
            let id = trim(rest[..j]);
            let sha = rest[j + 5..];
            if id.len > 64 || !is_hex(sha) || (sha.len != 12 && sha.len != 40) || !is_tier(tier) { ok = false; break; }
            b.id = id;
            b.sha = sha;
            b.tier = tier;
            last_end = term;
            continue;
        }
        if is_blank(content) { continue; }
        let key, value, is = match_header(content);
        if !is {
            prose = true;
            continue;
        }
        if prose {
            if stranded(key) {
                stranded_key = key;
                ok = false;
            }
            continue;
        }
        if has_crlf(value) { ok = false; break; }
        if key == "tier" {
            if seen_tier || !is_tier(trim(value)) || trim(value) != b.tier { ok = false; break; }
            seen_tier = true;
            last_end = term;
            continue;
        }
        if key == "RESULT" { ok = false; break; }
        if reserved(key) {
            // a mapped key twice is a duplicate header
            var dup = false;
            each_split(seen, 0) { k => if k == key { dup = true; } };
            if dup { ok = false; break; }
            seen.append(key);
            seen.push(0);
            if key == "REPO" { b.repo = value; ok = value.len <= 96; }
            else if key == "BASE" { b.base = value; ok = value.len <= 96; }
            else if key == "KIND" { b.kind = value; ok = value.len <= 24; }
            else if key == "TEST" { b.test = value; ok = value.len <= 192; }
            else if key == "DONE-WHEN" { b.done = value; }
            else if key == "SOURCE" { b.source = value; }
            else if key == "PRIORITY" { b.priority = trim(value); ok = is_priority(b.priority); }
            else if key == "PATHS" { ok = put_list(lists, "paths", value, 192, true); }
            else if key == "NEW" { ok = put_list(lists, "_new", value, 192, false); }
            else if key == "SHARED" { ok = put_list(lists, "shared", value, 192, false); }
            else if key == "DEPENDS-ON" { ok = put_list(lists, "needs", value, 64, false); }
            else if key == "DEADLINE" {
                let d = trim(value);
                let head = "finish within ";
                let tail = " minutes.";
                ok = d.len > head.len + tail.len && starts_with(d, head) && ends_with(d, tail);
                if ok {
                    let digits = d[head.len..d.len - tail.len];
                    for c in digits { if c < '0' || c > '9' { ok = false; } }
                    if ok {
                        let n, good = parse_int(digits);
                        ok = good && digits.len <= 18 && n <= 100000;
                        b.deadline = n;
                    }
                }
            }
            last_end = term;
            continue;
        }
        if key.len > 32 { ok = false; break; }
        if extras.len > 0 { extras.push(','); }
        extras.append("{\"key\": ");
        schema_rt::json_put_string(extras, key);
        extras.append(", \"value\": ");
        schema_rt::json_put_string(extras, value);
        extras.push('}');
        last_end = term;
    }
    if stranded_key.len > 0 {
        format(why, "stranded-key ", stranded_key);
        return false;
    }
    if !ok || b.tier.len == 0 { return malformed(why); }
    var json: u8[>..] = [];
    json.append("{\n\"id\": ");
    schema_rt::json_put_string(json, b.id);
    put_member(json, "sha", b.sha);
    put_member(json, "tier", b.tier);
    put_member(json, "repo", b.repo);
    put_member(json, "_base", b.base);
    put_member(json, "kind", b.kind);
    if b.priority.len > 0 { put_member(json, "priority", b.priority); }
    json.append(lists);
    put_member(json, "test", b.test);
    if b.done.len > 0 { put_member(json, "done_when", b.done); }
    format(json, ",\n\"deadline_minutes\": ", b.deadline);
    if b.source.len > 0 { put_member(json, "source", b.source); }
    json.append(",\n\"extra\": [");
    json.append(extras);
    json.push(']');
    if last_end < brief.len { put_member(json, "body", brief[last_end..]); }
    json.append("\n}\n");
    return FromJSON(r, json, why);
}

// ---- upgrade and the strict read (upgrade.go) ----

// loss names the report events that make a rewrite unsafe (§6.6); unknown,
// retained and widened alone are not loss (§4).
fn loss(rep: const schema_rt::LoadReport&, why: u8[>..]&) -> bool {
    if rep.retain_lost > 0 {
        why.append("retain-lost");
        return false;
    }
    if rep.damaged || rep.refused { return malformed(why); }
    return true;
}

// Upgrade rewrites wire through the retaining path, so every field the
// current declaration cannot name is preserved at the end of its own body
// (§6.6); it refuses when any value would be lost, and the input is never
// touched (tla/CardVersioning.tla).
fn Upgrade(r: Record&, wire: const u8[:], out: u8[>..]&, why: u8[>..]&) -> bool {
    r.data.append(wire);
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: MaxRetainBytes, id_capacity: MaxRetainIDs, used: 0, bytes: bytes, recs: recs, ids: ids };
    r.root = schema_rt::load_table_retain(cards::TableUnit, Root, r.data, r.vals, r.nodes, r.report, store);
    if r.root < 0 { return malformed(why); }
    if !check_card(r, why) { return false; }
    var saved: u8[>..<] = [];
    if schema_rt::save_table_retain(cards::TableUnit, Root, r.data, r.vals, r.nodes, r.root, store, r.report, saved) != 0 { return malformed(why); }
    if !loss(r.report, why) { return false; }
    out.append(saved);
    return true;
}

// ---- the task hash (edit.go): SHA-256 (FIPS 180-4) ----

let K: u32[64] = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

fn rotr(x: u32, n: u32) -> u32 { (x >> n) | (x << (32 - n)) }

// sha256_hex appends the digest of message as 64 lowercase hex digits.
fn sha256_hex(message: const u8[:], out: u8[>..]&) {
    var h: u32[8] = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
    var padded: u8[>..] = [];
    padded.append(message);
    padded.push(0x80);
    while padded.len % 64 != 56 { padded.push(0); }
    let bits = (message.len as! u64) * 8;
    var b: i64 = 7;
    while b >= 0 {
        padded.push(((bits >> ((b * 8) as! u64)) & 0xFF) as! u8);
        b -= 1;
    }
    var w: u32[64] = [0; 64];
    var chunk: i64 = 0;
    while chunk < padded.len {
        for i in 16 {
            let p = chunk + i * 4;
            let b0: u32 = padded[p];
            let b1: u32 = padded[p + 1];
            let b2: u32 = padded[p + 2];
            let b3: u32 = padded[p + 3];
            w[i] = (b0 << 24) | (b1 << 16) | (b2 << 8) | b3;
        }
        var i: i64 = 16;
        while i < 64 {
            let s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
            let s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
            w[i] = w[i - 16] + s0 + w[i - 7] + s1;
            i += 1;
        }
        var a = h[0];
        var bb = h[1];
        var c = h[2];
        var d = h[3];
        var e = h[4];
        var f = h[5];
        var g = h[6];
        var hh = h[7];
        for t in 64 {
            let big1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
            let ch = (e & f) ^ ((~e) & g);
            let t1 = hh + big1 + ch + K[t] + w[t];
            let big0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
            let maj = (a & bb) ^ (a & c) ^ (bb & c);
            let t2 = big0 + maj;
            hh = g;
            g = f;
            f = e;
            e = d + t1;
            d = c;
            c = bb;
            bb = a;
            a = t1 + t2;
        }
        h[0] += a;
        h[1] += bb;
        h[2] += c;
        h[3] += d;
        h[4] += e;
        h[5] += f;
        h[6] += g;
        h[7] += hh;
        chunk += 64;
    }
    for x in h { format_uint(out, x, 16, 8, '0'); }
}

// lossy names the read events that make a record unsafe to hash or rewrite
// (docs/SPEC-TABLES.md §4, §6.6).
fn lossy(rep: const schema_rt::LoadReport&, why: u8[>..]&) -> bool {
    if rep.unknown > 0 || rep.retain_lost > 0 || rep.damaged || rep.refused {
        why.append("unknown-present");
        return true;
    }
    return false;
}

fn frame(m: u8[>..]&, s: const u8[:]) {
    let n = s.len as! u64;
    for i in 8 { m.push(((n >> ((i * 8) as! u64)) & 0xFF) as! u8); }
    m.append(s);
}

fn frame_list(r: const Record&, tab: i64, name: const u8[:], m: u8[>..]&) {
    let s = slot(r, tab, r.root, name);
    let n = count(r, s) as! u64;
    for i in 8 { m.push(((n >> ((i * 8) as! u64)) & 0xFF) as! u8); }
    for k in count(r, s) {
        let e = elem(r, s, k);
        frame(m, text(r, slot(r, r.vals[e].tab, e, "s")));
    }
}

// TaskHash appends the hex digest of a record's TASK: body, paths, new,
// shared, test and done in that order, each framed as a little-endian u64
// length then its bytes, each list as a count then its framed elements; a
// lossy record refuses (docs/SPEC-TABLES.md §5; tla/CardHops.tla:
// TaskHashStable).
fn TaskHash(r: const Record&, out: u8[>..]&, why: u8[>..]&) -> bool {
    if lossy(r.report, why) { return false; }
    let tab = table_of(Root);
    var m: u8[>..] = [];
    let body, has_body = blob(r, slot(r, tab, r.root, "body"));
    frame(m, body);
    frame_list(r, tab, "paths", m);
    frame_list(r, tab, "_new", m);
    frame_list(r, tab, "shared", m);
    frame(m, text(r, slot(r, tab, r.root, "test")));
    let done, has_done = blob(r, slot(r, tab, r.root, "done"));
    frame(m, done);
    sha256_hex(m, out);
    return true;
}

// ---- the field edits (edit.go) ----

// editable decodes record src's own wire into the fresh record r, refusing a
// lossy source or decode: the edit rewrites from that decode, never from
// caller state, and src is never touched.
fn editable(src: const Record&, r: Record&, why: u8[>..]&) -> bool {
    if src.root < 0 { return malformed(why); }
    if lossy(src.report, why) { return false; }
    r.data.append(src.wire[..]);
    r.root = schema_rt::load_table(cards::TableUnit, Root, r.data, r.vals, r.nodes, r.report);
    if r.root < 0 { return malformed(why); }
    return !lossy(r.report, why);
}

fn validate(values: const (const u8[:])[:], bound: i64, why: u8[>..]&) -> bool {
    for s in values {
        if s.len > bound || !well_formed(s) { return malformed(why); }
    }
    return true;
}

// SetList replaces a string-element list (needs, paths): src edited into the
// fresh record r.
fn SetList(src: const Record&, r: Record&, name: const u8[:], values: const (const u8[:])[:], bound: i64, why: u8[>..]&) -> bool {
    if !validate(values, bound, why) { return false; }
    if values.len > MaxListEntries { return list_bound(why, name); }
    if !editable(src, r, why) { return false; }
    let tab = table_of(Root);
    let s = slot(r, tab, r.root, name);
    let e = desc(tab, field_of(tab, name)).elem;
    guard e else { return malformed(why); }
    let et = table_of(e.ref);
    let first = r.vals.len;
    for k in values.len { r.vals.push(schema_rt::LoadVal { shape: schema_rt::ShapeTab, .. }); }
    for v, k in values {
        let fs = r.vals.len;
        r.vals.push(schema_rt::LoadVal { shape: schema_rt::ShapeStr, state: schema_rt::ValPresent, off: r.data.len, len: v.len, .. });
        r.data.append(v);
        r.vals[first + k] = schema_rt::LoadVal { shape: schema_rt::ShapeTab, state: schema_rt::ValPresent, made: true, tab: et, first: fs, count: 1, .. };
    }
    r.vals[s] = schema_rt::LoadVal { shape: schema_rt::ShapeArr, state: schema_rt::ValPresent, made: true, first: first, count: values.len, len: values.len, .. };
    if !check_card(r, why) { return false; }
    return save(r, why);
}

// SetVariant sets an enum field to a declared variant (tier, priority, column).
fn SetVariant(src: const Record&, r: Record&, name: const u8[:], value: const u8[:], why: u8[>..]&) -> bool {
    let tab = table_of(Root);
    let d = desc(tab, field_of(tab, name));
    var v: i64 = 0;
    for x, i in d.variants { if x.name == value { v = i + 1; } }
    if v == 0 { return malformed(why); }
    if !editable(src, r, why) { return false; }
    let s = slot(r, tab, r.root, name);
    r.vals[s].state = schema_rt::ValPresent;
    r.vals[s].bits = schema_rt::Int128 { lo: v as! u64, hi: 0 };
    if !check_card(r, why) { return false; }
    return save(r, why);
}

// SetHolder sets the holder string.
fn SetHolder(src: const Record&, r: Record&, holder: const u8[:], why: u8[>..]&) -> bool {
    if holder.len > 32 || !well_formed(holder) { return malformed(why); }
    if !editable(src, r, why) { return false; }
    let tab = table_of(Root);
    let s = slot(r, tab, r.root, "holder");
    r.vals[s].state = schema_rt::ValPresent;
    r.vals[s].off = r.data.len;
    r.vals[s].len = holder.len;
    r.data.append(holder);
    if !check_card(r, why) { return false; }
    return save(r, why);
}

// ---- the view (docs/SPEC-TABLES.md §8) ----

// before: a sorts before b, byte by byte.
fn before(a: const u8[:], b: const u8[:]) -> bool {
    let n = if a.len < b.len { a.len } else { b.len };
    for i in n {
        if a[i] != b[i] { return a[i] < b[i]; }
    }
    return a.len < b.len;
}

// View appends one line per field of every table of the unit, tables in name
// order and each table's fields in declaration order:
// `<table> <field> <kind> <shape> <bound>`, every word from the descriptors.
fn View(out: u8[>..]&) {
    let tables = cards::TableUnit.tables;
    var order: i64[>..] = [];
    for t, i in tables {
        order.push(i);
        var j = order.len - 1;
        while j > 0 && before(tables[order[j]].name, tables[order[j - 1]].name) {
            let x = order[j - 1];
            order[j - 1] = order[j];
            order[j] = x;
            j -= 1;
        }
    }
    for i in order {
        for f in tables[i].fields {
            let shape = if f.shape == schema_rt::ShapeArr { "list" } else if f.shape == schema_rt::ShapeSBlob || f.shape == schema_rt::ShapeBBlob { "blob" } else { "value" };
            format(out, tables[i].name, " ", f.name, " ", f.type_name, " ", shape, " ");
            if shape == "value" && f.bound > 0 { format(out, f.bound); } else { out.push('-'); }
            out.push(10);
        }
    }
}

// ReportText appends the read report, one `Name=value` line per counter
// (FORMAT.md, The read report).
fn ReportText(rep: const schema_rt::LoadReport&, out: u8[>..]&) {
    format(out, "Retained=", rep.retained, "\nRetainLost=", rep.retain_lost, "\nWidened=", rep.widened, "\nUnknown=", rep.unknown, "\nKindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n");
}

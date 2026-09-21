// wire_test.nut — THE SQUIRREL LEG'S FOUNDATION TEST, RED FIRST
// (docs/SPEC-TABLES.md §3 the table wire, §3.4 the fixed form, §4 the read
// report, §5 the name hash; testdata/conformance/tables/MANIFEST.txt).
//
// Run:  sq test/conformance/squirrel/lib/wire_test.nut     (from the repo root)
// Exit: 0 green, 1 red — the script's return value is the exit code. One line
// per assertion, printed as it runs, so a red names itself.
//
// This file is what STAGE 0 owes before any row card runs: the §3 wire's
// little-endian scalars read back off REAL manifest files; the §3.4 layout
// prologue — the count, the seventeen-byte entries, the fnv1a64 hash — parses
// exactly as the spec states, with a byte offset per field; the header's hash
// is checked LAST so a broken layout is never reported as a lying header; a
// truncated file under twenty bytes is refused by name; and the writer
// round-trips one instance byte-identically.

local failed = 0;
local ran = 0;

function ok(desc, good) {
    ran = ran + 1;
    if (good) { ::print("ok   " + desc + "\n"); }
    else { ::print("FAIL " + desc + "\n"); failed = failed + 1; }
}

function sameBytes(a, b) {
    if (a.len() != b.len()) { return false; }
    for (local i = 0; i < a.len(); i++) { if (a[i] != b[i]) { return false; } }
    return true;
}

function copyBytes(b) {
    local c = ::blob(b.len());
    for (local i = 0; i < b.len(); i++) { c[i] = b[i]; }
    return c;
}

// THE MODULE — the one import every later row card makes.
local wire = null;
try {
    wire = ::dofile("test/conformance/squirrel/lib/wire.nut");
} catch (fromRoot) {
    try {
        wire = ::dofile("../lib/wire.nut");
    } catch (fromBeside) {
        ::print("FAIL load lib/wire.nut: " + fromRoot + " / " + fromBeside + "\n");
        return 1;
    }
}

// EVERYTHING ELSE runs inside one try, because a script that dies mid-run
// exits 0 in squirrel — an uncaught error must read RED, never green.
local function main() {

// REAL FILES the manifest names, found from the repo root or from beside this
// script — the driver contract's currency (test/conformance/README.md).
local DATA = null;
try {
    wire.readFile("testdata/wire/tables/chain_value_empty.bin");
    DATA = "testdata/wire/tables/";
} catch (fromRoot) {
    DATA = "../../../testdata/wire/tables/";
}

// ---------------------------------------------------------------------------
// §3: THE LITTLE-ENDIAN SCALARS, READ BACK OFF REAL FILES THE MANIFEST NAMES
// ---------------------------------------------------------------------------

local emptyChain = wire.readFile(DATA + "chain_value_empty.bin");
ok("chain_value_empty is the ten bytes §3 states of an empty table: a form byte, the zero reference, the eight-byte entry count", emptyChain.len() == 10);
ok("the form byte reads back 1 at the u8 width (§3: the variable form)", wire.u8At(emptyChain, 0) == 1);
ok("the body's zero reference reads back 0 at the u8 width", wire.u8At(emptyChain, 1) == 0);
ok("the empty table's entry count reads back 0 at the u64 width, little-endian", wire.u64At(emptyChain, 2) == 0);

local chain = wire.readFile(DATA + "chain_value.bin");
ok("chain_value's entry count reads back 4 in the final eight bytes (u64 LE)", wire.u64At(chain, chain.len() - 8) == 4);
ok("the first id-table entry reads back fnv1a64 of name at the u64 width — the hash IS the id (§5)", wire.u64At(chain, 26) == wire.fnv1a64("name"));
ok("the same eight bytes read back 0x8e631b86 at the u32 width — one little-endian spelling", wire.u32At(chain, 26) == 0x8e631b86);
ok("and 0x1b86 at the u16 width", wire.u16At(chain, 26) == 0x1b86);

local k2 = wire.readFile(DATA + "k2_as_k1.bin");
ok("k2_as_k1's uint16 payload reads back 3 at the u16 width (bytes 03 00)", wire.u16At(k2, 3) == 3);

local loadout = wire.readFile(DATA + "loadout_full.bin");
ok("loadout_full's damage payload reads back 0x41480000 at the f32 width — 12.5 as its IEEE-754 pattern (§3)", wire.f32At(loadout, 35) == 0x41480000);
ok("the float width is a pattern read: the same four bytes read back 0x41480000 at the u32 width, never a conversion", wire.u32At(loadout, 35) == 0x41480000);

local conn = wire.readFile(DATA + "backend_conn.bin");
ok("backend_conn's announced build version reads back 0x851cdd594a04b3fd at the u64 width (§3.3: reference 1, kind 9, eight bytes)", wire.u64At(conn, 3) == 0x851cdd594a04b3fd);

local block = wire.readFile(DATA + "block_render.bin");
ok("block_render's camera row reads back 0x3ff0000000000000 at the f64 width — the pinned row dump's own value", wire.f64At(block, 192) == 0x3ff0000000000000);
ok("the f64 width is a pattern read: the same eight bytes read back 0x3ff0000000000000 at the u64 width", wire.u64At(block, 192) == 0x3ff0000000000000);

// ---------------------------------------------------------------------------
// §3.4: THE LAYOUT PROLOGUE — COUNT, ENTRIES, HASH — AS THE SPEC STATES IT
// ---------------------------------------------------------------------------

// A table of every primitive width, the layout §3.4 states: the root, then its
// fields in declared order, each entry seventeen bytes — id, kind, constant
// size, child count — every number little-endian, no LEB128 anywhere.
local widths = [
    { name = "flag",    kind = 1,  size = 1 },
    { name = "tiny",    kind = 6,  size = 1 },
    { name = "small",   kind = 7,  size = 2 },
    { name = "word",    kind = 8,  size = 4 },
    { name = "big",     kind = 9,  size = 8 },
    { name = "angle",   kind = 10, size = 4 },
    { name = "precise", kind = 11, size = 8 },
];
local entries = [{ id = wire.fnv1a64("Scalars"), kind = 13, size = 28, children = 7 }];
foreach (f in widths) {
    entries.append({ id = wire.fnv1a64(f.name), kind = f.kind, size = f.size, children = 0 });
}
local layout = wire.layoutBytes(entries);

ok("the layout is the count and the entries §3.4 states: 4 + 17 * 8 = 140 bytes, no more", layout.len() == 140);
ok("the entry count reads back 8 in the layout's first four bytes (u32 LE)", wire.u32At(layout, 0) == 8);

// ONE INSTANCE of that table, every field off its default, the floats as the
// patterns a signalling NaN would need to survive (§3: no canonicalisation).
local values = { flag = 1, tiny = 0xab, small = 0xcdef, word = 0x89abcdef, big = 0xfedcba9876543210, angle = 0x7f800001, precise = 0x7ff0000000000001 };
local file = wire.saveFile(layout, [values]);
local read = wire.open(file);

ok("the file opens clean: not refused, no damage, every §4 counter zero", !read.refused && !read.report.malformed && read.report.unknown == 0 && read.report.kind_mismatch == 0 && read.report.widened == 0 && read.report.clamped == 0 && read.report.duplicate == 0);
ok("the file is the framing §3.4 states: 16 header + 4 length + 140 layout + 8 hash + 28 body = 196 bytes", file.len() == 196);
ok("the form byte is 3 and the seven reserved bytes are written zero (§3 THE FIRST BYTE)", file[0] == 3 && file[1] == 0 && file[2] == 0 && file[3] == 0 && file[4] == 0 && file[5] == 0 && file[6] == 0 && file[7] == 0);
ok("the layout length word at 16 reads back 140 (u32 LE)", wire.u32At(file, 16) == 140);
ok("the entry count parses back 8 at byte 20, the layout's first four bytes", wire.u32At(file, 20) == 8 && read.layout.count == 8);
ok("the parsed root is a table whose constant size is the sum of its fields: body 28", read.layout.entries[0].kind == 13 && read.layout.rootBody == 28);
ok("entry 2 is the facts §3.4 states for tiny: id fnv1a64, kind 6, size 1, no children", (function() { local e = read.layout.entries[2]; return e.id == wire.fnv1a64("tiny") && e.kind == 6 && e.size == 1 && e.children == 0; })());
ok("every field parses with a byte offset: flag at 0", wire.fieldByName(read.layout, "flag").offset == 0);
ok("tiny at 1", wire.fieldByName(read.layout, "tiny").offset == 1);
ok("small at 2", wire.fieldByName(read.layout, "small").offset == 2);
ok("word at 4", wire.fieldByName(read.layout, "word").offset == 4);
ok("big at 8", wire.fieldByName(read.layout, "big").offset == 8);
ok("angle at 16", wire.fieldByName(read.layout, "angle").offset == 16);
ok("precise at 20", wire.fieldByName(read.layout, "precise").offset == 20);
ok("fields resolve by name through §5's id: fieldByName of precise answers entry 7", wire.fieldByName(read.layout, "precise") == read.layout.entries[7]);
ok("the header's hash at 8 is fnv1a64 over the layout's 140 bytes exactly as written, and the open read agrees", wire.u64At(file, 8) == wire.hashOf(layout, 0, 140) && read.hash == wire.hashOf(layout, 0, 140));

// ---------------------------------------------------------------------------
// §3.4: THE HASH IS CHECKED LAST — A LYING HASH IS REFUSED BY NAME
// ---------------------------------------------------------------------------

// "It is checked LAST of the three so that a broken layout is never reported
// as a lying header" — break BOTH and the layout's own name must answer.
local bothBroken = copyBytes(file);
bothBroken[20] = bothBroken[20] ^ 0xff;
bothBroken[8] = bothBroken[8] ^ 0xff;
local rBoth = wire.open(bothBroken);
ok("a broken layout AND a lying hash answer layout_count_mismatch — the layout's own rule, never the lying header", rBoth.refused && rBoth.reason == "layout_count_mismatch");

local lyingOnly = copyBytes(file);
lyingOnly[8] = lyingOnly[8] ^ 0xff;
local rLying = wire.open(lyingOnly);
ok("a lying hash alone is refused BY NAME: layout_malformed", rLying.refused && rLying.reason == "layout_malformed");
ok("the lying-hash refusal moves none of §4's six counters and reports no damage", !rLying.report.malformed && rLying.report.unknown == 0 && rLying.report.kind_mismatch == 0 && rLying.report.widened == 0 && rLying.report.clamped == 0 && rLying.report.duplicate == 0);

// ---------------------------------------------------------------------------
// §3.4: A TRUNCATED FILE UNDER 20 BYTES IS REFUSED BY NAME; THE FORM BYTE FIRST
// ---------------------------------------------------------------------------

local truncated = ::blob(19);
for (local i = 0; i < 19; i++) { truncated[i] = file[i]; }
local rShort = wire.open(truncated);
ok("a truncated file under 20 bytes — no room for the layout's length word — is refused by name: layout_malformed", rShort.refused && rShort.reason == "layout_malformed");

local asVariable = wire.open(emptyChain);
ok("the REAL form-1 file chain_value_empty handed to the fixed reader answers previous_form — form 1 is the OLDER variable form", asVariable.refused && asVariable.reason == "previous_form");

local asMessage = copyBytes(file);
asMessage[0] = 2;
local rMessage = wire.open(asMessage);
ok("form 2 answers message_form_as_file — a batch where a file was expected", rMessage.refused && rMessage.reason == "message_form_as_file");

local asNewer = copyBytes(file);
asNewer[0] = 6;
local rNewer = wire.open(asNewer);
ok("form 6, a byte no form defines or reserves, answers newer_form", rNewer.refused && rNewer.reason == "newer_form");

// ---------------------------------------------------------------------------
// §3.4 THE WRITE: THE WRITER ROUND-TRIPS ONE INSTANCE BYTE-IDENTICALLY
// ---------------------------------------------------------------------------

local body = read.records[0].body;
ok("the record carries the layout's hash in its own first eight bytes", wire.u64At(file, body - 8) == read.hash);
ok("flag reads back 1 at byte offset 0", wire.u8At(file, body + wire.fieldByName(read.layout, "flag").offset) == values.flag);
ok("tiny reads back 0xab at byte offset 1", wire.u8At(file, body + wire.fieldByName(read.layout, "tiny").offset) == values.tiny);
ok("small reads back 0xcdef at byte offset 2", wire.u16At(file, body + wire.fieldByName(read.layout, "small").offset) == values.small);
ok("word reads back 0x89abcdef at byte offset 4", wire.u32At(file, body + wire.fieldByName(read.layout, "word").offset) == values.word);
ok("big reads back 0xfedcba9876543210 at byte offset 8, top bit and all", wire.u64At(file, body + wire.fieldByName(read.layout, "big").offset) == values.big);
ok("angle reads back the signalling-NaN pattern 0x7f800001 bit for bit — no canonicalisation (§3)", wire.f32At(file, body + wire.fieldByName(read.layout, "angle").offset) == 0x7f800001);
ok("precise reads back the pattern 0x7ff0000000000001 bit for bit (§3)", wire.f64At(file, body + wire.fieldByName(read.layout, "precise").offset) == 0x7ff0000000000001);

local readback = {};
foreach (f in widths) {
    local e = wire.fieldByName(read.layout, f.name);
    local v = null;
    if (e.size == 1) { v = wire.u8At(file, body + e.offset); }
    else if (e.size == 2) { v = wire.u16At(file, body + e.offset); }
    else if (e.size == 4) { v = wire.u32At(file, body + e.offset); }
    else { v = wire.u64At(file, body + e.offset); }
    readback[f.name] <- v;
}
local again = wire.saveFile(layout, [readback]);
ok("the writer round-trips the one instance byte-identically: what was read, re-saved, is the same 196 bytes", sameBytes(again, file));

// ---------------------------------------------------------------------------
// §4: THE READ REPORT SHAPE
// ---------------------------------------------------------------------------

ok("a clean read prints the reports.txt row: 0,0,0,0,0,false,read", wire.reportRow(read.report) == "0,0,0,0,0,false,read");
ok("a refusal prints the same six zeroes and the VERDICT that tells it apart: 0,0,0,0,0,false,refused", wire.reportRow(rLying.report) == "0,0,0,0,0,false,refused");

if (failed == 0) {
    ::print("wire: the squirrel foundation's " + ran + " assertions green\n");
    return 0;
}
::print("wire: " + failed + " of " + ran + " assertions red\n");
return 1;
}

try {
    return main();
} catch (e) {
    ::print("FAIL uncaught error: " + e + "\n");
    return 1;
}

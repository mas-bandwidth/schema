// THE LOBSTER CONFORMANCE PROGRAM (test/conformance/README.md;
// docs/PORT-LOBSTER.md, THE HARNESS). The shell driver beside this file runs
// it once per surface and generated unit:
//
//   lobster <unit dir>/conformance_main.lobster -- <repo root> <manifest> <surface> <unit> <case or ->
//
// Lobster imports are resolved when the program is compiled, so one program
// cannot reach every unit: the driver writes this file into each generated
// unit directory as conformance_main.lobster, after one `import .<Base>Table`
// line per table module the unit has, and runs it there.
// A unit the target refused has no directory, and the driver answers its cases
// `absent` without running anything.
//
// Inputs go IN as files, read by absolute path: a relative read_file resolves
// against the program's own directory, not the working directory. Answers come
// OUT on stdout, one or more lines per case — the case name, a space, the
// answer's bytes as lowercase hex (a long answer continues on further lines
// under the same name, each at most 40000 hex digits), or the word `absent` —
// which the driver writes as one file per case.
//
// Only the surfaces whose runtime has landed are listed (`list` prints them);
// the driver answers every other surface ABSENT (exit 2). What the leg does not
// carry, and why, is docs/PORT-LOBSTER.md's THE HARNESS.

import .load
import .schema_save
import .strict_and_refusals
import .block_form
import .message
import .cook
import .retain_unknown
import .json
import .conformance_hooks

// the surfaces this leg answers: the variable wire's load and save (§3, §4),
// the message form (§3.3), retain-unknown (§6.6), the cooked form's read,
// write and forgery battery (§7), and the block form's reflective read with
// its forgery battery (§19.2), each refusal with its reason, the JSON
// text form (§16), the framing vectors (docs/LAYERS/1-framing.md), and the
// write contract through each table writer (SPEC §5.1), and the 2026-11-22
// surfaces: the cook writer and the block builder held to it, the
// announcement, the read into caller values, and the text written under a
// comma-decimal locale (test/conformance/README.md)
let surfaces = ["wire", "message", "report", "framing", "retain", "retain-save", "cook", "cook-write", "cook-foreign",
    "block", "block-foreign", "block-dump", "forgery", "cook-forgery", "cook-reason", "block-reason",
    "json-read", "json-write", "json-hostile",
    "write-refusal", "write-refusal-fixed", "write-refusal-message", "write-refusal-json",
    "write-refusal-cook", "write-refusal-block", "announce", "read-into", "json-write-into"]

let arguments = command_line_arguments()
let argument_count = length(arguments)
assert argument_count >= 5
let repo_root = arguments[argument_count - 5]
let manifest_path = arguments[argument_count - 4]
let surface = arguments[argument_count - 3]
let unit_key = arguments[argument_count - 2]
// case_only is one case the driver reruns alone, or - for every case
let case_only = arguments[argument_count - 1]

def hexs(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)

// say writes one case's answer: its bytes as hex, in lines of at most 40000
// digits that the driver joins back under the case's name
def say(name:string, b:string):
    let h = hexs(b)
    if length(h) == 0:
        print(name + " ")
    var at = 0
    while at < length(h):
        print(name + " " + substring(h, at, min(40000, length(h) - at)))
        at += 40000

// words splits a manifest line on spaces, dropping the empty segments a run
// of spaces leaves
def words(line:string) -> [string]:
    let out:[string] = []
    for(tokenize(line, " ", " \t\r")) w:
        if length(w) > 0:
            push(out, w)
    return out

// rows are the manifest's lines of one kind that name this unit
// (test/conformance/harness/run.go, deriveManifest): the unit is the third word
// of an instance, a report and a block line alike
def rows(text:string, kind:string) -> [[string]]:
    let out:[[string]] = []
    for(tokenize(text, "\n", " \t\r")) line:
        let f = words(line)
        if length(f) >= 4 and f[0] == kind and f[2] == unit_key:
            push(out, f)
    return out

// fixture reads a repo-relative manifest path from the repository root
def fixture(path:string) -> string:
    let data = read_file(repo_root + "/" + path)
    assert data
    return data

// WIRE returns the complete canonical writeback after acceptance, or a refusal
// without a value. The harness determines which outcome each instance owes.
// An instance line is `instance <name> <unit> <root> <wire> [no-text]`.
def surface_wire(text:string):
    for(rows(text, "instance")) f:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load(f[3], fixture(f[4]), rep)
        if not rec:
            // a wire-refused instance (blob_shared, §2.5) owes the refusal
            // alone: no bytes, an absence naming the reason
            print(f[1] + " absent " + schema_rt.strict_reason(rep))
            continue
        let out, why = schema_rt.save_reasoned(rec)
        if why != "":
            // a value the writer refuses by name (blob_shared, §2.5): the
            // answer is the refusal, never bytes
            print(f[1] + " absent " + why)
        else:
            say(f[1], out)

// read_answer is an accepted read's whole answer (test/conformance/README.md):
// `read`, the §16 text of the value, and beside it, as <case>.wire, the
// canonical write-back wire, so a leg that decodes the right value but saves
// other bytes has not returned it
def read_answer(name:string, rec:schema_rt.Record):
    let js, why = schema_rt.json_write(rec)
    assert why == ""
    say(name, "read\n" + js)
    let out, swhy = schema_rt.save_reasoned(rec)
    assert swhy == ""
    say(name + ".wire", out)

// REPORT and FRAMING return one verdict and the complete canonical value
// after acceptance. A refusal has no value payload; diagnostics stay local.
// Their lines are `<surface> <case> <unit> <root> <wire>`.
def surface_report(text:string):
    for(rows(text, "report")) f:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load(f[3], fixture(f[4]), rep)
        if rec:
            read_answer(f[1], rec)
        else:
            say(f[1], "refused\n")

// The framing vectors are form-1 files pinning the variable form's rules
// (docs/LAYERS/1-framing.md). A root declared `fixed table` is read through
// the same declaration with its class variable, as the oracle reads it
// (test/conformance/harness/generate.go, framingVariableUnit), then restored,
// so the write-back is the root's own form as the oracle saves it.
def surface_framing(text:string):
    for(rows(text, "framing")) f:
        let d = schema_rt.table_of(f[3])
        let was_fixed = d.fixed
        d.fixed = false
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load(f[3], fixture(f[4]), rep)
        d.fixed = was_fixed
        if rec:
            read_answer(f[1], rec)
        else:
            say(f[1], "refused\n")

// block_root is the table a block image of the manifest projects, by the
// image's name, as every leg's driver names it (test/conformance/js/main.mjs)
def block_root(name:string) -> string:
    if find_string(name, "block_render") == 0:
        return "RenderFrame"
    if find_string(name, "block_padded") == 0:
        return "PaddedFrame"
    assert find_string(name, "block_torture") == 0
    return "TortureFrame"

// foreign is the image with its MAGIC word, the eight bytes at offset 0,
// reversed: what that word looks like to a reader of the other byte order
// (§19.1), so every leg on every host must refuse it
def foreign(data:string) -> string:
    if length(data) < 8:
        return data
    var out = ""
    var i = 7
    while i >= 0:
        out += substring(data, i, 1)
        i--
    return out + substring(data, 8, length(data) - 8)

// the BLOCK surfaces: Open the image (§19.2), `open` or `refuse`; the same with
// the magic reversed; and THE ROW DUMP (testdata/conformance/tables/FORMAT.md).
// A block line is `block <name> <unit> <file>`.
def surface_block(text:string, swap:bool):
    for(rows(text, "block")) f:
        var data = fixture(f[3])
        if swap:
            data = foreign(data)
        say(f[1], if schema_rt.block_open(block_root(f[1]), data): "open\n" else: "refuse\n")

def surface_block_dump(text:string):
    for(rows(text, "block")) f:
        say(f[1], schema_rt.block_dump(block_root(f[1]), fixture(f[3])))

// the MESSAGE surface (test/conformance/README.md, "`message` IS THE ONE
// SURFACE THAT READS TWO FILES"; docs/SPEC-TABLES.md §3.3): AnnounceRead the
// connection's announcement into one direction's vocabulary, LoadMessages the
// batch against it, SaveMessages, the bytes. A message line is
// `message <name> <connection> <root> <file wire> <message wire>`, and its
// third word is the CONNECTION, joined to its
// `connection <key> <unit> 0x<build> <announcement>` line for the unit. A root
// that is a variable table carries no message verbs (§3.3): a named absence.
def surface_message(text:string):
    let lines = tokenize(text, "\n", " \t\r")
    for(lines) line:
        let f = words(line)
        if length(f) >= 6 and f[0] == "message":
            var announcement = ""
            var unit = ""
            for(lines) other:
                let c = words(other)
                if length(c) >= 5 and c[0] == "connection" and c[1] == f[2]:
                    unit = c[2]
                    announcement = c[4]
            if unit == unit_key:
                if not schema_rt.is_message_root(f[3]):
                    print(f[1] + " absent variable table: the message form is fixed tables only (SPEC-TABLES §3.3)")
                else:
                    let v = schema_rt.new_vocabulary(max(schema_rt.message_entries_here(), 4096))
                    let rep = schema_rt.LoadReport {}
                    assert schema_rt.announce_read(v, fixture(announcement), rep)
                    let recs, count, ok = schema_rt.load_messages(f[3], v, fixture(f[5]), rep, schema_rt.message_batch_max)
                    assert ok and count == length(recs)
                    let out, n = schema_rt.save_messages(recs, schema_rt.LoadReport {})
                    assert n == length(out)
                    say(f[1], out)
        // message-batch <case> <connection> <root> <wire>: load with room for
        // every body and answer the count load_messages returned
        // (docs/SPEC-TABLES.md §3.3; FORMAT.md, `message-batch`)
        if length(f) >= 5 and f[0] == "message-batch":
            var announcement = ""
            var unit = ""
            for(lines) other:
                let c = words(other)
                if length(c) >= 5 and c[0] == "connection" and c[1] == f[2]:
                    unit = c[2]
                    announcement = c[4]
            if unit == unit_key:
                let v = schema_rt.new_vocabulary(max(schema_rt.message_entries_here(), 4096))
                assert schema_rt.announce_read(v, fixture(announcement), schema_rt.LoadReport {})
                let recs, count, ok = schema_rt.load_messages(f[3], v, fixture(f[4]), schema_rt.LoadReport {}, 256)
                assert length(recs) >= 0 and (ok or count == 0)  // both unused otherwise: the compiler warns on stdout, which is not an answer line
                say(f[1], "count " + string(count) + "\n")

// lines are the manifest's lines of one kind, whatever unit they name
def lines(text:string, kind:string) -> [[string]]:
    let out:[[string]] = []
    for(tokenize(text, "\n", " \t\r")) line:
        let f = words(line)
        if length(f) >= 2 and f[0] == kind:
            push(out, f)
    return out

// the COOK surface: Open the cook and write THE CANONICAL NODE DUMP, the walk
// through its own derefs (test/conformance/go/cooks.go). A cook line is
// `cook <case> <unit> <root> <file>`: the case names the answer and the root
// the reader, since one root can have more than one fixture.
def surface_cook(text:string):
    for(rows(text, "cook")) f:
        let dump, why = schema_rt.cook_dump(fixture(f[4]), f[3])
        if why != "":
            print("driver: " + f[1] + ": " + why)
        say(f[1], dump)

// the COOK-FOREIGN surface: the same cooks with the magic reversed, which this
// build's Open must refuse at the magic (§7.1)
def surface_cook_foreign(text:string):
    for(rows(text, "cook")) f:
        let why = schema_rt.cook_open_named(f[3], foreign(fixture(f[4])), 0, false)
        say(f[1], if why == "ok": "open\n" else: "refuse\n")

// Cook input is canonical JSON, or a valid wire for a graph beyond text's
// depth limit. An optional recipe aliases two named blob slots in memory;
// Cook preserves that graph even though Save and JSON cannot spell it (§2.5).
def cook_input(f:[string], recipe:[string]) -> schema_rt.Record?:
    let rep = schema_rt.LoadReport {}
    if recipe[2] == "-":
        return schema_rt.load(f[3], fixture(f[4]), rep)
    let path = recipe[2]
    let rec = schema_rt.json_read(f[3], fixture(path), rep)
    if rec and (recipe[3] != "-" or recipe[4] != "-"):
        var source_slot:schema_rt.Slot? = nil
        var alias_slot:schema_rt.Slot? = nil
        for(rec.fields) sl:
            if sl.fd.s == "bblob" and sl.fd.present == "":
                if sl.fd.name == recipe[3]: source_slot = sl
                if sl.fd.name == recipe[4]: alias_slot = sl
        assert source_slot and alias_slot and source_slot != alias_slot
        let blob = source_slot.cell.blob
        assert blob
        let node = schema_rt.Node { kind: 3, rec: nil, blob: blob }
        source_slot.cell.blob_node = node
        alias_slot.cell.blob = blob
        alias_slot.cell.blob_node = node
    return rec

// COOK-WRITE compares both byte orders to the existing independent goldens.
// A recipe is `cook-write <instance> <input-json-or-dash> <source-or-dash> <alias-or-dash>`.
def surface_cook_write(text:string):
    let insts = lines(text, "instance")
    for(lines(text, "cook-write")) cw:
        assert length(cw) == 5
        for(insts) f:
            if f[1] == cw[1] and f[2] == unit_key:
                let rec = cook_input(f, cw)
                assert rec
                let ct = schema_rt.cook_table_of(f[3])
                assert ct
                let size = schema_rt.cook_measure(rec, ct.build)
                let little = schema_rt.cook_write(rec, ct.build, false)
                let big = schema_rt.cook_write(rec, ct.build, true)
                assert length(little) == size and length(big) == size
                say(f[1], little)
                say(f[1] + "-be", big)

// claim is the buffer a forgery's caller holds (test/conformance/README.md):
// EXACTLY the extent it claims, the file copied in as far as it fits and zero
// past it; -1 is the file's own length
def claim(data:string, extent:int) -> string:
    if extent < 0 or extent == length(data):
        return data
    if extent < length(data):
        return substring(data, 0, extent)
    var pad = ""
    for(extent - length(data)):
        pad += "\x00"
    return data + pad

// block_unit and cook_unit name the unit a forgery's SUBJECT lives in: a
// block forgery's subject is a block line's name, a cook forgery's a cook
// line's root
def subject_unit(text:string, kind:string, subject:string) -> string:
    if kind == "block":
        for(lines(text, "block")) b:
            if b[1] == subject:
                return b[2]
    else:
        for(lines(text, "cook")) c:
            if length(c) >= 4 and c[3] == subject:
                return c[2]
    return ""

// forged opens one forgery line, `forgery <name> <kind> <subject> <file>
// <extent> <pointer>`, and answers the reason beside the refusal, or "ok"
def forged(f:[string]) -> string:
    let data = claim(fixture(f[4]), string_to_int(f[5]))
    let nobuf = f[6] == "null"
    let lead = if nobuf: 0 else: string_to_int(f[6])
    if f[2] == "block":
        return schema_rt.block_open_reason(block_root(f[3]), data, lead, nobuf)
    return schema_rt.cook_open_named(f[3], data, lead, nobuf)

// the FORGERY and COOK-FORGERY surfaces: `open` or `refuse` per forgery of the
// kind; and the two REASON surfaces: the refusal enum's value name per
// `refusal <forgery> <kind>` line, or `ok`
def surface_forgery(text:string, kind:string):
    for(lines(text, "forgery")) f:
        if length(f) >= 7 and f[2] == kind and subject_unit(text, kind, f[3]) == unit_key:
            say(f[1], if forged(f) == "ok": "open\n" else: "refuse\n")

def surface_reason(text:string, kind:string):
    let forgeries = lines(text, "forgery")
    for(lines(text, "refusal")) r:
        if length(r) >= 3 and r[2] == kind:
            for(forgeries) f:
                if f[1] == r[1] and subject_unit(text, kind, f[3]) == unit_key:
                    say(r[1], forged(f) + "\n")

// RETAIN returns the known canonical value, independently of the retention
// store's local byte cost. RETAIN-SAVE compares the retained wire separately.
// A line is `retain <case> <unit> <root> <wire> <capacity> <ids>`;
// `short` is one byte short of what this leg's own roomy load used.
def surface_retain(text:string, saves:bool):
    for(rows(text, "retain")) f:
        let wire = fixture(f[4])
        let ids = if f[6] == "full": 1 << 17 else: string_to_int(f[6])
        var capacity = 1 << 20
        if f[5] == "short":
            let probe = schema_rt.Retain { capacity: capacity, id_capacity: ids }
            schema_rt.load_retain(f[3], wire, probe, schema_rt.LoadReport {})
            capacity = max(0, probe.used - 1)
        let retain = schema_rt.Retain { capacity: capacity, id_capacity: ids }
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load_retain(f[3], wire, retain, rep)
        if not rec:
            say(f[1], "refused\n")
        elif saves:
            let out = schema_rt.save_retain(rec, retain, rep)
            assert not rep.refused
            let again_rep = schema_rt.LoadReport {}
            let again = schema_rt.save_retain(rec, retain, again_rep)
            assert not again_rep.refused and again == out
            say(f[1], out)
        else:
            let js, why = schema_rt.json_write(rec)
            assert why == ""
            say(f[1], "read\n" + js)
// the JSON surfaces (docs/SPEC-TABLES.md §16): an instance line whose sixth
// word is `no-text` has no text and is not asked (§16.7)
def has_text(f:[string]) -> bool:
    return not (length(f) > 5 and f[5] == "no-text")

// JSON-READ: read json/<name>.json, Save, compare the complete bytes.
// json-read and json-hostile use the same json_read entry point.
def surface_json_read(text:string):
    for(rows(text, "instance")) f:
        if has_text(f):
            let rep = schema_rt.LoadReport {}
            let rec = schema_rt.json_read(f[3], fixture("testdata/conformance/tables/json/" + f[1] + ".json"), rep)
            assert rec
            let out, why = schema_rt.save_reasoned(rec)
            assert why == ""
            say(f[1], out)

// JSON-WRITE: Load the wire, ToJson, the text, as <name>.json.
def surface_json_write(text:string):
    for(rows(text, "instance")) f:
        if has_text(f):
            let rep = schema_rt.LoadReport {}
            let rec = schema_rt.load(f[3], fixture(f[4]), rep)
            assert rec
            let out, why = schema_rt.json_write(rec)
            assert why == ""
            say(f[1] + ".json", out)
    // THE VALUES THE WRITER REFUSES (docs/SPEC-TABLES.md §16.14, W0): the
    // wire, then the line's damage put into storage in code, then ToJson,
    // which must refuse. A line is
    // `json-write-refusal <case> <unit> <root> <wire> <damage>`.
    for(rows(text, "json-write-refusal")) f:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load(f[3], fixture(f[4]), rep)
        assert rec
        if f[5] != "none":
            let damage = tokenize(f[5], ":", "")
            var hit = false
            for(rec.fields) sl:
                if sl.fd.name == damage[1]:
                    hit = true
                    assert damage[0] == "narrow"
                    sl.cell.s = "a\xffb"
            assert hit
        let out, why = schema_rt.json_write(rec)
        if why != "":
            say(f[1] + ".json", "refused\n")
        else:
            say(f[1] + ".json", out)

// JSON-HOSTILE: FromJson <tree>/<root>.json, `refused` or `read` followed
// by the complete canonical value. A line is
// `json-hostile <case> <unit> <root> <tree>`.
def surface_json_hostile(text:string):
    for(rows(text, "json-hostile")) f:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.json_read(f[3], fixture(f[4] + "/" + f[3] + ".json"), rep)
        if rec:
            read_answer(f[1], rec)
        else:
            say(f[1], "refused\n")

// THE WRITE-REFUSAL SURFACES (docs/SPEC.md §5.1; test/conformance/README.md):
// each row loads one wire, sets ONE field to the row's value and runs the
// writer its surface names; Lobster refuses in every build, so this answers
// from the ordinary build. A line is
// `write-refusal[-form] <case> <unit> <root> <wire> <path> <kind> <value>`,
// and a message row's <unit> is the CONNECTION, joined to its unit.

// wr_hex is a row's `bytes` value, hex digits two to a byte, as a string
def wr_hex(text:string) -> string:
    var out = schema_rt.zeros(length(text) / 2)
    var i = 0
    while i + 1 < length(text):
        let b, ok = string_to_int(substring(text, i, 2), 16)
        assert ok
        out = write_int8_le(out, i / 2, b)
        i += 2
    return out

// wr_float is a row's `float` value: nan, inf, -inf or a decimal
def wr_float(text:string) -> float:
    if text == "nan":
        return schema_rt.f64frombits(0x7FF8000000000000)
    if text == "inf":
        return schema_rt.f64frombits(0x7FF0000000000000)
    if text == "-inf":
        return schema_rt.f64frombits(0xFFF0000000000000)
    let v, ok = string_to_float(text)
    assert ok
    return v

// wr_int is a row's `int`, `count`, `ordinal` or `tag` value
def wr_int(text:string) -> int:
    let v, ok = string_to_int(text, 10)
    assert ok
    return v

// wr_segment splits one path segment, `name` or `name[i]`, into the name and
// the index, -1 for none
def wr_segment(segment:string) -> string, int:
    let open = find_string(segment, "[")
    if open < 0:
        return segment, -1
    assert substring(segment, length(segment) - 1, 1) == "]"
    return substring(segment, 0, open), wr_int(substring(segment, open + 1, length(segment) - open - 2))

// wr_value is the cell a slot holds at index, -1 for the slot's own cell
def wr_value(sl:schema_rt.Slot, index:int) -> schema_rt.Value:
    if sl.fd.s == "map":
        return sl.entries[index]
    if index >= 0:
        return sl.elems[index]
    return sl.cell

// wr_locate walks Path from the root to the slot its last segment names,
// through by-value tables, elements, map entries and pointer referents, and
// answers that slot and the last segment's index
def wr_locate(root:schema_rt.Record, path:string) -> schema_rt.Slot, int:
    let segments = tokenize(path, ".", "")
    var rec = root
    var i = 0
    while i < length(segments) - 1:
        let name, index = wr_segment(segments[i])
        let v = wr_value(schema_rt.slot_named(rec, name), index)
        let t = if v.tab: v.tab else: v.node
        assert t
        rec = t
        i++
    let name, index = wr_segment(segments[length(segments) - 1])
    return schema_rt.slot_named(rec, name), index

// wr_set sets the one field a row names: Path is dotted field names with
// `[i]` element indexes from the root, and Kind says what the value sets
def wr_set(root:schema_rt.Record, path:string, kind:string, text:string):
    if kind == "point":
        // the pointer at Path names the node the pointer at path Value
        // names: pointed at an enclosing node, a cycle (FL-28)
        let target, ti = wr_locate(root, path)
        let source, si = wr_locate(root, text)
        if target.fd.present != "":
            target.present = true
        wr_value(target, ti).node = wr_value(source, si).node
        return
    let segments = tokenize(path, ".", "")
    var rec = root
    for(segments) segment, i:
        let name, index = wr_segment(segment)
        let sl = schema_rt.slot_named(rec, name)
        if i < length(segments) - 1:
            if sl.fd.s == "union" and index < 0 and i == length(segments) - 2 and sl.cell.tab == nil:
                let arm_sl = sl.cell.arm
                assert arm_sl
                let av = arm_sl.cell
                if kind == "float":
                    av.f = wr_float(text)
                elif kind == "bytes":
                    av.s = wr_hex(text)
                elif kind == "length":
                    var s = ""
                    for(wr_int(text)): s += "a"
                    av.s = s
                elif kind == "int":
                    let n = wr_int(text)
                    av.i = n
                    av.w = schema_rt.Int128 { n, if n < 0: -1 else: 0 }
                else:
                    assert false
                return
            let v = wr_value(sl, index)
            let t = if v.tab: v.tab else: v.node
            assert t
            rec = t
            continue
        if sl.fd.present != "":
            sl.present = true
        var v = if index >= 0: sl.elems[index] else: sl.cell
        if index >= 0 and sl.fd.s == "arr" and sl.fd.ek == 12:
            let tr = v.tab
            assert tr
            v = tr.fields[0].cell
        if kind == "count":
            // new elements are copies of element 0's value; a count of -1
            // is a negative count the slot holds as it is, handed to the
            // writer (SF-28: Lobster storage holds the count as an int)
            let n = wr_int(text)
            sl.count = n
            while length(sl.elems) < n:
                push(sl.elems, if length(sl.elems) > 0: copy(sl.elems[0]) else: schema_rt.element_zero(sl.fd))
        elif kind == "elems":
            // the element count of a fixed [N]T or keyed [E]T array: more
            // are copies of element 0, fewer drop the tail; Lobster holds
            // an array as a list, so any count is held and handed over
            let n = wr_int(text)
            while length(sl.elems) < n:
                push(sl.elems, if length(sl.elems) > 0: copy(sl.elems[0]) else: schema_rt.element_zero(sl.fd))
            if length(sl.elems) > n:
                truncate(sl.elems, n)
            if sl.fd.array == "counted" or sl.fd.array == "list":
                sl.count = n
        elif kind == "mask":
            // a flags value's raw bits, unnamed ones included
            v.i = wr_int(text)
        elif kind == "dupkey":
            // 1 appends a copy of the first entry, its key held twice
            if wr_int(text) == 1:
                assert length(sl.entries) > 0
                push(sl.entries, copy(sl.entries[0]))
        elif kind == "length":
            // the used length, as the text and as the count beside it that
            // the cook writer and the block builder are handed
            let n = wr_int(text)
            var s = ""
            for(n): s += "a"
            v.s = s
            if index < 0:
                sl.count = n
        elif kind == "bytes":
            v.s = wr_hex(text)
        elif kind == "int":
            let n = wr_int(text)
            v.i = n
            v.w = schema_rt.Int128 { n, if n < 0: -1 else: 0 }
        elif kind == "ordinal":
            v.i = wr_int(text)
        elif kind == "tag":
            // the tag, and the arm it names at its zero; a tag past the arm
            // set holds no payload
            let tag = wr_int(text)
            v.i = tag
            v.tab = nil
            v.arm = nil
            var ud = sl.fd
            if index >= 0:
                let e = sl.fd.e
                assert e
                ud = e
            let un = schema_rt.lookup_union(ud.t)
            assert un
            if tag >= 1 and tag <= length(un.arms):
                let arm = un.arms[tag - 1]
                if arm.s == "tab":
                    v.tab = schema_rt.new_record(schema_rt.table_of(arm.t))
                elif arm.s != "void":
                    v.arm = schema_rt.new_slot(arm)
        else:
            assert kind == "float"
            v.f = wr_float(text)

// wr_rows are the rows of one write-refusal surface that this unit answers:
// a message row through its connection's unit. When the driver names one
// case (it reruns a unit case by case after the runtime aborted a run),
// only that row.
def wr_rows(text:string, kind:string) -> [[string]]:
    let out:[[string]] = []
    let connections = lines(text, "connection")
    for(lines(text, kind)) f:
        if length(f) >= 8 and (case_only == "-" or f[1] == case_only):
            var unit = f[2]
            if kind == "write-refusal-message":
                unit = ""
                for(connections) c:
                    if length(c) >= 3 and c[1] == f[2]:
                        unit = c[2]
            if unit == unit_key:
                push(out, f)
    return out

// wr_value_of is a row's starting value: its wire loaded, or for wire `-`
// the root's fresh value (a root with no file form, SF-05, SF-06)
def wr_value_of(f:[string]) -> schema_rt.Record:
    if f[4] == "-":
        return schema_rt.new_record(schema_rt.table_of(f[3]))
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load(f[3], fixture(f[4]), rep)
    assert rec
    return rec

def surface_write_refusal(text:string, kind:string):
    for(wr_rows(text, kind)) f:
        let rec = wr_value_of(f)
        wr_set(rec, f[5], f[6], f[7])
        var answer = "refused\n"
        if kind == "write-refusal-message":
            let out, n = schema_rt.save_messages([rec], schema_rt.LoadReport {})
            if n >= 0:
                assert n == length(out)
                answer = "wrote\n"
        elif kind == "write-refusal-json":
            // THE TEXT WRITER WRITES A ZERO BYTE (docs/SPEC-TABLES.md §16.3)
            // as \u0000: a text that holds one but does not spell it is
            // neither verdict
            let out, why = schema_rt.json_write(rec)
            if why == "":
                answer = "wrote\n"
                if f[6] == "bytes" and find_string(wr_hex(f[7]), "\x00") >= 0 and find_string(out, "\\u0000") < 0:
                    answer = "wrote without \\u0000\n"
        elif kind == "write-refusal-cook":
            // THE COOK WRITER (SPEC-TABLES §7.6): CookMeasure, then Cook in
            // little-endian order; -1 and "" are its refusals
            let ct = schema_rt.cook_table_of(f[3])
            let build = if ct: ct.build else: 0
            let size = schema_rt.cook_measure(rec, build)
            if size >= 0:
                let little = schema_rt.cook_write(rec, build, false)
                if little != "":
                    answer = if length(little) == size: "wrote\n" else: "wrote other than measured\n"
        elif kind == "write-refusal-block":
            // THE BLOCK BUILDER (SPEC-TABLES §19.1), handed the whole value:
            // "" is its refusal
            if schema_rt.block_build(f[3], rec) != "":
                answer = "wrote\n"
        else:
            // the root's own form's writer: a declared fixed root saves
            // through the fixed form's checked writer (save_reasoned)
            let out, why = schema_rt.save_reasoned(rec)
            if why == "":
                assert length(out) > 0
                answer = "wrote\n"
        say(f[1], answer)
        // a `point` row can leave a cycle, which outlives every reference
        // under reference counting: release clears the pointer edges
        schema_rt.release(rec)

// THE ANNOUNCEMENT (test/conformance/README.md, `announce`; SPEC-TABLES §3.3).
// A line is `announce <case> <unit> <root> <mode> <input>`.
def surface_announce(text:string):
    for(rows(text, "announce")) f:
        assert length(f) >= 6
        let mode = f[4]
        if mode == "write":
            // AnnounceMeasure, then Announce
            let n = schema_rt.announce_measure()
            let a = schema_rt.announce()
            if n < 0 or a == "":
                say(f[1], "refused\n")
            elif n != length(a):
                say(f[1], "announced other than measured\n")
            else:
                say(f[1], a)
        elif mode == "strict" or mode == "lenient":
            // a fresh vocabulary of the unit, its strict flag set or left at
            // its default, reads the connection's announcement
            let v = schema_rt.new_vocabulary(max(schema_rt.message_entries_here(), 4096))
            if mode == "strict":
                v.strict = true
            let ok = schema_rt.announce_read(v, fixture(f[5]), schema_rt.LoadReport {})
            say(f[1], if ok: "read\n" else: "refused\n")
        else:
            // SaveMessages of one body with no announcement read or written
            // on the connection (FL-18)
            assert mode == "send"
            var rec = schema_rt.new_record(schema_rt.table_of(f[3]))
            if f[5] != "-":
                let loaded = schema_rt.load(f[3], fixture(f[5]), schema_rt.LoadReport {})
                assert loaded
                rec = loaded
            let out, n = schema_rt.save_messages([rec], schema_rt.LoadReport {})
            if n < 0:
                say(f[1], "refused\n")
            elif n != length(out):
                say(f[1], "wrote other than its length\n")
            else:
                say(f[1], out)

// ri_verdict is one read's line: `read` when the call returned success and
// its report holds no refusal, `refused` when it returned its refusal.
// Lobster has no exceptions: a read cannot throw, so no line is `threw`.
def ri_verdict(ok:bool, reason:string) -> string:
    if not ok:
        return "refused\n"
    if reason != "":
        return "read with a refusal in its report: " + reason + "\n"
    return "read\n"

// ri_text is a value's ToJson text
def ri_text(rec:schema_rt.Record) -> string:
    let js, why = schema_rt.json_write(rec)
    if why != "":
        return "unwritable " + why + "\n"
    return js

// READ INTO THE CALLER'S VALUES (test/conformance/README.md, `read-into`;
// SPEC-TABLES §6.5, §3.4). A line is `read-into <case> <unit> <root> <form>
// <lawful> <damaged> <count> <announcement>`: ONE report and <count> values
// through a read of <lawful>, a read of <damaged>, the ToJson text of every
// value, and a read of <lawful> again.
def surface_read_into(text:string):
    for(rows(text, "read-into")) f:
        assert length(f) >= 9
        let root = f[3]
        let form = f[4]
        let steps = [f[5], f[6], "", f[5]]
        var out = ""
        if form == "fixed-nocap":
            // <Root>FixedLoad takes the caller values, and their count is
            // its capacity: no call omits it (FL-17), so no omitted capacity
            // can be asked of it
            out = "unrepresentable\n"
        elif form == "variable":
            // THE VARIABLE LOAD RETURNS A NEW VALUE: schema_rt.load takes no
            // caller value, so the driver holds the value the last read that
            // succeeded returned, and a refused read returns none (ruling 3)
            let rep = schema_rt.LoadReport {}
            var held = schema_rt.new_record(schema_rt.table_of(root))
            for(steps) step:
                if step == "":
                    out += ri_text(held)
                else:
                    let rec = schema_rt.load(root, fixture(step), rep)
                    if rec:
                        held = rec
                    out += ri_verdict(rec != nil, schema_rt.strict_reason(rep))
        elif form == "json":
            // FromJson into the caller value: json_read_into
            let rep = schema_rt.LoadReport {}
            let held = schema_rt.new_record(schema_rt.table_of(root))
            for(steps) step:
                if step == "":
                    out += ri_text(held)
                else:
                    let ok = schema_rt.json_read_into(held, fixture(step), rep)
                    out += ri_verdict(ok, schema_rt.strict_reason(rep))
        elif form == "message":
            // LoadMessages into the caller batch: load_messages_into, after
            // the connection's announcement is read into the vocabulary;
            // the values written are what the batch holds after the call
            let v = schema_rt.new_vocabulary(max(schema_rt.message_entries_here(), 4096))
            assert schema_rt.announce_read(v, fixture(f[8]), schema_rt.LoadReport {})
            let count = wr_int(f[7])
            let rep = schema_rt.LoadReport {}
            let batch:[schema_rt.Record] = []
            for(count):
                push(batch, schema_rt.new_record(schema_rt.table_of(root)))
            for(steps) step:
                if step == "":
                    for(batch) r:
                        out += ri_text(r)
                else:
                    let n, ok = schema_rt.load_messages_into(root, v, fixture(step), rep, count, batch)
                    out += ri_verdict(ok and n == length(batch), schema_rt.strict_reason(rep))
        else:
            // the fixed batch load, <Root>FixedLoad, through the unit adapter
            assert form == "fixed"
            let b = fixed_batch(root)
            if b:
                b.fresh(wr_int(f[7]))
                let rep = schema_rt.TableReport {}
                for(steps) step:
                    if step == "":
                        for(b.texts()) t:
                            out += t
                    else:
                        let n = b.load_into(fixture(step), rep)
                        out += ri_verdict(n >= 0, rep.reason)
            else:
                out = "no unit adapter serves " + root + " (test/conformance/lobster/units)\n"
        say(f[1], out)

// JSON-WRITE-INTO (test/conformance/README.md; FL-14, FL-15). A line is
// `json-write-into <case> <unit> <root> <wire>`. LOBSTER HAS NO PROCESS
// LOCALE: its builtins carry no setlocale, so SCHEMA_COMMA_LOCALE has nothing
// to be set through, and the row runs as it stands (ruling 1). The text
// writer, schema_rt.json_write, returns a fresh string and takes no caller
// buffer, so the answer is the text it returned, with no tail to check.
def surface_json_write_into(text:string):
    for(rows(text, "json-write-into")) f:
        let rec = schema_rt.load(f[3], fixture(f[4]), schema_rt.LoadReport {})
        assert rec
        let js, why = schema_rt.json_write(rec)
        say(f[1], if why != "": "refused\n" else: js)

if surface == "list":
    for(surfaces) s:
        print(s)
else:
    let text = read_file(manifest_path)
    assert text
    if surface == "wire":
        surface_wire(text)
    elif surface == "report":
        surface_report(text)
    elif surface == "framing":
        surface_framing(text)
    elif surface == "block":
        surface_block(text, false)
    elif surface == "message":
        surface_message(text)
    elif surface == "retain":
        surface_retain(text, false)
    elif surface == "retain-save":
        surface_retain(text, true)
    elif surface == "cook":
        surface_cook(text)
    elif surface == "cook-write":
        surface_cook_write(text)
    elif surface == "cook-foreign":
        surface_cook_foreign(text)
    elif surface == "forgery":
        surface_forgery(text, "block")
    elif surface == "cook-forgery":
        surface_forgery(text, "cook")
    elif surface == "block-reason":
        surface_reason(text, "block")
    elif surface == "cook-reason":
        surface_reason(text, "cook")
    elif surface == "json-read":
        surface_json_read(text)
    elif surface == "json-write":
        surface_json_write(text)
    elif surface == "json-hostile":
        surface_json_hostile(text)
    elif surface == "block-foreign":
        surface_block(text, true)
    elif surface == "announce":
        surface_announce(text)
    elif surface == "read-into":
        surface_read_into(text)
    elif surface == "json-write-into":
        surface_json_write_into(text)
    elif find_string(surface, "write-refusal") == 0:
        surface_write_refusal(text, surface)
    else:
        assert surface == "block-dump"
        surface_block_dump(text)

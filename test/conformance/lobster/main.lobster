// THE LOBSTER CONFORMANCE PROGRAM (test/conformance/README.md;
// docs/PORT-LOBSTER.md, THE HARNESS). The shell driver beside this file runs
// it once per surface and generated unit:
//
//   lobster <unit dir>/conformance_main.lobster -- <repo root> <manifest> <surface> <unit>
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

// the surfaces this leg answers: the variable wire's load and save (§3, §4),
// the message form (§3.3), retain-unknown (§6.6), the cooked form's read,
// write and forgery battery (§7), and the block form's reflective read with
// its forgery battery (§19.2), each refusal with its reason, the JSON
// text form (§16), and the framing vectors (docs/LAYERS/1-framing.md)
let surfaces = ["wire", "message", "report", "framing", "retain", "retain-save", "cook", "cook-write", "cook-foreign",
    "block", "block-foreign", "block-dump", "forgery", "cook-forgery", "cook-reason", "block-reason",
    "json-read", "json-write", "json-hostile"]

let arguments = command_line_arguments()
let argument_count = length(arguments)
assert argument_count >= 4
let repo_root = arguments[argument_count - 4]
let manifest_path = arguments[argument_count - 3]
let surface = arguments[argument_count - 2]
let unit_key = arguments[argument_count - 1]

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
    assert find_string(name, "block_padded") == 0
    return "PaddedFrame"

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
                    let v = schema_rt.new_vocabulary(schema_rt.message_entries_here())
                    let rep = schema_rt.LoadReport {}
                    assert schema_rt.announce_read(v, fixture(announcement), rep)
                    let recs, count, ok = schema_rt.load_messages(f[3], v, fixture(f[5]), rep, schema_rt.message_batch_max)
                    assert ok and count == length(recs)
                    let out, n = schema_rt.save_messages(recs, schema_rt.LoadReport {})
                    assert n == length(out)
                    say(f[1], out)

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
                    if damage[0] == "narrow":
                        sl.cell.s = "a\xffb"
                    else:
                        sl.cell.units = [0x61, 0xd800, 0x62]
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
    else:
        assert surface == "block-dump"
        surface_block_dump(text)

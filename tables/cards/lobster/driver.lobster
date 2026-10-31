// driver.lobster — the Lobster card conformance driver
// (tables/cards/conformance/FORMAT.md, test/conformance/README.md "The shape").
//
// It answers the driver contract over the nine surfaces this leg implements
// (load, save, render, parse, bounds, upgrade, edit, taskhash, view) through
// the facade in cards.lobster. The shell script beside it (driver) stages both
// files into the generated Card unit's directory, where Lobster resolves their
// imports, and runs this program as
//
//   lobster <unit>/cards_driver.lobster -- <repo root> <manifest> <verb> <outdir> <sabotage>
//
// Every path is absolute: a relative read_file resolves against the program's
// own directory, not the working directory. Answers are written one file per
// case into <outdir>. <sabotage> is `none` or the one surface a negative
// control damages (render, upgrade or taskhash): test-only, set by the shell
// driver from CARDS_LOBSTER_SABOTAGE_RENDER, _UPGRADE or _HASH, so the check
// must go red on that surface alone.

import .cards

let SURFACES = ["load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"]

let arguments = command_line_arguments()
let argument_count = length(arguments)
assert argument_count >= 5
let repo_root = arguments[argument_count - 5]
let manifest_path = arguments[argument_count - 4]
let verb = arguments[argument_count - 3]
let outdir = arguments[argument_count - 2]
let sabotage = arguments[argument_count - 1]

// fail stops the run with the message: an answer the leg could not give is a
// failure, never an absence
def fail(message:string):
    print("cards-driver: " + message)
    assert false

def read(path:string) -> string:
    let data = read_file(repo_root + "/" + path)
    if not data:
        fail("cannot read " + path)
    return cards.or_empty(data)

def write(name:string, data:string):
    if not write_file(outdir + "/" + name, data, 0, 1):
        fail("cannot write " + name)

def words(line:string) -> [string]:
    let out:[string] = []
    for(tokenize(line, " ", " \t\r")) w:
        if length(w) > 0:
            push(out, w)
    return out

def manifest_lines() -> [[string]]:
    let text = read_file(manifest_path)
    if not text:
        fail("cannot read the manifest " + manifest_path)
    let out:[[string]] = []
    for(tokenize(cards.or_empty(text), "\n", " \t\r")) line:
        let f = words(line)
        if length(f) > 0 and f[0][0] != '#':
            push(out, f)
    return out

def bool_word(b:bool) -> string:
    return if b: "true" else: "false"

// report_text writes the read report, one Name=value line per counter of the
// Go TableReport in declaration order (FORMAT.md, "The read report").
def report_text(r:schema_rt.LoadReport) -> string:
    return "Retained={r.retained}\nRetainLost={r.retain_lost}\nWidened={r.widened}\nUnknown={r.unknown}\nKindMismatch={r.kind_mismatch}\nClamped={r.clamped}\nDuplicate={r.duplicate}\nMalformed=" + bool_word(r.malformed) + "\n"

def refused(name:string, kind:string):
    write(name + ".refused", kind + "\n")

def split_list(v:string) -> [string]:
    if v == "":
        return []
    return cards.split_commas(v)

// load_or_fail is Load for a surface whose input is a clean card
def load_or_fail(path:string) -> cards.CardRecord:
    let record, err = cards.Load(read(path))
    if err:
        fail(path + ": " + err.message)
    assert record
    return record

// apply_edit applies the one Set* edit a manifest line carries; a refusal is
// the facade's, a malformed line is this driver's own.
def apply_edit(record:cards.CardRecord, edit:[string]) -> cards.CardError?:
    if length(edit) != 1:
        return cards.refuse("an edit line carries exactly one edit", "malformed")
    let eq = find_string(edit[0], "=")
    if eq < 0:
        return cards.refuse("edit " + edit[0] + " has no =", "malformed")
    let key = substring(edit[0], 0, eq)
    let value = substring(edit[0], eq + 1, length(edit[0]) - eq - 1)
    if key == "needs":
        return cards.SetNeeds(record, split_list(value))
    if key == "paths":
        return cards.SetPaths(record, split_list(value))
    if key == "tier":
        return cards.SetTier(record, value)
    if key == "priority":
        return cards.SetPriority(record, value)
    if key == "column":
        return cards.SetColumn(record, value)
    if key == "holder":
        return cards.SetHolder(record, value)
    return cards.refuse("unknown edit " + key, "malformed")

def one(c:[string]):
    let name = c[1]
    let input = c[2]
    if verb == "load":
        let record = load_or_fail(input)
        let text, err = cards.ToJSON(record)
        if err:
            fail(name + ": " + err.message)
        write(name + ".json", cards.or_empty(text))
        write(name + ".report", report_text(record.report))
    elif verb == "save" or verb == "bounds":
        let record, err = cards.FromJSON(read(input))
        if err:
            refused(name, err.kind)
        else:
            assert record
            write(name + ".wire", record.wire)
    elif verb == "render":
        let record = load_or_fail(input)
        let text, err = cards.Render(record)
        if err:
            fail(name + ": " + err.message)
        var out = cards.or_empty(text)
        if sabotage == "render":
            out = "\x00" + substring(out, 1, length(out) - 1)
        write(name + ".txt", out)
    elif verb == "parse":
        let record, err = cards.Parse(read(input))
        if err:
            refused(name, err.kind)
        else:
            assert record
            write(name + ".wire", record.wire)
    elif verb == "upgrade":
        let out, err, report = cards.Upgrade(read(input))
        if err:
            refused(name, err.kind)
        else:
            var wire = cards.or_empty(out)
            if sabotage == "upgrade":
                let flipped = write_int8_le(copy(wire), 0, wire[0] ^ 0xff)
                wire = flipped
            write(name + ".wire", wire)
            write(name + ".report", report_text(report))
    elif verb == "edit":
        let record = load_or_fail(input)
        let rest:[string] = []
        var k = 3
        while k < length(c):
            push(rest, c[k])
            k++
        let err = apply_edit(record, rest)
        if err:
            refused(name, err.kind)
        else:
            write(name + ".wire", record.wire)
    elif verb == "taskhash":
        let record = load_or_fail(input)
        let hex, err = cards.TaskHash(record)
        if err:
            fail(name + ": " + err.message)
        var out = cards.or_empty(hex)
        if sabotage == "taskhash":
            out = repeat_string("0", 64)
        write(name + ".hex", out)
    elif verb == "view":
        write("view.txt", cards.View())

if verb == "list":
    for(SURFACES) s:
        print(s)
else:
    var known = false
    for(SURFACES) s:
        if s == verb:
            known = true
    // an unknown surface is the shell driver's exit 2, checked before this runs
    assert known
    for(manifest_lines()) c:
        if c[0] == verb:
            one(c)

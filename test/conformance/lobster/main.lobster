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

// the surfaces this leg answers: the variable wire's load and save (§3, §4),
// and the block form's reflective read (§19.2) with its cross-endian refusal
let surfaces = ["wire", "report", "block", "block-foreign", "block-dump"]

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

// the WIRE surface: Load a form-1 wire through the TOLERANT read, as the
// reference leg's driver reads (§6.5), and Save it (§3). An instance line is
// `instance <name> <unit> <root> <wire> [no-text]`.
def surface_wire(text:string):
    for(rows(text, "instance")) f:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load_tolerant(f[3], fixture(f[4]), rep)
        assert rec
        say(f[1], schema_rt.save(rec))

// the REPORT surface: the six counters and the verdict (§4), as
// `u,k,w,c,d,m,verdict`. A report line is `report <case> <unit> <root> <wire>`.
def surface_report(text:string):
    for(rows(text, "report")) f:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load_tolerant(f[3], fixture(f[4]), rep)
        // a load answers no record only when it refused the wire
        assert rec or rep.refused
        let verdict = if rep.refused: "refused" else: "read"
        say(f[1], "{rep.unknown},{rep.kind_mismatch},{rep.widened},{rep.clamped},{rep.duplicate},{rep.malformed},{verdict}\n")

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
    elif surface == "block":
        surface_block(text, false)
    elif surface == "block-foreign":
        surface_block(text, true)
    else:
        assert surface == "block-dump"
        surface_block_dump(text)

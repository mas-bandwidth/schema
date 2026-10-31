// THE LOBSTER WIRE-FUZZ LEG (test/conformance/README.md, "The wire-fuzz
// driver"; docs/SPEC-TABLES.md §4.2; docs/PORT-LOBSTER.md, THE HARNESS).
//
// The harness speaks a binary stream on one pipe; this program cannot: the
// pinned console build reads stdin a line at a time (get_line, at most 999
// bytes a call) and its print is block-buffered on a pipe with no flush. So
// the host beside this file (test/conformance/lobster/wirefuzzhost) speaks
// the stream, runs this program once per generated UNIT (Lobster resolves
// imports when it compiles, so one program reaches one unit), and talks to
// it in COMMAND LINES on stdin and REPLY FILES on a named pipe:
//
//   lobster <unit dir>/wirefuzz_main.lobster -- <reply fifo> <mutant file> <arm>
//
//   root <Name>   reply one byte: 1 when this unit has a codec for the root
//                 in this arm, else 0
//   load <Name>   read the mutant file, load it at <Name> through the
//                 tolerant read, save what it decoded, and reply the
//                 harness's reply record for it, byte for byte
//
// Every reply is one write_file to the fifo: the open blocks until the host
// opens it, and the close is the end of the reply, so each reply is flushed
// before the next command is read and a crash is attributed to its mutant.
// An empty command line is the end of the stream.
//
// THE ARM (<arm>): `file` is the harness's ordinary run, and this leg carries
// the FIXED-class file roots in it — a fixed root always loads, and it answers
// the measure -1 (§6.5). A VARIABLE-class root has no region here (Lobster's
// load is a mutable record, not a region a LoadMeasure sizes), so the file arm
// answers it 0 and it rides the `builder` arm instead, which holds a mutable
// file reader to tablewire.Decode with no region preflight (the harness's
// --builder). The message form and retention are not carried: 0.

import .load
import .schema_save
import .strict_and_refusals

let arguments = command_line_arguments()
let argument_count = length(arguments)
assert argument_count >= 3
let reply_path = arguments[argument_count - 3]
let mutant_path = arguments[argument_count - 2]
let arm = arguments[argument_count - 1]
assert arm == "file" or arm == "builder"

def reply(b:string):
    assert write_file(reply_path, b, 0, 1)

// carried is whether this unit has a codec for root in this arm
def carried(name:string) -> bool:
    let d = schema_rt.lookup_table(name)
    if not d:
        return false
    if not d.table:
        return false
    if arm == "builder":
        return d.variable
    return not d.variable

// put32 and put64 write one little-endian field into the reply buffer
def put8(b:string, at:int, v:int) -> string:
    let nb, _ = write_int8_le(b, at, v)
    return nb

def put32(b:string, at:int, v:int) -> string:
    let nb, _ = write_int32_le(b, at, v)
    return nb

def put64(b:string, at:int, v:int) -> string:
    let nb, _ = write_int64_le(b, at, v)
    return nb

// THE REPLY RECORD (test/conformance/README.md): u8 loaded; i32 unknown,
// kind_mismatch, widened, clamped, duplicate; u8 malformed; u8 refused;
// i64 measure; i32 retained, retain_lost; i64 saved, then the saved bytes
def record(loaded:bool, rep:schema_rt.LoadReport, malformed:bool, refused:bool, saved:int, bytes:string) -> string:
    var b = ensure_size("", 47, 0)
    b = put8(b, 0, if loaded: 1 else: 0)
    b = put32(b, 1, rep.unknown)
    b = put32(b, 5, rep.kind_mismatch)
    b = put32(b, 9, rep.widened)
    b = put32(b, 13, rep.clamped)
    b = put32(b, 17, rep.duplicate)
    b = put8(b, 21, if malformed: 1 else: 0)
    b = put8(b, 22, if refused: 1 else: 0)
    b = put64(b, 23, -1)
    b = put32(b, 31, 0)
    b = put32(b, 35, 0)
    b = put64(b, 39, saved)
    if saved > 0:
        return b + bytes
    return b

// save_answer is the saved length and bytes: -2 for the blob_shared refusal
// every writer makes by name (§2.5), -1 for any other refusal
def save_answer(rec:schema_rt.Record) -> int, string:
    let out, why = schema_rt.save_reasoned(rec)
    if why == "blob_shared":
        return -2, ""
    if why != "":
        return -1, ""
    return length(out), out

def load_one(name:string):
    var data = read_file(mutant_path)
    if not data:
        data = ""
    let rep = schema_rt.LoadReport {}
    let got = schema_rt.load_tolerant(name, data, rep)
    // A FORM BYTE THIS READER DOES NOT CARRY is the one refusal the verdict
    // byte names (§3); a collection count past the storage cap stops the read
    // without refusing the file (the builder's CountRefusal).
    let form_refused = length(data) >= 1 and data[0] != 1
    if arm == "builder":
        // THE BUILDER ARM: loaded is whether the read finished its root body;
        // a stopped read saves nothing
        let stopped = rep.stopped
        if stopped or not got:
            schema_rt.release(got)
            reply(record(false, rep, rep.malformed, form_refused, -1, ""))
            return
        let n, out = save_answer(got)
        // the leg owns the graph it loaded and ends it (RELEASE)
        schema_rt.release(got)
        reply(record(true, rep, rep.malformed, false, n, out))
        return
    // THE FILE ARM, a fixed root: it always loads. A refused or empty wire
    // decodes nothing, so what is saved is the value read into, at its
    // declared defaults.
    var rec = got
    if not rec:
        rec = schema_rt.new_record(schema_rt.table_of(name))
    let n, out = save_answer(rec)
    schema_rt.release(rec)
    // a read that stopped short is damage, except a refusal, which is the
    // whole answer: nothing decoded, no counter moved (§3)
    reply(record(true, rep, rep.malformed or (rep.stopped and not form_refused), form_refused, n, out))

while true:
    let line = get_line("")
    if line == "":
        break
    let cmd = tokenize(line, " ", " \t\r")
    assert length(cmd) == 2
    if cmd[0] == "root":
        reply(if carried(cmd[1]): "\x01" else: "\x00")
    else:
        assert cmd[0] == "load"
        load_one(cmd[1])

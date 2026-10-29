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
//   root <Name> <form> <retain>   reply one byte: 1 when this unit has a
//                 codec for the roster entry in this arm, else 0
//   load <Name> <form> <retain>   read the mutant file, load it at <Name>
//                 through that entry's reader, save what it decoded, and
//                 reply the harness's reply record for it, byte for byte
//
// Every reply is one write_file to the fifo: the open blocks until the host
// opens it, and the close is the end of the reply, so each reply is flushed
// before the next command is read and a crash is attributed to its mutant.
// An empty command line is the end of the stream.
//
// THE ARM (<arm>) AND THE ENTRY: `file` is the harness's ordinary run and its
// --retain and --message runs; `builder` is its --builder run.
//
//   file, form 1      every table root. A fixed root always loads and answers
//                     the measure -1; a variable root answers load_measure
//                     (region_measure.lobster, Go's FileRegionMeasure), and a
//                     measure that refuses the framing loads nothing (§6.5).
//   file, retain 1    the variable roots, through load_retain and save_retain
//                     with both capacities large (§6.6), the measure as above
//                     and the two retention counters beside the six.
//   file, form 2      the fixed message roots, through load_messages_tolerant
//                     (load_messages_into, so a body the stream stops inside
//                     keeps what it decoded, as the engine's instance does)
//                     against the unit's own vocabulary with room for the
//                     wire's whole batch, body one saved through save_messages
//                     (§3.3). A variable message root answers 0: its batch
//                     region (the harness's sizeBatch) is not measured here.
//   builder, form 1   the variable roots, a mutable file reader held to
//                     tablewire.Decode with no region preflight.

import .load
import .schema_save
import .strict_and_refusals
import .region_measure
import .retain_unknown
import .message

let arguments = command_line_arguments()
let argument_count = length(arguments)
assert argument_count >= 3
let reply_path = arguments[argument_count - 3]
let mutant_path = arguments[argument_count - 2]
let arm = arguments[argument_count - 1]
assert arm == "file" or arm == "builder"

def reply(b:string):
    assert write_file(reply_path, b, 0, 1)

// carried is whether this unit has a codec for the roster entry in this arm
def carried(name:string, form:int, retain:int) -> bool:
    let d = schema_rt.lookup_table(name)
    if not d:
        return false
    if not d.table:
        return false
    if arm == "builder":
        return form == 1 and retain == 0 and d.variable
    if form == 2:
        return retain == 0 and not d.variable and schema_rt.is_message_root(name)
    if retain == 1:
        return d.variable
    return true

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
    return record_measured(loaded, rep, malformed, refused, -1, false, saved, bytes)

def record_measured(loaded:bool, rep:schema_rt.LoadReport, malformed:bool, refused:bool, measure:int, retain:bool, saved:int, bytes:string) -> string:
    var b = ensure_size("", 47, 0)
    b = put8(b, 0, if loaded: 1 else: 0)
    b = put32(b, 1, rep.unknown)
    b = put32(b, 5, rep.kind_mismatch)
    b = put32(b, 9, rep.widened)
    b = put32(b, 13, rep.clamped)
    b = put32(b, 17, rep.duplicate)
    b = put8(b, 21, if malformed: 1 else: 0)
    // A NaN OR AN INFINITY REFUSES THE READ (docs/SPEC-TABLES.md §3.3): the
    // refusal verdict says so, whatever else the reply carries
    b = put8(b, 22, if refused or rep.non_finite_float != "": 1 else: 0)
    b = put64(b, 23, measure)
    b = put32(b, 31, if retain: rep.retained else: 0)
    b = put32(b, 35, if retain: rep.retain_lost else: 0)
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

def load_one(name:string, form:int, retain:int):
    var data = read_file(mutant_path)
    if not data:
        data = ""
    if arm == "file" and form == 2:
        message_one(name, data)
        return
    if arm == "file" and schema_rt.table_of(name).variable:
        variable_one(name, data, retain == 1)
        return
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

// variable_one is a variable root in the region arms: the measure first,
// then the load, the plain one or the retaining one
def variable_one(name:string, data:string, retain:bool):
    let measure, _whole, refused = schema_rt.load_measure(name, data)
    if refused:
        // LOADMEASURE REFUSED THE FRAMING: no region, no root, no counter
        reply(record_measured(false, schema_rt.LoadReport {}, false, false, -1, retain, -1, ""))
        return
    let rep = schema_rt.LoadReport {}
    let form_refused = length(data) >= 1 and data[0] != 1
    if retain:
        let store = schema_rt.Retain { capacity: 1 << 20, id_capacity: 1 << 17 }
        let rec = schema_rt.load_retain(name, data, store, rep)
        let _probe, why = schema_rt.save_reasoned(rec)
        var n = -1
        var out = ""
        if why == "blob_shared":
            n = -2
        elif why == "":
            out = schema_rt.save_retain(rec, store, rep)
            n = if length(out) > 0: length(out) else: -1
        schema_rt.release(rec)
        reply(record_measured(true, rep, rep.malformed or (rep.stopped and not form_refused), form_refused, measure, true, n, out))
        return
    var rec = schema_rt.load_tolerant(name, data, rep)
    if not rec:
        rec = schema_rt.new_record(schema_rt.table_of(name))
    let n, out = save_answer(rec)
    schema_rt.release(rec)
    reply(record_measured(true, rep, rep.malformed or (rep.stopped and not form_refused), form_refused, measure, false, n, out))

// message_one is a fixed message root (§3.3): the batch read against the
// unit's own vocabulary, body one saved as a batch of one
def message_one(name:string, data:string):
    let rep = schema_rt.LoadReport {}
    let bodies:[schema_rt.Record] = []
    let _count, ok = schema_rt.load_messages_into(name, schema_rt.own_vocabulary(), data, rep, 256, bodies)
    var body = schema_rt.new_record(schema_rt.table_of(name))
    if length(bodies) > 0:
        body = bodies[0]
    let saved_rep = schema_rt.LoadReport {}
    let out, n = schema_rt.save_messages([body], saved_rep)
    for(bodies) b:
        schema_rt.release(b)
    let malformed = rep.malformed or (not ok and not rep.refused)
    reply(record(true, rep, malformed and not rep.refused, rep.refused, n, out))

while true:
    let line = get_line("")
    if line == "":
        break
    let cmd = tokenize(line, " ", " \t\r")
    assert length(cmd) == 4
    let form, _f = string_to_int(cmd[2])
    let retain, _r = string_to_int(cmd[3])
    if cmd[0] == "root":
        reply(if carried(cmd[1], form, retain): "\x01" else: "\x00")
    else:
        assert cmd[0] == "load"
        load_one(cmd[1], form, retain)

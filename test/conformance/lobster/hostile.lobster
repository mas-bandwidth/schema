// THE LOBSTER PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md;
// make/lobster-hostile.mk). One program per corpus schema: the runner
// (test/conformance/lobster/hostile) generates the schema's unit, stages this
// file in it as hostile_main.lobster after one `import .<Base>Table` per
// table module, and runs it once:
//
//   lobster hostile_main.lobster -- <corpus dir> <schema> <answers file>
//
// For every manifest line naming this schema it hands the raw input to the
// port's own reader and writes one answer line:
//
//   <name> accept <hex>               the reader accepted; the hex is the
//                                     decoded value's canonical text, written by
//                                     the port's own JSON writer (an
//                                     announcement accepts with no value: "-")
//   <name> refuse <reason>            the reader refused, by the engine's name
//   <name> unsaved <why>              accepted, but the writer refused the value
//
// The runner holds each answer to the committed engine verdict: a refusal by
// its reason, an accept by the SHA-256 of the canonical text, whose one shape
// difference from the engine's (internal/tabletext.Model.Write) is the trailing
// newline the port writes and the runner drops. Reads files only: no network,
// no clock.
//
// A carrier maps to the port's reader, as the engine's verdict does
// (internal/ci/hostilecorpus EngineVerdict):
//   wire              schema_rt.load_tolerant: a refusal by the form byte, the
//                     int32 count cap or a non-finite float; a read that stops
//                     short of its root terminator is `damaged`
//   text              schema_rt.json_read (the tolerant JSON text form)
//   message           the unit's own announcement read into a vocabulary, then
//                     load_messages_tolerant at capacity 1
//   message_novocab   load_messages_tolerant on a vocabulary never announced
//   announce          announce_read on a fresh vocabulary
//   announce_second   announce_read twice on one vocabulary
//   announce_bytes    announce_read with max_bytes 8
//   announce_entries  announce_read at capacity 1

import .load
import .schema_save
import .json
import .message

let arguments = command_line_arguments()
let argument_count = length(arguments)
assert argument_count >= 3
let corpus = arguments[argument_count - 3]
let schema = arguments[argument_count - 2]
let answers_path = arguments[argument_count - 1]

def hexs(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)

def saved(name:string, rec:schema_rt.Record) -> string:
    let text, why = schema_rt.json_write(rec)
    if why != "":
        return name + " unsaved " + why + "\n"
    return name + " accept " + hexs(text) + "\n"

// refused names a refusal by its reason: the message form's own name, or
// `damaged` for a read that reports damage without one
def refused(name:string, rep:schema_rt.LoadReport) -> string:
    if rep.refused and rep.reason != "":
        return name + " refuse " + rep.reason + "\n"
    return name + " refuse damaged\n"

def text_answer(name:string, table:string, data:string) -> string:
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.json_read(table, data, rep)
    if not rec:
        return name + " refuse damaged\n"
    let answer = saved(name, rec)
    schema_rt.release(rec)
    return answer

def message_answer(name:string, table:string, data:string, announced:bool) -> string:
    let rep = schema_rt.LoadReport {}
    var v = schema_rt.new_vocabulary(1 << 20)
    if announced:
        v = schema_rt.own_vocabulary()
    let out, n, ok = schema_rt.load_messages_tolerant(table, v, data, rep, 1)
    var answer = name + " refuse damaged\n"
    if rep.refused:
        answer = refused(name, rep)
    elif ok and n == 1 and length(out) == 1:
        answer = saved(name, out[0])
    for(out) r:
        schema_rt.release(r)
    return answer

def announce_answer(name:string, carrier:string, data:string) -> string:
    let rep = schema_rt.LoadReport {}
    var v = schema_rt.new_vocabulary(1 << 20)
    if carrier == "announce_entries":
        v = schema_rt.new_vocabulary(1)
    if carrier == "announce_bytes":
        v.max_bytes = 8
    if carrier == "announce_second":
        if not schema_rt.announce_read(v, data, rep):
            return refused(name, rep)
    if not schema_rt.announce_read(v, data, rep):
        return refused(name, rep)
    return name + " accept -\n"

def wire_answer(name:string, table:string, data:string) -> string:
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_tolerant(table, data, rep)
    if rep.refused:
        if length(data) >= 1 and data[0] == 2:
            return name + " refuse message_form_as_file\n"
        if length(data) >= 1 and data[0] != 1:
            return name + " refuse newer_form\n"
        return name + " refuse count_refusal\n"
    if rep.non_finite_float != "":
        // a NaN or infinity on the wire refuses the read by name (SPEC §4.3)
        return name + " refuse non_finite_float\n"
    if rep.stopped or not rec:
        schema_rt.release(rec)
        return name + " refuse damaged\n"
    let answer = saved(name, rec)
    schema_rt.release(rec)
    return answer

let manifest = read_file(corpus + "/manifest.tsv")
assert manifest
var out = ""
for(tokenize(manifest, "\n", "\r")) line:
    if length(line) == 0 or line[0] == '#':
        continue
    let f = tokenize(line, "\t", "")
    if length(f) < 9 or f[2] != schema:
        continue
    let name = f[0]
    let carrier = f[1]
    let table = f[3]
    var data = read_file(corpus + "/" + f[4])
    if not data:
        data = ""
    if carrier == "wire":
        out += wire_answer(name, table, data)
    elif carrier == "text":
        out += text_answer(name, table, data)
    elif carrier == "message" or carrier == "message_novocab":
        out += message_answer(name, table, data, carrier == "message")
    else:
        out += announce_answer(name, carrier, data)
assert write_file(answers_path, out, 0, 1)

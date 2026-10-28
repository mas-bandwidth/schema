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
//   <name> accept <saved wire hex>    the reader accepted; the value it decoded,
//                                     saved through the port's own writer
//   <name> refuse <reason>            the reader refused, by the engine's name
//   <name> unsaved <why>              accepted, but the writer refused the value
//   <name> absent <carrier>           a carrier this port has no reader for
//
// The runner holds each answer to the committed engine verdict: a refusal by
// its reason, an accept by the digest of the value's canonical text. Lobster
// has no text writer yet, so the runner reads the saved wire back through the
// engine and writes that text; the value compared is this reader's, carried by
// this writer, whose bytes the wire-fuzz differential holds to the engine's.
// Reads files only: no network, no clock.
//
// A carrier maps to the port's reader:
//   wire   schema_rt.load_tolerant (a refusal by the form byte, the int32
//          count cap or a non-finite float; a read that stops short of its
//          root terminator is `damaged`)
// The text carrier is absent: the corpus's text is the tolerant JSON text
// form (internal/tabletext Model.Read), whose Lobster reader is the JSON form
// card's (schema-lobster-json-form), not yet on this base; schema_rt.text_load
// is the strict hand-editable form (§16.9) and is not that reader. The message
// and announcement carriers are absent: the Lobster message form has not
// landed.

import .load
import .schema_save

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
    let out, why = schema_rt.save_reasoned(rec)
    if why != "":
        return name + " unsaved " + why + "\n"
    return name + " accept " + hexs(out) + "\n"

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
        return name + " refuse damaged\n"
    return saved(name, rec)

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
    if carrier != "wire":
        out += name + " absent " + carrier + "\n"
        continue
    var data = read_file(corpus + "/" + f[4])
    if not data:
        data = ""
    out += wire_answer(name, table, data)
assert write_file(answers_path, out, 0, 1)

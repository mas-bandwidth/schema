// THE GOOSE PORT RUNS THE SHARED HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md;
// make/goose-hostile-corpus.mk). One run per generated unit, as the
// conformance driver runs (every unit carries its own copy of the runtime):
//
//   runner.goose <corpus dir> <schema path>
//
// For every manifest case of that schema whose CARRIER this port reads, the
// generated strict read is handed the raw input and the runner prints one
// line: `<name> accept <saved wire hex> <text hex>` or `<name> refuse
// <reason>`, the text the value's canonical text (§16.1) without its last
// newline, `-` for a root with no text form. The
// reason is read off the engine's own report, never off the input:
// `count_refusal`, `non_finite_float`, a form refusal by the name the load's
// report gives it (`message_form_as_file`, `newer_form`), and `damaged` for
// every other read the strict read refuses (schema_rt::load_error: damage, a
// value out of range, a kind mismatch, a text list past its count or total;
// docs/plans/strict-only.md). A FIXED root's file form (form byte 3,
// docs/SPEC-TABLES.md §3.4) goes through that root's own <Root>FixedLoad, picked
// by the form byte (conformance_fixed, glued by make/goose-hostile.sh), as the
// C++ runner does; any other form byte goes through the variable read, which
// names it. A `message` case reads the unit's own announcement (the engine's,
// written beside the unit by make/goose-hostile.sh) into a fresh vocabulary,
// then the batch at capacity 1 through the root's <Root>LoadMessages
// (conformance_message): the message form's own refusals by name
// (newer_form, no_vocabulary, second_announcement, vocabulary_too_large,
// message_form_as_file, batch_too_large, as the engine's MessageReason names
// them), and `damaged` for every other, a bound's included: conformance
// compares the verdict, never a bound's own name (docs/rulings.json
// refusal-verdict-only). A case whose carrier the port
// does not read yet is `<name> absent <carrier>`. The make target holds the
// lines to the manifest.
import os;
import .conformance_unit;
import .wire_load;
import .wire_save;
import .json_value;
import .message;

fn hex(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    for x in b {
        out.push(digits[(x >> 4) as! i64]);
        out.push(digits[(x & 15) as! i64]);
    }
}

fn main() {
    let argv = args();
    if argv.len != 4 {
        write_stderr("usage: runner.goose <corpus dir> <schema path> <announcement>\n");
        exit(1);
    }
    let unit = conformance_unit();
    var text: u8[>..] = [];
    if !read_file(str(argv[1], "/manifest.tsv"), text) {
        write_stderr("runner: cannot read the corpus manifest\n");
        exit(1);
    }
    var announce: u8[>..] = [];
    if !read_file(argv[3], announce) {
        write_stderr("runner: cannot read the unit's announcement\n");
        exit(1);
    }
    var out: u8[>..] = [];
    for line in split(text, '\n') {
        if line.len == 0 || line[0] == '#' { continue; }
        let f = split(line, '\t');
        if f.len < 9 || f[2] != argv[2] { continue; }
        out.append(f[0]);
        if f[1] != "wire" && f[1] != "message" {
            out.append(" absent ");
            out.append(f[1]);
            out.push('\n');
            continue;
        }
        var data: u8[>..] = [];
        if !read_file(str(argv[1], "/", f[4]), data) {
            write_stderr(str("runner: cannot read ", f[4], "\n"));
            exit(1);
        }
        if f[1] == "message" {
            var msaved: u8[>..] = [];
            var mtext: u8[>..] = [];
            let mr = conformance_message(f[3], announce, data, msaved, mtext);
            if mr == -1 {
                out.append(" absent message-root\n");
                continue;
            }
            if mr == -2 {
                write_stderr(str("runner: the unit's own announcement was refused at ", f[0], "\n"));
                exit(1);
            }
            if mr == 0 {
                out.append(" accept ");
                hex(out, msaved);
                if mtext.len > 0 {
                    out.push(' ');
                    hex(out, mtext[0..mtext.len - 1]);
                } else {
                    out.append(" -");
                }
            } else if mr == schema_rt::MsgRefuseNewerForm || mr == schema_rt::MsgRefuseNoVocabulary || mr == schema_rt::MsgRefuseSecondAnnouncement || mr == schema_rt::MsgRefuseVocabularyTooLarge || mr == schema_rt::MsgRefuseMessageFormAsFile || mr == schema_rt::MsgRefuseBatchTooLarge {
                out.append(" refuse ");
                out.append(schema_rt::msg_reason_name(mr));
            } else {
                out.append(" refuse damaged");
            }
            out.push('\n');
            continue;
        }
        if data.len > 0 && data[0] == 3 {
            var fsaved: u8[>..] = [];
            var ftext: u8[>..] = [];
            let fr = conformance_fixed(f[3], data, fsaved, ftext);
            if fr >= 0 {
                if fr == 0 {
                    out.append(" accept ");
                    hex(out, fsaved);
                    if ftext.len > 0 {
                        out.push(' ');
                        hex(out, ftext[0..ftext.len - 1]);
                    } else {
                        out.append(" -");
                    }
                } else if fr == schema_rt::FixedRefuseNewerForm {
                    out.append(" refuse newer_form");
                } else if fr == schema_rt::FixedRefuseMessageFormAsFile {
                    out.append(" refuse message_form_as_file");
                } else if fr == 1000 {
                    // the fixed writer refused the value the load accepted:
                    // an error, never a refusal (a refusal conforms on its
                    // verdict alone, docs/rulings.json refusal-verdict-only)
                    out.append(" error fixed_save_refused");
                } else {
                    out.append(" refuse damaged");
                }
                out.push('\n');
                continue;
            }
        }
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(unit, f[3], data, vals, nodes, report);
        if report.non_finite {
            out.append(" refuse non_finite_float\n");
        } else if report.count_refused {
            out.append(" refuse count_refusal\n");
        } else if report.reason == schema_rt::LoadRefuseNewerForm || report.reason == schema_rt::LoadRefuseMessageFormAsFile {
            out.append(" refuse ");
            out.append(schema_rt::load_refusal_name(report.reason));
            out.push('\n');
        } else if report.refused || schema_rt::load_error(report, root) != schema_rt::LoadRefuseOk {
            out.append(" refuse damaged\n");
        } else {
            var saved: u8[>..<] = [];
            let refusal = schema_rt::save_table(unit, f[3], data, vals, nodes, root, saved);
            if refusal != 0 {
                // the writer refused the value the load accepted: an error,
                // never a refusal (refusal-verdict-only)
                out.append(" error save_refused:");
                out.append(schema_rt::save_refusal_name(refusal));
                out.push('\n');
            } else {
                out.append(" accept ");
                hex(out, saved);
                var canonical: u8[>..] = [];
                let n = schema_rt::json_save_table(unit, conformance_text(), f[3], data, vals, nodes, root, canonical);
                if n > 0 {
                    out.push(' ');
                    hex(out, canonical[0..n - 1]);
                } else {
                    out.append(" -");
                }
                out.push('\n');
            }
        }
    }
    write_stdout(out);
}

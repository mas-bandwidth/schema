// THE GOOSE PORT RUNS THE SHARED HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md;
// make/goose-hostile-corpus.mk). One run per generated unit, as the
// conformance driver runs (every unit carries its own copy of the runtime):
//
//   runner.goose <corpus dir> <schema path>
//
// For every manifest case of that schema whose CARRIER this port reads, the
// generated tolerant read is handed the raw input and the runner prints one
// line: `<name> accept <saved wire hex>` or `<name> refuse <reason>`. The
// reason is read off the engine's own report, never off the input: `damaged`
// for a malformed read the engine did not refuse, `count_refusal`,
// `non_finite_float`, and `refused` for a form refusal, which Goose's load
// does not name further. A case whose carrier the port does not read yet is
// `<name> absent <carrier>`. The make target holds the lines to the manifest.
import os;
import .conformance_unit;
import .wire_load;
import .wire_save;

fn hex(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    for x in b {
        out.push(digits[(x >> 4) as! i64]);
        out.push(digits[(x & 15) as! i64]);
    }
}

fn main() {
    let argv = args();
    if argv.len != 3 {
        write_stderr("usage: runner.goose <corpus dir> <schema path>\n");
        exit(1);
    }
    let unit = conformance_unit();
    var text: u8[>..] = [];
    if !read_file(str(argv[1], "/manifest.tsv"), text) {
        write_stderr("runner: cannot read the corpus manifest\n");
        exit(1);
    }
    var out: u8[>..] = [];
    for line in split(text, '\n') {
        if line.len == 0 || line[0] == '#' { continue; }
        let f = split(line, '\t');
        if f.len < 9 || f[2] != argv[2] { continue; }
        out.append(f[0]);
        if f[1] != "wire" {
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
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(unit, f[3], data, vals, nodes, report);
        if report.non_finite {
            out.append(" refuse non_finite_float\n");
        } else if report.count_refused {
            out.append(" refuse count_refusal\n");
        } else if report.refused {
            out.append(" refuse refused\n");
        } else if root < 0 || report.malformed || report.stopped {
            out.append(" refuse damaged\n");
        } else {
            var saved: u8[>..<] = [];
            let refusal = schema_rt::save_table(unit, f[3], data, vals, nodes, root, saved);
            if refusal != 0 {
                out.append(" refuse ");
                out.append(schema_rt::save_refusal_name(refusal));
                out.push('\n');
            } else {
                out.append(" accept ");
                hex(out, saved);
                out.push('\n');
            }
        }
    }
    write_stdout(out);
}

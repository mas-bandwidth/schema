// THE GOOSE CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-GOOSE.md,
// THE HARNESS). The shell driver beside this file runs it under the pinned
// compiler (`goose run main.goose -- <command> <manifest> [<unit>]`):
//
//   main.goose list <manifest>
//   main.goose <surface> <manifest> <unit>
//
// `list` prints the surfaces this leg answers, one per line. A surface run
// answers the cases of ONE unit: Goose compiles whole programs and every unit
// is one namespace with its own copy of the runtime, so the shell driver
// copies this file and the unit's glue (units/<unit>.goose, as unit.goose)
// beside that unit's generated modules (build/conformance-goose/<unit>) and
// runs it once per unit; a case no unit answers is written ABSENT by the
// shell driver. It prints one line per case on stdout: the case name, a space,
// and the answer's bytes as lowercase hex, or the word `absent`. A surface
// this leg does not answer exits 2, which the harness prints as ABSENT. The
// working directory is the repository root, so every path in the manifest is
// used as it is written.
//
// THE LEG ANSWERS json-hostile: the text form's hostile battery
// (docs/SPEC-TABLES.md §16.2, §16.3), FromJsonTolerant of <tree>/<root>.json
// and its report (internal/codegen/goosetable/json_form.go,
// runtime/schema_json.goose). The other surfaces read a form the port does
// not carry yet: the id-table wire, form 1 (wire, report, and json-read and
// json-write, whose other half is that wire: schema-goose-table-wire-load and
// -save; retain, retain-save: those and schema-goose-vt-retain-unknown), the
// message form (message: schema-goose-message-form), the cook (cook,
// cook-write, cook-foreign, cook-forgery, cook-reason: schema-goose-cook), the
// block form (block, block-foreign, block-dump, block-reason:
// schema-goose-block-form) and the forged images of both (forgery). The fixed
// form, form 3, is judged against the C++ reference's byte oracle by `make
// test-goose` (make/goose.mk).
import os;
import .unit;

fn hex_line(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    var i: i64 = 0;
    while i < b.len {
        out.push(digits[(b[i] >> 4) as i64]);
        out.push(digits[(b[i] & 15) as i64]);
        i += 1;
    }
}

// field is the n-th space-separated word of a manifest line.
fn field(line: const u8[:], n: i64) -> const u8[:] {
    var i: i64 = 0;
    var k: i64 = 0;
    while i < line.len {
        while i < line.len && (line[i] == ' ' || line[i] == '\t') { i += 1; }
        let start = i;
        while i < line.len && line[i] != ' ' && line[i] != '\t' { i += 1; }
        if start < i {
            if k == n { return line[start..i]; }
            k += 1;
        }
    }
    return line[0..0];
}

// json_hostile answers every json-hostile row of the unit: the verdict of
// FromJsonTolerant over <tree>/<root>.json.
fn json_hostile(manifest: const u8[:], unit_key: const u8[:], out: u8[>..]&) {
    var start: i64 = 0;
    var i: i64 = 0;
    while i <= manifest.len {
        if i == manifest.len || manifest[i] == '\n' {
            let line = manifest[start..i];
            if field(line, 0) == "json-hostile" && field(line, 2) == unit_key {
                let name = field(line, 1);
                var path: u8[>..] = [];
                path.append(field(line, 4));
                path.push('/');
                path.append(field(line, 3));
                path.append(".json");
                var text: u8[>..] = [];
                var answer: u8[>..] = [];
                out.append(name);
                if read_file(path, text) && unit_json_verdict(field(line, 3), text, answer) {
                    out.push(' ');
                    hex_line(out, answer);
                } else {
                    out.append(" absent");
                }
                out.push('\n');
            }
            start = i + 1;
        }
        i += 1;
    }
}

fn main() {
    let argv = args();
    if argv.len < 3 {
        write_stderr("usage: main.goose list|<surface> <manifest> [<unit>]\n");
        exit(1);
    }
    if argv[1] == "list" {
        write_stdout("json-hostile\n");
        return;
    }
    if argv.len != 4 {
        write_stderr("usage: main.goose <surface> <manifest> <unit>\n");
        exit(1);
    }
    var manifest: u8[>..] = [];
    if !read_file(argv[2], manifest) {
        write_stderr("main.goose: cannot read the manifest\n");
        exit(1);
    }
    var out: u8[>..] = [];
    if argv[1] == "json-hostile" {
        json_hostile(manifest, argv[3], out);
        write_stdout(out);
        return;
    }
    // a surface this leg does not answer: ABSENT, never a guessed answer
    exit(2);
}

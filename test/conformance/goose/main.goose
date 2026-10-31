// THE GOOSE CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-GOOSE.md,
// THE HARNESS). The shell driver beside this file runs it once per call,
// under the pinned compiler (`goose run main.goose -- <command> <manifest>`):
//
//   main.goose list <manifest>
//   main.goose <surface> <manifest>
//
// `list` prints the surfaces this leg answers, one per line. A surface run
// prints one line per case on stdout: the case name, a space, and the
// answer's bytes as lowercase hex, or the word `absent` (the shell driver
// writes `<case>.absent` for it). A surface this leg does not answer exits 2,
// which the harness prints as ABSENT. The working directory is the repository
// root, so every path in the manifest is used as it is written.
//
// THE LEG ANSWERS NO SURFACE YET, and says so rather than fake one. Every
// surface the harness runs (test/conformance/harness/run.go) reads a form the
// Goose port does not carry: the id-table wire, form 1 (wire, report:
// schema-goose-table-wire-load and -save; retain, retain-save: those and
// schema-goose-vt-retain-unknown), the message form, form 2 (message:
// schema-goose-message-form), the JSON text form (json-read, json-write,
// json-hostile: schema-goose-json-form), the cook (cook, cook-write,
// cook-foreign, cook-forgery, cook-reason: schema-goose-cook), the block form
// (block, block-foreign, block-dump, block-reason: schema-goose-block-form)
// and the forged images of both (forgery). The one codec the port carries is the fixed form,
// form 3, and no harness surface reads form 3: its judge is the C++
// reference's byte oracle, build/fixedform-corpus, which `make test-goose`
// holds every fixed root to (make/goose.mk; internal/codegen/goosetable,
// TestGooseFixedFormAgreesWithTheByteOracle). The carries registration agrees:
// compiler/carries_goose.go claims no construct.
//
// A card that carries a surface adds its name to `list` and its arm below,
// reading the manifest through os.read_file (docs/PORT-GOOSE.md, THE CARD MAP).
import os;

fn main() {
    let argv = args();
    if argv.len != 3 {
        write_stderr("usage: main.goose list|<surface> <manifest>\n");
        exit(1);
    }
    if argv[1] == "list" {
        // no surface is answered: the list is empty, and the harness never
        // runs a surface the list does not name
        return;
    }
    // a surface this leg does not answer: ABSENT, never a guessed answer
    exit(2);
}

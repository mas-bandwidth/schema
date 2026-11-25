// accel.goose — THE GOOSE LEG'S ACCELERATOR PROGRAM (accel_lib.goose holds
// its surfaces and their description; test/conformance/README.md). The shell
// driver runs it from a copy of the unit's directory:
//
//   accel.goose list <manifest>
//   accel.goose <surface> <manifest> <unit>
//   accel.goose batch <manifest> <unit> <surface>...
//   accel.goose block-lead <manifest> <unit>
//   accel.goose block-fuzz <manifest> <unit> <seed> <n>
//   accel.goose cook-fuzz <manifest> <unit> <seed> <n> <fixtures dir>
import os;
import .accel_lib;

fn main() {
    let argv = args();
    if argv.len < 3 {
        write_stderr("usage: accel.goose list <manifest> | <surface> <manifest> <unit>\n");
        exit(1);
    }
    let surface = argv[1];
    if surface == "list" {
        write_stdout("cook\ncook-write\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery\ncook-reason\nblock-reason\n");
        return;
    }
    if argv.len < 4 {
        write_stderr("usage: accel.goose <surface> <manifest> <unit>\n");
        exit(1);
    }
    let text = slurp(argv[2]);
    let lines = split(text, '\n');
    let unit = argv[3];
    // THE BATCH (test/conformance/goose/driver, "THE BATCH"), as main.goose's
    if surface == "batch" {
        var k: i64 = 4;
        while k < argv.len {
            write_stdout(str("#surface ", argv[k], "\n"));
            if !run_accel(argv[k], lines, unit) { exit(2); }
            write_stdout(str("#done ", argv[k], "\n"));
            k += 1;
        }
        return;
    }
    if run_accel(surface, lines, unit) { return; }
    if surface == "block-lead" { surface_block_lead(lines, unit); return; }
    if surface == "block-fuzz" && argv.len == 6 { fuzz_block(lines, unit, number(argv[4]), number(argv[5])); return; }
    if surface == "cook-fuzz" && argv.len == 7 { fuzz_cook(lines, unit, number(argv[4]), number(argv[5]), argv[6]); return; }
    // a surface this program does not answer: ABSENT, never a guessed answer
    exit(2);
}

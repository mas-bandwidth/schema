package goosetable_test

import (
	"os"
	"path/filepath"
	"testing"
)

// conformanceAccelNone is the conformance_accel.goose that
// test/conformance/goose/driver (accel_glue) writes for a unit with no block
// table and no cook root: every accelerator arm answers "not this root". The
// gates in this package that run main.goose drive only the wire, report,
// retention and dispatch surfaces, never an accelerator surface, so they place
// this module beside accel_lib.goose, which main.goose imports (one goose
// compile per unit answers every surface, the accelerators too).
const conformanceAccelNone = `// the conformance gates in internal/codegen/goosetable: no accelerator arm
import .block_form;
import .wire_load;
let _accel_none: schema_rt::AccelRecord[1] = [schema_rt::AccelRecord { .. }];
fn accel_block_reason(table: const u8[:], b: const u8[:], lead: i64) -> i64 { return -1; }
fn accel_block_bytes(table: const u8[:], b: const u8[:]) -> i64 { return -1; }
fn accel_block_record(table: const u8[:]) -> i64 { return -1; }
fn accel_block_records() -> const schema_rt::AccelRecord[:] { return _accel_none[0..0]; }
fn accel_cook_reason(root: const u8[:], b: const u8[:], lead: i64) -> i64 { return -1; }
fn accel_cook_record(root: const u8[:]) -> i64 { return -1; }
fn accel_cook_records() -> const schema_rt::AccelRecord[:] { return _accel_none[0..0]; }
fn accel_cook_measure(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], at: i64) -> i64 { return -2; }
fn accel_cook_write(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], at: i64, out: u8[:], big: bool) -> bool { return false; }
`

// conformanceArmsAbsent are the glue arms main.goose calls that the shell
// driver writes for the announce, write-refusal-cook and read-into surfaces
// (test/conformance/goose/driver, glue). These gates drive none of those
// surfaces, so every arm answers -1, a root with no such read or write, as the
// driver's own arm does for a unit without one. conformance_ri_verdict is the
// driver's helper as it writes it: main.goose's variable read-into calls it.
const conformanceArmsAbsent = `fn conformance_ri_verdict(out: u8[>..]&, ok: bool) {
    if ok { out.append("read"); } else { out.append("refused"); }
    out.push(10);
}
fn conformance_cook_var(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], at: i64) -> i64 { return -1; }
fn conformance_announce_write(out: u8[>..]&) -> i64 { return -1; }
fn conformance_announce_read(strict: bool, ann: const u8[:], out: u8[>..]&) -> i64 { return -1; }
fn conformance_announce_send(root: const u8[:], data: const u8[:], fresh: bool, out: u8[>..]&) -> i64 { return -1; }
fn conformance_fresh_json(root: const u8[:], out: u8[>..]&) -> i64 { return -1; }
fn conformance_read_into(root: const u8[:], form: const u8[:], lawful: const u8[:], damaged: const u8[:], count: i64, ann: const u8[:], out: u8[>..]&) -> i64 { return -1; }
`

// stageAccel completes a unit's directory the way the shell driver lays it
// out for main.goose: conformanceArmsAbsent goes on the end of the glue
// module, and accel_lib.goose and conformanceAccelNone go beside it. It runs
// after the glue's import list is built: the accelerator module imports the
// glue, never the reverse (the driver leaves both out of it too).
func stageAccel(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	lib, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "accel_lib.goose"))
	if err != nil {
		t.Fatal(err)
	}
	files["conformance_unit.goose"] = append(files["conformance_unit.goose"], conformanceArmsAbsent...)
	files["accel_lib.goose"] = lib
	files["conformance_accel.goose"] = []byte(conformanceAccelNone)
}

package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The OLDER, NARROWER writer and the newer reader of one table: every field
// the later build widened (docs/SPEC-TABLES.md §4's rungs: a signed kind up its
// ladder, an unsigned kind up its own, f32 into f64), and a list whose element
// widened, which is §4.1's element case beside the field case.
const wideningOlderSchema = `package widenprobe

table Rec
{
    small  int16
    mid    int32
    count  uint8
    ratio  float32
    scores []int16
}
`

const wideningNewerSchema = `package widenprobe

table Rec
{
    small  int32
    mid    int64
    count  uint32
    ratio  float64
    scores []int32
}
`

// TestWideningReasonsJavaScript is the JavaScript leg's direct widening test
// (docs/SPEC-TABLES.md §4, §4.1, §3, §6.5). It reads three writers' bytes
// through the newer reader and checks the value and the report:
//
//   - an OLDER, NARROWER writer's bytes, saved by that writer's own generated
//     module: five widenings, every value kept, and the reason empty, because
//     a widening is a successful read and Go's TableReport.Reason is empty for
//     a file read;
//   - the committed narrow list wire (list_scalar_widen.bin, the corpus's
//     reports.txt row `0,0,1,0,0,false,read`) beside its pristine twin;
//   - the REFUSALS, each named as the reference leg names it: form byte 2 is
//     `message_form_as_file`, form byte 3 (the fixed form, newer than this
//     one) and a byte no form defines are `newer_form`, every one with refused
//     set and no counter moved; and damage is the malformed flag with no
//     reason, as Go's Load leaves it.
//
// The cook's and the block's TableRefuseReason names (not_a_cook,
// foreign_order, wrong_build_version, reserved_not_zero, bad_alignment,
// truncated, unaligned_base, bad_layout) are judged by the cook-reason and
// block-reason conformance surfaces against the manifest's refusal rows.
func TestWideningReasonsJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	older := variableModuleFor(t, jsUnitOf(t, wideningOlderSchema), filepath.Join(work, "older"))
	newer := variableModuleFor(t, jsUnitOf(t, wideningNewerSchema), filepath.Join(work, "newer"))
	lists := variableModuleFor(t, loadUnitPaths(t, []string{filepath.Join(repo, "tables", "lists")}), filepath.Join(work, "lists"))

	narrowWire := filepath.Join(repo, "testdata", "wire", "tables", "fuzz-vectors", "list_scalar_widen.bin")
	pristineWire := filepath.Join(repo, "testdata", "wire", "tables", "list_tables.bin")

	script := filepath.Join(work, "probe.mjs")
	const probe = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const older = await import(pathToFileURL(process.argv[2]).href);
const newer = await import(pathToFileURL(process.argv[3]).href);
const lists = await import(pathToFileURL(process.argv[4]).href);
const problems = [];
const check = (what, ok, detail) => { if (!ok) { problems.push(what + ": " + JSON.stringify(detail, (k, v) => typeof v === "bigint" ? v.toString() + "n" : v)); } };
const quiet = (r) => r.unknown === 0 && r.kindMismatch === 0 && r.clamped === 0 && r.duplicate === 0;

// 1. the older, narrower writer through the newer reader
{
  const text = new older.TableVariableReport();
  const v = older.RecFromJson('{"small": -32768, "mid": -2147483648, "count": 255, "ratio": 1.5, "scores": [-7, 32767]}', text);
  check("older text", v !== null && text.reason === "" && !text.malformed, text);
  const wire = older.RecSave(v);
  check("older save", wire !== null, v);
  const report = new newer.TableVariableReport();
  const got = newer.RecLoad(wire, report);
  check("older read", got !== null, report);
  if (got !== null) {
    check("small", Number(got.small) === -32768, got.small);
    check("mid", BigInt(got.mid) === -2147483648n, got.mid);
    check("count", Number(got.count) === 255, got.count);
    check("ratio", got.ratio === 1.5, got.ratio);
    check("scores", got.scores.length === 2 && Number(got.scores[0]) === -7 && Number(got.scores[1]) === 32767, got.scores);
  }
  check("older report", report.reason === "" && report.widened === 5 && report.refused === 0 && !report.malformed && quiet(report), report);
}

// 2. the committed narrow list wire and its pristine twin
{
  const report = new lists.TableVariableReport();
  const value = lists.SaveLoad(new Uint8Array(readFileSync(process.argv[5])), report);
  check("narrow read", value !== null, report);
  if (value !== null) {
    check("narrow scores", value.scores.length === 3 && value.scores[0] === 10 && value.scores[1] === 0 && value.scores[2] === 0, value.scores);
  }
  check("narrow report", report.reason === "" && report.widened === 1 && report.refused === 0 && !report.malformed && quiet(report), report);
  const baseline = new lists.TableVariableReport();
  const clean = lists.SaveLoad(new Uint8Array(readFileSync(process.argv[6])), baseline);
  check("pristine read", clean !== null, baseline);
  if (clean !== null) {
    check("pristine scores", clean.scores.length === 3 && clean.scores[0] === 10 && clean.scores[1] === 20 && clean.scores[2] === 30, clean.scores);
  }
  check("pristine report", baseline.reason === "" && baseline.widened === 0 && !baseline.malformed && quiet(baseline), baseline);
}

// 3. the refusals, by name, and damage, by flag
{
  const good = newer.RecSave(newer.RecFromJson('{"mid": 1}', new newer.TableVariableReport()));
  check("empty wire", good !== null, null);
  const forms = [[2, "message_form_as_file"], [3, "newer_form"], [0, "newer_form"], [0xff, "newer_form"]];
  for (const [form, want] of forms) {
    const bytes = Uint8Array.from(good);
    bytes[0] = form;
    const report = new newer.TableVariableReport();
    const got = newer.RecLoad(bytes, report);
    check("form " + form, got === null && report.reason === want && report.refused === 1 && !report.malformed &&
      report.widened === 0 && quiet(report), report);
  }
  const damaged = new newer.TableVariableReport();
  const got = newer.RecLoad(new Uint8Array([1, 0, 0]), damaged);
  check("damage", got === null && damaged.malformed && damaged.reason === "" && damaged.refused === 0, damaged);
}

if (problems.length > 0) { console.error(problems.join("\n")); process.exit(1); }
console.log("widening reasons ok");
`
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, older, newer, lists, narrowWire, pristineWire)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("JavaScript widening probe failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// variableModuleFor generates a unit's variable module into dir and returns the
// module's path: the module is self-contained, so a probe imports it alone.
func variableModuleFor(t *testing.T, u *ir.Unit, dir string) string {
	t.Helper()
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generate variable files: %v", err)
	}
	home := runtimeHome(u) + "Variable.js"
	data, ok := files[home]
	if !ok {
		t.Fatalf("generated variable module %s is absent", home)
	}
	module := filepath.Join(dir, home)
	if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(module, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return module
}

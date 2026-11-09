package goosetable

// THE STRICT FIXED LOAD (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6; ROADMAP.md,
// Fixed Tables): <T>FixedLoad is the strict read — one read, the whole value
// or a refusal. It must never clamp. A bool or present byte other than 0 or 1
// is wire_damaged, refused by the scan before a byte lands; a ranged scalar or
// a forged ordinal (an enum ordinal past its top, a union tag past its arm
// count) is out_of_range, refused by the bounds pass after the run with every
// previously written batch slot reset to its declared defaults. The clamp pass
// remains only as the write contract's and the negative controls' helper.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// strictLoadSchema exercises every strict-read rule the card closes: a ranged
// scalar, an enum ordinal, a union tag, a compressed float, a bool, an
// optional whose present byte and live payload must be held, and a counted
// array of nested structs carrying a bool.
const strictLoadSchema = `package probe
enum Color { Red, Green, Blue }
union Pick {
    a int32
    b int32
}
fixed table Leaf {
    active bool = false
}
fixed table T {
    x int32 = 2 | min = -5, max = 5
    c Color = Red
    pick Pick
    aim float32 = 0.0 | min = -1.0, max = 2.0, resolution = 0.1
    flag bool = false
    note ?int32 | min = 0, max = 1000
    leaves [..2]Leaf
}
`

// fixedFunctionBody returns one generated function's body, named `fn name(`,
// through its closing "\n}\n".
func fixedFunctionBody(src, name string) (string, bool) {
	i := strings.Index(src, "fn "+name+"(")
	if i < 0 {
		return "", false
	}
	rest := src[i:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// strictFixedProblems reports every way the generated artifact violates the
// strict read. Empty means the artifact holds the rule.
func strictFixedProblems(code string) []string {
	var bad []string

	load, ok := fixedFunctionBody(code, "TFixedLoad")
	if !ok {
		bad = append(bad, "the generated module has no TFixedLoad")
	} else {
		if strings.Contains(load, "report.oor += _TFixedClamp(") {
			bad = append(bad, "TFixedLoad still clamps and counts oor instead of refusing")
		}
		for _, s := range []string{
			"let why = _TFixedStrictRefuse(values[k]);",
			"while j <= k { values[j] = T { .. }; j += 1; }",
			"return schema_rt::fixed_refuse(report, why, false);",
		} {
			if !strings.Contains(load, s) {
				bad = append(bad, "TFixedLoad is missing "+s)
			}
		}
	}

	scan, ok := fixedFunctionBody(code, "_TFixedScan")
	if !ok {
		bad = append(bad, "the generated module has no _TFixedScan")
	} else if !strings.Contains(scan, "return schema_rt::FixedRefuseWireDamaged;") {
		bad = append(bad, "_TFixedScan does not refuse a bool or present byte by wire_damaged")
	}

	strict, ok := fixedFunctionBody(code, "_TFixedStrictRefuse")
	if !ok {
		bad = append(bad, "the generated module has no _TFixedStrictRefuse")
	} else {
		if !strings.Contains(strict, "return schema_rt::FixedRefuseOutOfRange;") {
			bad = append(bad, "_TFixedStrictRefuse does not refuse out_of_range")
		}
		if !strings.Contains(strict, "v.NotePresent") {
			bad = append(bad, "_TFixedStrictRefuse does not walk the optional's live extent")
		}
		if strings.Contains(strict, "c += 1") || strings.Contains(strict, "= (-5); c") {
			bad = append(bad, "_TFixedStrictRefuse clamps instead of refusing")
		}
	}

	return bad
}

const strictLoadProbeMain = `fn copy_bytes(src: const u8[:], dst: u8[>..]&) {
    var i: i64 = 0;
    while i < src.len { dst.push(src[i]); i += 1; }
}

fn main() {
    var v0 = probe::T {
        X: 3,
        C: 1,
        Pick: probe::Pick { Type: 1, A: 11, B: 0 },
        Aim: 0.5,
        Flag: true,
        Note: 42,
        NotePresent: true,
        Leaves: [probe::Leaf { Active: true }, probe::Leaf { Active: false }],
        LeavesCount: 1,
    };
    var clean: u8[>..] = [];
    let _ = probe::TFixedSave([v0, v0], clean);
    let rec0 = clean.len - 2 * probe::TFixedRecordBytes + 8;
    let rec1 = clean.len - probe::TFixedRecordBytes + 8;
    // Field offsets inside T body:
    // X: +0 (4B), C: +4 (1B), Pick: +5 (1B tag + 4B arm = 5B), Aim: +10 (4B),
    // Flag: +14 (1B), NotePresent: +15 (1B), Note: +16 (4B), LeavesCount: +20 (4B),
    // Leaves[0].Active: +24 (1B), Leaves[1].Active: +25 (1B).

    // 1. Clean 2-record batch loads with 0 oor.
    var dst: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n_ok = probe::TFixedLoad(dst, clean, rep);
    print("clean ", n_ok, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X, " ", dst[1].Note);

    // 2. Second record ranged scalar out of range (X = 99) refuses out_of_range and resets dst[0].X to default 2.
    var bad_x: u8[>..] = [];
    copy_bytes(clean, bad_x);
    bad_x[rec1 + 0] = 99;
    dst = [probe::T { .. }; 2];
    let n_x = probe::TFixedLoad(dst, bad_x, rep);
    print("ranged_oor ", n_x, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X, " ", dst[1].X);

    // 3. Forged enum ordinal (C = 9) refuses out_of_range and resets batch.
    var bad_c: u8[>..] = [];
    copy_bytes(clean, bad_c);
    bad_c[rec1 + 4] = 9;
    dst = [probe::T { .. }; 2];
    let n_c = probe::TFixedLoad(dst, bad_c, rep);
    print("enum_oor ", n_c, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X, " ", dst[1].C as u64);

    // 4. Forged union tag (Pick.Type = 9) refuses out_of_range and resets batch.
    var bad_u: u8[>..] = [];
    copy_bytes(clean, bad_u);
    bad_u[rec1 + 5] = 9;
    dst = [probe::T { .. }; 2];
    let n_u = probe::TFixedLoad(dst, bad_u, rep);
    print("union_oor ", n_u, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X, " ", dst[1].Pick.Type as u64);

    // 5. Compressed float -0.0 (0x80000000) normalizes to +0.0, while 4.0 (0x40800000) refuses out_of_range.
    var neg_zero: u8[>..] = [];
    copy_bytes(clean, neg_zero);
    neg_zero[rec0 + 10] = 0x00; neg_zero[rec0 + 11] = 0x00; neg_zero[rec0 + 12] = 0x00; neg_zero[rec0 + 13] = 0x80;
    dst = [probe::T { .. }; 2];
    let n_nz = probe::TFixedLoad(dst, neg_zero, rep);
    print("neg_zero ", n_nz, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", schema_rt::fixed_f32_bits(dst[0].Aim) == 0);

    var bad_aim: u8[>..] = [];
    copy_bytes(clean, bad_aim);
    bad_aim[rec1 + 10] = 0x00; bad_aim[rec1 + 11] = 0x00; bad_aim[rec1 + 12] = 0x80; bad_aim[rec1 + 13] = 0x40;
    dst = [probe::T { .. }; 2];
    let n_aim = probe::TFixedLoad(dst, bad_aim, rep);
    print("float_oor ", n_aim, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X);

    // 6. Bool byte 2 (Flag = 2) and nested live bool byte 2 (Leaves[0].Active = 2) refuse wire_damaged before landing.
    var bad_bool: u8[>..] = [];
    copy_bytes(clean, bad_bool);
    bad_bool[rec1 + 14] = 2;
    dst = [probe::T { .. }; 2];
    let n_b = probe::TFixedLoad(dst, bad_bool, rep);
    print("bool_dmg ", n_b, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X);

    var bad_leaf_bool: u8[>..] = [];
    copy_bytes(clean, bad_leaf_bool);
    bad_leaf_bool[rec1 + 24] = 2;
    dst = [probe::T { .. }; 2];
    let n_lb = probe::TFixedLoad(dst, bad_leaf_bool, rep);
    print("leaf_bool_dmg ", n_lb, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X);

    // 7. Present byte 2 (NotePresent = 2) refuses wire_damaged.
    var bad_pres: u8[>..] = [];
    copy_bytes(clean, bad_pres);
    bad_pres[rec1 + 15] = 2;
    dst = [probe::T { .. }; 2];
    let n_p = probe::TFixedLoad(dst, bad_pres, rep);
    print("pres_dmg ", n_p, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", dst[0].X);

    // 8. Inactive optional payload (NotePresent = 0, Note = 9999) and counted-array slack (Leaves[1].Active = 255) are not live and succeed.
    var slack: u8[>..] = [];
    copy_bytes(clean, slack);
    slack[rec1 + 15] = 0;
    slack[rec1 + 16] = 0x0f; slack[rec1 + 17] = 0x27;
    slack[rec1 + 25] = 255;
    dst = [probe::T { .. }; 2];
    let n_sl = probe::TFixedLoad(dst, slack, rep);
    print("slack_ok ", n_sl, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", !dst[1].NotePresent, " ", dst[1].LeavesCount);
}
`

const strictLoadProbeWant = `clean 2 ok 0 3 42
ranged_oor -1 out_of_range 0 2 2
enum_oor -1 out_of_range 0 2 1
union_oor -1 out_of_range 0 2 0
neg_zero 2 ok 0 true
float_oor -1 out_of_range 0 2
bool_dmg -1 wire_damaged 0 2
leaf_bool_dmg -1 wire_damaged 0 2
pres_dmg -1 wire_damaged 0 2
slack_ok 2 ok 0 true 1`

func runStrictLoadFiles(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + strictLoadProbeMain)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", nil)))
}

func generateStrictLoadFiles(t *testing.T) map[string][]byte {
	t.Helper()
	u := parseCheckUnit(t, strictLoadSchema)
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
	return files
}

// TestGooseFixedLoadRefusesNeverClamps pins the strict read: the generated
// <T>FixedLoad refuses out_of_range and wire_damaged, never clamping.
func TestGooseFixedLoadRefusesNeverClamps(t *testing.T) {
	t.Parallel()
	files := generateStrictLoadFiles(t)
	code := string(files["ProbeTable.goose"])
	for _, b := range strictFixedProblems(code) {
		t.Error(b)
	}
	if got := runStrictLoadFiles(t, files); got != strictLoadProbeWant {
		t.Fatalf("strict load runtime probe:\n got:\n%s\nwant:\n%s", got, strictLoadProbeWant)
	}
}

// TestGooseFixedLoadRefusesNeverClampsNegativeControl plants each fault the
// strict read closes back into the real artifact and must see both the static
// checks and the Goose runtime probe turn red: a load that clamps again, and a
// scan that no longer refuses a bad bool or present byte.
func TestGooseFixedLoadRefusesNeverClampsNegativeControl(t *testing.T) {
	t.Parallel()
	files := generateStrictLoadFiles(t)
	code := string(files["ProbeTable.goose"])

	clamping := strings.Replace(code,
		"let why = _TFixedStrictRefuse(values[k]);\n        if why != 0 {\n            var j: i64 = 0;\n            while j <= k { values[j] = T { .. }; j += 1; }\n            return schema_rt::fixed_refuse(report, why, false);\n        }",
		"report.oor += _TFixedClamp(values[k]);", 1)
	if clamping == code {
		t.Fatal("negative control: the strict-refuse block is not in the generated load")
	}
	if len(strictFixedProblems(clamping)) == 0 {
		t.Fatal("negative control: restoring the clamp did not turn the strict checks red")
	}
	clampFiles := maps.Clone(files)
	clampFiles["ProbeTable.goose"] = []byte(clamping)
	if got := runStrictLoadFiles(t, clampFiles); got == strictLoadProbeWant {
		t.Fatal("negative control: runtime probe still passed when TFixedLoad clamped instead of refusing")
	}

	tolerant := strings.Replace(code,
		"return schema_rt::FixedRefuseWireDamaged;",
		"return 0;", -1)
	if tolerant == code {
		t.Fatal("negative control: the wire_damaged refusal is not in the generated scan")
	}
	if len(strictFixedProblems(tolerant)) == 0 {
		t.Fatal("negative control: letting a bad bool land did not turn the strict checks red")
	}
	tolFiles := maps.Clone(files)
	tolFiles["ProbeTable.goose"] = []byte(tolerant)
	if got := runStrictLoadFiles(t, tolFiles); got == strictLoadProbeWant {
		t.Fatal("negative control: runtime probe still passed when _TFixedScan let a bad bool/present byte land")
	}
}

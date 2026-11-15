package goosetable

// THE STRICT LINEAGE READ (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6, §5.4;
// docs/rulings.json strict-only-read): <T>FixedEvolveLoad is the strict read,
// on the identity plan and on every compiled older-layout plan alike. A ranged
// scalar outside the WRITER's range, a forged enum ordinal past the writer's
// variant count and a forged union tag past the writer's arm set refuse
// out_of_range in the bounds pass, and every element of the batch already
// written goes back to its declared defaults. It never clamps.

import (
	"encoding/binary"
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

const evolveStrictOldSchema = `package probe
enum Tier { Bronze, Silver, Gold }
union Pick {
    a int32
    b int32
}
fixed table Lineage {
    tier Tier
    pick Pick
    ticks int32 = 0 | min = 0, max = 100
    seq int32 = 42
}
`

const evolveStrictNewSchema = `package probe
enum Tier { Bronze, Silver, Gold, Platinum }
union Pick {
    a int32
    b int32
    c int32
}
fixed table Lineage {
    tier Tier
    pick Pick
    ticks int32 = 0 | min = 0, max = 200
    seq int32 = 42
}
`

// evolveStrictBody is one 14-byte Lineage record: tier, pick tag, pick arm,
// ticks and seq, in the layout both generations share.
func evolveStrictBody(tier, tag, arm, ticks, seq uint32) []byte {
	b := make([]byte, 14)
	b[0] = byte(tier)
	b[1] = byte(tag)
	binary.LittleEndian.PutUint32(b[2:], arm)
	binary.LittleEndian.PutUint32(b[6:], ticks)
	binary.LittleEndian.PutUint32(b[10:], seq)
	return b
}

func evolveStrictFiles(t *testing.T) (map[string][]byte, FixedEvolutionEntry, FixedEvolutionEntry) {
	t.Helper()
	uOld := parseCheckUnit(t, evolveStrictOldSchema)
	uNew := parseCheckUnit(t, evolveStrictNewSchema)
	eOld, ok := FixedEvolutionOf(uOld, "Lineage")
	if !ok {
		t.Fatal("missing old Lineage")
	}
	eNew, ok := FixedEvolutionOf(uNew, "Lineage")
	if !ok {
		t.Fatal("missing new Lineage")
	}
	files, err := goose.Generate(uNew)
	if err != nil {
		t.Fatalf("goose.Generate: %v", err)
	}
	tables, err := GenerateLineage(uNew, map[string][]FixedEvolutionEntry{"Lineage": {eOld}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	maps.Copy(files, tables)
	return files, eOld, eNew
}

const evolveStrictProbeMain = `
fn dirty(values: probe::Lineage[:]) {
    var j: i64 = 0;
    while j < values.len {
        values[j].Tier = 3;
        values[j].Pick = probe::Pick { Type: 1, A: 7, B: 0, C: 0 };
        values[j].Ticks = 99;
        values[j].Seq = 99;
        j += 1;
    }
}

fn show_evolve(name: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(name, data) { print(name, " read-failed"); return; }
    var values: probe::Lineage[2] = [probe::Lineage { .. }; 2];
    dirty(values);
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = probe::LineageFixedEvolveLoad(values, data, rep);
    print(name, " ", n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason), " ", rep.oor, " ", rep.widened,
        " ", values[0].Tier as u64, " ", values[0].Pick.Type as u64, " ", values[0].Ticks, " ", values[0].Seq,
        " ", values[1].Tier as u64, " ", values[1].Pick.Type as u64, " ", values[1].Ticks, " ", values[1].Seq);
}

fn show_strict(name: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(name, data) { print(name, " read-failed"); return; }
    var values: probe::Lineage[2] = [probe::Lineage { .. }; 2];
    dirty(values);
    var rep = schema_rt::FixedReport { .. };
    let n = probe::LineageFixedLoad(values, data, rep);
    print(name, " ", n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor,
        " ", values[0].Tier as u64, " ", values[0].Pick.Type as u64, " ", values[0].Ticks, " ", values[0].Seq,
        " ", values[1].Tier as u64, " ", values[1].Pick.Type as u64, " ", values[1].Ticks, " ", values[1].Seq);
}

fn main() {
    show_evolve("old_clean.bin");
    show_evolve("old_bad_ticks.bin");
    show_evolve("old_bad_ordinal.bin");
    show_evolve("old_bad_tag.bin");
    show_evolve("cur_bad_ticks.bin");
    show_strict("cur_bad_ticks.bin");
    show_evolve("cur_bad_ordinal.bin");
    show_strict("cur_bad_ordinal.bin");
    show_evolve("cur_bad_tag.bin");
    show_strict("cur_bad_tag.bin");
}
`

var evolveStrictWant = strings.Join([]string{
	"old_clean.bin 2 ok 0 0 1 1 50 7 2 2 60 8",
	"old_bad_ticks.bin -1 out_of_range 0 0 0 0 0 42 0 0 0 42",
	"old_bad_ordinal.bin -1 out_of_range 0 0 0 0 0 42 0 0 0 42",
	"old_bad_tag.bin -1 out_of_range 0 0 0 0 0 42 0 0 0 42",
	"cur_bad_ticks.bin -1 out_of_range 0 0 0 0 0 42 0 0 0 42",
	"cur_bad_ticks.bin -1 out_of_range 0 0 0 0 42 0 0 0 42",
	"cur_bad_ordinal.bin -1 out_of_range 0 0 0 0 0 42 0 0 0 42",
	"cur_bad_ordinal.bin -1 out_of_range 0 0 0 0 42 0 0 0 42",
	"cur_bad_tag.bin -1 out_of_range 0 0 0 0 0 42 0 0 0 42",
	"cur_bad_tag.bin -1 out_of_range 0 0 0 0 42 0 0 0 42",
}, "\n")

// evolveStrictProblems reports every way the generated lineage reader violates
// the strict read. Empty means the artifact holds the rule.
func evolveStrictProblems(code string) []string {
	var bad []string
	load, ok := fixedFunctionBody(code, "LineageFixedEvolveLoad")
	if !ok {
		return []string{"the generated module has no LineageFixedEvolveLoad"}
	}
	for _, s := range []string{"report.oor += _LineageFixedClamp(", "report.oor += 1"} {
		if strings.Contains(load, s) {
			bad = append(bad, "LineageFixedEvolveLoad still clamps or counts instead of refusing")
		}
	}
	for _, s := range []string{
		"let why = _LineageFixedStrictRefuse(values[k]);",
		"while j <= k { values[j] = Lineage { .. }; j += 1; }",
		"return schema_rt::fixed_evolve_refuse(report, why, false);",
		"return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseOutOfRange, false);",
	} {
		if !strings.Contains(load, s) {
			bad = append(bad, "LineageFixedEvolveLoad is missing "+s)
		}
	}
	return bad
}

func runEvolveStrictFiles(t *testing.T, files map[string][]byte, filesIn map[string][]byte) string {
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
	b.WriteString("\n" + evolveStrictProbeMain)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range filesIn {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", nil)))
}

func TestGooseFixedEvolveLoadRefusesNeverClamps(t *testing.T) {
	t.Parallel()
	files, eOld, eNew := evolveStrictFiles(t)
	code := string(files["ProbeTable.goose"])
	for _, b := range evolveStrictProblems(code) {
		t.Error(b)
	}
	filesIn := map[string][]byte{
		"old_clean.bin":       makeFixedFile(eOld.Wire, eOld.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 2, 22, 60, 8)),
		"old_bad_ticks.bin":   makeFixedFile(eOld.Wire, eOld.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 2, 22, 150, 8)),
		"old_bad_ordinal.bin": makeFixedFile(eOld.Wire, eOld.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(4, 2, 22, 60, 8)),
		"old_bad_tag.bin":     makeFixedFile(eOld.Wire, eOld.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 3, 22, 60, 8)),
		"cur_bad_ticks.bin":   makeFixedFile(eNew.Wire, eNew.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 2, 22, 250, 8)),
		"cur_bad_ordinal.bin": makeFixedFile(eNew.Wire, eNew.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(5, 2, 22, 60, 8)),
		"cur_bad_tag.bin":     makeFixedFile(eNew.Wire, eNew.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 4, 22, 60, 8)),
	}
	if got := runEvolveStrictFiles(t, files, filesIn); got != evolveStrictWant {
		t.Fatalf("strict lineage probe:\n got:\n%s\nwant:\n%s", got, evolveStrictWant)
	}
}

// TestGooseFixedEvolveLoadRefusesNeverClampsNegativeControl plants the clamp
// back into a copy of the generated lineage reader and must see both the static
// checks and the Goose runtime probe turn red.
func TestGooseFixedEvolveLoadRefusesNeverClampsNegativeControl(t *testing.T) {
	t.Parallel()
	files, eOld, eNew := evolveStrictFiles(t)
	code := string(files["ProbeTable.goose"])
	const strictBlock = `let why = _LineageFixedStrictRefuse(values[k]);
            if why != 0 {
                var j: i64 = 0;
                while j <= k { values[j] = Lineage { .. }; j += 1; }
                return schema_rt::fixed_evolve_refuse(report, why, false);
            }`
	clamping := strings.ReplaceAll(code, strictBlock, "report.oor += _LineageFixedClamp(values[k]);")
	if clamping == code {
		t.Fatal("negative control: the strict-refuse block is not in the generated lineage reader")
	}
	if len(evolveStrictProblems(clamping)) == 0 {
		t.Fatal("negative control: restoring the clamp did not turn the strict checks red")
	}
	clampFiles := maps.Clone(files)
	clampFiles["ProbeTable.goose"] = []byte(clamping)
	filesIn := map[string][]byte{
		"old_bad_ticks.bin":   makeFixedFile(eOld.Wire, eOld.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 2, 22, 150, 8)),
		"cur_bad_ticks.bin":   makeFixedFile(eNew.Wire, eNew.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 2, 22, 250, 8)),
		"cur_bad_ordinal.bin": makeFixedFile(eNew.Wire, eNew.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(5, 2, 22, 60, 8)),
		"cur_bad_tag.bin":     makeFixedFile(eNew.Wire, eNew.Layout, evolveStrictBody(1, 1, 11, 50, 7), evolveStrictBody(2, 4, 22, 60, 8)),
	}
	if got := runEvolveStrictFiles(t, clampFiles, filesIn); got == evolveStrictWant {
		t.Fatal("negative control: runtime probe still passed when LineageFixedEvolveLoad clamped instead of refusing")
	}
}

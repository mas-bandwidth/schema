package squirreltable_test

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSquirrelCardsConformance pins the Squirrel cards facade and driver
// (tables/cards/squirrel) to the strict read (docs/SPEC-TABLES.md §4, §6.5)
// and runs the native unit tests plus all nine card conformance surfaces
// (load, save, render, parse, bounds, upgrade, edit, taskhash, view) on
// read-then-write against tables/cards/conformance/expected/.
//
// TestSquirrelCardsConformanceNegativeControl plants retired tolerant entry
// points, retired report counters, and real facade sabotages into copies of
// the real tables/cards/squirrel/*.nut artifacts and requires each check to go
// red.

var squirrelCardsRetired = []string{
	"loadTolerant",
	"CardLoadTolerant",
	"FromJsonTolerant",
	"fromJsonTolerant",
	"CardFromJsonTolerant",
	"LoadRetainTolerant",
	"CardLoadRetainTolerant",
	"loadWalk",
	"CardLoadWalk",
	"fromJsonWalk",
	"CardFromJsonWalk",
	"loadRetainWalk",
	"CardLoadRetainWalk",
	"report.kindMismatch",
	"report.kindChanged",
	"report.outOfRange",
	"report.repeatedKey",
	"report.clamped",
	"report.duplicate",
	"report.malformed",
	"kindMismatch =",
	"clamped =",
	"duplicate =",
	"malformed =",
}

var squirrelCardsStrictCalls = []string{
	"T.CardLoad(wire, r)",
	"T.CardFromJson(text, r)",
	"T.CardLoadRetain(wire, store, r)",
	"T.CardSave(root)",
	"T.CardSaveRetain(root, store, out, r)",
}

var squirrelCardsSurfaces = []string{
	"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view",
}

func squirrelCardsSources(t *testing.T, root string) map[string]string {
	t.Helper()
	dir := filepath.Join(root, "tables", "cards", "squirrel")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".nut" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(b)
	}
	if len(files) == 0 {
		t.Fatal("no Squirrel cards sources found")
	}
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	files["Makefile"] = string(mk)
	return files
}

func squirrelCardsStrictCheck(files map[string]string) []string {
	var bad []string
	for name, src := range files {
		for _, retired := range squirrelCardsRetired {
			if strings.Contains(src, retired) {
				bad = append(bad, fmt.Sprintf("%s: retired tolerant surface %q", name, retired))
			}
		}
	}
	if cardsSrc, ok := files["cards.nut"]; ok {
		for _, call := range squirrelCardsStrictCalls {
			if !strings.Contains(cardsSrc, call) {
				bad = append(bad, fmt.Sprintf("cards.nut: missing strict call %q", call))
			}
		}
	} else {
		bad = append(bad, "cards.nut: missing")
	}
	if drv, ok := files["driver.nut"]; ok {
		const wantReportTail = `KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n`
		if !strings.Contains(drv, wantReportTail) {
			bad = append(bad, "driver.nut: missing strict report zero/false tail "+wantReportTail)
		}
	} else {
		bad = append(bad, "driver.nut: missing")
	}
	if mk, ok := files["Makefile"]; ok {
		if !strings.Contains(mk, "cards-conformance-all: cards-conformance-all-goose") {
			bad = append(bad, "Makefile: cards-conformance-all missing cards-conformance-all-goose prerequisite")
		}
	} else {
		bad = append(bad, "Makefile: missing")
	}
	return bad
}

func squirrelCardsGenerated(t *testing.T, root string) string {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "cards")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	genDir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(genDir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return genDir
}

func stageSquirrelCardsFacade(t *testing.T, facade map[string]string) string {
	t.Helper()
	stage := t.TempDir()
	cardsPath := filepath.ToSlash(filepath.Join(stage, "cards.nut"))
	for name, src := range facade {
		if name == "Makefile" {
			continue
		}
		if name == "driver.nut" || name == "cards_test.nut" {
			src = strings.ReplaceAll(src, `"tables/cards/squirrel/cards.nut"`, fmt.Sprintf("%q", cardsPath))
		}
		if err := os.WriteFile(filepath.Join(stage, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return stage
}

func runSquirrelCardsSurface(t *testing.T, root, sq, genDir string, facade map[string]string, manifestRel, surface string) (string, [][]string, error) {
	t.Helper()
	manifestPath := filepath.Join(root, manifestRel)
	f, err := os.Open(manifestPath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	var rows [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") || fields[0] != surface {
			continue
		}
		if len(fields) < 3 {
			return "", nil, fmt.Errorf("short manifest row: %s", sc.Text())
		}
		rows = append(rows, fields)
	}
	if err := sc.Err(); err != nil {
		return "", nil, err
	}
	if len(rows) == 0 {
		return "", nil, fmt.Errorf("no cases for %s", surface)
	}

	stage := stageSquirrelCardsFacade(t, facade)
	outDir := t.TempDir()
	cmd := exec.Command(sq, filepath.Join(stage, "driver.nut"), manifestRel, surface, outDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CARDS_SQUIRREL_GENERATED="+genDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		return outDir, rows, fmt.Errorf("sq driver (%s): err=%v\nstdout=%s\nstderr=%s", surface, err, stdout.String(), stderr.String())
	}
	return outDir, rows, nil
}

func checkSquirrelCardsSurfaceExpected(root, surface string, rows [][]string, outDir string) error {
	expectedDir := filepath.Join(root, "tables", "cards", "conformance", "expected")
	for _, r := range rows {
		name := r[1]
		var outs []string
		if _, err := os.Stat(filepath.Join(expectedDir, name+".refused")); err == nil {
			outs = []string{name + ".refused"}
		} else {
			switch surface {
			case "load":
				outs = []string{name + ".json", name + ".report"}
			case "upgrade":
				outs = []string{name + ".wire", name + ".report"}
			case "save", "bounds", "parse", "edit":
				outs = []string{name + ".wire"}
			case "render":
				outs = []string{name + ".txt"}
			case "taskhash":
				outs = []string{name + ".hex"}
			case "view":
				outs = []string{"view.txt"}
			}
		}
		for _, o := range outs {
			want, err := os.ReadFile(filepath.Join(expectedDir, o))
			if err != nil {
				return err
			}
			got, err := os.ReadFile(filepath.Join(outDir, o))
			if err != nil {
				return fmt.Errorf("%s/%s: %w", surface, o, err)
			}
			if !bytes.Equal(got, want) {
				return fmt.Errorf("%s/%s: output differs from expected", surface, o)
			}
		}
	}
	return nil
}

func TestSquirrelCardsConformance(t *testing.T) {
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sq := sqrun.Require(t)
	facade := squirrelCardsSources(t, root)
	if bad := squirrelCardsStrictCheck(facade); len(bad) != 0 {
		t.Fatalf("Squirrel cards sources are not on the strict read:\n%s", strings.Join(bad, "\n"))
	}

	genDir := squirrelCardsGenerated(t, root)
	stage := stageSquirrelCardsFacade(t, facade)
	unitCmd := exec.Command(sq, filepath.Join(stage, "cards_test.nut"))
	unitCmd.Dir = root
	unitCmd.Env = append(os.Environ(), "CARDS_SQUIRREL_GENERATED="+genDir)
	var unitOut, unitErr bytes.Buffer
	unitCmd.Stdout = &unitOut
	unitCmd.Stderr = &unitErr
	if err := unitCmd.Run(); err != nil || unitErr.Len() > 0 {
		t.Fatalf("cards_test.nut failed: err=%v\nstdout=%s\nstderr=%s", err, unitOut.String(), unitErr.String())
	}
	if !strings.Contains(unitOut.String(), "0 failed") {
		t.Fatalf("cards_test.nut summary missing 0 failed: %q", unitOut.String())
	}

	manifestRel := filepath.Join("tables", "cards", "conformance", "MANIFEST.txt")
	for _, surface := range squirrelCardsSurfaces {
		outDir, rows, err := runSquirrelCardsSurface(t, root, sq, genDir, facade, manifestRel, surface)
		if err != nil {
			t.Fatalf("surface %s: %v", surface, err)
		}
		if err := checkSquirrelCardsSurfaceExpected(root, surface, rows, outDir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSquirrelCardsConformanceNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sq := sqrun.Require(t)

	sabotages := []struct {
		name, file, from, to string
	}{
		{"wire load", "cards.nut", "T.CardLoad(wire, r)", "T.CardLoadTolerant(wire, r)"},
		{"JSON read", "cards.nut", "T.CardFromJson(text, r)", "T.CardFromJsonWalk(text, r)"},
		{"retaining load", "cards.nut", "T.CardLoadRetain(wire, store, r)", "T.CardLoadRetainTolerant(wire, store, r)"},
		{"retired report field", "cards.nut", "report.Unknown > 0", "report.Unknown > 0 || report.kindMismatch > 0"},
		{"driver report tail", "driver.nut", `KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n`, `KindMismatch=%d\nClamped=%d\nDuplicate=%d\nMalformed=%s\n`},
		{"cards-conformance-all goose", "Makefile", "cards-conformance-all: cards-conformance-all-goose", "# SABOTAGED: goose missing from cards-conformance-all"},
	}
	for _, s := range sabotages {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			files := squirrelCardsSources(t, root)
			src, ok := files[s.file]
			if !ok || !strings.Contains(src, s.from) {
				t.Fatalf("the %s sabotage plants nothing: %q not in the real %s", s.name, s.from, s.file)
			}
			files[s.file] = strings.Replace(src, s.from, s.to, 1)
			if bad := squirrelCardsStrictCheck(files); len(bad) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left the strict check green", s.name)
			}
		})
	}

	genDir := squirrelCardsGenerated(t, root)
	manifestRel := filepath.Join("tables", "cards", "conformance", "MANIFEST.txt")
	livePlants := []struct {
		surface, from, to string
	}{
		{"render", `return key + ": " + value + "\n";`, `return key + "=" + value + "\n";`},
		{"upgrade", `return Upgraded(out, report);`, `return Upgraded(save(root), report);`},
		{"taskhash", `putU64(out, text.len());`, `// SABOTAGED: no length frame`},
	}
	for _, p := range livePlants {
		t.Run("conformance-"+p.surface, func(t *testing.T) {
			t.Parallel()
			facade := squirrelCardsSources(t, root)
			src := facade["cards.nut"]
			if strings.Count(src, p.from) != 1 {
				t.Fatalf("sabotage anchor %q in cards.nut occurred %d times, want 1", p.from, strings.Count(src, p.from))
			}
			facade["cards.nut"] = strings.Replace(src, p.from, p.to, 1)
			outDir, rows, err := runSquirrelCardsSurface(t, root, sq, genDir, facade, manifestRel, p.surface)
			if err == nil && checkSquirrelCardsSurfaceExpected(root, p.surface, rows, outDir) == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s sabotage in cards.nut left conformance green", p.surface)
			}
		})
	}
}

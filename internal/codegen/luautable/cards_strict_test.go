package luautable_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauCardsConformanceStrict pins the Luau cards facade and driver
// (tables/cards/luau) to the strict read (docs/SPEC-TABLES.md §4, §6.5) and
// runs the native unit tests plus all nine card conformance surfaces
// (load, save, render, parse, bounds, upgrade, edit, taskhash, view) on
// read-then-write against tables/cards/conformance/expected/.
//
// TestLuauCardsConformanceStrictNegativeControl plants retired tolerant entry
// points, retired report counters, and real facade sabotages into copies of the
// real tables/cards/luau/*.luau artifacts and requires each check to go red.

var luauCardsRetired = []string{
	"loadTolerant",
	"FromJsonTolerant",
	"LoadRetainTolerant",
	"loadWalk",
	"fromJsonWalk",
	"loadRetainWalk",
	"report.kindMismatch",
	"report.clamped",
	"report.duplicate",
	"report.malformed",
	"kindMismatch =",
	"clamped =",
	"duplicate =",
	"malformed =",
}

var luauCardsStrictCalls = []string{
	`L.load("Card",`,
	`J.FromJson("Card",`,
	`R.LoadRetain("Card",`,
}

var luauCardsSurfaces = []string{
	"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view",
}

func luauCardsSources(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(root, "tables", "cards", "luau")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".luau" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(b)
	}
	if len(files) == 0 {
		t.Fatal("no Luau cards sources found")
	}
	return files
}

func luauCardsStrictCheck(files map[string]string) []string {
	var bad []string
	for name, src := range files {
		for _, retired := range luauCardsRetired {
			if strings.Contains(src, retired) {
				bad = append(bad, name+": retired tolerant surface "+retired)
			}
		}
	}
	if codec, ok := files["cards_codec.luau"]; ok {
		for _, call := range luauCardsStrictCalls {
			if !strings.Contains(codec, call) {
				bad = append(bad, "cards_codec.luau: missing strict call "+call)
			}
		}
	} else {
		bad = append(bad, "cards_codec.luau: missing")
	}
	if drv, ok := files["driver.luau"]; ok {
		const wantReportTail = `KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n`
		if !strings.Contains(drv, wantReportTail) {
			bad = append(bad, "driver.luau: missing strict report zero/false tail "+wantReportTail)
		}
	} else {
		bad = append(bad, "driver.luau: missing")
	}
	return bad
}

func luauCardsGenerated(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "cards")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return files
}

func luauCardsQuote(b []byte) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, v := range b {
		fmt.Fprintf(&out, "\\%03d", v)
	}
	out.WriteByte('"')
	return out.String()
}

func runLuauCardsSurface(t *testing.T, generated map[string][]byte, facade map[string]string, manifestRel, surface string) (map[string][]byte, [][]string, error) {
	t.Helper()
	f, err := os.Open(filepath.Join(root, manifestRel))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var rows [][]string
	files := map[string][]byte{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") || fields[0] != surface {
			continue
		}
		if len(fields) < 3 {
			return nil, nil, fmt.Errorf("short manifest row: %s", sc.Text())
		}
		rows = append(rows, fields)
		if fields[2] != "-" {
			b, err := os.ReadFile(filepath.Join(root, fields[2]))
			if err != nil {
				return nil, nil, err
			}
			files[fields[2]] = b
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}

	stage := t.TempDir()
	for name, b := range generated {
		if err := os.WriteFile(filepath.Join(stage, name), b, 0o600); err != nil {
			return nil, nil, err
		}
	}
	for name, src := range facade {
		if err := os.WriteFile(filepath.Join(stage, name), []byte(src), 0o600); err != nil {
			return nil, nil, err
		}
	}

	var input strings.Builder
	fmt.Fprintf(&input, "return {surface=%s,rows={\n", luauCardsQuote([]byte(surface)))
	for _, row := range rows {
		input.WriteByte('{')
		for _, field := range row {
			input.WriteString(luauCardsQuote([]byte(field)))
			input.WriteByte(',')
		}
		input.WriteString("},\n")
	}
	input.WriteString("},files={\n")
	for p, b := range files {
		fmt.Fprintf(&input, "[%s]=%s,\n", luauCardsQuote([]byte(p)), luauCardsQuote(b))
	}
	input.WriteString("}}\n")
	if err := os.WriteFile(filepath.Join(stage, "inputs.luau"), []byte(input.String()), 0o600); err != nil {
		return nil, nil, err
	}

	luauBin, why := luaurun.Interpreter()
	if luauBin == "" {
		t.Fatalf("luaurun: %s", why)
	}
	cmd := exec.Command(luauBin, "driver.luau")
	cmd.Dir = stage
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, rows, fmt.Errorf("luau driver.luau (%s): %w\n%s%s", surface, err, stdout.String(), stderr.String())
	}

	outputs := map[string][]byte{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 || fields[0] != "FILE" {
			return nil, rows, fmt.Errorf("unexpected Luau output %q", line)
		}
		name := strings.TrimPrefix(fields[1], "/")
		b, err := hex.DecodeString(fields[2])
		if err != nil {
			return nil, rows, err
		}
		outputs[name] = b
	}
	return outputs, rows, nil
}

func checkLuauCardsSurfaceExpected(surface string, rows [][]string, outputs map[string][]byte) error {
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
			got, ok := outputs[o]
			if !ok {
				return fmt.Errorf("%s/%s: driver wrote nothing", surface, o)
			}
			if !bytes.Equal(got, want) {
				return fmt.Errorf("%s/%s: output differs from expected", surface, o)
			}
		}
	}
	return nil
}

func TestLuauCardsConformanceStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and luau-analyze")
	facade := luauCardsSources(t)
	if bad := luauCardsStrictCheck(facade); len(bad) != 0 {
		t.Fatalf("Luau cards sources are not on the strict read:\n%s", strings.Join(bad, "\n"))
	}

	generated := luauCardsGenerated(t)
	stage := t.TempDir()
	for name, b := range generated {
		if err := os.WriteFile(filepath.Join(stage, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, src := range facade {
		if err := os.WriteFile(filepath.Join(stage, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stage, "inputs.luau"), []byte("return {surface=\"unit\",rows={},files={}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	luaurun.Analyze(t, stage, "cards.luau", "cards_codec.luau", "cards_test.luau", "driver.luau")

	unitOut, _, err := runLuauCardsSurface(t, generated, facade, filepath.Join("tables", "cards", "luau", "UNIT_INPUTS.txt"), "unit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unitOut["unit.txt"]), "0 failed") {
		t.Fatalf("unit tests failed: %q", string(unitOut["unit.txt"]))
	}

	manifestRel := filepath.Join("tables", "cards", "conformance", "MANIFEST.txt")
	for _, surface := range luauCardsSurfaces {
		outputs, rows, err := runLuauCardsSurface(t, generated, facade, manifestRel, surface)
		if err != nil {
			t.Fatalf("surface %s: %v", surface, err)
		}
		if err := checkLuauCardsSurfaceExpected(surface, rows, outputs); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLuauCardsConformanceStrictNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and luau-analyze")
	sabotages := []struct {
		name, file, from, to string
	}{
		{"wire load", "cards_codec.luau", `L.load("Card",`, `L.loadTolerant("Card",`},
		{"JSON read", "cards_codec.luau", `J.FromJson("Card",`, `J.FromJsonTolerant("Card",`},
		{"retaining load", "cards_codec.luau", `R.LoadRetain("Card",`, `R.LoadRetainTolerant("Card",`},
		{"retired report field", "cards.luau", `report.unknown > 0`, `report.unknown > 0 or report.kindMismatch > 0`},
		{"driver report tail", "driver.luau", `KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n`, `KindMismatch=%d\nClamped=%d\nDuplicate=%d\nMalformed=%s\n`},
	}
	for _, s := range sabotages {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			files := luauCardsSources(t)
			src, ok := files[s.file]
			if !ok || !strings.Contains(src, s.from) {
				t.Fatalf("the %s sabotage plants nothing: %q not in the real %s", s.name, s.from, s.file)
			}
			files[s.file] = strings.Replace(src, s.from, s.to, 1)
			if bad := luauCardsStrictCheck(files); len(bad) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left the strict check green", s.name)
			}
		})
	}

	generated := luauCardsGenerated(t)
	manifestRel := filepath.Join("tables", "cards", "conformance", "MANIFEST.txt")
	livePlants := []struct {
		surface, from, to string
	}{
		{"render", `key .. ": " .. value .. "\n"`, `key .. "=" .. value .. "\n"`},
		{"upgrade", `codec.SaveRetain(root, retain, report)`, `codec.Save(root)`},
		{"taskhash", `local function frame(text) return le64(#text) .. text end`, `local function frame(text) return text end`},
	}
	for _, p := range livePlants {
		t.Run("conformance-"+p.surface, func(t *testing.T) {
			t.Parallel()
			facade := luauCardsSources(t)
			src := facade["cards.luau"]
			if strings.Count(src, p.from) != 1 {
				t.Fatalf("sabotage anchor %q in cards.luau occurred %d times, want 1", p.from, strings.Count(src, p.from))
			}
			facade["cards.luau"] = strings.Replace(src, p.from, p.to, 1)
			outputs, rows, err := runLuauCardsSurface(t, generated, facade, manifestRel, p.surface)
			if err == nil && checkLuauCardsSurfaceExpected(p.surface, rows, outputs) == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s sabotage in cards.luau left conformance green", p.surface)
			}
		})
	}
}

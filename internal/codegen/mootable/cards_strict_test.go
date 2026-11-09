package mootable_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooCardsReadThenWrite pins the moo cards facade and driver
// (tables/cards/moo) to the strict read (docs/SPEC-TABLES.md §4, §6.5) and
// runs moo-analyze, the native unit tests, and all nine card conformance
// surfaces (load, save, render, parse, bounds, upgrade, edit, taskhash, view)
// on read-then-write against tables/cards/conformance/expected/.
//
// TestMooCardsReadThenWriteNegativeControl plants retired tolerant entry
// points, retired report counters, and real facade sabotages into copies of the
// real tables/cards/moo/*.moo artifacts and requires each check to go red.

var mooCardsRetired = []string{
	"loadTolerant",
	"FromJsonTolerant",
	"fromJsonTolerant",
	"LoadRetainTolerant",
	"loadWalk",
	"fromJsonWalk",
	"loadRetainWalk",
	"tolerant",
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

var mooCardsStrictCalls = []string{
	`L.ReadStrict("Card",`,
	`J.fromJson(T.CardDescriptor,`,
	`R.LoadRetain("Card",`,
}

func mooCardsSources(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(root, "tables", "cards", "moo")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".moo" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(b)
	}
	if len(files) == 0 {
		t.Fatal("no moo cards sources found")
	}
	return files
}

func mooCardsStrictCheck(files map[string]string) []string {
	var bad []string
	for name, src := range files {
		for _, r := range mooCardsRetired {
			if strings.Contains(src, r) {
				bad = append(bad, fmt.Sprintf("%s: retired tolerant surface %q", name, r))
			}
		}
	}
	if src, ok := files["cards_codec.moo"]; ok {
		for _, call := range mooCardsStrictCalls {
			if !strings.Contains(src, call) {
				bad = append(bad, fmt.Sprintf("cards_codec.moo: missing strict call %q", call))
			}
		}
	} else {
		bad = append(bad, "cards_codec.moo: missing")
	}
	if src, ok := files["driver.moo"]; ok {
		const strictReportTail = "KindMismatch=0\\nClamped=0\\nDuplicate=0\\nMalformed=false\\n"
		if !strings.Contains(src, strictReportTail) {
			bad = append(bad, "driver.moo: missing fixed strict report tail")
		}
	} else {
		bad = append(bad, "driver.moo: missing")
	}
	return bad
}

func mooCardsGenerated(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "cards")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	out, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mooCardsQuote(b []byte) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, v := range b {
		fmt.Fprintf(&out, "\\%03d", v)
	}
	out.WriteByte('"')
	return out.String()
}

func runMooCardsSurface(t *testing.T, generated map[string][]byte, facade map[string]string, manifestRel, surface string) (map[string][]byte, [][]string, error) {
	t.Helper()
	mooBin, why := moorun.Interpreter()
	if mooBin == "" {
		t.Fatalf("moorun: %s", why)
	}
	manifestPath := filepath.Join(root, manifestRel)
	f, err := os.Open(manifestPath)
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
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("no cases for %s", surface)
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
	fmt.Fprintf(&input, "return {surface=%s,rows={\n", mooCardsQuote([]byte(surface)))
	for _, row := range rows {
		input.WriteByte('{')
		for _, field := range row {
			input.WriteString(mooCardsQuote([]byte(field)))
			input.WriteByte(',')
		}
		input.WriteString("},\n")
	}
	input.WriteString("},files={\n")
	for path, b := range files {
		fmt.Fprintf(&input, "[%s]=%s,\n", mooCardsQuote([]byte(path)), mooCardsQuote(b))
	}
	input.WriteString("}}\n")
	if err := os.WriteFile(filepath.Join(stage, "inputs.moo"), []byte(input.String()), 0o600); err != nil {
		return nil, nil, err
	}

	cmd := exec.Command(mooBin, filepath.Join(stage, "driver.moo"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, rows, fmt.Errorf("moo: %w\n%s%s", err, stdout.String(), stderr.String())
	}

	outputs := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 || fields[0] != "FILE" {
			return nil, rows, fmt.Errorf("unexpected moo output %q", line)
		}
		name := strings.TrimPrefix(fields[1], "/")
		if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
			return nil, rows, fmt.Errorf("invalid output name %q", name)
		}
		b, err := hex.DecodeString(fields[2])
		if err != nil {
			return nil, rows, err
		}
		outputs[name] = b
	}
	return outputs, rows, nil
}

func checkMooCardsSurfaceExpected(surface string, rows [][]string, outputs map[string][]byte) error {
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

func TestMooCardsReadThenWrite(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted cards facade)")
	t.Parallel()
	facade := mooCardsSources(t)
	if bad := mooCardsStrictCheck(facade); len(bad) != 0 {
		t.Fatalf("moo cards sources are not on the strict read:\n%s", strings.Join(bad, "\n"))
	}

	generated := mooCardsGenerated(t)
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
	if err := os.WriteFile(filepath.Join(stage, "inputs.moo"), []byte("return {surface=\"unit\",rows={},files={}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := moorun.Analyze(t, stage, "cards.moo", "cards_codec.moo", "cards_test.moo", "driver.moo"); out != "" {
		t.Fatalf("moo-analyze was not silent:\n%s", out)
	}

	unitOut, _, err := runMooCardsSurface(t, generated, facade, filepath.Join("tables", "cards", "moo", "UNIT_INPUTS.txt"), "unit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unitOut["unit.txt"]), "0 failed") {
		t.Fatalf("unit tests failed: %q", string(unitOut["unit.txt"]))
	}

	manifestRel := filepath.Join("tables", "cards", "conformance", "MANIFEST.txt")
	for _, surface := range mooCardsSurfaces {
		outputs, rows, err := runMooCardsSurface(t, generated, facade, manifestRel, surface)
		if err != nil {
			t.Fatalf("surface %s: %v", surface, err)
		}
		if err := checkMooCardsSurfaceExpected(surface, rows, outputs); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMooCardsReadThenWriteNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted cards facade)")
	t.Parallel()
	sabotages := []struct {
		name, file, from, to string
	}{
		{"wire load", "cards_codec.moo", `L.ReadStrict("Card",`, `L.loadTolerant("Card",`},
		{"JSON read", "cards_codec.moo", `J.fromJson(T.CardDescriptor,`, `J.fromJsonTolerant(T.CardDescriptor,`},
		{"retaining load", "cards_codec.moo", `R.LoadRetain("Card",`, `R.LoadRetainTolerant("Card",`},
		{"retired report field", "cards.moo", `report.unknown > 0`, `report.unknown > 0 or report.kindChanged > 0`},
		{"driver report tail", "driver.moo", `KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n`, `KindMismatch=%d\nClamped=%d\nDuplicate=%d\nMalformed=%s\n`},
	}
	for _, s := range sabotages {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			files := mooCardsSources(t)
			src, ok := files[s.file]
			if !ok || !strings.Contains(src, s.from) {
				t.Fatalf("the %s sabotage plants nothing: %q not in the real %s", s.name, s.from, s.file)
			}
			files[s.file] = strings.Replace(src, s.from, s.to, 1)
			if bad := mooCardsStrictCheck(files); len(bad) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left the strict check green", s.name)
			}
		})
	}

	generated := mooCardsGenerated(t)
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
			facade := mooCardsSources(t)
			src := facade["cards.moo"]
			if strings.Count(src, p.from) != 1 {
				t.Fatalf("sabotage anchor %q in cards.moo occurred %d times, want 1", p.from, strings.Count(src, p.from))
			}
			facade["cards.moo"] = strings.Replace(src, p.from, p.to, 1)
			outputs, rows, err := runMooCardsSurface(t, generated, facade, manifestRel, p.surface)
			if err == nil && checkMooCardsSurfaceExpected(p.surface, rows, outputs) == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s sabotage in cards.moo left conformance green", p.surface)
			}
		})
	}
}

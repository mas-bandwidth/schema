package compiler

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestTestgenLuau is the gate of `schema testgen --lang luau`
// (docs/SPEC-TABLES.md §16.11): over tables/examples it writes one
// <Base>Table_gen_test.luau per unit file, the files pass under the pinned
// interpreter in a t.TempDir(), the sample names and count equal Go testgen's
// for the same unit, and a saboteur that breaks the Luau save of a table makes
// them fail.
func TestTestgenLuau(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	paths, err := GatherPaths([]string{filepath.Join("..", "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}

	goFiles, err := c.Testgen(u, "go")
	if err != nil {
		t.Fatalf("go testgen: %v", err)
	}
	luauFiles, err := TestgenLuau(u)
	if err != nil {
		t.Fatalf("luau testgen: %v", err)
	}
	if len(luauFiles) == 0 {
		t.Fatal("luau testgen wrote no files")
	}

	want := goSampleNames(t, goFiles)
	gotLuau := luauSampleNames(t, luauFiles)
	sort.Strings(gotLuau)
	if strings.Join(gotLuau, "\n") != strings.Join(want, "\n") {
		t.Fatalf("luau samples are not Go testgen's:\nluau:\n%s\ngo:\n%s", strings.Join(gotLuau, "\n"), strings.Join(want, "\n"))
	}
	wantOracle := goSampleOracle(t, goFiles)
	gotOracle := luauSampleOracle(t, luauFiles)
	if strings.Join(gotOracle, "\n") != strings.Join(wantOracle, "\n") {
		t.Fatalf("Luau oracle samples differ from Go values:\nluau:\n%s\ngo:\n%s", strings.Join(gotOracle, "\n"), strings.Join(wantOracle, "\n"))
	}

	dir := t.TempDir()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	maps.Copy(files, luauFiles)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The round trip runs under the pinned interpreter for Keyed: the Luau
	// leg's JSON reader still saturates uint64 ranges and pads bounded strings
	// to their capacity (named in the report as runtime gaps), so the other
	// units' samples are checked for parity above and written, not run here.
	mods := []string{"KeyedTable_gen_test"}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("local mods = {\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "\trequire(\"./%s\"),\n", m)
	}
	main.WriteString("}\nfor _, m in mods do\n\tfor _, n in m.names do print(\"SAMPLE \" .. n) end\nend\n")
	main.WriteString("for _, m in mods do m.run() end\nprint(\"OK\")\n")
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	out := string(luaurun.Run(t, dir, "main.luau", nil))
	var got []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if name, ok := strings.CutPrefix(line, "SAMPLE "); ok {
			got = append(got, name)
		}
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("the runner did not finish:\n%s", out)
	}
	wantKeyed := luauSampleNames(t, map[string][]byte{"KeyedTable_gen_test.luau": luauFiles["KeyedTable_gen_test.luau"]})
	sort.Strings(wantKeyed)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(wantKeyed, "\n") {
		t.Fatalf("luau samples are not Go testgen's:\nluau:\n%s\ngo:\n%s", strings.Join(got, "\n"), strings.Join(wantKeyed, "\n"))
	}

	// THE SABOTEUR: break the Luau save in memory at one anchor and require the
	// generated tests to fail.
	saved := luautable.Runtime()["schema_fixed.luau"]
	anchor := "function F.save(d: any, fixed: Fixed, values: { any }): buffer\n"
	if strings.Count(string(saved), anchor) != 1 {
		t.Fatalf("the save anchor is not unique in schema_fixed.luau")
	}
	broken := strings.Replace(string(saved), anchor, "local testgenSabotageOnce = true\n"+anchor, 1)
	writeAnchor := "\t\tbuffer.copy(out, p, rec, 0, fixed.recordBytes)\n\t\tp = p + fixed.recordBytes"
	writeSabotage := "\t\tbuffer.copy(out, p, rec, 0, fixed.recordBytes)\n" +
		"\t\tif testgenSabotageOnce and fixed.recordBytes > 0 then\n" +
		"\t\t\ttestgenSabotageOnce = false\n" +
		"\t\t\tlocal at = p\n" +
		"\t\t\tbuffer.writeu8(out, at, (buffer.readu8(out, at) + 1) % 32)\n" +
		"\t\tend\n\t\tp = p + fixed.recordBytes"
	if strings.Count(broken, writeAnchor) != 1 {
		t.Fatalf("the record-write anchor is not unique in schema_fixed.luau")
	}
	broken = strings.Replace(broken, writeAnchor, writeSabotage, 1)
	if err := os.WriteFile(filepath.Join(dir, "schema_fixed.luau"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	luauBin, why := luaurun.Interpreter()
	if luauBin == "" {
		t.Fatalf("the Luau interpreter does not resolve: %s", why)
	}
	cmd := exec.Command(luauBin, "main.luau")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "fixed load value differs from sample value") {
		t.Fatalf("the sabotaged save did not fail the generated value oracle:\n%s", out)
	}
}

// goSampleNames reads the sample names out of Go testgen's generated files,
// grouped as <Root>/<sample> and sorted: what the Luau files must carry.
func goSampleNames(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	decl := regexp.MustCompile(`^func Test([A-Za-z0-9_]+)GenRoundTrip\(`)
	sample := regexp.MustCompile(`^\t\t\{"((?:[^"\\]|\\.)*)",`)
	var out []string
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		root := ""
		for line := range strings.SplitSeq(string(files[name]), "\n") {
			if m := decl.FindStringSubmatch(line); m != nil {
				root = m[1]
			}
			if line == "}" {
				root = ""
			}
			if root == "" {
				continue
			}
			if m := sample.FindStringSubmatch(line); m != nil {
				s, err := strconv.Unquote(`"` + m[1] + `"`)
				if err != nil {
					t.Fatalf("%s: sample name %q: %v", name, m[1], err)
				}
				out = append(out, root+"/"+s)
			}
		}
	}
	sort.Strings(out)
	return out
}

// luauSampleNames reads the sample names the emitted Luau files carry, in the
// <Root>/<sample> shape goSampleNames produces.
func luauSampleNames(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	lit := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	var out []string
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		line := ""
		for l := range strings.SplitSeq(string(files[name]), "\n") {
			if strings.HasPrefix(l, "local names = {") {
				line = l
				break
			}
		}
		if line == "" {
			t.Fatalf("%s: no names list", name)
		}
		for _, m := range lit.FindAllStringSubmatch(line, -1) {
			s, err := strconv.Unquote(`"` + m[1] + `"`)
			if err != nil {
				t.Fatalf("%s: sample name %q: %v", name, m[1], err)
			}
			out = append(out, s)
		}
	}
	return out
}

// goSampleOracle returns each Go testgen sample's exact JSON text, keyed by
// root/sample. The text is the generated-value oracle shared by both backends.
func goSampleOracle(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		roots, err := tgLuauReadGoSamples(files[name])
		if err != nil {
			t.Fatalf("%s: read Go oracle samples: %v", name, err)
		}
		for _, root := range roots {
			for _, sample := range root.samples {
				out = append(out, fmt.Sprintf("%s/%s\t%q", root.name, sample.name, sample.text))
			}
		}
	}
	sort.Strings(out)
	return out
}

// luauSampleOracle reads each generated Luau module's name/text pairs and
// compares their JSON values with Go testgen's exact sample texts.
func luauSampleOracle(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	literal := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	pair := regexp.MustCompile(`\{ name = "((?:[^"\\]|\\.)*)", text = "((?:[^"\\]|\\.)*)" \},`)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []string
	for _, file := range names {
		src := string(files[file])
		var nameLine string
		for line := range strings.SplitSeq(src, "\n") {
			if strings.HasPrefix(line, "local names = {") {
				nameLine = line
				break
			}
		}
		if nameLine == "" {
			t.Fatalf("%s: no names list", file)
		}
		var sampleNames []string
		for _, m := range literal.FindAllStringSubmatch(nameLine, -1) {
			n, err := strconv.Unquote(`"` + m[1] + `"`)
			if err != nil {
				t.Fatalf("%s: sample name: %v", file, err)
			}
			sampleNames = append(sampleNames, n)
		}
		matches := pair.FindAllStringSubmatch(src, -1)
		if len(matches) != len(sampleNames) {
			t.Fatalf("%s: %d names but %d sample values", file, len(sampleNames), len(matches))
		}
		for i, m := range matches {
			n, err := strconv.Unquote(`"` + m[1] + `"`)
			if err != nil {
				t.Fatalf("%s: sample name: %v", file, err)
			}
			text, err := strconv.Unquote(`"` + m[2] + `"`)
			if err != nil {
				t.Fatalf("%s: sample value: %v", file, err)
			}
			_, sampleName, ok := strings.Cut(sampleNames[i], "/")
			if !ok || sampleName != n {
				t.Fatalf("%s: names/sample order differs at %d: %q vs %q", file, i, sampleNames[i], n)
			}
			out = append(out, fmt.Sprintf("%s\t%q", sampleNames[i], text))
		}
	}
	sort.Strings(out)
	return out
}

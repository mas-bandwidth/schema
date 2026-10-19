package compiler

import (
	"fmt"
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
)

// TestTestgenLuau is the gate of `schema testgen --lang luau`
// (docs/SPEC-TABLES.md §16.11): over tables/examples it writes one
// <Base>Table_gen_test.luau per unit file, the files pass under the pinned
// interpreter in a t.TempDir(), the sample names and count equal Go testgen's
// for the same unit, and a saboteur that breaks the Luau save of a table makes
// them fail.
func TestTestgenLuau(t *testing.T) {
	t.Parallel()
	paths, err := GatherPaths([]string{filepath.Join("..", "tables", "examples", "Keyed.schema")})
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
	dir := t.TempDir()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	for name, data := range luauFiles {
		files[name] = data
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var mods []string
	for name := range luauFiles {
		mods = append(mods, strings.TrimSuffix(name, ".luau"))
	}
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
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if name, ok := strings.CutPrefix(line, "SAMPLE "); ok {
			got = append(got, name)
		}
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("the runner did not finish:\n%s", out)
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("luau samples are not Go testgen's:\nluau:\n%s\ngo:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// THE SABOTEUR: break the Luau save in memory at one anchor and require the
	// generated tests to fail.
	saved := luautable.Runtime()["fixed.luau"]
	anchor := "function F.save(d: any, fixed: Fixed, values: { any }): buffer\n"
	if strings.Count(string(saved), anchor) != 1 {
		t.Fatalf("the save anchor is not unique in fixed.luau")
	}
	broken := strings.Replace(string(saved), anchor, anchor+"\terror(\"sabotaged\", 0)\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "fixed.luau"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	luauBin, why := luaurun.Interpreter()
	if luauBin == "" {
		t.Fatalf("the Luau interpreter does not resolve: %s", why)
	}
	cmd := exec.Command(luauBin, "main.luau")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the sabotaged save did not make the generated tests fail:\n%s", out)
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
		for _, line := range strings.Split(string(files[name]), "\n") {
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

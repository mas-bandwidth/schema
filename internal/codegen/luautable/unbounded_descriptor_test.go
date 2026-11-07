package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauUnboundedDescriptorIsExplicit holds docs/SPEC-TABLES.md §8.1 in the
// Luau table surface: a container with no declared bound carries the explicit
// `unbounded = true` fact with `bound = 0` beside it, and nothing else does,
// so a reflection reader can tell "no bound" from a bound of 2^31 - 1.
//
// It generates one unit with map[string(8)]int32, map[int32]int32, []int32 and
// [..4]int32, loads the generated descriptor under the pinned interpreter and
// prints each field's unbounded/bound pair, and holds that pair to the same
// field's UnboundedContainer/ArrayBound in the generated Go TableFieldInfo.
// Every generated module passes luau-analyze.
func TestLuauUnboundedDescriptorIsExplicit(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Root
{
    tags map[string(8)]int32
    ints map[int32]int32
    nums []int32
    quad [..4]int32
}
`
	dir := t.TempDir()
	src := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(src, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}

	// the Go answer to hold the Luau descriptor to (docs/SPEC-TABLES.md §8.1)
	goFiles, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("the Go table surface: %v", err)
	}
	want := goUnboundedFacts(t, goFiles, "Root")
	if len(want) != 4 {
		t.Fatalf("the Go descriptor names %d fields, want 4", len(want))
	}

	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("the Luau type surface: %v", err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("the Luau table surface: %v", err)
	}
	maps.Copy(files, tables)
	mod := ""
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			mod = strings.TrimSuffix(name, ".luau")
		}
	}
	if mod == "" {
		t.Fatal("the unit emitted no table module")
	}
	main := fmt.Sprintf(`--!strict
local T = require("./%s")
local d = T.RootDescriptor
for _, f in d.fields do
	print(f.name .. " " .. tostring(f.unbounded == true) .. " " .. tostring(f.bound))
end
`, mod)
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	seen := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		var name string
		var unbounded bool
		var bound string
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "%s %t %s", &name, &unbounded, &bound); err != nil {
			t.Fatalf("the probe printed %q: %v", line, err)
		}
		w, ok := want[name]
		if !ok {
			t.Errorf("the Luau descriptor names field %q, which the Go descriptor does not", name)
			continue
		}
		seen[name] = true
		gotBound, err := strconv.ParseInt(bound, 10, 32)
		if err != nil {
			t.Fatalf("field %s: bound %q: %v", name, bound, err)
		}
		if unbounded != w.unbounded || int32(gotBound) != w.bound {
			t.Errorf("field %s: the Luau descriptor reports unbounded=%v bound=%d; the generated Go TableFieldInfo reports UnboundedContainer=%v ArrayBound=%d",
				name, unbounded, gotBound, w.unbounded, w.bound)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("the Luau descriptor does not report field %s", name)
		}
	}
	var analyze []string
	for name := range files {
		analyze = append(analyze, name)
	}
	sort.Strings(analyze)
	luaurun.Analyze(t, dir, analyze...)
}

type unboundedFacts struct {
	unbounded bool
	bound     int32
}

var (
	goFieldName    = regexp.MustCompile(`^"([^"]*)"`)
	goArrayBound   = regexp.MustCompile(`ArrayBound: (\d+),`)
	goUnboundedTag = regexp.MustCompile(`UnboundedContainer:\s*true,`)
)

// goUnboundedFacts reads the generated Go TableFieldInfo rows for one table:
// per field, its UnboundedContainer and ArrayBound columns (§8.1).
func goUnboundedFacts(t *testing.T, files map[string][]byte, table string) map[string]unboundedFacts {
	t.Helper()
	marker := "var " + table + "TableFields = []TableFieldInfo{"
	var src string
	for _, data := range files {
		if strings.Contains(string(data), marker) {
			src = string(data)
			break
		}
	}
	if src == "" {
		t.Fatalf("the generated Go has no %q", marker)
	}
	_, body, _ := strings.Cut(src, marker)
	out := map[string]unboundedFacts{}
	for block := range strings.SplitSeq(body, "\n\t{Name: ") {
		m := goFieldName.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		b := goArrayBound.FindStringSubmatch(block)
		if b == nil {
			t.Fatalf("field %s: the generated Go has no ArrayBound", m[1])
		}
		bound, err := strconv.ParseInt(b[1], 10, 32)
		if err != nil {
			t.Fatalf("field %s: ArrayBound %q: %v", m[1], b[1], err)
		}
		out[m[1]] = unboundedFacts{unbounded: goUnboundedTag.MatchString(block), bound: int32(bound)}
	}
	return out
}

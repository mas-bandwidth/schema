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
	"github.com/mas-bandwidth/schema/v2/ir"
)

const unboundedProbeSchema = `package probe

table Root
{
    tags map[string(8)]int32
    ints map[int32]int32
    nums []int32
    quad [..4]int32
}
`

func buildUnboundedProbeFiles(t *testing.T) (*ir.Unit, map[string]unboundedFacts, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(src, []byte(unboundedProbeSchema), 0o644); err != nil {
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
	return u, want, files
}

func verifyLuauUnboundedDescriptor(t *testing.T, want map[string]unboundedFacts, files map[string][]byte) error {
	t.Helper()
	dir := t.TempDir()
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
		return fmt.Errorf("the unit emitted no table module")
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
			return fmt.Errorf("the probe printed %q: %w", line, err)
		}
		w, ok := want[name]
		if !ok {
			return fmt.Errorf("the Luau descriptor names field %q, which the Go descriptor does not", name)
		}
		seen[name] = true
		gotBound, err := strconv.ParseInt(bound, 10, 32)
		if err != nil {
			return fmt.Errorf("field %s: bound %q: %w", name, bound, err)
		}
		if unbounded != w.unbounded || int32(gotBound) != w.bound {
			return fmt.Errorf("field %s: the Luau descriptor reports unbounded=%v bound=%d; the generated Go TableFieldInfo reports UnboundedContainer=%v ArrayBound=%d",
				name, unbounded, gotBound, w.unbounded, w.bound)
		}
	}
	for name := range want {
		if !seen[name] {
			return fmt.Errorf("the Luau descriptor does not report field %s", name)
		}
	}
	return nil
}

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
	_, want, files := buildUnboundedProbeFiles(t)
	if err := verifyLuauUnboundedDescriptor(t, want, files); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var analyze []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		analyze = append(analyze, name)
	}
	sort.Strings(analyze)
	luaurun.Analyze(t, dir, analyze...)
}

// TestLuauUnboundedDescriptorNegativeControl mutates the unbounded/bound
// columns in a broken twin of probeTable.luau and requires
// verifyLuauUnboundedDescriptor to fail against the Go TableFieldInfo
// reference.
func TestLuauUnboundedDescriptorNegativeControl(t *testing.T) {
	t.Parallel()
	_, want, baseFiles := buildUnboundedProbeFiles(t)

	controls := []struct {
		name   string
		from   string
		to     string
		expect string
	}{
		{
			name:   "drop-list-unbounded",
			from:   `s = "arr", array = "list", bound = 0, min = 0, ek = 4, unbounded = true`,
			to:     `s = "arr", array = "list", bound = 0, min = 0, ek = 4`,
			expect: "field nums: the Luau descriptor reports unbounded=false bound=0; the generated Go TableFieldInfo reports UnboundedContainer=true ArrayBound=0",
		},
		{
			name:   "nonzero-map-bound",
			from:   `s = "map", t = "RootTagsEntry", ek = 13, bound = 0, unbounded = true`,
			to:     `s = "map", t = "RootTagsEntry", ek = 13, bound = 8, unbounded = true`,
			expect: "field tags: the Luau descriptor reports unbounded=true bound=8; the generated Go TableFieldInfo reports UnboundedContainer=true ArrayBound=0",
		},
		{
			name:   "spurious-bounded-array-unbounded",
			from:   `s = "arr", array = "counted", bound = 4, min = 0, ek = 4`,
			to:     `s = "arr", array = "counted", bound = 4, min = 0, ek = 4, unbounded = true`,
			expect: "field quad: the Luau descriptor reports unbounded=true bound=4; the generated Go TableFieldInfo reports UnboundedContainer=false ArrayBound=4",
		},
	}

	for _, ctl := range controls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			files := maps.Clone(baseFiles)
			src := string(files["probeTable.luau"])
			if strings.Count(src, ctl.from) != 1 {
				t.Fatalf("probeTable.luau carries %q %d times, want 1", ctl.from, strings.Count(src, ctl.from))
			}
			files["probeTable.luau"] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			err := verifyLuauUnboundedDescriptor(t, want, files)
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): broken probeTable.luau passed verifyLuauUnboundedDescriptor", ctl.name)
			}
			if !strings.Contains(err.Error(), ctl.expect) {
				t.Fatalf("NEGATIVE CONTROL (%s) failed with %q, want substring %q", ctl.name, err.Error(), ctl.expect)
			}
			t.Logf("red as required (%s): %v", ctl.name, err)
		})
	}
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

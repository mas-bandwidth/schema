package luautable_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const viewProbeSource = `package viewprobe

enum Hue
{
    Red
    Green
}

flags Caps
{
    Read
    Write
}

type Reached
{
    Value uint8
}

type Unreached
{
    Count uint16
    Cap   Caps
}

type Arm
{
    Amount int32
}

union Choice
{
    arm Arm
}

table Root
{
    reached Reached
}
`

func buildViewProbeFiles(t *testing.T) (*ir.Unit, map[string][]byte, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ViewProbe.schema")
	if err := os.WriteFile(path, []byte(viewProbeSource), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		if files[name] != nil {
			t.Fatalf("file %s emitted twice", name)
		}
		files[name] = data
	}
	return u, files, tables
}

func verifyLuauEveryTypeHasAView(t *testing.T, u *ir.Unit, files map[string][]byte) error {
	t.Helper()
	const view = "ViewProbeView.luau"
	if files[view] == nil {
		return fmt.Errorf("no view module %s: every type has a view (docs/SPEC-TABLES.md §8.2)", view)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	viewNames := declaredViewNames(u)
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(viewProbe(viewNames)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	lines := strings.Split(out, "\n")
	got := map[string]bool{}
	var structs []string
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "CLAUSE "); ok {
			return fmt.Errorf("view clause violation: %s", after)
		}
		fields := strings.Split(line, " ")
		if len(fields) != 2 {
			return fmt.Errorf("the probe printed %q", line)
		}
		got[fields[0]] = true
		if fields[1] == "type" {
			structs = append(structs, fields[0])
		}
	}
	for _, name := range viewNames {
		if !got[name] {
			return fmt.Errorf("%s has no descriptor through %s (docs/SPEC-TABLES.md §8.2)", name, view)
		}
	}
	sort.Strings(structs)
	goTypes, err := goViewTypes(u)
	if err != nil {
		return err
	}
	if strings.Join(structs, ",") != strings.Join(goTypes, ",") {
		return fmt.Errorf("the view's struct descriptors %v are not the Go view's type list %v", structs, goTypes)
	}
	return nil
}

// TestLuauEveryTypeHasAView holds docs/SPEC-TABLES.md §8.2 for the Luau leg:
// every type in the unit has a descriptor, a type no table reaches included,
// in a view module beside the table surface (§8.5). The table module's bytes
// are pinned to the BASE digest, so carrying the view changes nothing a game
// links; the view's struct descriptors are the type list the generated Go
// view carries; and every generated module passes luau-analyze.
func TestLuauEveryTypeHasAView(t *testing.T) {
	t.Parallel()
	u, files, tables := buildViewProbeFiles(t)

	tableModule := tables["ViewProbeTable.luau"]
	if tableModule == nil {
		t.Fatal("no table module")
	}
	sum := sha256.Sum256(tableModule)
	gotDigest := hex.EncodeToString(sum[:])
	if gotDigest != wantTableDigest {
		t.Errorf("the table module's bytes changed from BASE: got %s, want %s", gotDigest, wantTableDigest)
	}

	if err := verifyLuauEveryTypeHasAView(t, u, files); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	var analyze []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".luau") {
			analyze = append(analyze, name)
		}
	}
	sort.Strings(analyze)
	luaurun.Analyze(t, dir, analyze...)
}

// TestLuauEveryTypeHasAViewNegativeControl breaks one §8.2 clause at a time in
// ViewProbeView.luau and requires verifyLuauEveryTypeHasAView to fail on that
// clause.
func TestLuauEveryTypeHasAViewNegativeControl(t *testing.T) {
	t.Parallel()
	u, baseFiles, _ := buildViewProbeFiles(t)

	controls := []struct {
		name   string
		from   string
		to     string
		expect string
	}{
		{
			name:   "drop-unreached-type",
			from:   `name = "Unreached"`,
			to:     `name = "MissingUnreached"`,
			expect: "Unreached has no descriptor through ViewProbeView.luau",
		},
		{
			name:   "outside-nonzero-id",
			from:   "name = \"Unreached\",\n\tid = 0i,",
			to:     "name = \"Unreached\",\n\tid = 1i,",
			expect: "Unreached.id=1",
		},
		{
			name:   "outside-field-json",
			from:   `{ id = 0i, name = "Count", slot = "Count", o = 0`,
			to:     `{ id = 0i, name = "Count", slot = "Count", json = "Count", o = 0`,
			expect: "Unreached.Count.json=Count",
		},
		{
			name:   "outside-enum-nonzero-id",
			from:   `variants = { { "Red", 0i, false }, { "Green", 0i, false } }`,
			to:     `variants = { { "Red", 1i, false }, { "Green", 0i, false } }`,
			expect: "Hue.Red.id=1",
		},
	}

	for _, ctl := range controls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			files := maps.Clone(baseFiles)
			src := string(files["ViewProbeView.luau"])
			if strings.Count(src, ctl.from) != 1 {
				t.Fatalf("ViewProbeView.luau carries %q %d times, want 1", ctl.from, strings.Count(src, ctl.from))
			}
			files["ViewProbeView.luau"] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			err := verifyLuauEveryTypeHasAView(t, u, files)
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): broken ViewProbeView.luau passed verifyLuauEveryTypeHasAView", ctl.name)
			}
			if !strings.Contains(err.Error(), ctl.expect) {
				t.Fatalf("NEGATIVE CONTROL (%s) failed with %q, want substring %q", ctl.name, err.Error(), ctl.expect)
			}
			t.Logf("red as required (%s): %v", ctl.name, err)
		})
	}
}

// wantTableDigest is ViewProbeTable.luau's sha256 at BASE. The view is a leaf:
// adding it may not change the table module a game links (docs/SPEC-TABLES.md
// §8.5). Moved when the table module began registering the unit's wire ids
// (retainids.go, docs/SPEC-TABLES.md §6.6), a change to the module itself.
const wantTableDigest = "41f89eb6bd8e2e89b396b4cee5d4391ac3f406c16b325f7193e587a53d3e68be"

// declaredViewNames is every `type`, `union`, `enum` and `flags` the unit
// declares, the set §8.2 says every one of has a view.
func declaredViewNames(u *ir.Unit) []string {
	var names []string
	for name := range u.Structs {
		names = append(names, name)
	}
	for name := range u.Tables {
		names = append(names, name)
	}
	for name := range u.Unions {
		names = append(names, name)
	}
	for name := range u.Enums {
		names = append(names, name)
	}
	for name := range u.Flags {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// viewProbe loads the table modules and the view, collects every
// <Name>Descriptor, checks §8.2's outside-closure id=0 and json=nil clauses,
// and prints one line per descriptor: its name and whether it is a table, a
// type (a non-table field list) or another declaration (docs/SPEC-TABLES.md
// §8.1, §8.2).
func viewProbe(names []string) string {
	var b strings.Builder
	b.WriteString(`local seen: { [string]: string } = {}
local T = require("./ViewProbeTable")
local V = require("./ViewProbeView")
local function collect(m: any, outside: boolean)
  for k, v in m do
    if type(k) == "string" and string.sub(k, -10) == "Descriptor" and type(v) == "table" and type(v.name) == "string" then
      local kind = "other"
      if v.fields ~= nil and v.table == true then
        kind = "table"
      elseif v.fields ~= nil then
        kind = "type"
      end
      seen[v.name] = kind
      if not table.isfrozen(v) then
        print("CLAUSE " .. v.name .. ".unfrozen")
      end
      if outside and v.fields ~= nil then
        if v.id ~= 0i then
          print("CLAUSE " .. v.name .. ".id=" .. tostring(v.id))
        end
        for _, f in v.fields do
          if f.id ~= 0i then
            print("CLAUSE " .. v.name .. "." .. tostring(f.name) .. ".id=" .. tostring(f.id))
          end
          if f.json ~= nil then
            print("CLAUSE " .. v.name .. "." .. tostring(f.name) .. ".json=" .. tostring(f.json))
          end
        end
      elseif outside and v.arms ~= nil then
        for _, a in v.arms do
          if a.id ~= 0i then
            print("CLAUSE " .. v.name .. "." .. tostring(a.name) .. ".id=" .. tostring(a.id))
          end
          if a.json ~= nil then
            print("CLAUSE " .. v.name .. "." .. tostring(a.name) .. ".json=" .. tostring(a.json))
          end
        end
      elseif outside and v.kind == "enum" and v.variants ~= nil then
        for _, pair in v.variants do
          if pair[2] ~= 0i then
            print("CLAUSE " .. v.name .. "." .. tostring(pair[1]) .. ".id=" .. tostring(pair[2]))
          end
        end
      end
    end
  end
end
collect(T, false)
collect(V, true)
`)
	b.WriteString(`local names = {`)
	for i, name := range names {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q", name)
	}
	b.WriteString(`}
for _, name in ipairs(names) do
  local kind = seen[name]
  if kind ~= nil then print(name .. " " .. kind) end
end
`)
	return b.String()
}

// goViewTypes is the Type list of the generated Go view file: the type list
// the oracle (§8.2, §8.3) carries for this unit.
func goViewTypes(u *ir.Unit) ([]string, error) {
	files, err := gotable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range files {
		if !strings.HasSuffix(name, "View.go") {
			continue
		}
		text := string(data)
		start := strings.Index(text, "Types:[]ViewType{")
		if start < 0 {
			start = strings.Index(text, "Types: []ViewType{")
		}
		if start < 0 {
			return nil, fmt.Errorf("%s has no Types block", name)
		}
		end := strings.Index(text[start:], "NumTables:")
		if end < 0 {
			return nil, fmt.Errorf("%s has no Types block", name)
		}
		block := text[start : start+end]
		re := regexp.MustCompile(`\{Name: "([^"]+)"`)
		var names []string
		for _, m := range re.FindAllStringSubmatch(block, -1) {
			names = append(names, m[1])
		}
		sort.Strings(names)
		return names, nil
	}
	return nil, fmt.Errorf("gotable emitted no View.go")
}

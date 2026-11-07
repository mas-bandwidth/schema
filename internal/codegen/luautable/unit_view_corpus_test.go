package luautable_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// unitViewListingMain prints UnitView()'s listing, with the generated modules
// and nothing else on hand.
const unitViewListingMain = `--!strict
local view = require("./schema_view")
print((string.gsub(view.UnitView().listing(), "\n$", "")))
`

type luauViewUnit struct {
	key   string
	paths []string
}

// luauViewUnits is THE VIEW CORPUS (docs/SPEC-TABLES.md §8.7), read from the
// Makefile's VIEW_CORPUS as internal/viewlisting reads it, followed by every
// unit of testdata/conformance/tables/MANIFEST.txt.
func luauViewUnits(t *testing.T) []luauViewUnit {
	t.Helper()
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	entries := viewlisting.CorpusEntries(string(makefile))
	if len(entries) == 0 {
		t.Fatal("the Makefile declares no VIEW_CORPUS entries")
	}
	var out []luauViewUnit
	for _, entry := range entries {
		dir, _, ok := strings.Cut(entry, ":")
		if !ok {
			t.Fatalf("VIEW_CORPUS entry %q is not dir:package", entry)
		}
		source := filepath.Join(root, "tables", dir)
		if dir == "wide" {
			source = filepath.Join(root, "examples-wide")
		}
		paths, err := compiler.GatherPaths([]string{source})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, luauViewUnit{"corpus-" + dir, paths})
	}
	units, _ := readManifest(t)
	keys := make([]string, 0, len(units))
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		out = append(out, luauViewUnit{"manifest-" + key, paths})
	}
	return out
}

// luauViewFiles loads one unit and generates both Luau surfaces over it, the
// view included (GenerateView for a table-free unit).
func luauViewFiles(t *testing.T, paths []string) (*ir.Unit, map[string][]byte) {
	t.Helper()
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	more, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Tables) == 0 {
		if more, err = luautable.GenerateView(u); err != nil {
			t.Fatal(err)
		}
	}
	maps.Copy(files, more)
	return u, files
}

// luauListView runs the listing program over generated files.
func luauListView(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(unitViewListingMain), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(luaurun.Run(t, dir, "main.luau", nil))
}

// TestLuauUnitViewCorpus is the CORPUS GATE for the Luau registry
// (docs/SPEC-TABLES.md §8.3, §8.7). For every VIEW_CORPUS unit, and every unit
// of the tables conformance manifest, the listing UnitView() prints under the
// pinned interpreter is byte-identical to internal/viewlisting's listing of the
// unit's IR, the pin every other registry is held to: every declaration,
// field, variant, bit, arm and constant with its file, id, text key, type
// spelling, doc and tags, and a type no table reaches with no wire identity.
func TestLuauUnitViewCorpus(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the unit view's listing)")
	t.Parallel()
	units := luauViewUnits(t)
	for _, vu := range units {
		u, files := luauViewFiles(t, vu.paths)
		t.Run(vu.key, func(t *testing.T) {
			t.Parallel()
			pin := viewlisting.Listing(u)
			if got := luauListView(t, files); got != pin {
				t.Errorf("UnitView() listing differs from internal/viewlisting.\n%s", firstDifference(pin, got))
			}
		})
	}
	t.Logf("%d units listed from UnitView()", len(units))
}

// TestLuauUnitViewNegativeControl breaks each claim the corpus gate holds, one
// at a time in a copy of the generated text, and requires the gate RED on the
// line that claim owns (docs/SPEC-TABLES.md §8.7):
//
//   - unflattened: the listing writes a doc's newline verbatim, splitting the
//     exhibit's multi-line doc (tabledemo) across lines;
//   - drop: one declaration dropped from the registry's tables set;
//   - doc: one field's doc emptied, where the pin has the text;
//   - id: one field of a type no table reaches given a table-wire id.
func TestLuauUnitViewNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the unit view's listing)")
	t.Parallel()
	load := func(t *testing.T, dir string) (*ir.Unit, map[string][]byte) {
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, dir)})
		if err != nil {
			t.Fatal(err)
		}
		return luauViewFiles(t, paths)
	}
	patch := func(t *testing.T, files map[string][]byte, re *regexp.Regexp, repl func(string) string) {
		t.Helper()
		data := files["schema_view.luau"]
		loc := re.FindIndex(data)
		if loc == nil {
			t.Fatalf("the sabotage anchor %s is not in schema_view.luau", re)
		}
		t.Logf("sabotage: %.120s", data[loc[0]:loc[1]])
		files["schema_view.luau"] = []byte(string(data[:loc[0]]) + repl(string(data[loc[0]:loc[1]])) + string(data[loc[1]:]))
	}
	red := func(t *testing.T, u *ir.Unit, files map[string][]byte, expect string) {
		t.Helper()
		pin := viewlisting.Listing(u)
		got := luauListView(t, files)
		if got == pin {
			t.Fatal("NEGATIVE CONTROL FAILED: the sabotage left the listing equal to the pin")
		}
		diff := firstDifference(pin, got)
		if !strings.Contains(diff, expect) {
			t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on %q:\n%s", expect, diff)
		}
		t.Logf("red as required:\n%s", diff)
	}
	t.Run("unflattened", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		patch(t, files, regexp.MustCompile(regexp.QuoteMeta(`local out = string.gsub(escaped, "\n", "\\n")`)), func(string) string {
			return `local out = escaped`
		})
		red(t, u, files, `\n`)
	})
	t.Run("drop", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		patch(t, files, regexp.MustCompile(`\ttables = \{\n\t\t[^\n]*\n`), func(string) string {
			return "\ttables = {\n"
		})
		red(t, u, files, "table ")
	})
	t.Run("doc", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		patch(t, files, regexp.MustCompile(`,optional=(?:true|false),doc="(?:[^"\\\n]|\\.)+"`), func(s string) string {
			return s[:strings.Index(s, `,doc="`)] + `,doc=""`
		})
		red(t, u, files, "doc=")
	})
	t.Run("id", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "test/tables/CsView.schema")
		patch(t, files, regexp.MustCompile(`\{name="grade",json="-",type="[^"]*",id="0000000000000000"`), func(s string) string {
			return strings.Replace(s, `id="0000000000000000"`, `id="0100000000000000"`, 1)
		})
		red(t, u, files, "Orphan field grade")
	})
}

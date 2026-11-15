package squirreltable_test

// Functional test (runs the pinned Squirrel interpreter through sqrun.Run):
// saving a FRESH instance of every root in one schema writes the Go engine's
// bytes (docs/SPEC-TABLES.md §3, §4). Writers elide what readers default, so
// a plain int32 at its declared default (`0`, or a declared nonzero value)
// rides on nothing; a `| required` int32 rides even while it holds `0`; an
// absent optional and an inactive union arm write nothing.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const freshDefaultsSchema = `package probe

union Choice
{
    count int32
}

table Plain
{
    zero int32
    five int32 = 5
    flag bool
}

table Required
{
    zero int32 | required
}

table Extra
{
    maybe ?int32
    choice Choice
}
`

// TestSquirrelSaveMatchesTheEngineOnFreshDefaults holds every root of
// freshDefaultsSchema to the Go engine's bytes when the value is built fresh
// on each side (docs/SPEC-TABLES.md §3). The all-default root is exactly the
// form byte, its terminator and an empty id table, so the plain int32 is
// elided; the required root carries its zero; the optional and the union arm
// are absent and write nothing. The negative control patches the emitted save
// so a scalar default always rides, and the comparison must turn on the
// all-default root, so the equality is load-bearing.
func TestSquirrelSaveMatchesTheEngineOnFreshDefaults(t *testing.T) {
	t.Parallel()
	unit := unitFromSchema(t, freshDefaultsSchema)
	m := tabletext.NewModel(unit)
	roots := m.Roots()

	want := map[string]string{}
	for _, root := range roots {
		st := m.Lookup(root)
		if st == nil || !st.IsTable || st.IsMapEntry() {
			continue
		}
		wire, err := tablewire.Encode(m, m.New(st))
		if err != nil {
			t.Fatalf("%s: the Go engine refuses a fresh instance: %v", root, err)
		}
		want[root] = hex.EncodeToString(wire)
	}
	// The canonical all-default file: form 1, the root's zero terminator and
	// an id table with no entries. The plain int32, the declared nonzero
	// default and the bool all elide (§3).
	const allDefault = "01000000000000000000"
	if got := want["Plain"]; got != allDefault {
		t.Fatalf("Plain: a fresh all-default value wrote %q, want %q", got, allDefault)
	}
	// `| required` always rides, at its zero too (§2.3).
	if got := want["Required"]; got == allDefault {
		t.Fatalf("Required: a fresh required int32 elided, want its field on the wire")
	}
	// An absent optional and a None union elide (§2.3, §2.6).
	if got := want["Extra"]; got != allDefault {
		t.Fatalf("Extra: an absent optional or an inactive union arm wrote %q, want %q", got, allDefault)
	}

	got := freshDefaultsSave(t, unit, roots, nil)
	for _, root := range roots {
		if want[root] == "" {
			continue
		}
		if got[root] != want[root] {
			t.Errorf("%s: Save wrote %q, the Go engine's bytes are %q", root, got[root], want[root])
		}
	}

	control := freshDefaultsSave(t, unit, roots, func(tables map[string][]byte) {
		const name = "schema_table_save.nut"
		src := string(tables[name])
		const anchor = "  return scalarDefault(val, d.def);"
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("negative control: the save runtime holds %d scalar-default elisions, want 1", strings.Count(src, anchor))
		}
		tables[name] = []byte(strings.Replace(src, anchor, "  return false;", 1))
	})
	if control["Plain"] == want["Plain"] {
		t.Fatalf("negative control: a save that always rides a scalar default still matched the engine on Plain")
	}
	t.Logf("compared %d fresh roots against tablewire; the negative control turns Plain from %s", len(want), want["Plain"])
}

// freshDefaultsSave writes the unit's generated modules, runs one Squirrel
// save of every root's fresh `make()` value, and answers the hex bytes per
// root. patch, when not nil, edits the emitted runtime files before they are
// written (a control's scratch copy).
func freshDefaultsSave(t *testing.T, unit *ir.Unit, roots []string, patch func(map[string][]byte)) map[string]string {
	t.Helper()
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if patch != nil {
		patch(tables)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "fresh")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	quoted := make([]string, len(roots))
	for i, root := range roots {
		quoted[i] = fmt.Sprintf("%q", root)
	}
	probe := fmt.Sprintf(`local schema = dofile("fresh/schema_module.nut", true);
local T = schema.require(%q);
local S = schema.require("schema_table_save");
foreach (root in [%s]) {
  local v = T.Descriptors[root].make();
  local b = S.save(T, root, v);
  local h = b == null ? "null" : "";
  if (b != null) for (local i = 0; i < b.len(); i++) h += format("%%02x", b[i] & 0xff);
  print(root + "|" + h + "\n");
}
`, squirreltable.ModuleName(unit), strings.Join(quoted, ", "))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))

	saved := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			t.Fatalf("save probe line %q is not a root and its bytes", line)
		}
		saved[parts[0]] = parts[1]
	}
	return saved
}

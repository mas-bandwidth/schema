package squirreltable_test

// A CALLER'S EQUAL STRINGS SAVE BY VALUE (docs/SPEC-TABLES.md §2.5): only one
// blob RECORD named from two slots on the wire is blob_shared. A Squirrel
// string is a value, so a value the caller built with two equal strings in two
// *string slots holds two blobs, and the save writes two records. The strict
// read takes the bytes back with both strings, and the re-save of what it read
// is byte-identical. The negative control plants the old identity back (a
// caller's *string keyed by its content), which refuses the save blob_shared.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

const equalStringsSchema = "package probe\n\ntable Root\n{\n    a *string\n    b *string\n    child *Child\n}\n\ntable Child\n{\n    c *string\n}\n"

// equalStringsPlant is the old identity: a caller's *string keyed by its
// content, so two equal strings are one node named twice.
const (
	equalStringsAnchor = "  if (typeof id != \"string\") return id;\n"
	equalStringsPlant  = "  return id;\n"
)

func equalStringsRun(t *testing.T, plant bool) string {
	t.Helper()
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", loadSource(t, equalStringsSchema))
	if plant {
		path := filepath.Join(dir, "probe", "schema_table_save.nut")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		if n := strings.Count(src, equalStringsAnchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the plant's anchor appears %d times in schema_table_save.nut, want once", n)
		}
		writeFile(t, path, []byte(strings.Replace(src, equalStringsAnchor, equalStringsPlant, 1)))
	}
	var p strings.Builder
	fmt.Fprintf(&p, `local schema = dofile("probe/schema_module.nut", true);
local T = schema.require(%q);
local hex = function(b) { local s = ""; for (local i = 0; i < b.len(); i++) s += format("%%02x", b[i] & 0xff); return s; };
local v = T.Root();
v.A = "same";
v.B = "same";
v.Child = T.Child();
v.Child.C = "same";
local w = T.RootSave(v);
if (w == null) { print("refused\n"); return; }
local r = {};
local back = T.RootLoad(w, r);
if (back == null) { print("load refused\n"); return; }
print("a=" + back.A + " b=" + back.B + " c=" + back.Child.C + "\n");
local again = T.RootSave(back);
print("resave " + (again != null && hex(again) == hex(w) ? "same" : "differs") + "\n");
`, mod)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	return strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil)))
}

func TestSquirrelCallersEqualStringsSaveByValue(t *testing.T) {
	t.Parallel()
	got := equalStringsRun(t, false)
	want := "a=same b=same c=same\nresave same"
	if got != want {
		t.Fatalf("a caller's equal *strings: got %q, want %q", got, want)
	}
}

func TestSquirrelCallersEqualStringsNegativeControl(t *testing.T) {
	t.Parallel()
	if got := equalStringsRun(t, true); got != "refused" {
		t.Fatalf("NEGATIVE CONTROL FAILED: with a caller's *string keyed by its content the save answered %q, want refused (blob_shared)", got)
	}
}

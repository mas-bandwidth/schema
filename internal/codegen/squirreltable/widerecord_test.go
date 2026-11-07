package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordSchema is a record past 4 GiB (docs/SPEC-TABLES.md §11): Huge
// holds three 2 GiB Parts and a field past 4 GiB. Every leg generates and
// compiles it with each stored size whole.
const wideRecordSchema = "../../../testdata/wire/tables/storage-cap/wide/Wide.schema"

// wideRecordProbe compiles the generated module and prints every size its
// storage layout holds: each record's size, and each array, list or map
// field's element size and alignment, by the field's wire id. The Squirrel
// leg keeps values as objects, so a size is the one storage fact it holds;
// it has no field offsets. The probe never constructs a record.
const wideRecordProbe = `local schema = dofile("wide/schema_module.nut", true);
local T = schema.require(%q);
foreach (name, t in T.StorageLayout.tables) {
  print(name + ".size=" + t.size + "\n");
  foreach (id, e in t.elems) print(name + ".elem." + id + "=" + e[0] + "," + e[1] + "\n");
}
`

// wideRecordWant is what ir answers for every line the probe prints.
func wideRecordWant(u *ir.Unit) map[string]string {
	want := map[string]string{}
	entry := u.Tables["Root"].Fields[2].MapEntry
	for name, st := range map[string]*ir.Struct{"Part": u.Tables["Part"], "Huge": u.Tables["Huge"], "Root": u.Tables["Root"], entry.Name: entry} {
		want[name+".size"] = strconv.FormatInt(ir.RecordLayout(u, st).Size, 10)
		for _, f := range st.Fields {
			key := name + ".elem." + strconv.FormatInt(int64(ir.TableFieldWireId(f)), 10)
			switch {
			case f.IsMap():
				l := ir.RecordLayout(u, f.MapEntry)
				want[key] = fmt.Sprintf("%d,%d", l.Size, l.Align)
			case f.IsList() || f.Array != ir.ArrayNone:
				size, align := ir.ListElementLayout(u, f)
				want[key] = fmt.Sprintf("%d,%d", size, align)
			}
		}
	}
	return want
}

// wideRecordRun generates the wide unit, applies patch to the table module
// (nil for none), compiles it in Squirrel and answers what disagrees with ir.
func wideRecordRun(t *testing.T, patch func(string) string) []string {
	t.Helper()
	src, err := os.ReadFile(wideRecordSchema)
	if err != nil {
		t.Fatal(err)
	}
	u := loadSource(t, string(src))
	dir := t.TempDir()
	mod := emitUnit(t, dir, "wide", u)
	modPath := filepath.Join(dir, "wide", mod+".nut")
	if patch != nil {
		text, err := os.ReadFile(modPath)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, modPath, []byte(patch(string(text))))
	}
	writeFile(t, filepath.Join(dir, "main.nut"), fmt.Appendf(nil, wideRecordProbe, mod))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	want := wideRecordWant(u)
	var bad []string
	for k, w := range want {
		if got[k] != w {
			bad = append(bad, fmt.Sprintf("%s: got %q, want %s", k, got[k], w))
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			bad = append(bad, fmt.Sprintf("%s=%s: not a size ir derives", k, v))
		}
	}
	if huge, _ := strconv.ParseInt(want["Huge.size"], 10, 64); huge <= 1<<32 {
		bad = append(bad, "Huge is not past 4 GiB, so the unit holds nothing wide")
	}
	return bad
}

// TestSquirrelWideRecordCompiles: the Squirrel leg generates a record past
// 4 GiB, the generated module compiles, and every record size and element
// size it holds equals ir's, past 2^32 where ir's is.
func TestSquirrelWideRecordCompiles(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	t.Parallel()
	if bad := wideRecordRun(t, nil); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestSquirrelWideRecordNegativeControl narrows Huge's size to 32 bits in the
// generated module and requires the probe to go red.
func TestSquirrelWideRecordNegativeControl(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	t.Parallel()
	src, err := os.ReadFile(wideRecordSchema)
	if err != nil {
		t.Fatal(err)
	}
	u := loadSource(t, string(src))
	size := ir.RecordLayout(u, u.Tables["Huge"]).Size
	from := fmt.Sprintf("Huge = { size = %d,", size)
	bad := wideRecordRun(t, func(text string) string {
		if !strings.Contains(text, from) {
			t.Fatalf("the generated module holds no %q", from)
		}
		return strings.Replace(text, from, fmt.Sprintf("Huge = { size = %d,", size&0xffffffff), 1)
	})
	if len(bad) == 0 {
		t.Fatal("a Huge size narrowed to 32 bits still matched ir")
	}
}

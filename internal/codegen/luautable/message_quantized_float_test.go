package luautable_test

// A quantized float32 rides as its step index at bits_required(0, count), and
// an enum-keyed array rides as the count of present slots then one (key
// reference, element) pair per slot (docs/SPEC-TABLES.md §3.2, §3.3). The
// message walk copies the reference codec's bytes. A reader that does not
// declare the float skips that entry at the announced width, so the batch it
// saves is the batch it was given.

import (
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const luauQuantizedFloatSchema = `package probe

enum Slot { red, green, blue }

fixed table Root
{
    speed float32 | min = 0.0, max = 10.0, resolution = 0.01
    by [Slot]int32
}
`

const luauQuantizedFloatReaderSchema = `package probe

enum Slot { red, green, blue }

fixed table Root
{
    by [Slot]int32
}
`

const luauQuantizedFloatJSON = `{"speed":3.5,"by":{"red":11,"blue":22}}`

func TestMessageQuantizedFloatAndEnumKeyedArray(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	unit := luauQuantizedFloatUnit(t, luauQuantizedFloatSchema)
	model := tabletext.NewModel(unit)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(luauQuantizedFloatJSON), &report) || !report.Silent() {
		t.Fatalf("source: %+v", report)
	}
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	announce := tablewire.Announce(unit)
	shape := luauQuantizedFloatShape(t, unit)
	if shape.Bits != 10 {
		t.Fatalf("quantized width %d, want 10", shape.Bits)
	}
	if got := ir.TableMessageQuantize(shape, float32(3.5)); got != 350 {
		t.Fatalf("step index %d, want 350", got)
	}

	dir, mods := luauQuantizedFloatEmit(t, unit)
	luauQuantizedFloatMatches(t, dir, mods, announce, batch)

	reader := luauQuantizedFloatUnit(t, luauQuantizedFloatReaderSchema)
	rdir, rmods := luauQuantizedFloatEmit(t, reader)
	luauQuantizedFloatMatches(t, rdir, rmods, announce, batch)
}

func luauQuantizedFloatUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(files)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func luauQuantizedFloatShape(t *testing.T, unit *ir.Unit) ir.TableMessageShape {
	t.Helper()
	st := unit.Tables["Root"]
	if st == nil {
		t.Fatal("no Root")
	}
	for _, f := range st.Fields {
		if f.Name != "speed" {
			continue
		}
		e := ir.TableFieldEntry(f)
		if e.Kind != ir.TableKindF32 || e.Shape.Packing != ir.TableMessageQuantized {
			t.Fatalf("speed kind %d packing %d", e.Kind, e.Shape.Packing)
		}
		return e.Shape
	}
	t.Fatal("Root declares no speed")
	return ir.TableMessageShape{}
}

func luauQuantizedFloatEmit(t *testing.T, u *ir.Unit) (string, []string) {
	t.Helper()
	out, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(out, tables)
	dir := t.TempDir()
	var mods []string
	for name, data := range out {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	return dir, mods
}

func luauQuantizedFloatMatches(t *testing.T, dir string, mods []string, announce, batch []byte) {
	t.Helper()
	out := messageMain(t, dir, mods, "Root", announce, batch)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "count 1 malformed false") {
		t.Fatalf("walk: %q", out)
	}
	want := hex.EncodeToString(batch)
	if lines[len(lines)-1] != want {
		t.Fatalf("luau batch %s\nwant       %s", lines[len(lines)-1], want)
	}
}

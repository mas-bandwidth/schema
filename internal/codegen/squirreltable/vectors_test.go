package squirreltable_test

// THE PINNED VECTORS THE SQUIRREL LEG ANSWERS AS THE GO ENGINE DOES
// (docs/SPEC-TABLES.md §4.2; testdata/wire/tables/fuzz-vectors/INDEX.txt).
// Each case sends one pinned vector through the wire-fuzz driver's builder arm
// (test/conformance/squirrel/wirefuzz.nut, the arm every VARIABLE root of this
// leg runs under) over units generated here, and holds the reply to the Go
// engine's builder read on the same bytes (fuzzEngine, fuzzVerdict). Each has a
// negative control that plants the rule's old spelling back, in the emitted
// runtime or in a copy of the driver, and requires the verdict to go red:
//
//   - map_count_cap: a builder's count refusal rides the completion and
//     save-failure channel, so the driver's refused byte does not carry it;
//   - map_repeated_key_widen: a key met under a foreign kind and then under a
//     kind that widens is the widened key, and the map keeps its entries;
//   - wire_length_overflow: a length near 2^63 reads as the sentinel, so a
//     cursor plus a length is bounded and never wraps;
//   - distinct_empty_string_blobs: two string records of equal bytes are two
//     records, so the save writes both rather than refusing blob_shared.

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

type sqVectorCase struct {
	vector, unit, tables, root string
	// file is the emitted file the plant lands in, or "driver"; from appears
	// in it exactly once
	file, from, to string
}

var sqVectorCases = []sqVectorCase{
	{"map_count_cap", "mapdemo", "tables/maps", "Docs", "driver",
		"putU8(r, report.refused && !countRefused ? 1 : 0);", "putU8(r, report.refused ? 1 : 0);"},
	{"map_repeated_key_widen", "mapdemo", "tables/maps", "Pairs", "schema_table_load.nut",
		"{ widened = true; bad = false; }", "widened = true;"},
	{"wire_length_overflow", "graphdemo", "tables/pointers", "Scene", "schema_table_load.nut",
		"return v >= 0x4000000000000000 ? 0x4000000000000000 : v;", "return v;"},
	{"distinct_empty_string_blobs", "blobdemo", "tables/blobs", "Catalog", "schema_table_load.nut",
		"    if (p.fd.shape == \"sblob\") _M.noteStringOrigin(p.holder, p.key, value, token);\n", ""},
}

// sqVectorVerdict answers the first disagreement between the driver and the
// engine on the case's vector, or "" when they agree.
func sqVectorVerdict(t *testing.T, c sqVectorCase, plant bool) string {
	t.Helper()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, repo, []string{c.tables})
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tf, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tf)
	edit := func(src []byte) []byte {
		if n := strings.Count(string(src), c.from); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the %s plant's anchor appears %d times in %s, want once", c.vector, n, c.file)
		}
		return []byte(strings.Replace(string(src), c.from, c.to, 1))
	}
	if plant && c.file != "driver" {
		files[c.file] = edit(files[c.file])
	}
	dir := t.TempDir()
	out := filepath.Join(dir, c.unit)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	driver := filepath.Join(repo, "test", "conformance", "squirrel", "wirefuzz.nut")
	if plant && c.file == "driver" {
		src, err := os.ReadFile(driver)
		if err != nil {
			t.Fatal(err)
		}
		driver = filepath.Join(dir, "wirefuzz.nut")
		writeFile(t, driver, edit(src))
	}
	leg, known := startFuzzLegAt(t, dir, driver, true, []fuzzRoot{{c.unit, c.root}})
	if !known[0] {
		t.Fatalf("the driver's builder arm does not carry %s.%s", c.unit, c.root)
	}
	wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "fuzz-vectors", c.vector+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	want, err := fuzzEngine(m, m.Lookup(c.root), true, wire)
	if err != nil {
		t.Fatal(err)
	}
	got, err := leg.ask(0, wire)
	if err != nil {
		return "the driver died on the vector: " + err.Error()
	}
	if v := fuzzVerdict(got, want); v != "" {
		return v + "\n  squirrel: " + got.String() + "\n  engine:   " + want.String()
	}
	return ""
}

func TestSquirrelPinnedVectorsAgreeWithTheGoEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	for _, c := range sqVectorCases {
		t.Run(c.vector, func(t *testing.T) {
			t.Parallel()
			if v := sqVectorVerdict(t, c, false); v != "" {
				t.Fatalf("%s.%s: %s", c.unit, c.root, v)
			}
		})
	}
}

func TestSquirrelPinnedVectorsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	for _, c := range sqVectorCases {
		t.Run(c.vector, func(t *testing.T) {
			t.Parallel()
			if v := sqVectorVerdict(t, c, true); v == "" {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s fix reverted left its vector green", c.vector)
			}
		})
	}
}

package mootable_test

// STRICT CELLS for the cook half (cook-write, cook-open, block-read):
// the strict read is one read — the whole value or a refusal — and a
// forgery is held to its verdict alone (docs/rulings.json
// refusal-verdict-only): it must refuse and name some reason, or open;
// which reason name it gives is never compared across languages.
//
//   cook-write  every manifest cook-write row is cooked by the emitted
//               table_cookwrite.moo byte for byte as the tool's files in
//               both byte orders, and the little-endian file opens and
//               reads back (table_cookread.moo) to the cooked value.
//   cook-open   every cook fixture opens and reads back to the value the
//               tool's Uncook finds in it, and every cook forgery answers
//               its pinned verdict.
//   block-read  every block dump equals the pinned dump byte for byte,
//               and every block forgery answers its pinned verdict.
//
// TestMooCookStrictCellsNegativeControl plants one defect per cell in
// the real emitted artifact (table_cookwrite.moo, table_cookread.moo,
// table_blockread.moo in the generated unit on disk) and requires each
// cell to go red.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// plantStrictCookDefect writes one planted defect into a generated
// unit's emitted file on disk; a file the unit does not emit is left
// alone, and a present file must carry the anchor exactly once.
func plantStrictCookDefect(t *testing.T, g *generatedUnit, file, anchor, repl string) {
	t.Helper()
	if g.files[file] == nil {
		return
	}
	path := filepath.Join(g.dir, file)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the anchor %q is not in %s exactly once: the control would be vacuous", anchor, file)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, repl, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plantStrictCookDefects plants one defect per strict cell:
// cook-write's reference delta one slot long, cook-open's byte-order
// check removed, and block-read's pitch check removed.
func plantStrictCookDefects(t *testing.T, g *generatedUnit) {
	t.Helper()
	plantStrictCookDefect(t, g, "table_cookwrite.moo", "putNum64(ctx, at, to - at)", "putNum64(ctx, at, to - at + 8)")
	plantStrictCookDefect(t, g, "table_cookread.moo", "\tif buffer.readu32(b, at + 16) ~= 1 or buffer.readu32(b, at + 20) ~= 0 then\n", "\tif false then\n")
	plantStrictCookDefect(t, g, "table_blockread.moo", "\t\t\tif stride ~= f.stride then\n", "\t\t\tif false then\n")
}

// cookStrictCellsFails runs all three cells and answers every failure,
// prefixed by its cell. mutate, when set, plants defects in each
// generated unit first (the negative control).
func cookStrictCellsFails(t *testing.T, mutate func(*testing.T, *generatedUnit)) []string {
	t.Helper()
	var fails []string

	// cook-write: byte-for-byte in both orders, reread equal.
	rows := cookWriteRows(t)
	wfails, answered, _ := cookWriteFailures(t, rows, mutate)
	for _, f := range wfails {
		fails = append(fails, "cook-write: "+f)
	}
	if answered != 2*len(rows) {
		fails = append(fails, fmt.Sprintf("cook-write: answered %d of %d cases", answered, 2*len(rows)))
	}

	units, _ := readManifest(t)

	// cook-open: fixtures read back equal; forgeries answer verdicts.
	g := generateUnit(t, units, "graphdemo")
	if g.err != nil {
		t.Fatalf("unit graphdemo: %v", g.err)
	}
	if mutate != nil {
		mutate(t, g)
	}
	m := tabletext.NewModel(g.unit)
	fixtures := map[string][]byte{}
	for _, row := range manifestLines(t, "cook") {
		caseName, rootName := row[1], row[3]
		data := cookFixture(t, caseName, rootName)
		fixtures[caseName] = data
		inst, err := tablecook.Uncook(m, m.Lookup(rootName), data)
		if err != nil {
			t.Fatalf("%s: the tool does not uncook its own fixture: %v", caseName, err)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: %v", caseName, err)
		}
		if out := g.run(t, cookProbe(g, rootName, 0, len(data), false, true), map[string][]byte{"wire": wire, "cook": data}); out != "ok" {
			fails = append(fails, fmt.Sprintf("cook-open: fixture %s: %s", caseName, out))
		}
	}
	cookFgs := forgeries(t, "cook")
	if len(cookFgs) == 0 {
		fails = append(fails, "cook-open: the manifest names no cook forgery")
	}
	for _, fg := range cookFgs {
		base, ok := fixtures[fg.base]
		if !ok {
			t.Fatalf("%s: no fixture %s", fg.name, fg.base)
		}
		data, lead, extent, null := fg.materialize(t, base)
		got := g.run(t, cookProbe(g, fg.subject, lead, extent, null, false), map[string][]byte{"cook": data})
		if forgeryVerdict(got) != fg.verdict {
			fails = append(fails, fmt.Sprintf("cook-open: %s: moo answers %q, want the verdict %q", fg.name, got, fg.verdict))
		}
	}

	// block-read: dumps equal the pinned dumps; forgeries answer verdicts.
	bg := generateUnit(t, units, "blockdemo")
	if bg.err != nil {
		t.Fatalf("unit blockdemo: %v", bg.err)
	}
	if mutate != nil {
		mutate(t, bg)
	}
	blockRows := manifestLines(t, "block")
	if len(blockRows) == 0 {
		fails = append(fails, "block-read: the manifest names no block")
	}
	for _, row := range blockRows {
		data, err := os.ReadFile(filepath.Join(root, row[3]))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(root, row[4]))
		if err != nil {
			t.Fatal(err)
		}
		got := bg.run(t, blockMain(bg, blockTableOf(row[1]), 0, len(data), false, true), map[string][]byte{"block": data})
		if got != strings.TrimRight(string(want), "\n") {
			fails = append(fails, fmt.Sprintf("block-read: %s: the moo dump differs from %s: %s", row[1], row[4], firstLineDiff(got, string(want))))
		}
	}
	blockFgs := forgeries(t, "block")
	if len(blockFgs) == 0 {
		fails = append(fails, "block-read: the manifest names no block forgery")
	}
	for _, fg := range blockFgs {
		base, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", fg.base+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		data, lead, extent, null := fg.materialize(t, base)
		got := bg.run(t, blockMain(bg, blockTableOf(fg.subject), lead, extent, null, false), map[string][]byte{"block": data})
		if forgeryVerdict(got) != fg.verdict {
			fails = append(fails, fmt.Sprintf("block-read: %s: moo answers %q, want the verdict %q", fg.name, got, fg.verdict))
		}
	}
	return fails
}

func TestMooCookStrictCells(t *testing.T) {
	slowtest.Gate(t, "moo and go build (it runs the emitted cook and block reads and writes)")
	t.Parallel()
	for _, f := range cookStrictCellsFails(t, nil) {
		t.Error(f)
	}
}

// TestMooCookStrictCellsNegativeControl plants one defect per cell in
// the emitted artifacts on disk and requires each cell to go red: the
// cook-write delta defect moves the cooked bytes, the removed
// byte-order check lets a cook forgery open, and the removed pitch
// check lets block_pitch open.
func TestMooCookStrictCellsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo and go build (it runs the emitted cook and block reads and writes)")
	t.Parallel()
	fails := cookStrictCellsFails(t, plantStrictCookDefects)
	for _, cell := range []string{"cook-write", "cook-open", "block-read"} {
		red := false
		for _, f := range fails {
			if strings.HasPrefix(f, cell+":") {
				red = true
				break
			}
		}
		if !red {
			t.Errorf("%s: the planted defects left the cell green (%d findings)", cell, len(fails))
		}
	}
	if len(fails) > 0 {
		t.Logf("planted defects: %d findings; first %s", len(fails), fails[0])
	}
}

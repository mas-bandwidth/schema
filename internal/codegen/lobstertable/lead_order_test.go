package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE BLOCK LEAD GATE (docs/PORTING.md I5) and THE ORDER WORD (I7). The
// manifest's block_base_unaligned row holds one lead; this gate places every
// committed block image at every lead 0..64 past an aligned base and requires
// Open at 0 and 64 and a refusal by name, unaligned_base, at 1..63, because an
// unaligned read succeeds on every host this repo builds on and the fuzz
// oracle alone cannot see the check go missing.

// blockLeadImages is every committed block image and the root it opens at.
var blockLeadImages = []struct{ name, root, path string }{
	{"block_render", "RenderFrame", "testdata/wire/tables/block_render.bin"},
	{"block_padded", "PaddedFrame", "testdata/wire/tables/block_padded.bin"},
}

// blockLeadRun answers Open for every image at every lead and returns the
// placements that did not answer what the gate requires.
func blockLeadRun(t *testing.T, sabotage func(string) string) []string {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["blockdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("blockdemo is refused: %v", err)
	}
	if sabotage != nil {
		p := filepath.Join(g.dir, "block_form.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sabotage(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	for _, m := range append(g.mods, "block_form") {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	inputs := map[string][]byte{}
	for i, img := range blockLeadImages {
		data, err := os.ReadFile(filepath.Join(root, img.path))
		if err != nil {
			t.Fatal(err)
		}
		inputs[img.name] = data
		fmt.Fprintf(&b, "let d%d = read_file(%q)\nassert d%d\n", i, img.name, i)
		fmt.Fprintf(&b, "for(65) lead:\n    print(%q + \" {lead} \" + schema_rt.block_open_reason(%q, d%d, lead, false))\n", img.name, img.root, i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	seen := 0
	var bad []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("not a lead answer: %q", line)
		}
		seen++
		want := "unaligned_base"
		if f[1] == "0" || f[1] == "64" {
			want = "ok"
		}
		if f[2] != want {
			bad = append(bad, fmt.Sprintf("%s at lead %s answered %s, wanted %s", f[0], f[1], f[2], want))
		}
	}
	if seen != 65*len(blockLeadImages) {
		t.Fatalf("%d lead answers, want %d", seen, 65*len(blockLeadImages))
	}
	return bad
}

// TestLobsterBlockLead holds every block image at every lead 0..64.
func TestLobsterBlockLead(t *testing.T) {
	t.Parallel()
	if bad := blockLeadRun(t, nil); len(bad) > 0 {
		t.Fatalf("block leads that disagree: %v", bad)
	}
	t.Logf("%d block images x 65 leads: 0 and 64 open, 1..63 refuse unaligned_base", len(blockLeadImages))
}

// TestLobsterBlockLeadNegativeControl rewrites the residue check to `lead <
// 0`, so every lead opens, and requires the gate red on lead 1 of the first
// image and on all 63 unaligned leads of each.
func TestLobsterBlockLeadNegativeControl(t *testing.T) {
	t.Parallel()
	const residue = "    if lead % 64 != 0:\n        return \"unaligned_base\"\n"
	bad := blockLeadRun(t, func(src string) string {
		if strings.Count(src, residue) != 1 {
			t.Fatal("the control's anchor, the residue check, is not in block_form.lobster once")
		}
		return strings.Replace(src, residue, "    if lead < 0:\n        return \"unaligned_base\"\n", 1)
	})
	const first = "block_render at lead 1 answered ok, wanted unaligned_base"
	if len(bad) != 63*len(blockLeadImages) || !slices.Contains(bad, first) {
		t.Fatalf("with the residue check removed, %d placements went red (want %d, among them %q): %v", len(bad), 63*len(blockLeadImages), first, bad)
	}
	t.Logf("negative control: the residue check removed, %s, and every other unaligned lead", first)
}

// TestLobsterOrderWordNegativeControl is I7's reversed witness: a file whose
// magic is intact and whose order word names the other order is refused by
// name (not_a_cook) in TestLobsterBlockForgeries (block_byte_order) and
// TestLobsterCookForgeries (cook_byte_order_0, _2, _3), and with the order
// word's check removed from Open exactly those rows go red, the block's and the
// cook's alike, while the magic-reversed rows stay refused as foreign_order.
func TestLobsterOrderWordNegativeControl(t *testing.T) {
	t.Parallel()
	t.Run("block", func(t *testing.T) {
		t.Parallel()
		const word = "    if read_uint64_le(data, 16) != block_byte_order:\n        return \"not_a_cook\"\n"
		rows, got := blockForgeryRun(t, func(src string) string {
			if strings.Count(src, word) != 1 {
				t.Fatal("the control's anchor, the block order word, is not in block_form.lobster once")
			}
			return strings.Replace(src, word, "", 1)
		})
		if bad := checkReasons(rows, got); fmt.Sprint(bad) != "[block_byte_order]" {
			t.Fatalf("with the order word unchecked, the block rows that went red are %v, want exactly [block_byte_order]", bad)
		}
	})
	t.Run("cook", func(t *testing.T) {
		t.Parallel()
		const word = "    if read_uint64_le(data, 16) != cook_byte_order_little:\n        return \"not_a_cook\"\n"
		rows, got := cookForgeryRun(t, func(src string) string {
			if strings.Count(src, word) != 1 {
				t.Fatal("the control's anchor, the cook order word, is not in cook.lobster once")
			}
			return strings.Replace(src, word, "", 1)
		})
		bad := checkReasons(rows, got)
		if len(bad) == 0 || !slices.Contains(bad, "cook_byte_order_2") {
			t.Fatalf("with the order word unchecked, the cook rows that went red are %v, want cook_byte_order_2 among them", bad)
		}
		for _, b := range bad {
			if !strings.HasPrefix(b, "cook_byte_order_") {
				t.Fatalf("with the order word unchecked, %s went red too (all: %v); only the order-word rows may", b, bad)
			}
		}
	})
}

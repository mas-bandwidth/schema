package mootable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// I5 checks every residue, including a nonzero aligned base. Random forgery
// alone cannot guarantee it visits every lead. The caller supplies the same
// valid image and extent at each lead; only its placement changes.
func mooBlockLeadVerdicts(t *testing.T, sabotage bool) (matched, mismatched int) {
	t.Helper()
	units, _ := readManifest(t)
	images := 0
	for _, row := range manifestLines(t, "block") {
		if len(row) < 5 {
			t.Fatalf("short block manifest row: %v", row)
		}
		g := generateUnit(t, units, row[2])
		if g.err != nil {
			t.Fatal(g.err)
		}
		if sabotage {
			name := "table_blockread.moo"
			src := g.files[name]
			anchor := []byte("if at % ALIGN ~= 0 then\n\t\treturn nil, \"unaligned_base\"\n\tend")
			if bytes.Count(src, anchor) != 1 {
				t.Fatal("alignment sabotage anchor must occur exactly once")
			}
			files := map[string][]byte{}
			for name, data := range g.files {
				files[name] = data
			}
			files[name] = bytes.Replace(src, anchor, []byte("-- SABOTAGED: the caller base is not checked"), 1)
			g = writeUnit(t, g.unit, files)
		}
		dump, err := os.ReadFile(filepath.Join(root, row[4]))
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(dump))
		if len(fields) < 2 || fields[0] != "projection" {
			t.Fatalf("invalid block dump %s", row[4])
		}
		rootName := fields[1]
		mod := ""
		for _, name := range g.tableModules() {
			if strings.Contains(string(g.files[name+".moo"]), "M."+rootName+"BlockInfo = ") {
				mod = name
			}
		}
		if mod == "" {
			t.Fatalf("no block info for %s", rootName)
		}
		wire, err := os.ReadFile(filepath.Join(root, row[3]))
		if err != nil {
			t.Fatal(err)
		}
		main := fmt.Sprintf(`local B=require("./table_blockread")
local D=require(%q).%sBlockInfo
local image=require("./inputs").image
local size=buffer.len(image)
for lead=0,64 do
 local placed=buffer.create(lead+size)
 buffer.copy(placed,lead,image,0,size)
 local h,why=B.BlockOpen(D,placed,lead,size)
 print(tostring(lead).." "..(if h then "open" else tostring(why)))
end
`, "./"+mod, rootName)
		lines := strings.Split(g.run(t, main, map[string][]byte{"image": wire}), "\n")
		if len(lines) != 65 {
			t.Fatalf("%s: %d lead verdicts, want 65", row[1], len(lines))
		}
		for lead, answer := range lines {
			prefix := strconv.Itoa(lead) + " "
			want := "unaligned_base"
			if lead == 0 || lead == 64 {
				want = "open"
			}
			if answer == prefix+want {
				matched++
				continue
			}
			mismatched++
			// The planted check must affect exactly the unaligned bases. A crash,
			// changed aligned case, different refusal, or skipped row cannot count.
			if !sabotage || lead == 0 || lead == 64 || answer != prefix+"open" {
				t.Errorf("%s at lead %d: got %q, want %q", row[1], lead, answer, prefix+want)
			}
		}
		images++
	}
	if images != 2 {
		t.Fatalf("block lead corpus: %d images, want both committed images", images)
	}
	return matched, mismatched
}
func TestMooBlockLead(t *testing.T) {
	slowtest.Gate(t, "pinned moo over both block images at every base lead 0..64")
	matched, mismatched := mooBlockLeadVerdicts(t, false)
	t.Logf("lead matrix: %d matched, %d mismatched", matched, mismatched)
	if matched != 130 || mismatched != 0 {
		t.Fatal("all 130 placements must match the alignment rule")
	}
}
func TestMooBlockLeadNegativeControl(t *testing.T) {
	slowtest.Gate(t, "pinned moo with its actual block base-alignment check removed")
	matched, mismatched := mooBlockLeadVerdicts(t, true)
	t.Logf("alignment sabotage: %d aligned cases unchanged, %d unaligned placements wrongly opened", matched, mismatched)
	if matched != 4 || mismatched != 126 {
		t.Fatal("exactly 126 unaligned placements must expose the removed check")
	}
}

package goosetable_test

// THE BLOCK FORM'S GATES for Goose (docs/SPEC-TABLES.md §19.1, §19.2, §19.5;
// docs/PORTING.md M7, I5), under the pinned compiler, each a generated
// program in t.TempDir():
//
//	TestGooseBlockBuilderMatchesTheGoldens  the two-language golden: the
//	    frames test/tables/block_main.cpp pins, built through Begin and the
//	    TYPED row writers (<Table>Block<Array>Put), are
//	    testdata/wire/tables/block_render.bin and block_padded.bin byte for
//	    byte, and open
//	TestGooseBlockLeadGate                   I5: every committed block image
//	    at every lead 0..64 — 0 and 64 open, 1..63 refuse unaligned_base
//
// TestGooseBlockNegativeControl breaks the runtime once per gate and
// requires that gate, and only that gate, to go red.
//
// The read surfaces (block, block-dump, the forgery battery, the cook's open,
// node dump and write) are the conformance harness's, over
// test/conformance/goose/accel.goose; their controls are
// test/conformance/goose/negative-control-accel (make/goose.mk).
//
// Unit tests here use no real time, no socket and no network: each run is
// `goose run` of a generated program in t.TempDir().

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// blockImages are the committed block images the manifest names.
var blockImages = []struct{ name, table string }{
	{"block_render", "RenderFrame"},
	{"block_padded", "PaddedFrame"},
}

// blockUnitFiles generates tables/block for Goose (with `override` replacing
// runtime files) plus a module, all.goose, that imports every generated
// module, so a program needs one import.
func blockUnitFiles(t *testing.T, override map[string][]byte) map[string][]byte {
	t.Helper()
	gathered, err := compiler.GatherPaths(prefix([]string{"tables/block"}))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("tables/block does not load: %v", err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	maps.Copy(files, override)
	mods := make([]string, 0, len(files))
	for name := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
	}
	sort.Strings(mods)
	var all strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&all, "import .%s;\n", m)
	}
	files["all.goose"] = []byte(all.String())
	return files
}

// blockSabotage replaces one line of a runtime file, failing if it is not
// there exactly once, so a control can never pass vacuously over moved code.
func blockSabotage(t *testing.T, file, from, to string) map[string][]byte {
	t.Helper()
	data := goosetable.Runtime()[file]
	if n := strings.Count(string(data), from); n != 1 {
		t.Fatalf("sabotage: %s holds %q %d times, want once", file, from, n)
	}
	return map[string][]byte{file: []byte(strings.Replace(string(data), from, to, 1))}
}

// buildBlocks runs testdata/block_build.goose and answers what differs from
// the two goldens ("" for nothing).
func buildBlocks(t *testing.T, override map[string][]byte) string {
	t.Helper()
	files := blockUnitFiles(t, override)
	main, err := os.ReadFile(filepath.Join("testdata", "block_build.goose"))
	if err != nil {
		t.Fatal(err)
	}
	files["block_build.goose"] = []byte(strings.Replace(string(main), "import .PaddedBlock;", "import .PaddedBlock;\nimport .all;", 1))
	out := gooserun.Run(t, t.TempDir(), "block_build.goose", files)
	lines := strings.Fields(string(out))
	if len(lines) != len(blockImages) {
		return fmt.Sprintf("the builder printed %d frames, want %d", len(lines), len(blockImages))
	}
	var bad []string
	for i, img := range blockImages {
		got, err := hex.DecodeString(lines[i])
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", img.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) == string(want) {
			continue
		}
		at := 0
		for at < len(got) && at < len(want) && got[at] == want[at] {
			at++
		}
		bad = append(bad, fmt.Sprintf("%s: %d bytes built, %d pinned, first difference at byte %d", img.name, len(got), len(want), at))
	}
	return strings.Join(bad, "; ")
}

func TestGooseBlockBuilderMatchesTheGoldens(t *testing.T) {
	t.Parallel()
	if diff := buildBlocks(t, nil); diff != "" {
		t.Fatalf("the Goose builder does not reproduce the C++ goldens: %s", diff)
	}
}

// leadProgram opens every block image at every lead 0..64 and prints one
// line per wrong answer, then `ok`.
const leadProgram = `import os;
import .all;

fn reason(table: const u8[:], b: const u8[:], lead: i64) -> i64 {
    if table == "RenderFrame" { return blockdemo::RenderFrameBlockOpenReason(b, lead); }
    return blockdemo::PaddedFrameBlockOpenReason(b, lead);
}

fn image(file: const u8[:], table: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(file, data) { write_stderr("cannot read an image\n"); exit(1); }
    for lead in 65 {
        let got = reason(table, data, lead);
        var want = schema_rt::TableRefuseUnalignedBase;
        if lead == 0 || lead == 64 { want = schema_rt::TableRefuseOk; }
        if got != want {
            write_stdout(str(file, " at lead ", lead, " answered ", schema_rt::table_refuse_name(got), ", wanted ", schema_rt::table_refuse_name(want), "\n"));
        }
    }
}

fn main() {
    image("block_render.bin", "RenderFrame");
    image("block_padded.bin", "PaddedFrame");
    write_stdout("ok\n");
}
`

// blockLead runs the lead program and answers its wrong leads.
func blockLead(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	files := blockUnitFiles(t, override)
	files["lead.goose"] = []byte(leadProgram)
	for _, img := range blockImages {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", img.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		files[img.name+".bin"] = data
	}
	lines := strings.Split(strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "lead.goose", files))), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "ok" {
		t.Fatalf("the lead program did not finish: %q", lines)
	}
	return lines[:len(lines)-1]
}

func TestGooseBlockLeadGate(t *testing.T) {
	t.Parallel()
	if wrong := blockLead(t, nil); len(wrong) > 0 {
		t.Fatalf("the block lead gate (docs/PORTING.md I5) is red:\n%s", strings.Join(wrong, "\n"))
	}
	t.Logf("%d block images x 65 leads: 0 and 64 open, 1..63 refuse unaligned_base", len(blockImages))
}

// TestGooseBlockNegativeControl breaks each gate's mechanism once:
//
//   - the base's alignment clause of Open is dropped (I5): the lead gate must
//     go red naming lead 1, and the builder stays green;
//   - Begin stops rounding an array's start up to its alignment (§19.1): the
//     builder must go red, and the lead gate stays green.
func TestGooseBlockNegativeControl(t *testing.T) {
	t.Parallel()
	t.Run("lead", func(t *testing.T) {
		t.Parallel()
		o := blockSabotage(t, "block_form.goose", "    if lead % TableBlockAlign != 0 { return TableRefuseUnalignedBase; }\n", "")
		wrong := blockLead(t, o)
		if len(wrong) == 0 || !strings.Contains(wrong[0], "at lead 1 answered ok") {
			t.Fatalf("lead: the gate stayed green without the alignment clause: %q", wrong)
		}
		if diff := buildBlocks(t, o); diff != "" {
			t.Errorf("lead: the builder went red too, so the control does not localise the clause: %s", diff)
		}
		t.Logf("lead: %d wrong leads, first %q; the builder stays green", len(wrong), wrong[0])
	})
	t.Run("begin-align", func(t *testing.T) {
		t.Parallel()
		o := blockSabotage(t, "block_form.goose", "    let at = (offset + start_align - 1) / start_align * start_align;\n", "    let at = offset;\n")
		diff := buildBlocks(t, o)
		if diff == "" {
			t.Fatal("begin-align: the builder reproduced the goldens with unaligned array starts")
		}
		if wrong := blockLead(t, o); len(wrong) > 0 {
			t.Errorf("begin-align: the lead gate went red too: %q", wrong)
		}
		t.Logf("begin-align: %s; the lead gate stays green", diff)
	})
}

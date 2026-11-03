package goosetable_test

// THE BUILD VERSION for Goose (docs/SPEC-TABLES.md §7, §19.2, §20.7), the
// gates the Go leg's cell names (internal/codegen/gotable/refusal_test.go,
// TestAcceleratorTypedRefusals; cookwrite_test.go, the version at byte 8),
// over tables/block, the unit test/tables/block_main.cpp pins:
//
//	TestGooseBuildVersion
//	    constant    the generated BuildVersion is `schema build-version`'s
//	                (ir.BuildVersion) and the one the C++ reference stamped
//	                into both committed block images
//	    block-check each of the 64 bits of a C++ block's version word, flipped,
//	                refuses wrong_build_version; the version clause precedes
//	                truncation
//	    cook-write  <Root>Cook writes the version at byte 8, in the order the
//	                call names
//	    cook-check  each of the 64 bits of a written cook's version word,
//	                flipped, refuses wrong_build_version, and the clause
//	                precedes alignment and truncation
//
// TestGooseBuildVersionNegativeControl breaks each clause once and requires
// its own lines red and every other line green.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const buildVersionProgram = `import os;
import .all;

fn fail(clause: const u8[:], why: const u8[:]) { write_stdout(str(clause, " ", why, "\n")); }

fn word(b: const u8[:], at: i64, big: bool) -> u64 {
    var v: u64 = 0;
    for i in 8 {
        let k = if big { i } else { 7 - i };
        v = (v << 8) | (b[at + k] as u64);
    }
    return v;
}

fn reason_name(r: i64) -> const u8[:] { return schema_rt::table_refuse_name(r); }

fn block_check() {
    var img: u8[>..] = [];
    if !read_file("block_padded.bin", img) { write_stderr("cannot read the block image\n"); exit(1); }
    let ok = blockdemo::PaddedFrameBlockOpenReason(img, 0);
    if ok != schema_rt::TableRefuseOk { fail("block-check", str("the C++ block answered ", reason_name(ok))); }
    for bit in 64 {
        let at = 8 + bit / 8;
        let mask = (1 << (bit % 8)) as! u8;
        img[at] ^= mask;
        let r = blockdemo::PaddedFrameBlockOpenReason(img, 0);
        if r != schema_rt::TableRefuseWrongBuildVersion { fail("block-check", str("version bit ", bit, " answered ", reason_name(r))); }
        img[at] ^= mask;
    }
    // the version clause precedes truncation (§19.2's order)
    img[8] ^= 1;
    let short = blockdemo::PaddedFrameBlockOpenReason(img[0..img.len - 1], 0);
    if short != schema_rt::TableRefuseWrongBuildVersion { fail("block-check", str("version and short answered ", reason_name(short))); }
    img[8] ^= 1;
}

fn cook_write_and_check() {
    let v = blockdemo::PaddedFrame { .. };
    let n = blockdemo::PaddedFrameCookMeasure();
    for order in 2 {
        let big = order == 1;
        var out: u8[>..] = [];
        for i in n { out.push(0xAA); }
        if !blockdemo::PaddedFrameCook(v, out, big) { fail("cook-write", "the writer refused the default value"); return; }
        let w = word(out, 8, big);
        if w != blockdemo::BuildVersion { fail("cook-write", str("big=", big, " wrote ", w, " at byte 8")); }
        if big { continue; }
        let ok = blockdemo::PaddedFrameCookOpenReason(out, 0);
        if ok != schema_rt::TableRefuseOk { fail("cook-check", str("the written cook answered ", reason_name(ok))); return; }
        for bit in 64 {
            let at = 8 + bit / 8;
            let mask = (1 << (bit % 8)) as! u8;
            out[at] ^= mask;
            let r = blockdemo::PaddedFrameCookOpenReason(out, 0);
            if r != schema_rt::TableRefuseWrongBuildVersion { fail("cook-check", str("version bit ", bit, " answered ", reason_name(r))); }
            out[at] ^= mask;
        }
        // the version clause precedes the alignment word and truncation (§7's order)
        out[8] ^= 1;
        let align = out[40];
        out[40] = 0;
        let r1 = blockdemo::PaddedFrameCookOpenReason(out, 0);
        if r1 != schema_rt::TableRefuseWrongBuildVersion { fail("cook-check", str("version and alignment answered ", reason_name(r1))); }
        out[40] = align;
        let r2 = blockdemo::PaddedFrameCookOpenReason(out[0..out.len - 1], 0);
        if r2 != schema_rt::TableRefuseWrongBuildVersion { fail("cook-check", str("version and short answered ", reason_name(r2))); }
        out[8] ^= 1;
    }
}

fn main() {
    write_stdout(str("constant ", blockdemo::BuildVersion, "\n"));
    block_check();
    cook_write_and_check();
    write_stdout("done\n");
}
`

// buildVersionProbe runs the program and answers its red lines by clause.
func buildVersionProbe(t *testing.T, override map[string][]byte) map[string][]string {
	t.Helper()
	files := blockUnitFiles(t, override)
	files["buildversion.goose"] = []byte(buildVersionProgram)
	img, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "block_padded.bin"))
	if err != nil {
		t.Fatal(err)
	}
	files["block_padded.bin"] = img
	out := strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "buildversion.goose", files)))
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || lines[len(lines)-1] != "done" {
		t.Fatalf("the build-version program did not finish: %q", lines)
	}
	red := map[string][]string{}
	constant, ok := strings.CutPrefix(lines[0], "constant ")
	if !ok {
		t.Fatalf("the program printed no constant: %q", lines[0])
	}
	goose, err := strconv.ParseUint(constant, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	gathered, err := compiler.GatherPaths(prefix([]string{"tables/block"}))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatal(err)
	}
	if want := ir.BuildVersion(u); goose != want {
		red["constant"] = append(red["constant"], fmt.Sprintf("Goose's BuildVersion is %#016x, `schema build-version tables/block` is %#016x", goose, want))
	}
	for _, img := range blockImages {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", img.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if cpp := binary.LittleEndian.Uint64(data[8:]); cpp != goose {
			red["constant"] = append(red["constant"], fmt.Sprintf("the C++ reference stamped %#016x into %s, Goose's BuildVersion is %#016x", cpp, img.name, goose))
		}
	}
	for _, line := range lines[1 : len(lines)-1] {
		clause, why, _ := strings.Cut(line, " ")
		red[clause] = append(red[clause], why)
	}
	return red
}

func TestGooseBuildVersion(t *testing.T) {
	t.Parallel()
	for clause, why := range buildVersionProbe(t, nil) {
		t.Errorf("%s: %s", clause, strings.Join(why, "; "))
	}
}

// TestGooseBuildVersionNegativeControl:
//
//   - the block's Open stops comparing the version word: block-check red;
//   - the cook's Open stops comparing it: cook-check red;
//   - the cook writer stamps the version with its low bit flipped: cook-write
//     red (and the written cook no longer opens, which is cook-check's).
func TestGooseBuildVersionNegativeControl(t *testing.T) {
	t.Parallel()
	for _, ctl := range []struct {
		name, file, from, to string
		red                  []string
	}{
		{"block-check", "block_form.goose", "    if accel_u64(b, 8) != build { return TableRefuseWrongBuildVersion; }\n", "", []string{"block-check"}},
		{"cook-check", "cook_form.goose", "    if accel_u64(b, 8) != build { return TableRefuseWrongBuildVersion; }\n", "", []string{"cook-check"}},
		{"cook-write", "cook_form.goose", "    cook_put(b, 8, build, 8, big);", "    cook_put(b, 8, build ^ 1, 8, big);", []string{"cook-write", "cook-check"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			red := buildVersionProbe(t, blockSabotage(t, ctl.file, ctl.from, ctl.to))
			if len(red[ctl.red[0]]) == 0 {
				t.Fatalf("%s: the %s clause stayed green", ctl.name, ctl.red[0])
			}
			for clause, why := range red {
				owned := false
				for _, c := range ctl.red {
					owned = owned || c == clause
				}
				if !owned {
					t.Errorf("%s: %s went red too, which the break does not own: %s", ctl.name, clause, why[0])
				}
			}
			t.Logf("%s: %d red lines on %s, first %q", ctl.name, len(red[ctl.red[0]]), ctl.red[0], red[ctl.red[0]][0])
		})
	}
}

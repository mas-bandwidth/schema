package ctable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE VARIABLE SAVE HOLDS THE WRITE CONTRACT IN A DEBUG BUILD (docs/SPEC.md
// §5.1 rows 1 to 6 and 10) on every field, arm payload and element, required
// or not. C is an assert leg: a row the declaration does not admit trips
// schema_assert in a debug build (or is refused there, rows 5 and 6, which C
// refuses in every build), and the release build carries no assert. Each row
// is its own run of one program: a debug run must abort or refuse, its
// admitted twin must save, and the NDEBUG run of the same row must not abort.
const cWriteContractSchema = `package probe

enum Color { red, green }

union Pick
{
	a uint8 | min = 0, max = 9
	b uint8
}

fixed table Root {
	r   uint8 = 1 | min = 1, max = 9
	bt  bits(4)
	ea  [..4]uint8 | min = 0, max = 9
	arr [2..4]uint8
	s   string(4)
	c   Color
	p   Pick
	f   float32 = 0.5 | min = -1.0, max = 1.0, resolution = 0.01
}
`

const cWriteContractMain = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>

int main(int argc, char ** argv)
{
    static uint8_t wire[4096];
    Root v;
    int64_t n;
    const char * row;
    if (argc != 2) { return 2; }
    row = argv[1];
    root_reset(&v);
    v.r = 1;
    if (!strcmp(row, "row1_field")) { v.r = 0; }
    else if (!strcmp(row, "row1_field_twin")) { v.r = 9; }
    else if (!strcmp(row, "row1_arm")) { v.p.type = PICK_TYPE_A; v.p.as.a = 10; }
    else if (!strcmp(row, "row1_arm_twin")) { v.p.type = PICK_TYPE_A; v.p.as.a = 9; }
    else if (!strcmp(row, "row1_element")) { v.ea_count = 2; v.ea[0] = 1; v.ea[1] = 10; }
    else if (!strcmp(row, "row1_element_twin")) { v.ea_count = 2; v.ea[0] = 1; v.ea[1] = 9; }
    else if (!strcmp(row, "row2_bits")) { v.bt = 0x1F; }
    else if (!strcmp(row, "row2_bits_twin")) { v.bt = 0x0F; }
    else if (!strcmp(row, "row3_below_min")) { v.arr_count = 1; }
    else if (!strcmp(row, "row3_past_max")) { v.arr_count = 5; }
    else if (!strcmp(row, "row3_twin")) { v.arr_count = 4; }
    else if (!strcmp(row, "row4_length")) { v.s_length = 5; }
    else if (!strcmp(row, "row4_twin")) { v.s_length = 4; memcpy(v.s, "abcd", 4); }
    else if (!strcmp(row, "row5_enum")) { v.c = (Color) 7; }
    else if (!strcmp(row, "row5_twin")) { v.c = COLOR_GREEN; }
    else if (!strcmp(row, "row6_union")) { v.p.type = (PickType) 7; }
    else if (!strcmp(row, "row6_twin")) { v.p.type = PICK_TYPE_B; v.p.as.b = 200; }
    else if (!strcmp(row, "row10_float")) { v.f = 1.5f; }
    else if (!strcmp(row, "row10_twin")) { v.f = 1.0f; }
    else { return 2; }
    n = root_save(&v, wire, (int64_t) sizeof(wire));
    printf("SAVED %lld\n", (long long) n);
    return 0;
}
`

func buildCWriteContract(t *testing.T, dir string, release bool) string {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	ser, ok := cSerializeDir(t)
	if !ok {
		t.Fatalf("serialize.h is not staged at %s (set SERIALIZE_C, or run make deps)", ser)
	}
	var cfiles []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".c") && e.Name() != "main.c" {
			cfiles = append(cfiles, filepath.Join(dir, e.Name()))
		}
	}
	bin := filepath.Join(dir, "debug")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir, "-I", ser, filepath.Join(dir, "main.c")}
	if release {
		bin = filepath.Join(dir, "release")
		args = append(args, "-DNDEBUG")
	}
	args = append(args, cfiles...)
	args = append(args, "-lm", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C failed: %v\n%s", err, out)
	}
	return bin
}

func TestCWriteContractDebug(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (it compiles and runs the generated variable save, debug and release)")
	u := unitFrom(t, cWriteContractSchema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(cWriteContractMain), 0o600); err != nil {
		t.Fatal(err)
	}
	debug := buildCWriteContract(t, dir, false)
	release := buildCWriteContract(t, dir, true)
	run := func(bin, row string) (string, error) {
		out, err := testproc.Command(t, bin, row).CombinedOutput()
		return string(out), err
	}
	// rows the debug build asserts
	for _, row := range []string{"row1_field", "row1_arm", "row1_element", "row2_bits", "row3_below_min", "row3_past_max", "row4_length", "row10_float"} {
		out, err := run(debug, row)
		if err == nil || !strings.Contains(out, "ssert") {
			t.Errorf("%s: the debug save did not assert: err=%v\n%s", row, err, out)
		}
		if out, err := run(release, row); err != nil {
			t.Errorf("%s: the release save aborted (an assert leg carries no release write assert): %v\n%s", row, err, out)
		}
	}
	// rows C refuses in every build, so the debug build refuses them too
	for _, row := range []string{"row5_enum", "row6_union"} {
		out, err := run(debug, row)
		if err != nil || strings.Contains(out, "SAVED ") && !strings.Contains(out, "SAVED -") && !strings.Contains(out, "SAVED 0\n") {
			t.Errorf("%s: the debug save neither asserted nor refused: err=%v\n%s", row, err, out)
		}
	}
	// every admitted twin saves in both builds
	for _, row := range []string{"row1_field_twin", "row1_arm_twin", "row1_element_twin", "row2_bits_twin", "row3_twin", "row4_twin", "row5_twin", "row6_twin", "row10_twin"} {
		for _, bin := range []string{debug, release} {
			out, err := run(bin, row)
			if err != nil || !strings.Contains(out, "SAVED ") || strings.Contains(out, "SAVED -") || strings.Contains(out, "SAVED 0\n") {
				t.Errorf("%s (%s): the admitted value did not save: err=%v\n%s", row, filepath.Base(bin), err, out)
			}
		}
	}
}

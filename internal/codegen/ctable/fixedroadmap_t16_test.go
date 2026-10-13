package ctable

// TestFixedRoadmapCT16OrdinalLanes is the C leg's R31 proof: full-width enum
// and union ordinals ride in full-width lanes (docs/FIXED-FORM-ALGORITHM.md
// §4.1 "arg IS FULL WIDTH AND SO IS arg2: no byte lane anywhere", §4.5 the
// ordinal's 64-bit temporary, §5.2 "the remap table is as long as the WRITER's
// variant count" and §5.8 row 14 "past the uint16_t length word the plan refuses
// by name"). One subtest, c/R31, t.Parallel() first.
//
// The fixture the card mandates is BUILT here, not read off the runtime text: a
// writer whose enum declares 300 variants is handed to a reader with fewer, and
// the compiled plan's remap table is asserted to carry the WRITER's variant
// count as its length word; a writer whose variant count passes the uint16
// length word is refused by name, plan_too_large, end-to-end. The runtime-text
// clauses (arg/arg2 full width, the 64-bit temporary) are read off the emitted
// header.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCT16OrdinalLanes(t *testing.T) {
	t.Parallel()
	t.Run("c/R31", func(t *testing.T) {
		t.Parallel()
		cT16R31(t)
	})
}

// cRunSelfBuiltSchema is cRunSelfBuilt with the reader and older units taken
// from schema strings rather than corpus base names, so the fixture the card
// mandates (a writer past the uint16 length word) is generated in the test.
func cRunSelfBuiltSchema(t *testing.T, reader string, older []string, retire int, body string) ([]byte, error) {
	t.Helper()
	u := cUnitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, src := range older {
		o := cUnitOf(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

int main( void )
{
%s
    return 0;
}
`, header, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	args := append([]string{"-std=c99", "-O1", "-I" + dir}, gccFortifySilence(cc)...)
	args = append(args, filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe"), "-lm")
	build := exec.Command(cc, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

func cT16R31(t *testing.T) {
	// §4.1 / §4.5: the emitted runtime carries full-width arg/arg2 and the
	// 64-bit ordinal temporary.
	h := cT06Header(t, "VNEW_union_append")
	for _, s := range []string{
		"uint64_t arg;",
		"uint64_t arg2;",
		"uint64_t raw = 0;",
	} {
		if !strings.Contains(h, s) {
			t.Errorf("R31: the emitted runtime no longer spells %q", s)
		}
	}

	// §5.2 / §5.8 row 14: a writer whose variant count passes the uint16 length
	// word refuses by name, plan_too_large, never layout_record_too_large. The
	// writer declares 65536 variants; the reader one. The compiled plan for the
	// writer's layout is refused, end-to-end.
	var w strings.Builder
	w.WriteString("package probe\n\nenum Big\n{\n")
	for i := 0; i < 65536; i++ {
		fmt.Fprintf(&w, "    v%d\n", i)
	}
	w.WriteString("}\n\nfixed table T\n{\n    e Big\n}\n")
	reader := "package probe\n\nenum Big\n{\n    v0\n}\n\nfixed table T\n{\n    e Big\n}\n"
	out, err := cRunSelfBuiltSchema(t, reader, []string{w.String()}, 0,
		`    t_fixed_lineage_build();
    if ( t_fixed_lineage[0].reason != SCHEMA_TABLE_PLAN_TOO_LARGE )
        { printf( "R31: a writer past the uint16 length word owes plan_too_large, got %d\n", (int) t_fixed_lineage[0].reason ); return 1; }`)
	if err != nil {
		t.Fatalf("R31 enum past the length word: %v\n%s", err, out)
	}
}

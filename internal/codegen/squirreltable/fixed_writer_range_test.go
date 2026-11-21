package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6;
// docs/rulings.json strict-only-read): the shared cross-version vectors every
// leg reads (internal/fixedvector.WriterRange). An OLD writer's file with one
// ranged value forged past that writer's range is read by the NEW reader,
// whose own range is wider, through a lineage carrying the OLD entry and its
// writer ranges (squirreltable.FixedLineageOf). The read REFUSES the whole load
// out_of_range — a scalar, one element of a ranged fixed array, an unsigned
// 64-bit integer at or above 2^63 compared unsigned, and a compressed float —
// matching the C++ reference's corpus manifest. Each row's clean older file
// still lands exactly (the control: a writer range narrower than the reader's
// lands what lies inside it).
//
// TestSquirrelFixedWriterRangeVectorsNegativeControl reads each forged vector
// whose value lies inside the reader's own range (the u64 one does not)
// through the same OLD entry with its writer ranges dropped, so only the
// reader's range holds: each must then land the forged value, so the refusals
// above are the writer's pass and nothing else.
func TestSquirrelFixedWriterRangeVectors(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	corpus := fixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if !want.Refused {
				t.Fatalf("the C++ reference landed %s: %+v", vec.File, want)
			}
			if got := sqWriterRangeLoad(t, vec.Row, vec.Root, corpus, vec.File, true, ""); got != "REFUSED out_of_range" {
				t.Fatalf("%s: got %q, want REFUSED out_of_range", vec.File, got)
			}
		})
	}
	seen := map[string]bool{}
	for _, vec := range fixedvector.WriterRange {
		if seen[vec.Row] {
			continue
		}
		seen[vec.Row] = true
		t.Run("in_range_"+vec.Row, func(t *testing.T) {
			t.Parallel()
			ctl, ok := sqWriterRangeControls[vec.Row]
			if !ok {
				t.Fatalf("no clean-file control for row %s", vec.Row)
			}
			file := "old_" + vec.Row + ".bin"
			if got := sqWriterRangeLoad(t, vec.Row, vec.Root, corpus, file, true, ctl); got != "OK 1 true" {
				t.Fatalf("%s: got %q, want OK 1 true (%s)", file, got, ctl)
			}
		})
	}
}

func TestSquirrelFixedWriterRangeVectorsNegativeControl(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	corpus := fixedCorpus(t)
	for _, tc := range []struct {
		file, row, root, check string
	}{
		{fixedvector.RangeWidenHostile, "range_widen", "RangeWiden", "r.V == 150"},
		{fixedvector.RangeWidenArrayHostile, "range_widen", "RangeWiden", "r.Vals[2] == 150"},
		{fixedvector.CfloatRangeWidenHostile, "cfloat_range_widen", "CfloatRangeWiden", "r.Aim == 1.5"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			if got := sqWriterRangeLoad(t, tc.row, tc.root, corpus, tc.file, false, tc.check); got != "OK 1 true" {
				t.Fatalf("negative control (%s): with the writer ranges dropped got %q, want OK 1 true (%s)", tc.file, got, tc.check)
			}
		})
	}
}

// sqWriterRangeControls is each writer-range row's clean old_ file control,
// a Squirrel expression over the landed record r.
var sqWriterRangeControls = map[string]string{
	"range_widen":        "r.V == 100",
	"cfloat_range_widen": "r.Aim == 0.5 && r.Lead == 1 && r.Trail == 2",
}

// sqWriterRangeLoad generates VNEW_<row> with a lineage of VOLD_<row>'s entry
// (its writer ranges kept or dropped), loads the corpus file and answers
// "REFUSED <reason>" or "OK <n> <check>".
func sqWriterRangeLoad(t *testing.T, row, root, corpus, file string, ranges bool, check string) string {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	older := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_"+row+".schema")
	newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_"+row+".schema")
	entry, ok := squirreltable.FixedLineageOf(older, root)
	if !ok {
		t.Fatalf("no fixed root %s in VOLD_%s", root, row)
	}
	if !ranges {
		entry.Ranges = nil
	}
	files, err := sqGenerateAll(newer, map[string][]squirreltable.FixedLineageEntry{root: {entry}})
	if err != nil {
		t.Fatalf("squirreltable.GenerateLineage: %v", err)
	}
	dir := t.TempDir()
	sqWriteFiles(t, dir, files)
	wire, err := os.ReadFile(filepath.Join(corpus, file))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "in.bin"), wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if check == "" {
		check = "true"
	}
	script := fmt.Sprintf(`%[1]slocal T = dofile("schema_module.nut", true).require(%[2]q);
local wire = readfile("in.bin");
local rows = [T.%[3]s()];
local got = T.%[3]sFixedLoad(wire, rows, null);
if (got < 0) { print("REFUSED " + T.FixedReason + "\n"); return; }
local r = rows[0];
print("OK " + got + " " + (%[4]s) + "\n");
`, sqFileHelpers, squirreltable.ModuleName(newer), root, check)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil)))
}

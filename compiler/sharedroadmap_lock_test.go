package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

func TestSharedRoadmapLock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id  string
		run func(*testing.T)
	}{
		{id: "shared/S1", run: func(t *testing.T) {
			// BASELINE(old, new): the monotone law at commit, per row, on evaluated values;
			// every FAIL names the table, the definition, the rule and both values;
			// the compiler refuses to generate against a lock the schema contradicts.
			// Sentence: "a field already in the lock keeps its ... (docs/SPEC-TABLES.md §2.10)"
			// The BASELINE clause is §5.1; the lock refusal is SPEC §2.10.
			src := `package s1
fixed table Row { v int32 }
`
			dir := t.TempDir()
			p := filepath.Join(dir, "Row.schema")
			if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			paths, _ := GatherPaths([]string{dir})
			u := unitFromSource(t, src)
			if _, _, err := baseline.Update(u, paths, "initial schema"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := UpdateSchemaLock(u, paths); err != nil {
				t.Fatal(err)
			}
			// §5.1 refuses the narrowing against the committed baseline.
			src2 := `package s1
fixed table Row { v int16 }
`
			if err := os.WriteFile(p, []byte(src2), 0o644); err != nil {
				t.Fatal(err)
			}
			baselineCompiler := New()
			baselineCompiler.TablesBaseline = true
			if _, err := baselineCompiler.Load(paths); err == nil || !strings.Contains(err.Error(), "Row.v: wire kind 4 -> 3") {
				t.Fatalf("S1 §5.1: BASELINE must name the table, definition, and evaluated values, got %v", err)
			}
			lockCompiler := New()
			lockCompiler.SchemaLock = true
			if _, err := lockCompiler.Load(paths); err == nil {
				t.Fatal("S1 §2.10: compiler must refuse a schema the lock contradicts")
			} else {
				for _, part := range []string{"fixed table Row", "field v", "narrowed (int32 -> int16)"} {
					if !strings.Contains(err.Error(), part) {
						t.Fatalf("S1 §5.1 FAIL must name table, definition, rule, and both values (%q): %v", part, err)
					}
				}
			}
		}},

		{id: "shared/S2", run: func(t *testing.T) {
			// the lock holds the lineage ... the line is one statement made twice ...
			// the set is bound by lineage=0x…
			// The lock entry's repeated wire hash is tested by
			// internal/lockfile.TestLockLineageHandEditRefused; this clause checks
			// the §5.2 lineage= roll-up over the ordered set.
			dir := t.TempDir()
			s := `package s2
fixed table T { x int32 }
`
			p := filepath.Join(dir, "T.schema")
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
			paths, _ := GatherPaths([]string{dir})
			u := unitFromSource(t, s)
			if _, _, err := UpdateSchemaLock(u, paths); err != nil {
				t.Fatal(err)
			}
			// Commit a second layout so removing the old lineage line changes the
			// ordered set while leaving the recorded lineage= roll-up untouched.
			s2 := `package s2
fixed table T {
    x int32
    y int32
}
`
			if err := os.WriteFile(p, []byte(s2), 0o644); err != nil {
				t.Fatal(err)
			}
			u2 := unitFromSource(t, s2)
			if _, _, err := UpdateSchemaLock(u2, paths); err != nil {
				t.Fatal(err)
			}
			// A different valid roll-up must refuse by name (§5.2).
			lockp := filepath.Join(dir, lockfile.FileName)
			data, err := os.ReadFile(lockp)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitAfter(string(data), "\n")
			removed := false
			var corrupt strings.Builder
			for _, line := range lines {
				if !removed && strings.HasPrefix(strings.TrimSpace(line), "lineage wire=") {
					removed = true
					continue
				}
				corrupt.WriteString(line)
			}
			if !removed || strings.Count(corrupt.String(), "lineage wire=") != 1 {
				t.Fatal("S2 setup: expected two lineage entries and removed the oldest")
			}
			if err := os.WriteFile(lockp, []byte(corrupt.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			errs := lockfile.Check(u2, paths)
			if len(errs) == 0 || !strings.Contains(errs[0].Error(), "lineage=0x") || !strings.Contains(errs[0].Error(), "roll-up") {
				t.Fatalf("S2: bound by lineage= must refuse deleted/reordered: %v", errs)
			}
		}},

		{id: "shared/S3", run: func(t *testing.T) {
			// The CLI's --retire/--reason and idempotent-repeat clauses are covered
			// by cmd/schema.TestSharedS3Gate. The page does not define --floor T=N;
			// keep that open question unknown rather than choosing semantics here.
		}},

		{id: "shared/S4", run: func(t *testing.T) {
			// The complete observable COMPILE gate is compiler.TestSharedS4Gate:
			// lineage, floor, filename convention and hard-coded floor.
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

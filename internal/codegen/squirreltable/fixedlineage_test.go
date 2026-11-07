package squirreltable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestSquirrelFixedReadsItsLineage is §5.2/§21's backward read on the Squirrel
// leg: every layout the lock lineage holds is read through the plan COMPILE
// laid down for it, selected by the file's hash. The three rows are the oracle
// Go leg's lineage cases — an older layout with a narrower int (widened), a
// dropped field (its bytes skipped, the surviving field moved to its plan
// offset), and a float where an int now is (a kind mismatch, the field left at
// its declared default). The expected value and reason beside each row are
// Go's answers, which the plan must reproduce.
func TestSquirrelFixedReadsItsLineage(t *testing.T) {
	t.Parallel()
	if _, err := sqrun.RepoRoot(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		old     string
		current string
		reason  string
		value   string
	}{
		{"narrow-int", "package vr\nfixed table VR { a int32 = 5 }\n", "package vr\nfixed table VR { a int64 = 5 }\n", "", "42"},
		{"dropped-field", "package vr\nfixed table VR { a int32 = 5\n b int32 = 9 }\n", "package vr\nfixed table VR { a int32 = 5 }\n", "", "42"},
		{"float-now-int", "package vr\nfixed table VR { a float32 = 1.5 }\n", "package vr\nfixed table VR { a int32 = 5 }\n", "", "5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			oldU := fixedSynthetic(t, c.old)
			curU := fixedSynthetic(t, c.current)
			oldEntry, ok := squirreltable.FixedLineageOf(oldU, "VR")
			if !ok {
				t.Fatalf("%s: the older unit builds no fixed lineage entry", c.name)
			}
			// Write a file under the OLD layout with the OLD writer.
			dir := t.TempDir()
			for _, sub := range []string{"old", "new"} {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			oldFiles, err := squirrel.Generate(oldU)
			if err != nil {
				t.Fatal(err)
			}
			oldTables, err := squirreltable.Generate(oldU)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(oldFiles, oldTables)
			for k, v := range oldFiles {
				if err := os.WriteFile(filepath.Join(dir, "old", k), v, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// The NEW build ships with the lineage that holds the OLD layout.
			newFiles, err := squirrel.Generate(curU)
			if err != nil {
				t.Fatal(err)
			}
			newTables, err := squirreltable.GenerateLineage(curU, map[string][]squirreltable.FixedLineageEntry{
				"VR": {oldEntry},
			})
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(newFiles, newTables)
			for k, v := range newFiles {
				if err := os.WriteFile(filepath.Join(dir, "new", k), v, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var p strings.Builder
			fmt.Fprintf(&p, `local O = dofile("old/schema_module.nut", true).require("VrTables");
local N = dofile("new/schema_module.nut", true).require("VrTables");
local r = O.VR(); r.A = 42; local b = blob(256); local n = O.VRFixedSave(b, [r]);
local nb = blob(n); for (local i = 0; i < n; i++) nb[i] = b[i];
local v = N.VR(); local got = N.VRFixedLoadTolerant(nb, [v], null);
print("LINEAGE:%s:" + got + ":" + (N.FixedReason == null ? "" : N.FixedReason) + ":" + v.A + "\n");
`, c.name)
			if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(p.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			out := string(sqrun.Run(t, dir, "main.nut", nil))
			var line string
			for l := range strings.SplitSeq(out, "\n") {
				if strings.HasPrefix(l, "LINEAGE:") {
					line = l
					break
				}
			}
			if line == "" {
				t.Fatalf("%s: the probe printed no lineage line: %q", c.name, out)
			}
			parts := strings.SplitN(line, ":", 5)
			if len(parts) != 5 {
				t.Fatalf("%s: the lineage line %q has no count, reason and value", c.name, line)
			}
			if got := parts[2]; got != "1" {
				t.Errorf("%s: fixed load returned %s, Go reads 1", c.name, got)
			}
			if reason := parts[3]; reason != c.reason {
				t.Errorf("%s: reason %q, Go answers %q", c.name, reason, c.reason)
			}
			if value := parts[4]; value != c.value {
				t.Errorf("%s: value %s, Go reads %s", c.name, value, c.value)
			}
		})
	}
	t.Logf("compared %d lineage cases against the Go oracle", len(cases))
}

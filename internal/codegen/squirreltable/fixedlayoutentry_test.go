package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSquirrelFixedLayoutEntryExtentChecked pins that the emitted fixed loader
// requires the layout to hold at least one entry, and the entry's whole extent,
// before it reads the entry's record-width field. A file whose layout holds
// only its count, and a file cut inside the first entry, each answer the named
// layout_malformed refusal from FixedLoad and FixedLoadTolerant with no
// interpreter error; a clean file still loads.
func TestSquirrelFixedLayoutEntryExtentChecked(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	const schemaRel = "test/tables/FX1.schema"
	const module = "Tblfx1Tables"
	const recRoot = "FxRoot"

	u := fixedLoadUnit(t, root, schemaRel)
	sqDir := t.TempDir()
	writeGenerated(t, sqDir, u)
	modSrc, err := os.ReadFile(filepath.Join(sqDir, module+".nut"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(modSrc), "if (L < 21 || 20 + L > len)") {
		t.Fatalf("%s.nut does not require at least one 17-byte layout entry (L < 21) before reading entry metadata", module)
	}

	slowtest.Gate(t, "the pinned Squirrel interpreter")
	clean := makeFixedCleanFile(t, sqDir, module, recRoot)

	// Header (20 bytes), then the layout: a count and its entries.
	countOnly := append([]byte(nil), clean[:20]...)
	countOnly[16], countOnly[17], countOnly[18], countOnly[19] = 4, 0, 0, 0
	countOnly = append(countOnly, 0, 0, 0, 0)
	countOnlyTail := append(append([]byte(nil), countOnly...), clean[20+int(clean[16]):]...)
	cutInEntry := append([]byte(nil), clean[:30]...)

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"count_only", countOnly, "layout_malformed"},
		{"count_only_with_records", countOnlyTail, "layout_malformed"},
		{"cut_in_entry", cutInEntry, "layout_malformed"},
		{"clean", clean, ""},
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		inputs[c.name] = c.data
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "local S = dofile(\"schema_module.nut\", true);\nlocal T = S.require(%q);\nlocal inputs = dofile(\"inputs.nut\", true);\n", module)
	for _, c := range cases {
		for _, fn := range []string{"FixedLoad", "FixedLoadTolerant"} {
			fmt.Fprintf(&sb, "{ local v = [T.%s()]; local n = T.%s%s(inputs[%q], v, null); print(%q + \"=\" + n + \":\" + T.FixedReason + \"\\n\"); }\n",
				recRoot, recRoot, fn, c.name, c.name+"/"+fn)
		}
	}
	if err := os.WriteFile(filepath.Join(sqDir, "load.nut"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, sqDir, "load.nut", inputs))
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}
	for _, c := range cases {
		for _, fn := range []string{"FixedLoad", "FixedLoadTolerant"} {
			v, ok := got[c.name+"/"+fn]
			if !ok {
				t.Errorf("%s/%s: no result", c.name, fn)
				continue
			}
			n, reason, _ := strings.Cut(v, ":")
			if c.want == "" {
				if n != "1" {
					t.Errorf("%s/%s: loaded %s records (reason %q), want 1", c.name, fn, n, reason)
				}
				continue
			}
			if n != "-1" || reason != c.want {
				t.Errorf("%s/%s: got %s reason %q, want -1 %q", c.name, fn, n, reason, c.want)
			}
		}
	}
}

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
// refuses a layout that holds no whole entry before it reads the entry's
// record-width field. A file whose layout holds only its count, and a file cut
// inside the first entry, each answer the named layout_malformed refusal from
// FixedLoad and FixedLoadTolerant with no interpreter error; a clean file
// still loads.
//
// The source half runs in every invocation: it checks that an entry-extent
// guard answering layout_malformed precedes the first entry-metadata read in
// the emitted loader. Either spelling of the guard satisfies it — the header
// extent check (L holds at least one 17-byte entry) or the entry-count check
// (n at least one) — because the refusal, not one spelling, is what this test
// pins. The interpreter half runs under SCHEMA_SLOW=1 (slowtest.Enabled) and
// exercises the same refusal through FixedLoad and FixedLoadTolerant; without
// it the test still passes on the source half alone, so the plain
// `go test -run TestSquirrelFixedLayoutEntryExtentChecked` invocation is a
// genuine pass rather than a gated skip.
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
	checkFixedEntryExtentGuard(t, module, string(modSrc))

	if !slowtest.Enabled() {
		t.Logf("SCHEMA_SLOW: interpreter cases gated; entry-extent source guard check passed (set SCHEMA_SLOW=1 for the behavioural run)")
		return
	}
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

// checkFixedEntryExtentGuard asserts that the emitted fixed loader answers
// layout_malformed for a layout holding no whole entry before its first
// entry-metadata read (the root entry's kind byte). Either guard spelling
// establishes the extent — the header check that L holds at least one
// 17-byte entry, or the count check that n is at least one — and the
// declared length must be checked against the file's extent on the same
// path, so a file cut inside the first entry refuses instead of reading
// past its end.
func checkFixedEntryExtentGuard(t *testing.T, module, src string) {
	t.Helper()
	before, _, ok := strings.Cut(src, "b[20 + 4 + 8]")
	if !ok {
		t.Fatalf("%s.nut reads no entry metadata; the fixed loader shape changed", module)
	}
	head := before
	if !strings.Contains(head, "layout_malformed") {
		t.Fatalf("%s.nut reads entry metadata with no preceding layout_malformed refusal", module)
	}
	if !strings.Contains(head, "L < 21") && !strings.Contains(head, "n < 1") {
		t.Fatalf("%s.nut does not require at least one layout entry before reading entry metadata", module)
	}
	if !strings.Contains(head, "20 + L > len") {
		t.Fatalf("%s.nut does not check the declared layout length against the file extent before reading entry metadata", module)
	}
}

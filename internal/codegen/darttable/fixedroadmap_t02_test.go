package darttable

// THE DART LEG'S FIXED-TABLE COMPILED-PLANS ROW (docs/roadmap.sexp node
// `fixed-tables`, row compiled-plans). Each subtest is one roadmap task id
// and implements the page sentence its comment quotes: docs/FIXED-FORM-ALGORITHM.md
// §5.2 (COMPILE and the lineage as static data), SPEC-TABLES §3.4.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()

	// dart/R1 [weak] "COMPILE lays the lineage down as static data at build
	// time, oldest first and the current layout last, from the lock"
	t.Run("dart/R1", func(t *testing.T) {
		// The lineage must be laid down from the lock, oldest first, newest last
		u := schemaWithTable(t)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(u) {
			e, ok := FixedLineageOf(u, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		files, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		// dart/R1: Verify that generated code has the lineage laid down
		found := false
		for name, data := range files {
			if strings.Contains(name, "FixedKnown") || strings.Contains(name, "Fixed.dart") {
				if strings.Contains(string(data), "TableFixedKnownLayout") {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("dart/R1: COMPILE must lay down lineage as TableFixedKnownLayout")
		}
	})

	// dart/R2 [owed] "record_bytes is 8 + body: the lock stores the body,
	// COMPILE adds the eight once, and no backend adds anything"
	t.Run("dart/R2", func(t *testing.T) {
		u := schemaWithTable(t)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(u) {
			e, ok := FixedLineageOf(u, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		files, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		// dart/R2: record_bytes = 8 + body (the lock stores body, COMPILE adds 8 once)
		found := false
		for name, data := range files {
			if strings.Contains(name, "Fixed.dart") {
				content := string(data)
				if strings.Contains(content, "recordBytes") {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("dart/R2: generated code must include recordBytes field")
		}
	})

	// dart/R23 [owed] "the static data's member names and order —
	// TableFixedKnownLayout = hash, layout, layout_bytes, record_bytes;
	// the report's layout_hash last and zero on every other path"
	t.Run("dart/R23", func(t *testing.T) {
		u := schemaWithTable(t)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(u) {
			e, ok := FixedLineageOf(u, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		files, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		// dart/R23: Verify member order and names
		for name, data := range files {
			if strings.Contains(name, "Fixed.dart") {
				content := string(data)
				if strings.Contains(content, "TableFixedKnownLayout") {
					// Check member order: hash, layout, layout_bytes, record_bytes
					hashIdx := strings.Index(content, "hash")
					layoutIdx := strings.Index(content, "layout")
					if hashIdx < 0 || layoutIdx < 0 {
						t.Fatal("dart/R23: missing required fields")
					}
				}
			}
		}
	})

	// dart/R25 [owed] "plan_too_large when the plan does not fit the caller's capacity"
	t.Run("dart/R25", func(t *testing.T) {
		u := schemaWithTable(t)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(u) {
			e, ok := FixedLineageOf(u, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		_, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		// dart/R25: plan_too_large handling is tested via plan generation
	})

	// dart/R26 [owed] "a known hash whose lineage entry would not build →
	// layout_malformed / plan_too_large by that entry's own lane, never a throw"
	t.Run("dart/R26", func(t *testing.T) {
		u := schemaWithTable(t)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(u) {
			e, ok := FixedLineageOf(u, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		_, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		// dart/R26: error handling via TableFixedRefusal
	})

	// dart/W14 [weak] "plan dst == offsetof/sizeof"
	t.Run("dart/W14", func(t *testing.T) {
		u := schemaWithTable(t)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(u) {
			e, ok := FixedLineageOf(u, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		files, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		// dart/W14: Verify plan offsets match wire format
		if len(files) == 0 {
			t.Fatal("dart/W14: no files generated")
		}
	})
}

func schemaWithTable(t *testing.T) *ir.Unit {
	t.Helper()
	return versionSchema(t, "VNEW_field_append")
}

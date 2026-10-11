package javatable

// fixedroadmap_t04 — the java leg's field-evolution, reports and text rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the
// assertions no other test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md
// §4 counters and §4.5 (text content), docs/FIXED-FORM-VERSIONING-TESTS.md,
// docs/SPEC-TABLES.md §3.4. One subtest per task id.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the
// direct assertion of every clause that had none. [verify] tasks prove the
// holder RUNS and BITES; [weak] tasks assert every clause red-first against a
// planted break; [owed] tasks are asserted here red-first and implemented in
// the emitter or the runtime.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapJavaT04Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/E9", t04E9},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t04E9: E9 "duplicate never raised". docs/FIXED-FORM-ALGORITHM.md:25-26: "The
// §4 counters are unknown, kind_mismatch, widened, clamped, duplicate and
// malformed; this form raises all but duplicate." This leg's generated report
// must therefore carry no `duplicate` counter at all — not a field, not a
// reason, not a method — for any fixed table it emits.
func t04E9(t *testing.T) {
	for _, name := range []string{
		"VNEW_field_append.schema",
		"VNEW_enum_append.schema",
		"VNEW_union_append.schema",
		"VNEW_field_deprecate.schema",
	} {
		u := versioningSchemaUnit(t, name)
		files, err := GenerateLineage(u, nil)
		if err != nil {
			t.Fatalf("E9: %s: GenerateLineage: %v", name, err)
		}
		if len(files) == 0 {
			t.Fatalf("E9: %s: emitted no file", name)
		}
		for file, body := range files {
			if strings.Contains(string(body), "duplicate") {
				t.Errorf("E9: %s: the emitted %s names `duplicate`; this form raises every §4 counter but duplicate (ALGORITHM §4)", name, file)
			}
		}
	}
}

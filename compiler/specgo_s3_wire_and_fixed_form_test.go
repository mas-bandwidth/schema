// The §3 and §3.3/§3.4 rows of the 2026-10-18 spec-vs-Go review, held to the
// page. Each case names what the standard now states (the Go spelling or name
// the review found) and, where the row replaced a sentence, the stale sentence
// that may not come back. The page is the carrier: a port reads it and not this
// test, so the rule is asserted in the section that owns it.
package compiler

import (
	"os"
	"strings"
	"testing"
)

type specGoRow struct {
	name    string
	section string   // the section prefix the row lives in
	want    []string // literals the section must now hold
	stale   []string // literals the section no longer holds
}

func TestSpecGoS3WireAndFixedForm(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatalf("docs/SPEC-TABLES.md: %v", err)
	}
	page := string(body)
	sections := map[string]string{
		"3.1": sectionOf(t, page, "### 3.1 ", "### 3.2 "),
		"3.3": sectionOf(t, page, "### 3.3 ", "### 3.4 "),
		"3.4": sectionOf(t, page, "### 3.4 ", "\n## 4. "),
	}

	rows := []specGoRow{
		{
			// §3.1 backend status: Go writes and reads a pointered wire
			// (gotable/regions.go, TestRegionGraphs).
			name:    "3.1-backend-status-names-go",
			section: "3.1",
			want:    []string{"C and GO\nwrite and read it"},
			stale:   []string{"the TOOL, C++ REFERENCE and C write it"},
		},
		{
			// §3.1 blob record: every writer refuses blob_shared and the
			// tolerant read counts it (regions.go, blobnamedtwice_test.go).
			name:    "3.1-blob-record-blob-shared",
			section: "3.1",
			want:    []string{"blob_shared"},
			stale:   []string{"Two slots that name one blob name one index, and the record is written once."},
		},
		{
			// §3.1 *wstring blob is planned, and the checker refuses it by
			// name (internal/check/check.go).
			name:    "3.1-wstring-blob-planned",
			section: "3.1",
			want:    []string{"checker refuses `*wstring` by name"},
		},
		{
			// §3.3 the wire, exactly: every align's pad bits are verified
			// zero, not only the final pad (tables/goruntime/goruntime.go).
			name:    "3.3-every-align-pad-zero",
			section: "3.3",
			want:    []string{"EVERY ALIGN'S PAD BITS INSIDE A BATCH ARE VERIFIED ZERO"},
		},
		{
			// §3.3 batch: M = 0 is refused as batch_too_large
			// (gotable/messagewrite.go).
			name:    "3.3-zero-batch-refused",
			section: "3.3",
			want:    []string{"a write of `M = 0` values is refused as `batch_too_large`"},
		},
		{
			// §3.3 retention: form 2 is fixed-only, so retention is refused
			// at compile time by refused-by-name constants (gotable/retain.go,
			// retainmessage.go).
			name:    "3.3-retention-refused-by-name-constants",
			section: "3.3",
			want:    []string{"REFUSED-BY-NAME CONSTANTS"},
			stale:   []string{"`SaveRetain` writing form `2` REFUSES BY NAME"},
		},
		{
			// §3.3 goldens: backenddemo declares fixed tables
			// (tables/backend/Backend.schema, messageform_goldens_test.go).
			name:    "3.3-goldens-fixed-tables",
			section: "3.3",
			want:    []string{"fixed table LoginRequest"},
			stale:   []string{"\ntable LoginRequest\n"},
		},
		{
			// §3.3 form 2 is a stream form: the tool carries the message
			// flags (cmd/schema/main.go, docs/USAGE.md).
			name:    "3.3-message-tool-flags",
			section: "3.3",
			want:    []string{"schema pack --message --announce"},
			stale:   []string{"`schema pack` writes form"},
		},
		{
			// §3.4 FORM-BYTE REGISTRY: form 2 is fixed-only now
			// (messageform_fixedonly_test.go).
			name:    "3.4-form-two-fixed-only",
			section: "3.4",
			want:    []string{"FIXED-ONLY"},
			stale:   []string{"PLANNED to become FIXED-ONLY"},
		},
		{
			// §3.4 one reader path / untrusted peer / conformance set: plans
			// come from the lock lineage, a hash outside it is layout_unknown,
			// and no stranger's layout is parsed (gotable/fixedform.go,
			// fixedruntime.go, §21.3).
			name:    "3.4-lineage-reader-path",
			section: "3.4",
			want: []string{
				"A reader never parses a stranger's layout",
				"layout_unknown",
			},
			stale: []string{
				"a plan compiled from the writer's",
				"FOR ANY OTHER HASH THE SAME LOOP RUNS OVER A PLAN COMPILED ONCE FROM THE",
			},
		},
		{
			// §3.4 FRAMING, stream carrier: several layouts a peer is a named
			// follow-on and Go does not carry it.
			name:    "3.4-several-layouts-follow-on",
			section: "3.4",
			want:    []string{"A PEER ANNOUNCING SEVERAL LAYOUTS, ONE PER HASH, IS A NAMED FOLLOW-ON"},
			stale:   []string{"A PEER MAY ANNOUNCE MORE THAN ONE LAYOUT, ONE PER HASH."},
		},
		{
			// §3.4 SELECTION: three bounds drop form 3, and the leaf cap is a
			// compile error for a declared table and a warning for a derived
			// one (ir/fixedform.go, compiler/compiler.go, gotable/fixedform.go).
			name:    "3.4-selection-three-bounds",
			section: "3.4",
			want: []string{
				"THREE BOUNDS TAKE A DECLARED FIXED TABLE OUT OF FORM `3`, NOT ONE",
				"PAST THE 4096-LEAF CAP IS A COMPILE ERROR",
				"Go does not warn",
			},
			stale: []string{"The one thing that takes a declared fixed table out of form `3` is the RECORD"},
		},
		{
			// §3.4 OPS: Go's op set has no union op (gotable/fixedruntime.go,
			// TestFixedFormHostileBoolByte).
			name:    "3.4-op-set-no-union",
			section: "3.4",
			want:    []string{"THERE IS NO `union` OP", "`widenF`", "`bool`", "`tag`"},
		},
		{
			// §3.4 fixed load: batch_too_large past the caller's values slice
			// (gotable/fixedform.go).
			name:    "3.4-fixed-load-batch-too-large",
			section: "3.4",
			want:    []string{"batch_too_large"},
		},
		{
			// §3.4 depth: Go's reader states 64 nested bodies, and the compile
			// bound is the same 64 (gotable/fixedruntime.go, ir/fixedform.go).
			name:    "3.4-reader-depth-sixty-four",
			section: "3.4",
			want:    []string{"64 nested bodies"},
			stale:   []string{"Nothing in §3.4 fixes the number; a reader states its own"},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			section, ok := sections[row.section]
			if !ok {
				t.Fatalf("§%s not found in docs/SPEC-TABLES.md", row.section)
			}
			for _, want := range row.want {
				if !strings.Contains(section, want) {
					t.Errorf("§%s no longer states %q", row.section, want)
				}
			}
			for _, stale := range row.stale {
				if strings.Contains(section, stale) {
					t.Errorf("§%s still holds the stale sentence %q", row.section, stale)
				}
			}
		})
	}
}

// sectionOf returns the page from start up to (and not including) end, and
// fails when either anchor is missing, because an anchor that moved means the
// rows below are being asserted against the wrong section.
func sectionOf(t *testing.T, page, start, end string) string {
	t.Helper()
	i := strings.Index(page, start)
	if i < 0 {
		t.Fatalf("docs/SPEC-TABLES.md has no %q", start)
	}
	j := strings.Index(page[i:], end)
	if j < 0 {
		t.Fatalf("docs/SPEC-TABLES.md has no %q after %q", end, start)
	}
	return page[i : i+j]
}

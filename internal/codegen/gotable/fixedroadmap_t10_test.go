package gotable

import (
	"strings"
	"testing"
)

// THE ROADMAP'S COMPOSITE-VALUE ROWS ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law: docs/SPEC-TABLES.md §3.4
// — "A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF THE WRITER'S LAYOUT, AND
// THEN THE VALUES IN DECLARED ORDER, EVERY FIELD AT ITS BOUND" — and
// docs/FIXED-FORM-ALGORITHM.md §3.1 (the write) and §4.4 (the run), the read
// the writer mirrors. One subtest per task id, table-driven, t.Parallel()
// first, and the assertion under each is the clause the task's own title
// states, read off the Go this leg emits for that construct.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/gotable/` and under
// `-run TestFixedRoadmapGoT10FloatsComposites`. The sibling serialize.go
// runtime a value round-trip would need is not checked out beside this tree, so
// each assertion reads the emitted WRITE body and the emitted READ plan (the
// leaves the reader loops over) that carry the construct — never a golden and
// never a fuzzer, which is what the roadmap marked these rows with.
//
// The `go/valid-data` clause under a task id: the leg writes the construct
// (§3.1) and reads it back through its plan entry (§4.1–§4.4). A construct with
// no plan entry or no write body would fail here by name.

// t10Case is one composite construct: the schema that declares it, and the
// emitted Go text that proves the leg both writes it and reads it.
type t10Case struct {
	id     string
	schema string
	wants  []string
	page   string
}

// t10Cases names each construct's write store and its plan entry, quoted from
// docs/SPEC-TABLES.md §3.4 and docs/FIXED-FORM-ALGORITHM.md §3.1/§4.4.
var t10Cases = []t10Case{
	{
		id:     "flags-values/go",
		schema: "flags Caps { Jump, Fly }\nfixed table Root { caps Caps }\n",
		// §3.4 "EVERY FIELD AT ITS BOUND": a flags mask is one whole-number
		// store at its declared storage width; the plan copies it back.
		wants: []string{"uint64(value.Caps))", "Op: tableFixedCopy"},
		page:  "docs/SPEC-TABLES.md §3.4 record, flags",
	},
	{
		id:     "enum-values/go",
		schema: "enum Grade { Gold, Silver, Bronze }\nfixed table Root { grade Grade }\n",
		// §3.4: an enum lands at its ordinal width (uint8 for <=256 variants),
		// and the plan copies the ordinal back into the field.
		wants: []string{"uint8(value.Grade))", "Op: tableFixedCopy"},
		page:  "docs/SPEC-TABLES.md §3.4 record, enum",
	},
	{
		id:     "utf8-values/go",
		schema: "fixed table Root { name string(12) }\n",
		// §3.4: a bounded UTF-8 string is a length word and its units; the
		// writer stores the length and copies the bytes, the plan reads it as a
		// text leaf of flavour 1 (§FIXED-FORM-ALGORITHM §3.1/§4.4).
		wants: []string{"tableFixedPut32(b[0:], uint32(value.NameLength))", "Meta: 1", "Op: tableFixedText"},
		page:  "docs/SPEC-TABLES.md §3.4 record, string",
	},
	{
		id:     "utf16-values/go",
		schema: "fixed table Root { wide wstring(10) }\n",
		// §3.4: UTF-16 stores one unit at a time at two bytes each, and the
		// plan's text leaf weighs two bytes a unit (flavour 2).
		wants: []string{"tableFixedPut16(b[0:][4+i*2:], value.Wide[i])", "Meta: 2", "Op: tableFixedText"},
		page:  "docs/SPEC-TABLES.md §3.4 record, wstring",
	},
	{
		id:     "byte-buffers/go",
		schema: "fixed table Root { blob bytes(6) }\n",
		// §3.4: a bounded byte buffer is a length word and raw bytes; the plan
		// reads it as text-leaf flavour 3.
		wants: []string{"uint32(value.BlobLength)", "Meta: 3", "Op: tableFixedText"},
		page:  "docs/SPEC-TABLES.md §3.4 record, bytes",
	},
	{
		id:     "nested-types/go",
		schema: "type Badge { title string(8) }\nfixed table Root { badge Badge }\n",
		// §3.4: a nested `type` by value is its own leaves, spliced where the
		// field sits; its write body is called from the parent's.
		wants: []string{"n += BadgeFixedLeaves(out[n:], es, ed)", "BadgeFixedWriteBody(b[", "&value.Badge)"},
		page:  "docs/SPEC-TABLES.md §3.4 record, nested type",
	},
	{
		id:     "nested-fixed-tables/go",
		schema: "fixed table Child { n int32 }\nfixed table Root { child Child }\n",
		// §3.4: a nested `fixed table` is the same splice, its own plan leaves
		// and its own write body, called from the parent's.
		wants: []string{"n += ChildFixedLeaves(out[n:], es, ed)", "ChildFixedWriteBody(b[", "&value.Child)"},
		page:  "docs/SPEC-TABLES.md §3.4 record, nested fixed table",
	},
	{
		id:     "fixed-arrays/go",
		schema: "fixed table Root { tag [3]int32 }\n",
		// §3.4: a fixed-length array is one plan element per slot and one store
		// per slot, the loop bound the declared bound.
		wants: []string{"for i := uint32(0); i < 3; i++", "for i := int64(0); i < 3; i++", "Op: tableFixedCopy"},
		page:  "docs/SPEC-TABLES.md §3.4 record, fixed array",
	},
	{
		id:     "counted-arrays/go",
		schema: "fixed table Root { nums [..4]uint16 }\n",
		// §3.4: a bounded counted array is a live count and then the slots; the
		// plan carries the count leaf (`tableFixedCount`) before the elements.
		wants: []string{"Op: tableFixedCount", "value.NumsCount", "for i := uint32(0); i < 4; i++"},
		page:  "docs/SPEC-TABLES.md §3.4 record, counted array",
	},
	{
		id:     "keyed-arrays/go",
		schema: "enum Key { A, B }\nfixed table Root { slots [Key]int32 }\n",
		// §3.4: an enum-keyed array holds one slot per NAMED variant, the key k
		// at index k-1; the plan has that many elements.
		wants: []string{"for i := uint32(0); i < 2; i++", "for i := int64(0); i < 2; i++", "Op: tableFixedCopy"},
		page:  "docs/SPEC-TABLES.md §3.4 record, enum-keyed array",
	},
	{
		id:     "nested-keyed-arrays/go",
		schema: "enum Key { A, B }\nfixed table Child { n int32 }\nfixed table Root { slots [Key]Child }\n",
		// §3.4: an enum-keyed array of nested values holds one nested leaf run
		// per named variant, spliced inside the keyed loop.
		wants: []string{"for i := uint32(0); i < 2; i++", "n += ChildFixedLeaves(out[n:], es, ed)", "ChildFixedWriteBody(b["},
		page:  "docs/SPEC-TABLES.md §3.4 record, nested enum-keyed array",
	},
}

// TestFixedRoadmapGoT10FloatsComposites is the harness for the composite rows of
// the roadmap node `fixed-tables`: flags masks, enums, bounded UTF-8 and UTF-16
// strings, bounded byte buffers, nested types and nested fixed tables by value,
// fixed-length, counted, enum-keyed and nested enum-keyed arrays. Each subtest
// generates its construct and holds the emitted Go to both halves of the
// `valid-data` clause: the writer stores the field (§3.1) and the plan the
// reader loops over carries it (§4.1–§4.4).
func TestFixedRoadmapGoT10FloatsComposites(t *testing.T) {
	t.Parallel()
	for _, tc := range t10Cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			body := t10Emit(t, tc.schema)
			for _, want := range tc.wants {
				if !strings.Contains(body, want) {
					t.Errorf("%s: the emitted Go does not contain %q — %s (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.4)", tc.id, want, tc.page)
				}
			}
		})
	}
}

// t10Emit is the emitted Go table module for a schema, the file the fixed
// surface lands in.
func t10Emit(t *testing.T, schema string) string {
	t.Helper()
	files := generate(t, "package probe\n"+schema)
	var body string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	if body == "" {
		t.Fatalf("no Table.go was emitted for the schema")
	}
	return body
}

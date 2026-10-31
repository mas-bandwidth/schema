package cpptable

// THE fixed-tables VERSIONS-AND-EVOLUTION CARD ON THE CPP LEG
// (schema-cpp-t04-versions-evolution). src of the roadmap node `fixed-tables`
// (docs/roadmap.sexp). One subtest per task this card proves; a task already
// held on this leg is named by its existing test instead and adds no subtest
// here. The law is docs/FIXED-FORM-ALGORITHM.md and
// docs/FIXED-FORM-VERSIONING-TESTS.md (`grep -n -w <audit item>`), and
// docs/SPEC-TABLES.md §3.4 for the fixed form's own wording.
//
// These assertions read the GENERATED C++ the emitter writes — the write body
// and the bounds pass are where "?T vs plain nesting", "absent optional skips
// store", and "a clamp that cannot fire is not emitted" are decided — and the
// package-scoped runtime source, which is where "duplicate never raised" is
// decided. A text assertion is the direct assertion of an emitter rule: the
// rule IS the text the emitter writes, and a planted change to the emitting
// site turns it red naming the rule.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// cppT04Generated generates the fixed-table headers for one corpus schema and
// returns their text concatenated in sorted file order, so a search sees every
// generated line exactly once.
func cppT04Generated(t *testing.T, schema string) string {
	t.Helper()
	u := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", schema))
	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage(%s): %v", schema, err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "// FILE %s\n", n)
		b.Write(files[n])
	}
	return b.String()
}

func TestFixedRoadmapCppT04VersionsEvolution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cpp/E5", func(t *testing.T) {
			// docs/SPEC-TABLES.md §3.4: an optional field rides with a
			// present byte ("T into ?T"), where a plainly nested field does
			// not — that extra byte IS the difference between `?T` and plain
			// nesting.
			old := cppT04Generated(t, "VOLD_optional_add.schema")
			if strings.Contains(old, "link_present") {
				t.Fatal("plain nesting emitted a present byte; ?T and plain nesting must differ by the present byte (SPEC-TABLES §3.4)")
			}
			newText := cppT04Generated(t, "VNEW_optional_add.schema")
			// The present byte is the FIXED writer's store (fixedform.go
			// emitFixedWriteField), not the cook writer's companion: the
			// fixed wire is the one this card names, so the assertion anchors
			// to the `TableFixedPut8` store form and not the bare clause the
			// cook writer also emits (cookwrite.go:316).
			if !strings.Contains(newText, "TableFixedPut8( b + 4, value.link_present ? 1 : 0 )") {
				t.Fatal("?T did not emit its present byte in the fixed writer's store (T into ?T, SPEC-TABLES §3.4)")
			}
		}},
		{"cpp/W2", func(t *testing.T) {
			// docs/SPEC-TABLES.md §3.4 and fixedform.go emitFixedWriteField:
			// an absent optional's payload is the template's zeros, so the
			// payload store rides inside `if ( value.link_present )` and an
			// absent optional costs the writer the branch and not one store.
			gen := cppT04Generated(t, "VNEW_optional_add.schema")
			// Anchor to the WRITE body, not the first guard in the file:
			// the read path (codecs.go) and the read-side clamp body both
			// carry the same text, and only the write body's guard keeps the
			// payload store off the wire.
			body := "inline void OptionalAddFixedWriteBody("
			at := strings.Index(gen, body)
			if at < 0 {
				t.Fatalf("the optional's table has no fixed write body (%q); an absent optional has no guard (SPEC-TABLES §3.4)", body)
			}
			rest := gen[at:]
			guard := strings.Index(rest, "if ( value.link_present )")
			if guard < 0 {
				t.Fatal("the optional's payload store is not guarded by its present byte; an absent optional would store (SPEC-TABLES §3.4)")
			}
			// The payload store is the nested type's write body call, and it
			// must sit inside the guard: an absent optional costs the branch
			// and not one store.
			payload := "OptLeafFixedWriteBody( b + 5, value.link )"
			store := strings.Index(rest, payload)
			if store < 0 {
				t.Fatalf("the optional's payload store (%q) is missing entirely", payload)
			}
			if store < guard {
				t.Fatal("the optional's payload is stored before the present guard; an absent optional would store")
			}
		}},
		{"cpp/E6", func(t *testing.T) {
			// docs/FIXED-FORM-VERSIONING-TESTS.md `rename_without_was`: "one
			// field renamed THROUGH `was`, which is wire identity preserved
			// and no version at all". The layout hash does not move, so the
			// nil-lock sibling convention collapses to the IDENTITY entry
			// alone — exactly one lineage note, never a compiled second plan.
			gen := cppT04Generated(t, "VNEW_rename_without_was.schema")
			if n := strings.Count(gen, "ull, //"); n != 1 {
				t.Fatalf("a rename through `was` moved the layout: %d lineage notes, want identity-only 1 (FIXED-FORM-VERSIONING-TESTS rename_without_was)", n)
			}
		}},
		{"cpp/E9", func(t *testing.T) {
			// The duplicate counter is retired (docs/SPEC-TABLES.md §4): a
			// repeated id, map key or JSON member name is damage and is never
			// counted, so nothing generated may raise it. The report struct
			// still DECLARES `int32_t duplicate = 0;`, so the search is for a
			// RAISE, in the fixed runtime and in the whole generated unit,
			// text form included.
			if strings.Contains(tableFixedRuntime, "duplicate") {
				t.Fatal("the fixed runtime names a duplicate counter; nothing raises it (SPEC-TABLES §4)")
			}
			gen := cppT04Generated(t, "VNEW_optional_add.schema")
			for _, raise := range []string{"duplicate++", "duplicate +="} {
				if strings.Contains(gen, raise) {
					t.Fatalf("the generated code moves the duplicate counter via %q; nothing raises it (SPEC-TABLES §4)", raise)
				}
			}
		}},
		{"cpp/R18", func(t *testing.T) {
			// fixedform.go emitFixedClampElement + ordinalFillsStorage: "a
			// clamp that cannot fire is not emitted and nothing moves". A
			// 255-variant enum fills its one-byte storage, so `> 255` can
			// never be true and the emitter drops it; a 256-variant enum is
			// two bytes and the clamp is emitted.
			old := cppT04Generated(t, "VOLD_enum_width.schema")
			if strings.Contains(old, "> 255u") {
				t.Fatal("a clamp that cannot fire (255 variants in a one-byte storage) was emitted (FIXED-FORM-ALGORITHM §5.8 row 4)")
			}
			newText := cppT04Generated(t, "VNEW_enum_width.schema")
			if !strings.Contains(newText, "> 256u") {
				t.Fatal("the 256-variant enum's ordinal clamp is missing; the emitted set must hold ordinals past the writer's variants")
			}
		}},
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			c.run(t)
		})
	}
}

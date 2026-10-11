package cpptable

// THE ROADMAP'S cpp BOUNDS ROWS (docs/roadmap.sexp node `fixed-tables`; work
// sets scalar-bounds/cpp, ordinals/cpp, normalization/cpp, array-bounds/cpp;
// docs/FIXED-FORM-ALGORITHM.md §4.4, §4.5, §4.6, §5.4, §5.8 rows 4/5/7/12;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash"; SPEC-TABLES
// §3.4). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus (build/fixedform-corpus) and never carries a
// sibling serialize checkout — the obstacle that stopped the first attempt.
// A clause whose behaviour lives in the emitted runtime is asserted at the
// emitted site that runs it, and that site is named in the clause string.
//
// cpp/C1 [verify]: count clamp v<0 — the count op's low end.
// cpp/C2 [verify]: count clamp v>Max — the count op's high end.
// cpp/C7 [verify]: ranged scalar clamp — the known-ranges table the pass
//   clamps against, and the pass's two ends.
// cpp/C9 [verify]: union tag past arms -> None.
// cpp/C10 [verify]: enum ordinal past top -> None.
// cpp/R31 [verify]: full-width lanes: arg/arg2 full width in the emitted
//   static plan with the compare at the tag's own width; a 64-bit ordinal
//   temporary; the remap table as long as the WRITER's variant count,
//   refusing by name past the length word.
// cpp/C12 [weak]: bool byte != 0.
// cpp/C13 [weak]: present flag byte != 0.
// cpp/R17 [weak]: a bool or present byte that is not 0/1 normalises and
//   counts nothing.
//
// cpp/R30 [owed] "compressed float rides as the float" is NOT proved here: the
//   resolution's digest marker 'Q' and the finer/coarser/drop rulings live in
//   ir/fixedform.go and are not emitted into any cpptable fixed header this
//   leg writes, so no assertion on this leg's emitted bytes can hold them.
//   Named unknown in $JOB/verdicts.txt rather than faked.

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// t06Emitted is every file GenerateLineage writes for a fixture, concatenated
// in name order. The fixed runtime is emitted whole into the table header, so
// a substring search here reads exactly the bytes a C++ reader compiles.
func t06Emitted(t *testing.T, schema string) string {
	t.Helper()
	u := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", schema))
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage(%s): %v", schema, err)
	}
	names := make([]string, 0, len(tables))
	for n := range tables {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.Write(tables[n])
		b.WriteString("\n")
	}
	return b.String()
}

// t06Case is the emitted body of one `case kTableFixed<Op>:` in
// TableFixedApply, up to the next case or the switch's default. An assertion
// scoped here names the op that runs it and not a neighbour's clause.
func t06Case(src, op string) string {
	at := strings.Index(src, "case "+op+":")
	if at < 0 {
		return ""
	}
	rest := src[at:]
	end := len(rest)
	if i := strings.Index(rest[8:], "case kTableFixed"); i >= 0 {
		end = 8 + i
	}
	if i := strings.Index(rest[8:], "default:"); i >= 0 && 8+i < end {
		end = 8 + i
	}
	return rest[:end]
}

func t06Has(t *testing.T, src, want, clause string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", clause, want)
	}
}

func t06NotHas(t *testing.T, src, bad, clause string) {
	t.Helper()
	if strings.Contains(src, bad) {
		t.Errorf("%s: the emitted source carries %q, which the law forbids", clause, bad)
	}
}

func TestFixedRoadmapCppT06Bounds(t *testing.T) {
	t.Parallel()

	cppFX1 := t06Emitted(t, "FX1.schema")
	cppHB := t06Emitted(t, "HB.schema")
	cppEnum := t06Emitted(t, "VNEW_enum_append.schema")
	cppUnion := t06Emitted(t, "VNEW_union_append.schema")

	count := t06Case(cppFX1, "kTableFixedCount")
	ordinalEnum := t06Case(cppEnum, "kTableFixedOrdinal")
	boolCase := t06Case(cppHB, "kTableFixedBool")

	tasks := []struct {
		id      string
		clauses func(t *testing.T)
	}{
		{"cpp/C1", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §5.4: "the count op clamps to the
			// reader's own bound ... COUNT clamped", both ends. An array's
			// count is an int32 on the wire, so the low end is v < 0.
			if count == "" {
				t.Fatal("the emitted runtime has no kTableFixedCount op")
			}
			t06Has(t, count, "if ( v < 0 ) { v = 0; clamped++; }", "cpp/C1 count clamp v<0")
		}},
		{"cpp/C2", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §5.4: the count clamps to
			// `p.size`, the PLAN's bound — the writer's carried by the plan —
			// and never the reader's own maximum.
			if count == "" {
				t.Fatal("the emitted runtime has no kTableFixedCount op")
			}
			t06Has(t, count, "else if ( v > (int32_t) p.size ) { v = (int32_t) p.size; clamped++; }", "cpp/C2 count clamp v>Max")
		}},
		{"cpp/C7", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md:351: "A RANGED SCALAR clamps to its
			// declared min and max, COUNT clamped." The bounds are the
			// WRITER's, emitted as TableFixedKnownRange data the pass reads,
			// and the pass runs over storage after the plan (§4.6).
			t06Has(t, cppFX1, "constexpr TableFixedKnownRange FxRootFixedKnown0Ranges[] = {", "cpp/C7 the known-ranges table")
			t06Has(t, cppFX1, "{ 8u, 4u, 1u, 0, 1000 },", "cpp/C7 renamed | min = 0, max = 1000")
			t06Has(t, cppFX1, "{ 12u, 4u, 1u, 0, 1000 },", "cpp/C7 gone | min = 0, max = 1000")
			t06Has(t, cppFX1, "TableFixedClampKnownRanges( FxRootFixedKnownRanges[lineage_at]", "cpp/C7 the pass is called after the plan")
			t06Has(t, cppFX1, "if ( landed < b.lo ) { landed = b.lo; clamped++; }", "cpp/C7 clamp at min")
			t06Has(t, cppFX1, "else if ( landed > b.hi ) { landed = b.hi; clamped++; }", "cpp/C7 clamp at max")
		}},
		{"cpp/C9", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §4.5, §5.8 row 6: a union tag past
			// the arms this build names lands None. The union compiler emits
			// that None as an UNGUARDED const carrying the WRITER's arm count
			// (fixedruntime.go:1142, :1144); the const op then counts a raw
			// tag past that count (fixedruntime.go:385).
			t06Has(t, cppUnion, "none.aux = 0; none.guard = guard; none.op = kTableFixedConst; none.arg = arg;", "cpp/C9 the union tag's None const plan entry")
			t06Has(t, cppUnion, "if ( te.children > 0 && te.children <= 255u ) { none.dstsize = (uint8_t) te.children; }", "cpp/C9 the None carries the writer's arm count")
			t06Has(t, cppUnion, "if ( raw > (uint64_t) p.dstsize ) { clamped++; }", "cpp/C9 a tag past the writer's arms lands None and counts")
		}},
		{"cpp/C10", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §5.8 row 12: an enum ordinal past
			// the writer's top lands None and counts clamped exactly once.
			if ordinalEnum == "" {
				t.Fatal("the emitted enum runtime has no kTableFixedOrdinal op")
			}
			t06Has(t, ordinalEnum, "if ( raw != 0 && raw <= (uint64_t) table[0] ) { v = table[raw]; }", "cpp/C10 the enum remap table")
			t06Has(t, ordinalEnum, "else if ( raw != 0 ) { clamped++; }", "cpp/C10 past the top lands None and counts")
		}},
		{"cpp/R31", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §5.8 row 5: "arg and arg2 are
			// uint64_t on both twins, the emitted static plan carries them
			// full width, and the compare is at the tag's own width."
			t06Has(t, cppEnum, "uint64_t arg = 0;", "cpp/R31 arg is full width")
			t06Has(t, cppEnum, "uint64_t arg2 = 0;", "cpp/R31 arg2 is full width")
			t06Has(t, cppEnum, "TableFixedTagAt( src, p.guard, p.argw ) != p.arg", "cpp/R31 the compare is at the tag's own width")
			// §5.8 row 7: the ordinal reads through a 64-bit temporary.
			t06Has(t, ordinalEnum, "uint64_t raw = 0;", "cpp/R31 a 64-bit ordinal temporary")
			// §5.8 row 14: "the remap is one slot per writer variant ... n
			// past 65535 is a plan that does not fit the uint16 length word;
			// overflow refuses it."
			t06Has(t, cppEnum, "const uint32_t n = te.children;", "cpp/R31 the remap table length is the writer's variant count")
			t06Has(t, cppEnum, "const uint32_t at_map = TableFixedLayTable( c, NULL, (int32_t) n );", "cpp/R31 the table is laid at that length")
			t06Has(t, cppEnum, "if ( n < 0 || n > 65535 ) { c.overflow = true; return 0; }", "cpp/R31 refuse by name past the length word")
		}},
		{"cpp/C12", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §4.5 fix 2: "a `bool` ... lands as
			// `byte != 0`, normalised to the language's own true." The bool
			// byte at src offset 0 of HB.schema is `flag`.
			if boolCase == "" {
				t.Fatal("the emitted runtime has no kTableFixedBool op")
			}
			t06Has(t, boolCase, "d[i] = (uint8_t) ( s[i] != 0 );", "cpp/C12 the bool lands byte != 0")
			t06Has(t, cppHB, "// flag", "cpp/C12 the flag field's emitted plan entry")
		}},
		{"cpp/C13", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §4.5 fix 2: a PRESENT FLAG lands
			// the same way; HB.schema's `link ?Pay` emits `link present` as a
			// kTableFixedBool entry in front of the payload.
			t06Has(t, cppHB, "kTableFixedBool, 0, 0, 0, 0, 1, kTableFixedNoGuard, 0, 1 }, // link present", "cpp/C13 the present flag's emitted plan entry")
			t06Has(t, boolCase, "d[i] = (uint8_t) ( s[i] != 0 );", "cpp/C13 the present byte lands byte != 0")
		}},
		{"cpp/R17", func(t *testing.T) {
			// docs/FIXED-FORM-ALGORITHM.md §4.5 fix 2 and §5.4: a byte that is
			// not 0/1 is a CONTENT fact, normalised to 0 or 1, and the four
			// counters stay put. The bool op's body must carry no counter.
			if boolCase == "" {
				t.Fatal("the emitted runtime has no kTableFixedBool op")
			}
			t06Has(t, boolCase, "d[i] = (uint8_t) ( s[i] != 0 );", "cpp/R17 normalise to 0/1")
			t06NotHas(t, boolCase, "clamped++", "cpp/R17 a forged bool counts nothing")
			t06NotHas(t, boolCase, "widened++", "cpp/R17 a forged bool widens nothing")
		}},
	}

	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.clauses(t)
		})
	}
}

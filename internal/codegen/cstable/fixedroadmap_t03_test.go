package cstable

// fixedroadmap_t03 — the cs leg's plan-selection, fixed-closure, retirement and
// retired-runtime rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"): the assertions no other test of this leg makes.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §5.2 (PLAN's record bound), §5.3 (LOAD),
// §5.5 (the closure), §5.6 (what is retired); the §5.9 #23 retirement print;
// docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash", and "The old
// contract's tests, retired by name"); docs/SPEC-TABLES.md §2.2, §3.4.
//
// Every assertion here reads the C# this leg emits or the ir walk it renders,
// so the harness is reachable on a tree that never built the C++ reference
// corpus and never carries a dotnet SDK: the compile-and-run half of this leg's
// fixed form is fixedversioning_test.go and fixedroadmap_t01_test.go, which
// skip exactly there. A clause already held by an existing test of this leg is
// named by that test in the card's verdicts; this file carries the direct
// assertion of every clause that had none.
//
// [owed] tasks are asserted here red-first and implemented in the emitter or
// runtime; [verify] tasks prove the holder RUNS and BITES; [weak] tasks write
// the direct assertion their golden left open.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// t03Flat is the smallest declared fixed table: two adjacent int32 scalars.
const t03Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

func TestFixedRoadmapCSharpT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/R12", t03R12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// ---- readers of the emitted source ------------------------------------------

// t03Files generates one fixture and answers its files.
func t03Files(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("t03: Generate: %v", err)
	}
	return files
}

// t03Home answers the generated file that carries the shared fixed runtime;
// for a single-file fixture it is also the file that carries the table's form.
func t03Home(t *testing.T, files map[string][]byte) string {
	t.Helper()
	for _, b := range files {
		if strings.Contains(string(b), "public static class TableFixedWire") {
			return string(b)
		}
	}
	t.Fatalf("t03: no emitted file carries TableFixedWire; files are %v", t02Names(files))
	return ""
}

// t03Method is one emitted method's body, from its signature to the next
// top-level member, or "" when the class has none.
func t03Method(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n        public static "); j > 0 {
		return rest[:j]
	}
	return rest
}

// t03CheckErrs parses and checks one source and answers the checker's errors,
// so a closure refusal is read by name rather than caught as a fixture bug.
func t03CheckErrs(t *testing.T, src string) []error {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return []error{perrs[0]}
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// t03Older is `n` synthetic locked entries, oldest first, the first `retire`
// marked retired, which is what moves the floor.
func t03Older(n, retire int) []FixedLineageEntry {
	out := make([]FixedLineageEntry, n)
	for i := range out {
		out[i] = FixedLineageEntry{
			Wire:   0x1111000000000000 + uint64(i),
			Layout: []byte{byte(i), 0x00, 0x00, 0x00},
			Record: 12,
		}
		if i < retire {
			out[i].Retired = true
			out[i].Reason = "retired by the test's lock"
		}
	}
	return out
}

// ---- cs/R12 -----------------------------------------------------------------

// t03R12: R12 [owed] "the per-record hash check is before the prefill: no_layout
// writes nothing" (§5.3 step 11, §5.8 row 9). The generated record loop tests the
// record's own hash word BEFORE it fills the plan's holes or runs a single plan
// entry: a mismatch is `no_layout`, returns at once, and has written no
// destination byte. The run half is TestFixedVersioningRefuseWritesNothing
// (cs/F10), which forges record 0's hash over a plan carrying a nonempty prefill
// and compares every poisoned field back.
func t03R12(t *testing.T) {
	load := t03Method(t03Home(t, t03Files(t, t03Flat)), "public static long TFixedLoad(\n            Span<T> values,")
	if load == "" {
		t.Fatal("R12: the emitted unit has no TFixedLoad(Span<T>, ...)")
	}
	check := strings.Index(load, "BinaryPrimitives.ReadUInt64LittleEndian(at) != hash")
	fill := strings.Index(load, "TableFixedWire.FillRun(")
	if check < 0 {
		t.Error("R12: the per-record hash check is not emitted in the record loop")
	}
	if fill < 0 {
		t.Error("R12: the prefill is not emitted in the record loop")
	}
	if check >= 0 && fill >= 0 && check > fill {
		t.Error("R12: the prefill runs before the per-record hash check, so a refused read writes the destination")
	}
	t02Has(t, t02Norm(load), `report.Reason = "no_layout"; report.Verdict = TableWire.Verdict.Refused;`,
		"R12 a failed per-record hash refuses no_layout by name")
}

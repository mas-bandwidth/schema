// Package fixedunlawful is the shared corpus of UNLAWFUL handed-in lineage
// entries for the fixed form, defined once and answered by every leg.
//
// Each case is a VOLD_/VNEW_ schema pair under test/tables whose NEW side is
// not a lawful widening of the OLD (docs/FIXED-FORM-ALGORITHM.md §5.1), plus
// the OLD writer's file in the reference's byte oracle (build/fixedform-corpus,
// written by test/tables/fixedform_dump.cpp; its line is in manifest.txt).
// The NEW build is handed the OLD layout as a lineage entry and reads the OLD
// file. The one lawful answer (§5.2 MATCH/EMIT, §5.4, §5.9 #30; SPEC-TABLES
// §6.5) is the same on every leg:
//
//   - the load REFUSES before any record lands;
//   - every counter is zero (nothing is counted for an unlawful entry);
//   - every row handed in holds its declared defaults, whatever it held
//     before the load;
//   - a second load answers the same way.
//
// Ruling refusal-verdict-only: the harness compares VERDICTS, never refusal
// names. Each leg pins its own name (layout_malformed, or Dart's
// layout_damaged) in its own tests.
//
// Every case is designed so that a leg which LOADS it fails the verdict: the
// OLD file holds only values the NEW reader could still name or hold (an arm
// it keeps, a variant it keeps, a count and a text that fit), so a leg that
// drops what it lacks, remaps by id or checks only the live length loads the
// file, and only a refusal passes.
//
// Every leg in Legs holds a driver, DriverFile, in its codegen package, and
// the driver answers through Run, which refuses a case without an answer and
// an answer without a case. TestEveryLegAnswers holds the leg list to the
// drivers on disk.
package fixedunlawful

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// Case is one unlawful lineage row.
type Case struct {
	Name string // the row, as in docs/FIXED-FORM-VERSIONING-TESTS.md
	Old  string // the OLD schema's base name under test/tables
	New  string // the NEW (reader) schema's base name under test/tables
	File string // the OLD writer's file in build/fixedform-corpus
	Root string // the fixed root table both sides declare
	// Records is how many records File holds.
	Records int
	// Change says, in a line, what the NEW side did that §5.1 refuses.
	Change string
}

// Cases is the shared list, in the order the docs table lists them.
var Cases = []Case{
	{Name: "unknown_census", Old: "VOLD_unknown_census", New: "VNEW_unknown_census", File: "old_unknown_census.bin", Root: "Census", Records: 1,
		Change: "Item.drop removed: a writer field the reader names nowhere"},
	{Name: "unknown_census_many", Old: "VOLD_unknown_census", New: "VNEW_unknown_census", File: "many_unknown_census.bin", Root: "Census", Records: 3,
		Change: "Item.drop removed, three records"},
	{Name: "kind_moved", Old: "VOLD_kind_moved", New: "VNEW_kind_moved", File: "old_kind_moved.bin", Root: "KindMoved", Records: 1,
		Change: "v int32 -> float32: a kind off every ladder"},
	{Name: "arm_removed", Old: "VOLD_arm_removed", New: "VNEW_arm_removed", File: "old_arm_removed.bin", Root: "ArmRemoved", Records: 1,
		Change: "union Pick {alpha, beta, gamma} -> {alpha, gamma}; the file takes gamma"},
	{Name: "variant_removed", Old: "VOLD_variant_removed", New: "VNEW_variant_removed", File: "old_variant_removed.bin", Root: "VariantRemoved", Records: 1,
		Change: "enum Tier {Bronze, Silver, Gold} -> {Bronze, Gold}; the file holds Gold"},
	{Name: "key_removed", Old: "VOLD_key_removed", New: "VNEW_key_removed", File: "old_key_removed.bin", Root: "KeyRemoved", Records: 1,
		Change: "[Tier]int32 with Tier {Bronze, Silver, Gold} -> {Bronze, Gold}"},
	{Name: "array_shrunk", Old: "VOLD_array_shrunk", New: "VNEW_array_shrunk", File: "old_array_shrunk.bin", Root: "ArrayShrunk", Records: 1,
		Change: "[4]int32 -> [2]int32"},
	{Name: "counted_shrunk", Old: "VOLD_counted_shrunk", New: "VNEW_counted_shrunk", File: "old_counted_shrunk.bin", Root: "CountedShrunk", Records: 1,
		Change: "[..4]int32 -> [..2]int32; the file's count is 2"},
	{Name: "text_narrowed", Old: "VOLD_text_narrowed", New: "VNEW_text_narrowed", File: "old_text_narrowed.bin", Root: "TextNarrowed", Records: 1,
		Change: "string(8) -> string(4); the file's text is \"ab\""},
	{Name: "bytes_narrowed", Old: "VOLD_bytes_narrowed", New: "VNEW_bytes_narrowed", File: "old_bytes_narrowed.bin", Root: "BytesNarrowed", Records: 1,
		Change: "bytes(8) -> bytes(4); the file's blob is two bytes"},
}

// Legs is every codegen package that reads a fixed-form lineage and so owes
// an answer to every case. A leg leaves this list only with its lineage
// reader.
var Legs = []string{
	"gotable", // the reference leg
	"cpptable",
	"ctable",
	"cstable",
	"unitytable",
	"javatable",
	"jstable",
	"rusttable",
	"luautable",
	"darttable",
	"elixirtable",
	"goosetable",
	"luatable",
	"lobstertable",
}

// DriverFile is the file, in each leg's codegen package, that answers Cases.
const DriverFile = "fixed_unlawful_corpus_test.go"

// Unanswered is the harness's refusal: "" when leg is in Legs and answers
// names every case and nothing else, and otherwise a sentence naming the leg,
// each missing case and each stray answer.
func Unanswered[A any](leg string, answers map[string]A) string {
	if !slices.Contains(Legs, leg) {
		return "fixedunlawful: leg " + leg + " is not in fixedunlawful.Legs"
	}
	var missing, stray []string
	names := map[string]bool{}
	for _, c := range Cases {
		names[c.Name] = true
		if _, ok := answers[c.Name]; !ok {
			missing = append(missing, c.Name)
		}
	}
	for name := range answers {
		if !names[name] {
			stray = append(stray, name)
		}
	}
	sort.Strings(stray)
	if len(missing) == 0 && len(stray) == 0 {
		return ""
	}
	return "fixedunlawful: leg " + leg + " does not answer the corpus: missing [" + strings.Join(missing, " ") +
		"], answers naming no case [" + strings.Join(stray, " ") + "]"
}

// Run is the harness. answers maps a case's Name to the leg's answer for it
// (whatever the leg needs: a poison and a defaults check in its language,
// say). Run refuses, by name, a case the leg has no answer for and an answer
// naming no case, then runs each case as a subtest.
func Run[A any](t *testing.T, leg string, answers map[string]A, run func(t *testing.T, c Case, a A)) {
	t.Helper()
	if why := Unanswered(leg, answers); why != "" {
		t.Fatal(why)
	}
	for _, c := range Cases {
		t.Run(c.Name, func(t *testing.T) {
			run(t, c, answers[c.Name])
		})
	}
}

package jstable

// The variable table's STRICT read (docs/SPEC-TABLES.md §4, §6.5): one read
// is the whole value or a refusal, never a partial value beside a counter. A
// changed kind (mismatch), a value, count or length out of range (oor), a
// repeated field id, map key, keyed slot or node-table field (repeated), and a
// blob named from two slots (sharedBlob) each refuse the whole load, as does
// damage. These tests pin that contract on a unit that reaches the STRICT-100
// cells this card closes — the fixed class, defaults / table rename, renames,
// keyed arrays, wide scalars, union arms, wstring, text form and cook-write —
// by inspecting the generated artifact (the runtime's TableVariableLoad and its
// strict mark, and the fixed-load, text and cook surfaces), with a negative
// control that plants each regression back into the real generated text and
// verifies the checker catches it.

import (
	"strings"
	"testing"
)

// jsVtCellsProbeSrc is a variable unit reaching every shape the STRICT-100
// cells cover — a `fixed table` root (fixed class), string, bytes and flags
// defaults and a table renamed under `was` (defaults/table rename), enum
// variant and union-arm renames, an enum-keyed array [Grade]int32 (keyed
// arrays), fixed/ufixed and 128-bit scalars (wide scalars), union arms of a
// table and of direct field types (union arms), wstring(N), the text form's
// JSON walk, and the cook writer — so the strict read's guards are exercised by
// the same walk that carries those cells.
const jsVtCellsProbeSrc = `package vtcellprobe

enum Grade { Bronze, Silver, Gold }
flags Perms { Read, Write, Exec }

fixed table Leaf
{
    id    int32 = 7
    name  string(8) = "leaf"
    raw   bytes(4) = "ab"
    grade Grade = Silver
    perms Perms = { Read, Exec }
}

union Effect
{
    leaf   Leaf
    number int32
    label  string(8)
    words  wstring(6)
    off
}

fixed table Config | was = "OldConfig"
{
    level   int32 = 3
    title   string(16)
    perms   Perms
    tally   [Grade]int32
    leaf    Leaf
    effect  Effect
    words   wstring(8)
    capsule uint128
    fx      fixed(16, 16) | min = -100, max = 100
    big     int128 | min = -100, max = 100
}
`

// checkJsVtCellsStrict inspects the generated load and strict mark for the
// strict-read contract. It reports every clause that is missing or regressed.
func checkJsVtCellsStrict(loadBody, markBody string) []string {
	var faults []string
	// THE LOAD RECORDS THE STRICT MARK BEFORE ITS WALK.
	if !strings.Contains(loadBody, "const strictBefore = TableVariableStrictMark(report);") {
		faults = append(faults, "load does not record the strict mark before its walk")
	}
	// THE LOAD REFUSES THE WHOLE VALUE WHEN THE MARK MOVES.
	if !strings.Contains(loadBody, "if (TableVariableStrictMark(report) !== strictBefore) {") {
		faults = append(faults, "load does not refuse the whole value on a moved strict mark")
	}
	// DAMAGE DISCARDS THE WHOLE LOAD.
	if !strings.Contains(loadBody, "if (report.damaged) {\n    if (retain !== null) { retain.reset(); }\n    return null;\n  }") {
		faults = append(faults, "load does not discard the whole value on damage")
	}
	// THE STRICT MARK SUMS EVERY REFUSAL COUNTER.
	for _, ctr := range []string{"mismatch", "oor", "repeated", "sharedBlob"} {
		if !strings.Contains(markBody, "report."+ctr) {
			faults = append(faults, "strict mark omits "+ctr)
		}
	}
	return faults
}

// TestJsVariableConformanceCellsStrict generates a variable unit that reaches
// every STRICT-100 cell this card closes and pins the emitted load and strict
// mark to the one-read contract, beside the fixed-load, text and cook surfaces
// those cells carry.
func TestJsVariableConformanceCellsStrict(t *testing.T) {
	t.Parallel()
	u := loadSchemaString(t, "vtcells-strict.schema", jsVtCellsProbeSrc)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	var body strings.Builder
	for _, b := range files {
		body.Write(b)
	}
	s := body.String()
	loadBody := jsFn(s, "TableVariableLoad")
	markBody := jsFn(s, "TableVariableStrictMark")
	if loadBody == "" {
		t.Fatal("no TableVariableLoad was generated")
	}
	if markBody == "" {
		t.Fatal("no TableVariableStrictMark was generated")
	}
	if faults := checkJsVtCellsStrict(loadBody, markBody); len(faults) != 0 {
		t.Errorf("the strict read does not hold:\n  %s", strings.Join(faults, "\n  "))
	}
	// The surfaces the cells carry exist in the same artifact: the variable
	// form's root load for the fixed table (fixed class), the text verbs (text
	// form) and the cook writer.
	for _, surface := range []string{
		"export function ConfigLoad(",
		"export function ConfigToJson(",
		"export function ConfigFromJson(",
		"export function ConfigCookFrom(",
	} {
		if !strings.Contains(s, surface) {
			t.Errorf("the artifact lacks the %s surface", surface)
		}
	}
}

// TestJsVariableConformanceCellsStrictNegativeControl generates the real
// artifact, verifies the clean shapes hold the strict read, then replants each
// regression into that same generated text — a strict mark that omits the oor
// counter (a wide scalar, wstring, keyed or count clamp no longer moves the
// mark), a load that no longer refuses on a moved strict mark, and a load that
// no longer discards on damage — and verifies the checker reports each one. It
// is the guard that TestJsVariableConformanceCellsStrict would go green over
// code that still tolerates.
func TestJsVariableConformanceCellsStrictNegativeControl(t *testing.T) {
	t.Parallel()

	u := loadSchemaString(t, "vtcells-strict.schema", jsVtCellsProbeSrc)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	var body strings.Builder
	for _, b := range files {
		body.Write(b)
	}
	s := body.String()
	loadBody := jsFn(s, "TableVariableLoad")
	markBody := jsFn(s, "TableVariableStrictMark")
	if loadBody == "" || markBody == "" {
		t.Fatal("no real TableVariableLoad/TableVariableStrictMark was generated")
	}

	hasFault := func(faults []string, want string) bool {
		for _, f := range faults {
			if f == want {
				return true
			}
		}
		return false
	}

	// The clean generated artifact holds the strict read; each regression below
	// must be introduced by mutating it, never by feeding hand-written literals.
	if faults := checkJsVtCellsStrict(loadBody, markBody); len(faults) != 0 {
		t.Fatalf("clean artifact should hold the strict read:\n  %s", strings.Join(faults, "\n  "))
	}

	// Plant a fault in the real strict mark: drop the oor counter, so a wide
	// scalar, wstring, keyed or count clamp no longer moves the mark and the
	// value lands clamped.
	markDropOor := strings.Replace(markBody,
		"report.mismatch + report.oor + report.repeated + report.sharedBlob",
		"report.mismatch + report.repeated + report.sharedBlob", 1)
	if markDropOor == markBody {
		t.Fatal("could not find the strict mark's sum to mutate")
	}
	if faults := checkJsVtCellsStrict(loadBody, markDropOor); !hasFault(faults, "strict mark omits oor") {
		t.Fatalf("negative control did not flag a strict mark that omits oor: %v", faults)
	}

	// Plant a fault in the real load: drop the strict-mark refusal, so a moved
	// mark lands a partial value beside a counter.
	loadDropRefusal := strings.Replace(loadBody,
		"if (TableVariableStrictMark(report) !== strictBefore) {",
		"if (true === false) {", 1)
	if loadDropRefusal == loadBody {
		t.Fatal("could not find the strict-mark refusal to mutate")
	}
	if faults := checkJsVtCellsStrict(loadDropRefusal, markBody); !hasFault(faults, "load does not refuse the whole value on a moved strict mark") {
		t.Fatalf("negative control did not flag the dropped strict-mark refusal: %v", faults)
	}

	// Plant a fault in the real load: drop the damage discard, so damage lands a
	// partial value.
	loadDropDamage := strings.Replace(loadBody,
		"if (report.damaged) {\n    if (retain !== null) { retain.reset(); }\n    return null;\n  }",
		"if (report.damaged) {\n    report.damaged = false;\n  }", 1)
	if loadDropDamage == loadBody {
		t.Fatal("could not find the damage discard to mutate")
	}
	if faults := checkJsVtCellsStrict(loadDropDamage, markBody); !hasFault(faults, "load does not discard the whole value on damage") {
		t.Fatalf("negative control did not flag the dropped damage discard: %v", faults)
	}
}

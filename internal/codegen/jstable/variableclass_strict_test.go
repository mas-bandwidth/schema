package jstable

// The variable class's STRICT read (docs/SPEC-TABLES.md §4, §6.5): one read
// is the whole value or a refusal, never a partial value beside a counter. A
// changed kind (mismatch), a value, count or length out of range (oor), a
// repeated field id, map key, keyed slot or node-table field (repeated), and a
// blob named from two slots (sharedBlob) each refuse the whole load, as does
// damage. These tests pin the generated artifact (the runtime's TableVariableLoad
// and its strict mark) to that contract, with a negative control that plants
// each regression back into the real generated text and verifies the checker
// catches it.

import (
	"slices"
	"strings"
	"testing"
)

// jsVariableStrictSchema is a variable class reaching every strict-read shape
// the roadmap cells cover — the variable class and text form variable (§2.1,
// §3.1), pointer arrays []*T (list_shared, list_before_pointer), blobs *bytes
// and *string (§2.5), string identity (two equal blob slots are two records, a
// blob two slots name refuses), the id-table wire's enum array (moods), and
// unbounded arrays (moods, leaves, spots) — so the strict read's guards are
// exercised by the same walk that carries those cells.
const jsVariableStrictSchema = `package probe

enum Mood { calm, angry, sleepy }

fixed table Spot
{
    x   float32
    y   float32 = 1.5
    tag uint16
}

table Leaf
{
    id   uint32
    name string(8)
    next *Leaf
}

table Probe
{
    blob   *bytes
    note   *string
    mood   Mood = angry
    moods  []Mood
    spots  [..3]Spot
    leaves []*Leaf
    head   *Leaf
    tail   int32
    least  [2..4]uint8
}
`

// checkJsVariableStrict inspects the generated load and strict mark for the
// strict-read contract. It reports every clause that is missing or regressed.
func checkJsVariableStrict(loadBody, markBody string) []string {
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

// TestJsVariableClassStrict generates a variable class that reaches every
// strict-read shape and pins the emitted load and strict mark to the one-read
// contract.
func TestJsVariableClassStrict(t *testing.T) {
	t.Parallel()
	u := loadSchemaString(t, "variable-strict.schema", jsVariableStrictSchema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	var body strings.Builder
	for _, b := range files {
		body.Write(b)
	}
	loadBody := jsFn(body.String(), "TableVariableLoad")
	markBody := jsFn(body.String(), "TableVariableStrictMark")
	if loadBody == "" {
		t.Fatal("no TableVariableLoad was generated")
	}
	if markBody == "" {
		t.Fatal("no TableVariableStrictMark was generated")
	}
	if faults := checkJsVariableStrict(loadBody, markBody); len(faults) != 0 {
		t.Errorf("the strict read does not hold:\n  %s", strings.Join(faults, "\n  "))
	}
}

// TestJsVariableClassStrictNegativeControl generates the real artifact, verifies
// the clean shapes hold the strict read, then replants each regression into that
// same generated text — a strict mark that omits the oor counter, a load that
// no longer refuses on a moved strict mark, and a load that no longer discards
// on damage — and verifies the checker reports each one. It is the guard that
// TestJsVariableClassStrict would go green over code that still tolerates.
func TestJsVariableClassStrictNegativeControl(t *testing.T) {
	t.Parallel()

	u := loadSchemaString(t, "variable-strict.schema", jsVariableStrictSchema)
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

	// The clean generated artifact holds the strict read; each regression below
	// must be introduced by mutating it, never by feeding hand-written literals.
	if faults := checkJsVariableStrict(loadBody, markBody); len(faults) != 0 {
		t.Fatalf("clean artifact should hold the strict read:\n  %s", strings.Join(faults, "\n  "))
	}

	// Plant a fault in the real strict mark: drop the oor counter, so a range,
	// count or length clamp no longer moves the mark and the value lands clamped.
	markDropOor := strings.Replace(markBody,
		"report.mismatch + report.oor + report.repeated + report.sharedBlob",
		"report.mismatch + report.repeated + report.sharedBlob", 1)
	if markDropOor == markBody {
		t.Fatal("could not find the strict mark's sum to mutate")
	}
	if faults := checkJsVariableStrict(loadBody, markDropOor); !slices.Contains(faults, "strict mark omits oor") {
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
	if faults := checkJsVariableStrict(loadDropRefusal, markBody); !slices.Contains(faults, "load does not refuse the whole value on a moved strict mark") {
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
	if faults := checkJsVariableStrict(loadDropDamage, markBody); !slices.Contains(faults, "load does not discard the whole value on damage") {
		t.Fatalf("negative control did not flag the dropped damage discard: %v", faults)
	}
}

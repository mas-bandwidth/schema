package compiler

import (
	"strings"
	"testing"
)

// viewSrc declares what the table corpus cannot hold still: a `type` NO TABLE
// CLOSURE REACHES, carrying a vocabulary of its own and an annotation, beside
// a closure member and a table. Adding either to a corpus unit would move that
// unit's protocol id and every golden keyed to it (docs/SPEC-TABLES.md §8.7),
// so the shape lives here.
const viewSrc = `package probe

enum Lonely { Alpha, Beta }

flags Marks { One, Two }

/// A type no table reaches.
type Orphan | inspected
{
    grade Lonely
    marks Marks
    label string(8)
    slots [Lonely]int32
}

type Reached
{
    hits int32
}

table Root
{
    reached Reached
    count   int32
}
`

// TestViewCarriesTheTypesNoTableReaches holds §8.2's two rules where the
// corpus cannot: a `type` outside the table closure carries its descriptor in
// the VIEW FILE rather than beside the table runtime, and the two columns that
// describe the TABLE WIRE are empty on it — its field ids were never checked
// for collisions, so filling them would hand a tool two fields under one id
// and a text-form key for a text form that does not exist.
func TestViewCarriesTheTypesNoTableReaches(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, viewSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	header, source := string(files["ProbeView.h"]), string(files["ProbeView.cpp"])
	if header == "" || source == "" {
		t.Fatal("no ProbeView pair")
	}
	for _, want := range []string{
		"const TableTypeInfo * OrphanTableType();", // declared in the header (§8.5)
		"void OrphanReset( Orphan & value );",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("the view header does not carry %q", want)
		}
	}
	// AN ENUM ONLY AN OUT-OF-CLOSURE TYPE REACHES needs no value <-> id pair,
	// because the descriptor that names it hands out no id: §8.2 says the id
	// columns answer 0 outside a closure, so the view emits none of the pair.
	if strings.Contains(header, "TableEnumId( Lonely value") {
		t.Error("the view header carries an identity pair for an enum no table closure reaches — nothing calls it (§8.2)")
	}
	if strings.Contains(string(files["ProbeTable.h"]), "OrphanTableType") {
		t.Error("the table header carries the descriptor of a type no table reaches — it belongs in the view file (§8.5)")
	}
	// the two table-wire columns, empty on every field of Orphan (§8.2)
	for _, field := range []string{"grade", "marks", "label", "slots"} {
		row := descriptorRow(t, source, field)
		if !strings.Contains(row, `{ "`+field+`", NULL,`) {
			t.Errorf("%s: the json column is not NULL outside a table closure: %s", field, row)
		}
		if !strings.Contains(row, "0x0000000000000000ull") {
			t.Errorf("%s: the id column is not the reserved id outside a table closure: %s", field, row)
		}
	}
	// a closure member keeps both, exactly as it always did
	if strings.Contains(string(files["ProbeTable.h"]), `{ "hits", NULL,`) {
		t.Error("a field of a type INSIDE the closure lost its text key")
	}
	// THE ID FUNCTIONS ANSWER 0 (§8.2), the same answer the registry's
	// ViewVariant rows give: a descriptor outside a closure hands out no id,
	// and the function is present rather than NULL because a null id function
	// beside a non-null name function is what identifies a FLAGS field (§8.1).
	zero := "+[]( uint64_t ) -> uint64_t { return 0; }"
	for _, field := range []string{"grade", "slots"} {
		if row := descriptorRow(t, source, field); !strings.Contains(row, zero) {
			t.Errorf("%s: the id function outside a table closure is not the zero lambda: %s", field, row)
		}
	}
}

// TestViewVariantIdsAreReservedOutsideTheClosure holds the other half of §8.2:
// an enum, a flags or a union no table closure reaches carries the reserved id
// on every variant row, because §5's refusals are scoped to the closure and
// nothing ever checked those ids.
func TestViewVariantIdsAreReservedOutsideTheClosure(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, viewSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	source := string(files["ProbeView.cpp"])
	// the variants pack from 1, and row 0 is None
	for value, variant := range map[string]string{"1": "Alpha", "2": "Beta"} {
		want := `{ ` + value + `, "` + variant + `", 0x0000000000000000ull,`
		if !strings.Contains(source, want) {
			t.Errorf("enum Lonely's variant %s does not carry the reserved id: no row matching %q", variant, want)
		}
	}
	// the declaration's own annotation reaches the registry
	if !strings.Contains(source, `"Orphan", "Probe.schema", false, OrphanTableType(), "A type no table reaches.", 1, Orphan_view_tags`) {
		t.Error("the ViewType row does not carry the declaration's doc and tags")
	}
}

// descriptorRow returns the generated line carrying one field's descriptor.
func descriptorRow(t *testing.T, source, field string) string {
	t.Helper()
	for line := range strings.SplitSeq(source, "\n") {
		if strings.Contains(line, `{ "`+field+`", `) {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no descriptor row for %s", field)
	return ""
}

package cpptable

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCppArrayElementKindsClosed holds docs/SPEC-TABLES.md §3's closed element
// kind lists on the C++ leg: an array body (kind 14) may carry 1–13, 15, 17,
// 18–29 and 30, and any other element kind is damage under any count, a count
// of 0 included, in an unknown field as in one this reader declares. Each
// pinned vector runs through graphdemo Scene's strict load and the verdict
// alone is compared (docs/rulings.json refusal-verdict-only); the kind-17
// vectors are the read controls. The nested rows hold the same list under a
// table, an array of tables, an array of unions, a keyed body of tables and a
// union arm, through a unit with no pointers so the skip walk is the only walk.
func TestCppArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	framing := filepath.Join(repo, "testdata", "wire", "tables", "framing")
	cases := []struct {
		name string
		read bool
	}{
		{"array_elem_kind_32_count_2", false},
		{"array_elem_kind_31_count_1", false},
		{"array_elem_kind_16_count_1", false},
		{"array_elem_unassigned_count_0", false},
		{"array_elem_reserved_count_0", false},
		{"array_elem_zero_count_0", false},
		{"array_elem_kind_17_count_0", true},
		{"array_elem_kind_17_count_1", true},
	}
	u := loadGraphDirUnit(t, filepath.Join(repo, "tables", "pointers"))
	var body strings.Builder
	for _, c := range cases {
		wire, err := os.ReadFile(filepath.Join(framing, c.name+".bin"))
		if err != nil {
			t.Fatalf("%s: the pinned vector is missing: %v", c.name, err)
		}
		fmt.Fprintf(&body, `
    {
        const uint8_t wire[] = { %s };
        const bool want = %t;
        const int64_t need = SceneLoadMeasure( wire, (int64_t) sizeof( wire ) );
        std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
        uint8_t * aligned = region.data();
        while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
        TableReport g; memset( &g, 0, sizeof( g ) );
        const Scene * root = SceneLoad( aligned, need > 0 ? need : 4096, wire, (int64_t) sizeof( wire ), &g );
        if ( ( root != NULL ) != want ) { printf( "%s: the strict load reported the wrong verdict (root=%%d want=%%d)\n", root != NULL ? 1 : 0, (int) want ); return 1; }
        if ( !want && !g.damaged && !g.refused ) { printf( "%s: a refused load reported neither damage nor a refusal\n" ); return 1; }
    }
`, cppBytes(wire), c.read, c.name, c.name)
	}
	graphWire := cppUnknownNodeArrayWire()
	fmt.Fprintf(&body, `
    {
        const uint8_t wire[] = { %s };
        const int64_t need = SceneLoadMeasure( wire, (int64_t) sizeof( wire ) );
        std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
        uint8_t * aligned = region.data();
        while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
        TableReport g; memset( &g, 0, sizeof( g ) );
        const Scene * root = SceneLoad( aligned, need > 0 ? need : 4096, wire, (int64_t) sizeof( wire ), &g );
        if ( root != NULL ) { printf( "nested_unknown_node_array_kind_34_count_0: the strict load read an off-list element kind in an unknown record\n" ); return 1; }
        if ( !g.damaged && !g.refused ) { printf( "nested_unknown_node_array_kind_34_count_0: a refused load reported neither damage nor a refusal\n" ); return 1; }
    }
`, cppBytes(graphWire))
	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("the element-kind probe failed: %v\n%s", err, out)
	}

	nested := loadUnitFromSource(t, cppNestedElementKindsUnit)
	var nestedBody strings.Builder
	for _, c := range cppNestedElementKindWires() {
		fmt.Fprintf(&nestedBody, `
    {
        const uint8_t wire[] = { %s };
        const int64_t need = RootLoadMeasure( wire, (int64_t) sizeof( wire ) );
        std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
        uint8_t * aligned = region.data();
        while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
        TableReport g; memset( &g, 0, sizeof( g ) );
        const Root * root = RootLoad( aligned, need > 0 ? need : 4096, wire, (int64_t) sizeof( wire ), &g );
        if ( root != NULL ) { printf( "%s: the strict load read a nested off-list element kind\n" ); return 1; }
        if ( !g.damaged && !g.refused ) { printf( "%s: a refused load reported neither damage nor a refusal\n" ); return 1; }
    }
`, cppBytes(c.wire), c.name, c.name)
	}
	out, err = cppHeldRun(t, nested, nestedBody.String())
	if err != nil {
		t.Fatalf("the nested element-kind probe failed: %v\n%s", err, out)
	}
}

// cppUnknownNodeArrayWire is a Scene whose head names one record under a type
// id no graphdemo table hashes to. The typed load cannot open that record, so
// the graph walk over every kind-17 payload is the only walk that sees the
// off-list array body inside it.
func cppUnknownNodeArrayWire() []byte {
	ids := []uint64{
		ir.TableWireId("head"),
		ir.TableNodeWireId,
		ir.TableWireId("mystery"),
		ir.TableWireId("graphUnknown"),
	}
	recordBody := cppElemField(3, ir.TableKindArray, []byte{34, 0})
	record := append(cppElemLeb(4), cppElemLeb(uint64(len(recordBody)))...)
	record = append(record, recordBody...)
	table := append(cppElemLeb(1), record...)
	root := []byte{1, ir.TableKindPointer, 2} // Scene.head names node index 2
	root = append(root, 2, ir.TableKindString)
	root = append(root, cppElemLeb(uint64(len(table)))...)
	root = append(root, table...)
	root = append(root, 0)
	return cppElemFile(ids, root)
}

// cppNestedElementKindsUnit has a variable-length root and no pointers, so an
// unknown field is walked by TableReader::skip alone.
const cppNestedElementKindsUnit = `package elemnested

table Root
{
    a int32 = 0
}
`

func cppElemLeb(v uint64) []byte { return binary.AppendUvarint(nil, v) }

// cppElemFile frames one root body with its id table (docs/SPEC-TABLES.md §3).
func cppElemFile(ids []uint64, body []byte) []byte {
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

// cppElemField is one field under a kind whose payload is L-framed, then the
// body's terminator.
func cppElemField(ref uint64, kind byte, content []byte) []byte {
	out := append(cppElemLeb(ref), kind)
	out = append(out, cppElemLeb(uint64(len(content)))...)
	out = append(out, content...)
	return append(out, 0)
}

// cppUnionField is one union field: the arm reference, the arm's kind, its L
// and its payload, with no outer L (docs/SPEC-TABLES.md §3).
func cppUnionField(ref uint64, union []byte) []byte {
	out := append(cppElemLeb(ref), ir.TableKindUnion)
	out = append(out, union...)
	return append(out, 0)
}

type cppNestedElementKindWire struct {
	name string
	wire []byte
}

// cppNestedElementKindWires nests an array or keyed body whose element kind is
// 34, off both closed lists, at a count of 0, under each container a skip can
// meet. Every wire refuses: the header is judged under any count.
func cppNestedElementKindWires() []cppNestedElementKindWire {
	ids := []uint64{ir.TableWireId("zzz")}
	offListArray := []byte{34, 0} // element kind 34, count 0
	offListKeyed := []byte{34, 0}
	tableWithArray := cppElemField(1, ir.TableKindArray, offListArray)
	tableWithKeyed := cppElemField(1, ir.TableKindKeyed, offListKeyed)
	tableElement := append(cppElemLeb(uint64(len(tableWithArray))), tableWithArray...)
	keyedElement := append(cppElemLeb(uint64(len(tableWithArray))), tableWithArray...)
	arrayOfTables := append([]byte{ir.TableKindTable, 1}, tableElement...)
	keyedOfTables := append([]byte{ir.TableKindTable, 1, 1}, keyedElement...)
	unionArm := []byte{1, ir.TableKindArray, 2, 34, 0}
	arrayOfUnions := append([]byte{ir.TableKindUnion, 1}, unionArm...)
	return []cppNestedElementKindWire{
		{"nested_table_array_kind_34_count_0", cppElemFile(ids, cppElemField(1, ir.TableKindTable, tableWithArray))},
		{"nested_table_keyed_kind_34_count_0", cppElemFile(ids, cppElemField(1, ir.TableKindTable, tableWithKeyed))},
		{"nested_array_of_tables_kind_34_count_0", cppElemFile(ids, cppElemField(1, ir.TableKindArray, arrayOfTables))},
		{"nested_array_of_unions_kind_34_count_0", cppElemFile(ids, cppElemField(1, ir.TableKindArray, arrayOfUnions))},
		{"nested_keyed_of_tables_kind_34_count_0", cppElemFile(ids, cppElemField(1, ir.TableKindKeyed, keyedOfTables))},
		{"nested_union_arm_kind_34_count_0", cppElemFile(ids, cppUnionField(1, unionArm))},
	}
}

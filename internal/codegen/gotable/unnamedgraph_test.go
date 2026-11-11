package gotable

import "testing"

// TestGoUnnamedRecordsHeldToGraphRules: a node record of a type this reader
// cannot name is still a vertex of the pointer graph (docs/SPEC-TABLES.md §4,
// §6.5). Every record must be reached, no pointer may close a cycle, and the
// plain load, ReadStrict and the retaining load agree on the verdict.
func TestGoUnnamedRecordsHeldToGraphRules(t *testing.T) {
	t.Parallel()
	runGeneratedTables(t, oneReadSchema, oneReadPrelude+"\nvar _ = rand.Int\n"+unnamedGraphConditions)
}

const unnamedGraphConditions = `
func stranger(ids *TableIds, f func(w *TableWriter)) record {
	return record{0x0bad0bad0bad0bad, body(ids, func(w *TableWriter) { f(w); w.Put8(0) })}
}

const unknownField = uint64(0x1234567890abcdef)


func retainLoad(wire []byte) bool {
	retain := TableRetain{Bytes: make([]byte, 4096), Ids: make([]TableRetainID, 128)}
	var report TableReport
	return NodeLoadRetain(region(1<<16), wire, &retain, &report) != nil
}

func TestUnnamedGraph(t *testing.T) {
	var ids TableIds
	accepts(t, "a record of an unnameable type reached by a pointer",
		file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, stranger(&ids, func(w *TableWriter) {})))
	var seen TableReport
	reached := file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, stranger(&ids, func(w *TableWriter) {}))
	if NodeLoad(region(max(NodeLoadMeasure(reached), 1)), reached, &seen) == nil || seen.Unknown == 0 {
		t.Fatalf("a reached unnameable record did not read as one unknown: %+v", seen)
	}
	if !retainLoad(reached) { t.Fatal("the retaining read refused a reached record of an unnameable type") }
	if _, err := NodeReadStrict(reached); err != nil { t.Fatalf("ReadStrict refused a reached record of an unnameable type: %v", err) }
	accepts(t, "a chain of unnameable records reached from the root",
		file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
			stranger(&ids, func(w *TableWriter) { ptr(w, unknownField, 3) }), stranger(&ids, func(w *TableWriter) {})))
	accepts(t, "a named record reached only through an unnameable one",
		file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
			stranger(&ids, func(w *TableWriter) { ptr(w, unknownField, 3) }), node(&ids, func(w *TableWriter) {})))

	// (a) a record under a type id this reader does not declare that no
	// pointer reaches is damage
	unreached := file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		leaf(&ids, 3), stranger(&ids, func(w *TableWriter) {}))
	refuses(t, "an unreferenced record of an unnameable type", unreached)
	if retainLoad(unreached) { t.Fatal("the retaining read accepted an unreferenced record of an unnameable type") }
	if _, err := NodeReadStrict(unreached); err == nil { t.Fatal("ReadStrict accepted an unreferenced record of an unnameable type") }

	// (b) a record of an unnameable type whose own pointer names itself is a
	// cycle, even though no declared slot can name it
	self := file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		stranger(&ids, func(w *TableWriter) { ptr(w, unknownField, 2) }))
	refuses(t, "an unnameable record's pointer to itself", self)
	if retainLoad(self) { t.Fatal("the retaining read accepted an unnameable record's pointer to itself") }
	if _, err := NodeReadStrict(self); err == nil { t.Fatal("ReadStrict accepted an unnameable record's pointer to itself") }

	// a cycle through two unnameable records, and one through a named record
	// and an unnameable one, are cycles as well
	loop := file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		stranger(&ids, func(w *TableWriter) { ptr(w, unknownField, 3) }), stranger(&ids, func(w *TableWriter) { ptr(w, unknownField, 2) }))
	refuses(t, "two unnameable records naming each other", loop)
	if retainLoad(loop) { t.Fatal("the retaining read accepted two unnameable records naming each other") }
	mixed := file(&ids, func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 3) }), stranger(&ids, func(w *TableWriter) { ptr(w, unknownField, 2) }))
	refuses(t, "a named record and an unnameable one naming each other", mixed)
	if retainLoad(mixed) { t.Fatal("the retaining read accepted a named record and an unnameable one naming each other") }

	// (c) many unreferenced records of unnameable types are all damage
	var many []record
	many = append(many, leaf(&ids, 3))
	for range 8 { many = append(many, stranger(&ids, func(w *TableWriter) {})) }
	refuses(t, "many unreferenced records of unnameable types", file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, many...))

	// the named-type controls still refuse
	refuses(t, "a named record no pointer reaches", file(&ids, func(w *TableWriter) {}, leaf(&ids, 3)))
	refuses(t, "a named cycle", file(&ids, func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(&ids, func(w *TableWriter) { ptr(w, field(fNext), 2) })))
}
`

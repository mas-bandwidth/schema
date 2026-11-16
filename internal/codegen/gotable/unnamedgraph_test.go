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
func stranger(f func(w *TableWriter)) record {
	return record{0x0bad0bad0bad0bad, func(ids *TableIds) []byte {
		return body(ids, func(w *TableWriter) { f(w); w.Put8(0) })
	}}
}

const unknownField = uint64(0x1234567890abcdef)


func retainLoad(wire []byte) bool {
	retain := TableRetain{Bytes: make([]byte, 4096), Ids: make([]TableRetainID, 128)}
	var report TableReport
	return NodeLoadRetain(region(1<<16), wire, &retain, &report) != nil
}

func TestUnnamedGraph(t *testing.T) {
	accepts(t, "a record of an unnameable type reached by a pointer",
		file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, stranger(func(w *TableWriter) {})))
	var seen TableReport
	reached := file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, stranger(func(w *TableWriter) {}))
	if NodeLoad(region(max(NodeLoadMeasure(reached), 1)), reached, &seen) == nil || seen.Unknown == 0 {
		t.Fatalf("a reached unnameable record did not read as one unknown: %+v", seen)
	}
	if !retainLoad(reached) { t.Fatal("the retaining read refused a reached record of an unnameable type") }
	if _, err := NodeReadStrict(reached); err != nil { t.Fatalf("ReadStrict refused a reached record of an unnameable type: %v", err) }
	accepts(t, "a chain of unnameable records reached from the root",
		file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
			stranger(func(w *TableWriter) { ptr(w, unknownField, 3) }), stranger(func(w *TableWriter) {})))
	accepts(t, "a named record reached only through an unnameable one",
		file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
			stranger(func(w *TableWriter) { ptr(w, unknownField, 3) }), node(func(w *TableWriter) {})))

	// (a) a record under a type id this reader does not declare that no
	// pointer reaches is damage
	unreached := file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		leaf(3), stranger(func(w *TableWriter) {}))
	refuses(t, "an unreferenced record of an unnameable type", unreached)
	if retainLoad(unreached) { t.Fatal("the retaining read accepted an unreferenced record of an unnameable type") }
	if _, err := NodeReadStrict(unreached); err == nil { t.Fatal("ReadStrict accepted an unreferenced record of an unnameable type") }

	// (b) a record of an unnameable type whose own pointer names itself is a
	// cycle, even though no declared slot can name it
	self := file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		stranger(func(w *TableWriter) { ptr(w, unknownField, 2) }))
	refuses(t, "an unnameable record's pointer to itself", self)
	if retainLoad(self) { t.Fatal("the retaining read accepted an unnameable record's pointer to itself") }
	if _, err := NodeReadStrict(self); err == nil { t.Fatal("ReadStrict accepted an unnameable record's pointer to itself") }

	// a cycle through two unnameable records, and one through a named record
	// and an unnameable one, are cycles as well
	loop := file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		stranger(func(w *TableWriter) { ptr(w, unknownField, 3) }), stranger(func(w *TableWriter) { ptr(w, unknownField, 2) }))
	refuses(t, "two unnameable records naming each other", loop)
	if retainLoad(loop) { t.Fatal("the retaining read accepted two unnameable records naming each other") }
	mixed := file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, field(fLeaf), 3) }), stranger(func(w *TableWriter) { ptr(w, unknownField, 2) }))
	refuses(t, "a named record and an unnameable one naming each other", mixed)
	if retainLoad(mixed) { t.Fatal("the retaining read accepted a named record and an unnameable one naming each other") }

	// a pointer in a field this reader cannot name is an edge of the graph
	// too: a loop through one is a cycle, and a record reached only by one is
	// reached
	hidden := file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, unknownField, 3) }), stranger(func(w *TableWriter) { ptr(w, unknownField, 2) }))
	refuses(t, "a named record's unknown field and an unnameable record naming each other", hidden)
	if retainLoad(hidden) { t.Fatal("the retaining read accepted a named record's unknown field and an unnameable record naming each other") }
	refuses(t, "a named record's unknown field naming itself", file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, unknownField, 2) })))
	refuses(t, "two named records naming each other through an unknown field", file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, field(fNext), 3) }), node(func(w *TableWriter) { ptr(w, unknownField, 2) })))
	hiddenReach := file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, unknownField, 3) }), stranger(func(w *TableWriter) {}))
	accepts(t, "an unnameable record reached only by a named record's unknown field", hiddenReach)
	if !retainLoad(hiddenReach) { t.Fatal("the retaining read refused an unnameable record reached only by a named record's unknown field") }
	accepts(t, "a named record reached only by a named record's unknown field", file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, unknownField, 3) }), node(func(w *TableWriter) {})))
	accepts(t, "records reached only by the root's unknown fields", file(func(w *TableWriter) { ptr(w, unknownField, 2); ptr(w, unknownField+1, 3) },
		node(func(w *TableWriter) {}), stranger(func(w *TableWriter) {})))
	refuses(t, "a root's unknown field naming a cycle", file(func(w *TableWriter) { ptr(w, unknownField, 2) },
		node(func(w *TableWriter) { ptr(w, field(fNext), 3) }), node(func(w *TableWriter) { ptr(w, unknownField, 2) })))

	// a record whose TYPE reference resolves to a reserved transport id is
	// malformed, whether a pointer reaches it or not (docs/SPEC-TABLES.md §3.1)
	for _, id := range []uint64{0xffffffffffffffff, 0xfffffffffffffffe, 0xfffffffffffffffd} {
		reserved := file(func(w *TableWriter) { ptr(w, unknownField, 2) },
			record{id, func(ids *TableIds) []byte {
				return body(ids, func(w *TableWriter) { w.Put8(0) })
			}})
		refuses(t, "a reached record whose type id is reserved", reserved)
		if retainLoad(reserved) { t.Fatal("the retaining read accepted a record whose type id is reserved") }
		if _, err := NodeReadStrict(reserved); err == nil { t.Fatal("ReadStrict accepted a record whose type id is reserved") }
	}

	// (c) many unreferenced records of unnameable types are all damage
	var many []record
	many = append(many, leaf(3))
	for range 8 { many = append(many, stranger(func(w *TableWriter) {})) }
	refuses(t, "many unreferenced records of unnameable types", file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, many...))

	// the named-type controls still refuse
	refuses(t, "a named record no pointer reaches", file(func(w *TableWriter) {}, leaf(3)))
	refuses(t, "a named cycle", file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, field(fNext), 2) })))
}

func blob(id uint64, data []byte) record { return record{id, func(*TableIds) []byte { return data }} }

// both reads one input on every path, the retaining load included, and
// requires the verdict given.
func both(t *testing.T, what string, wire []byte, want bool) {
	t.Helper()
	if want { accepts(t, what, wire) } else { refuses(t, what, wire) }
	if retainLoad(wire) != want { t.Fatalf("%s: the retaining read answered %v", what, !want) }
}

// A blob record is a leaf of the graph under every root (docs/SPEC-TABLES.md
// §2.5, §3.1, §4): Node reaches no *bytes and no *string, so this reader
// places no blob, yet a blob record's bytes are never read as a body, it is
// named from exactly one slot, its text is well formed, and a declared slot
// that names one at another type is damage.
func TestBlobRecordsAreLeavesUnderEveryRoot(t *testing.T) {
	var ids TableIds
	spells := func(index uint64) []byte { return body(&ids, func(w *TableWriter) { ptr(w, unknownField, index); w.Put8(0) }) }
	both(t, "a bytes blob whose bytes spell a pointer, beside a record no pointer reaches",
		file(func(w *TableWriter) { ptr(w, unknownField, 2) }, blob(tableBytesTypeID, spells(3)), stranger(func(w *TableWriter) {})), false)
	both(t, "a bytes blob whose bytes spell a pointer to itself",
		file(func(w *TableWriter) { ptr(w, unknownField, 2) }, blob(tableBytesTypeID, spells(2))), true)
	both(t, "a string blob reached by an unknown field",
		file(func(w *TableWriter) { ptr(w, unknownField, 2) }, blob(tableStringTypeID, []byte("hello"))), true)
	both(t, "a bytes blob reached through an unnameable record",
		file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, stranger(func(w *TableWriter) { ptr(w, unknownField, 3) }), blob(tableBytesTypeID, []byte{0})), true)
	both(t, "a string blob reached through a named record's unknown field",
		file(func(w *TableWriter) { ptr(w, field(fNext), 2) }, node(func(w *TableWriter) { ptr(w, unknownField, 3) }), blob(tableStringTypeID, []byte("hello"))), true)
	both(t, "a bytes blob named by two unknown fields",
		file(func(w *TableWriter) { ptr(w, unknownField, 2); ptr(w, unknownField+1, 2) }, blob(tableBytesTypeID, []byte{0})), false)
	both(t, "a bytes blob named by the root and by an unnameable record",
		file(func(w *TableWriter) { ptr(w, field(fLeaf), 2); ptr(w, unknownField, 3) }, stranger(func(w *TableWriter) { ptr(w, unknownField, 3) }), blob(tableBytesTypeID, []byte{0})), false)
	both(t, "a bytes blob no pointer reaches",
		file(func(w *TableWriter) {}, blob(tableBytesTypeID, []byte{0})), false)
	// text that is not well formed, in bytes that would also frame a body
	both(t, "a string blob that is not well-formed text",
		file(func(w *TableWriter) { ptr(w, unknownField, 2) }, blob(tableStringTypeID, body(&ids, func(w *TableWriter) { i32(w, unknownField, -1); w.Put8(0) }))), false)
	both(t, "a declared *Node slot naming a bytes blob",
		file(func(w *TableWriter) { ptr(w, field(fNext), 2) }, blob(tableBytesTypeID, []byte{0})), false)
	both(t, "a declared *Leaf slot naming a string blob",
		file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, blob(tableStringTypeID, []byte{0})), false)
	// the engine counts two unknowns here: the field and the record
	var seen TableReport
	reached := file(func(w *TableWriter) { ptr(w, unknownField, 2) }, blob(tableStringTypeID, []byte("hello")))
	if NodeLoad(region(max(NodeLoadMeasure(reached), 1)), reached, &seen) == nil || seen.Unknown != 2 {
		t.Fatalf("a reached blob this root places nowhere did not read as one unknown field and one unknown record: %+v", seen)
	}
}
`

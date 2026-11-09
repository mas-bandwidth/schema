package gotable

// measuremalformed_test.go (card schema-go-fuzz-measure-accepts-malformed)
// pins the invariant the random-table differential's measure-accepts-malformed
// class broke: the strict measure, LoadMeasureReason, refuses (-1 and a
// reason) exactly the damaged variable wires that the tolerant load reports
// Malformed or refuses, and on every other wire answers the size the load
// needs. The tolerant load keeps recovering what it recovered before (the
// digest of its result over every damaged wire is pinned per unit). The units
// and values are the differential's seeds 2, 4 and 1, where the hits were
// found (18, 838 and 5853 of them at BASE).

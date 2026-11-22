package gotable

import "testing"

// TestBuilderFromJSONLockedRefusesIntoTheReport pins docs/SPEC-TABLES.md §6.5
// and §16.2 (audit SF-30): a builder read on a locked arena resets the report
// FIRST and then refuses into it, so a reused report never keeps the stale
// success of an earlier read, and the refusal is named.
func TestBuilderFromJSONLockedRefusesIntoTheReport(t *testing.T) {
	runGenerated(t, "package probe\ntable Root { entries map[int32]int32\n next int32 }\n", "package probe\n"+
		"import \"testing\"\n"+
		"func TestLockedFromJSON(t *testing.T){\n"+
		" var b RootBuilder;if !b.Init(){t.Fatal(\"init\")};defer b.Shutdown()\n"+
		" var report TableReport\n"+
		" if !RootFromJSON(&b,[]byte(\"{\\\"next\\\":5}\"),&report)||report.Err()!=nil{t.Fatalf(\"an unlocked read refused: %+v\",report)}\n"+
		" if !b.Lock(){t.Fatal(\"lock\")}\n"+
		" report.Unknown=3\n"+
		" if RootFromJSON(&b,[]byte(\"{\\\"next\\\":6}\"),&report)||report.Unknown!=0||report.Err()==nil{t.Fatalf(\"a locked read left a stale report, or refused with no reason: %+v\",report)}\n"+
		" if RootFromJSONWith(&b,[]byte(\"{\\\"next\\\":6}\"),nil,TableJSONOptions{}){t.Fatal(\"a locked read with no report answered true\")}\n"+
		"}\n")
}

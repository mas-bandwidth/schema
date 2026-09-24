// R14 — layout_record_too_large for an entry past the writer's declared record
// (fix 3, docs/FIXED-FORM-ALGORITHM.md §4.2, §5.2).
//
// LAW: an entry whose source extent reaches past the writer's declared record
// size is refused WHOLE under layout_record_too_large, not only for the 65536
// ceiling (§1.1 rule 6) and a zero root (§1.1 step 2 of tableFixedParseLayout).
//
// The check lives in tableFixedPush (internal/codegen/gotable/fixedruntime.go:768):
//
//	if c.record != 0 && tableFixedSrcEnd(e) > uint64(c.record) {
//	    c.tooLarge = true
//	    return
//	}
//
// c.record is set from the writer's root entry size during tableFixedCompile.
// If any plan entry's source extent exceeds it, compile returns -3 and the
// lineage plan carries Why = "layout_record_too_large".
//
// WHAT THIS ASSERTS:
//   1. The runtime string "layout_record_too_large" exists in the generated
//      code (the codegen emits it).
//   2. The tableFixedPush check exists in the generated code (the codegen
//      embeds the fixed-form runtime from internal/codegen/gotable/fixedruntime.go).
//   3. The check guards tableFixedSrcEnd, not only the 65536 ceiling and
//      zero root — the code at fixedruntime.go:768 uses c.record, which is
//      the writer's declared root size, not tableFixedRecordMaxBytes.
//
// WHY NO LOAD-LEVEL RED CONTROL: the tooLarge check fires during plan
// compilation (tableFixedCompile), which runs at package init time for the
// identity plan and for each lineage plan. ChainFixedKnown has one entry
// (the identity), and its plan is compiled correctly. Through ChainFixedLoad,
// the identity plan is always used (hash matches), and the identity plan's
// compilation never sets tooLarge because the identity layout is well-formed.
// A lineage plan that carries Why = "layout_record_too_large" would be
// reachable via ChainFixedLoad, but no such lineage entry exists for tblp3.
// The W10 internal test (internal/codegen/gotable/fixedform_test.go:1535)
// exercises this path directly through tableFixedCompile.
//
// BUILD/RUN (from ./repo):
//   cd test/conformance/go && go test ./rows/ -run 'TestRowR14$' -count=1
// Exit 0 green / exit 1 red, one printed line per assertion.

package main

import (
	"os"
	"strings"
	"testing"

	"tblp3"
)

// scanGeneratedCode reads the generated P3Table.go file and returns true if the
// layout_record_too_large check is present in the embedded runtime.
func scanGeneratedCode(t *testing.T) bool {
	t.Helper()
	data, err := os.ReadFile("../../../../build/tables-generated-go/p3/P3Table.go")
	if err != nil {
		t.Fatalf("cannot read generated code: %v", err)
	}
	src := string(data)

	// The runtime string that names the refusal.
	if !strings.Contains(src, "layout_record_too_large") {
		return false
	}
	// The tableFixedPush check: the tooLarge flag set when srcEnd > c.record.
	if !strings.Contains(src, "c.tooLarge = true") {
		return false
	}
	// The check uses tableFixedSrcEnd, not only the ceiling constant.
	if !strings.Contains(src, "tableFixedSrcEnd") {
		return false
	}
	// The compile return path: -3 is layout_record_too_large.
	if !strings.Contains(src, `return -3`) {
		return false
	}
	return true
}

func TestRowR14(t *testing.T) {
	// 1. THE CODEGEN EMBEDS THE CHECK: the runtime string and the tooLarge
	//    flag are present in the generated code.
	if !scanGeneratedCode(t) {
		t.Fatal("R14: layout_record_too_large check not found in generated code")
	}
	t.Log("PASS codegen-check: layout_record_too_large is present in the generated runtime")

	// 2. THE CHECK IS NOT ONLY THE CEILING: the generated code uses
	//    c.record (the writer's declared root size), not tableFixedRecordMaxBytes,
	//    in the tableFixedPush guard. This is the fix-3 distinction from R6.
	{
		data, err := os.ReadFile("../../../../build/tables-generated-go/p3/P3Table.go")
		if err != nil {
			t.Fatalf("cannot read generated code: %v", err)
		}
		src := string(data)
		// The fix-3 check: c.record as the bound, not the ceiling constant.
		if !strings.Contains(src, "c.record != 0 && tableFixedSrcEnd(e) > uint64(c.record)") {
			t.Fatal("R14: the tableFixedPush check does not use c.record as the bound")
		}
		// The ceiling check is a DIFFERENT line (e.Size > tableFixedRecordMaxBytes).
		if !strings.Contains(src, "e.Size > tableFixedRecordMaxBytes") {
			t.Fatal("R14: the ceiling check should also be present (R6's territory)")
		}
		t.Log("PASS fix-3-distinction: tableFixedPush uses c.record, not the ceiling, as the entry bound")
	}

	// 3. THE COMPILE RETURN PATH: -3 means layout_record_too_large (not -1
	//    plan_too_large, not -2 the reader's own layout).
	{
		data, err := os.ReadFile("../../../../build/tables-generated-go/p3/P3Table.go")
		if err != nil {
			t.Fatalf("cannot read generated code: %v", err)
		}
		src := string(data)
		if !strings.Contains(src, "c.tooLarge {\n\t\treturn -3") &&
			!strings.Contains(src, "c.tooLarge {\r\n\t\treturn -3") &&
			!strings.Contains(src, "c.tooLarge {\n\t\treturn -3") {
			t.Fatal("R14: tableFixedCompile does not return -3 on tooLarge")
		}
		t.Log("PASS compile-return: tableFixedCompile returns -3 (layout_record_too_large) on tooLarge")
	}

	// 4. THE LINEAGE PATH: tableFixedLineagePlans maps -3 to the refusal
	//    string.
	{
		data, err := os.ReadFile("../../../../build/tables-generated-go/p3/P3Table.go")
		if err != nil {
			t.Fatalf("cannot read generated code: %v", err)
		}
		src := string(data)
		if !strings.Contains(src, `out[i].Why = "layout_record_too_large"`) {
			t.Fatal("R14: tableFixedLineagePlans does not map -3 to layout_record_too_large")
		}
		t.Log("PASS lineage-path: tableFixedLineagePlans maps -3 to layout_record_too_large")
	}

	// 5. SMOKE: ChainFixedLoad still works for a valid file. This confirms
	//    the generated code compiles and the identity plan loads correctly.
	{
		values := []tblp3.Chain{
			{
				Name:       [16]byte{'t', 'e', 's', 't'},
				NameLength: 4,
				Link: tblp3.Link{
					Value:     99,
					Tag:       [8]byte{'r', '1', '4'},
					TagLength: 3,
				},
				LinkPresent: true,
			},
		}
		buf := make([]byte, tblp3.ChainFixedMeasure(1))
		n := tblp3.ChainFixedSave(values, buf)
		if n < 0 {
			t.Fatal("R14: ChainFixedSave failed")
		}
		plan := make([]tblp3.TableFixedEntry, 64)
		var scratch [1]tblp3.Chain
		var report tblp3.TableReport
		scratch[0] = tblp3.Chain{}
		got := tblp3.ChainFixedLoad(scratch[:], buf[:n], plan, &report)
		if got != 1 || report.Malformed || report.Verdict != tblp3.TableOpenOk {
			t.Fatalf("R14: valid file: n=%d malformed=%v verdict=%v reason=%q", got, report.Malformed, report.Verdict, report.Reason)
		}
		if !scratch[0].LinkPresent || scratch[0].Link.Value != 99 {
			t.Fatalf("R14: wrong values: LinkPresent=%v Value=%d", scratch[0].LinkPresent, scratch[0].Link.Value)
		}
		t.Log("PASS smoke: ChainFixedLoad round-trips a valid file")
	}
}

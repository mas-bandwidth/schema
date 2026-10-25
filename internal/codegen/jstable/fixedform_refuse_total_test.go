package jstable

import (
	"strings"
	"testing"
)

func checkFixedRefuseIsTotal(loadBody, decodeBody string) (loopFault, decodeFault string) {
	loopIdx := strings.Index(loadBody, "for (let k = 0; k < n; k++)")
	endLoopIdx := strings.Index(loadBody, "at += recordBytes;")
	if loopIdx < 0 || endLoopIdx < 0 || endLoopIdx <= loopIdx {
		return "FixedLoad missing record loop", ""
	}
	loopBody := loadBody[loopIdx:endLoopIdx]
	tailBody := loadBody[endLoopIdx:]
	if strings.Contains(loopBody, "TableFixedRun(entries, entryCount, bytes, srcView, at + 8, image, imageView, remap, report)") ||
		strings.Contains(loopBody, "FixedDecode(values[k], imageView, 0, report)") ||
		!strings.Contains(tailBody, "report.clamped +=") ||
		!strings.Contains(tailBody, "report.widened +=") ||
		!strings.Contains(tailBody, "report.unknown += censusUnknown;") {
		loopFault = "record loop writes clamped/widened directly to caller's report instead of committing after the loop beside censusUnknown"
	}

	textIdx := strings.Index(decodeBody, "TableFixedTextOk(")
	writeIdx := strings.Index(decodeBody, "value.")
	if textIdx < 0 || writeIdx < 0 || textIdx > writeIdx {
		decodeFault = "FixedDecode writes to value (values[k]) before checking TableFixedTextOk"
	}
	return loopFault, decodeFault
}

func TestFixedJsTextRefusalIsTotal(t *testing.T) {
	t.Parallel()

	// In-memory negative control: the checker run on the BASE emission held as a
	// string must report both faults.
	const baseLoad = `export function FixedTableFixedLoad(bytes, values, report, plan = null) {
  for (let k = 0; k < n; k++) {
    TableFixedRun(entries, entryCount, bytes, srcView, at + 8, image, imageView, remap, report);
    if (report.refused !== 0) { return -1; }
    FixedTableFixedDecode(values[k], imageView, 0, report);
    if (report.refused !== 0) { return -1; }
    at += recordBytes;
  }
  report.unknown += censusUnknown;
  report.kindMismatch += censusKind;
  return n;
}`
	const baseDecode = `export function FixedTableFixedDecode(value, view, at, report) {
  value.Sequence = view.getUint32(at, true);
  {
    let n = view.getInt32(at + 1126, true);
    if (n < 0) { n = 0; report.clamped++; } else if (n > 15) { n = 15; report.clamped++; }
    value.PlayerNameLength = n;
  }
  if (!TableFixedTextOk(TableFixedTextUtf8, view, at + 1130, value.PlayerNameLength)) { report.refused = TableFixedRefusal.TextIllFormed; return; }
}`
	if f1, f2 := checkFixedRefuseIsTotal(baseLoad, baseDecode); f1 == "" || f2 == "" {
		t.Fatalf("negative control failed to detect both BASE faults: loopFault=%q decodeFault=%q", f1, f2)
	}

	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	loadSrc := string(files["FixedTableTable.js"])
	homeSrc := string(files["BenchTable.js"])
	if loadSrc == "" || homeSrc == "" {
		t.Fatal("missing generated FixedTableTable.js or BenchTable.js")
	}
	loadBody := jsFn(loadSrc, "FixedTableFixedLoad")
	decodeBody := jsFn(homeSrc, "FixedTableFixedDecode")
	if loadBody == "" || decodeBody == "" {
		t.Fatal("missing FixedTableFixedLoad or FixedTableFixedDecode")
	}

	loopFault, decodeFault := checkFixedRefuseIsTotal(loadBody, decodeBody)
	if loopFault != "" {
		t.Errorf("REFUSE-is-total (1): %s\n%s", loopFault, loadBody)
	}
	if decodeFault != "" {
		t.Errorf("REFUSE-is-total (2): %s\n%s", decodeFault, decodeBody)
	}
}

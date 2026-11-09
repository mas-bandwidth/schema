package jstable

// The fixed roadmap rows' strict-read contract (docs/FIXED-FORM-ALGORITHM.md
// §4.5, §4.6, §5.3, §5.4; docs/SPEC-TABLES.md §4, §6.5):
//   - the decode's check loop and bounds pass refuse count_over_bound,
//     length_over_bound, out_of_range and wire_damaged by name, never clamping
//     or counting report.oor / report.clamped;
//   - the runtime's count, text, ordinal and const ops refuse by name, never
//     clamping and counting report.oor;
//   - a refusal at record k in <T>FixedLoad resets every record the read wrote
//     (0..k) back to its Init<T> defaults via <T>FixedRefuseBatch before
//     returning -1.

import (
	"strings"
	"testing"
)

const jsFixedRoadmapStrictSchema = `package probe

enum E { a, b }

union U {
  first int32
  second int32
}

fixed table R {
  lead uint32
  flag bool
  opt ?int32
  e E
  u U
  rng int32 | min = -10, max = 10
  arr [..4]int32
  s string(8)
}
`

func checkJsFixedRoadmapRowsStrict(loadBody, refuseBatchBody, decodeBody, runBody string) []string {
	var faults []string
	for _, want := range []string{
		"TableFixedRefusal.WireDamaged",
		"TableFixedRefusal.LengthOverBound",
		"TableFixedRefusal.CountOverBound",
		"TableFixedRefusal.OutOfRange",
	} {
		if !strings.Contains(decodeBody, want) {
			faults = append(faults, "decode lacks "+want)
		}
	}
	for _, want := range []string{
		"TableFixedRefusal.LengthOverBound",
		"TableFixedRefusal.CountOverBound",
		"TableFixedRefusal.OutOfRange",
	} {
		if !strings.Contains(runBody, want) {
			faults = append(faults, "runtime lacks "+want)
		}
	}
	if strings.Contains(decodeBody, "report.oor++") || strings.Contains(decodeBody, "report.clamped") {
		faults = append(faults, "decode still clamps and counts oor")
	}
	if strings.Contains(runBody, "report.oor++") || strings.Contains(runBody, "report.clamped") {
		faults = append(faults, "runtime still clamps and counts oor")
	}
	if refuseBatchBody == "" {
		faults = append(faults, "no RFixedRefuseBatch emitted")
	} else if !strings.Contains(refuseBatchBody, "for (let j = 0; j <= k; j++) { InitR(values[j]); }") {
		faults = append(faults, "RFixedRefuseBatch does not reset values[0..k] via InitR")
	}
	if !strings.Contains(loadBody, "return RFixedRefuseBatch(values, k, stage, report);") {
		faults = append(faults, "RFixedLoad does not call RFixedRefuseBatch on stage.refused")
	}
	if strings.Contains(loadBody, "if (stage.refused !== 0) { report.refused = stage.refused;") {
		faults = append(faults, "RFixedLoad exits on stage.refused without RFixedRefuseBatch")
	}
	return faults
}

func generateJsFixedRoadmapStrictBodies(t *testing.T) (loadBody, refuseBatchBody, decodeBody, runBody string) {
	t.Helper()
	u := loadSchemaString(t, "roadmap_strict.schema", jsFixedRoadmapStrictSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, body := range files {
		s := string(body)
		if l := jsFn(s, "RFixedLoad"); l != "" {
			loadBody = l
		}
		if rb := jsFn(s, "RFixedRefuseBatch"); rb != "" {
			refuseBatchBody = rb
		}
		if d := jsFn(s, "RFixedDecode"); d != "" {
			decodeBody = d
		}
		if r := jsFn(s, "TableFixedRun"); r != "" {
			runBody = r
		}
	}
	if loadBody == "" || decodeBody == "" || runBody == "" {
		t.Fatalf("missing generated functions (load=%v decode=%v run=%v)", loadBody != "", decodeBody != "", runBody != "")
	}
	return loadBody, refuseBatchBody, decodeBody, runBody
}

func TestJsFixedRoadmapRowsStrict(t *testing.T) {
	t.Parallel()
	loadBody, refuseBatchBody, decodeBody, runBody := generateJsFixedRoadmapStrictBodies(t)
	if faults := checkJsFixedRoadmapRowsStrict(loadBody, refuseBatchBody, decodeBody, runBody); len(faults) != 0 {
		t.Errorf("the strict fixed roadmap read does not hold:\n  %s", strings.Join(faults, "\n  "))
	}
}

func TestJsFixedRoadmapRowsStrictNegativeControl(t *testing.T) {
	t.Parallel()
	loadBody, refuseBatchBody, decodeBody, runBody := generateJsFixedRoadmapStrictBodies(t)

	hasFault := func(faults []string, want string) bool {
		for _, f := range faults {
			if f == want {
				return true
			}
		}
		return false
	}

	if faults := checkJsFixedRoadmapRowsStrict(loadBody, refuseBatchBody, decodeBody, runBody); len(faults) != 0 {
		t.Fatalf("clean artifact should hold the strict fixed roadmap read:\n  %s", strings.Join(faults, "\n  "))
	}

	// 1. Mutate RFixedLoad in the real artifact so a record-loop refusal returns
	// -1 directly without calling RFixedRefuseBatch.
	loadNoReset := strings.Replace(loadBody,
		"if (stage.refused !== 0) { return RFixedRefuseBatch(values, k, stage, report); }",
		"if (stage.refused !== 0) { report.refused = stage.refused; report.damaged = stage.damaged; return -1; }", 1)
	if loadNoReset == loadBody {
		t.Fatal("could not find RFixedRefuseBatch call in RFixedLoad to mutate")
	}
	if faults := checkJsFixedRoadmapRowsStrict(loadNoReset, refuseBatchBody, decodeBody, runBody); !hasFault(faults, "RFixedLoad exits on stage.refused without RFixedRefuseBatch") {
		t.Fatalf("negative control did not flag RFixedLoad skipping RFixedRefuseBatch: %v", faults)
	}

	// 2. Mutate RFixedRefuseBatch in the real artifact so it omits the InitR loop.
	batchNoInit := strings.Replace(refuseBatchBody,
		"for (let j = 0; j <= k; j++) { InitR(values[j]); }",
		"", 1)
	if batchNoInit == refuseBatchBody {
		t.Fatal("could not find InitR loop in RFixedRefuseBatch to mutate")
	}
	if faults := checkJsFixedRoadmapRowsStrict(loadBody, batchNoInit, decodeBody, runBody); !hasFault(faults, "RFixedRefuseBatch does not reset values[0..k] via InitR") {
		t.Fatalf("negative control did not flag RFixedRefuseBatch omitting InitR: %v", faults)
	}

	// 3. Mutate the ranged-scalar refusal in the real RFixedDecode into a clamp
	// that counts report.oor++.
	decRangeClamp := strings.Replace(decodeBody,
		"if (value.rng < -10 || value.rng > 10) { report.refused = TableFixedRefusal.OutOfRange; return; }",
		"if (value.rng < -10) { value.rng = -10; report.oor++; } else if (value.rng > 10) { value.rng = 10; report.oor++; }", 1)
	if decRangeClamp == decodeBody {
		t.Fatal("could not find ranged-scalar refusal in RFixedDecode to mutate")
	}
	if faults := checkJsFixedRoadmapRowsStrict(loadBody, refuseBatchBody, decRangeClamp, runBody); !hasFault(faults, "decode still clamps and counts oor") {
		t.Fatalf("negative control did not flag decode ranged-scalar clamp: %v", faults)
	}

	// 4. Mutate the bool/present WireDamaged refusals out of the real RFixedDecode.
	decNoWireDamaged := strings.ReplaceAll(decodeBody, "TableFixedRefusal.WireDamaged", "0")
	if decNoWireDamaged == decodeBody {
		t.Fatal("could not find TableFixedRefusal.WireDamaged in RFixedDecode to mutate")
	}
	if faults := checkJsFixedRoadmapRowsStrict(loadBody, refuseBatchBody, decNoWireDamaged, runBody); !hasFault(faults, "decode lacks TableFixedRefusal.WireDamaged") {
		t.Fatalf("negative control did not flag missing WireDamaged in decode: %v", faults)
	}

	// 5. Mutate the text-length refusal in the real TableFixedRun into a clamp
	// that counts report.oor++.
	runTextClamp := strings.Replace(runBody,
		"if (v < 0 || v > cap) { report.refused = TableFixedRefusal.LengthOverBound; return; }",
		"if (v < 0) { v = 0; report.oor++; } else if (v > cap) { v = cap; report.oor++; }", 1)
	if runTextClamp == runBody {
		t.Fatal("could not find text-length refusal in TableFixedRun to mutate")
	}
	if faults := checkJsFixedRoadmapRowsStrict(loadBody, refuseBatchBody, decodeBody, runTextClamp); !hasFault(faults, "runtime still clamps and counts oor") {
		t.Fatalf("negative control did not flag runtime text-length clamp: %v", faults)
	}
}

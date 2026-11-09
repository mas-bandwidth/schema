package jstable

// The fixed form's STRICT read (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6,
// §5.3): one read is the whole value or a refusal, never a clamp that lands a
// partially-repaired value beside a `clamped` counter. The cut turns every
// clamp into a refusal by name — count_over_bound and length_over_bound in the
// check loop, out_of_range in the bounds pass, and wire_damaged for a bool or
// present byte other than 0 or 1. These tests pin the generated artifact (the
// runtime's ops and the per-type decode) to that contract, with a negative
// control that plants each clamp back into the real shapes and verifies the
// checker catches it.

import (
	"strings"
	"testing"
)

// jsFixedNeverClampsSchema is a fixed table reaching every clamp path the cut
// rewrites: a counted array (count), a string (length), a ranged scalar, an
// enum ordinal, and a bool byte.
const jsFixedNeverClampsSchema = `package probe

enum E { a, b }

fixed table R {
  lead uint32
  flag bool
  e E
  rng int32 | min = -10, max = 10
  arr [..4]int32
  s string(8)
}
`

// checkJsFixedLoadNeverClamps inspects the generated decode and runtime for the
// strict-read contract. It reports every clause that is missing or regressed.
func checkJsFixedLoadNeverClamps(decodeBody, runBody string) []string {
	var faults []string
	// THE DECODE'S CHECK LOOP AND BOUNDS PASS REFUSE BY NAME.
	for _, want := range []string{
		"TableFixedRefusal.WireDamaged",     // a bool or present byte other than 0/1
		"TableFixedRefusal.LengthOverBound", // a text length past its cap
		"TableFixedRefusal.CountOverBound",  // a counted-array count past its bound
		"TableFixedRefusal.OutOfRange",      // a ranged scalar or an ordinal past the set
	} {
		if !strings.Contains(decodeBody, want) {
			faults = append(faults, "decode lacks "+want)
		}
	}
	// THE RUNTIME OPS REFUSE BY NAME, NEVER CLAMP-AND-COUNT.
	for _, want := range []string{
		"TableFixedRefusal.LengthOverBound",
		"TableFixedRefusal.CountOverBound",
		"TableFixedRefusal.OutOfRange",
	} {
		if !strings.Contains(runBody, want) {
			faults = append(faults, "runtime lacks "+want)
		}
	}
	// NEITHER PATH MAY STILL COUNT A CLAMP.
	if strings.Contains(decodeBody, "report.oor++") {
		faults = append(faults, "decode still clamps and counts oor")
	}
	if strings.Contains(runBody, "report.oor++") {
		faults = append(faults, "runtime still clamps and counts oor")
	}
	return faults
}

// TestJsFixedLoadRefusesNeverClamps generates a fixed table that reaches every
// clamp path and pins the emitted decode and runtime to the strict read.
func TestJsFixedLoadRefusesNeverClamps(t *testing.T) {
	t.Parallel()
	u := loadSchemaString(t, "neverclamps.schema", jsFixedNeverClampsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var decodeBody, runBody string
	for _, body := range files {
		s := string(body)
		if d := jsFn(s, "RFixedDecode"); d != "" {
			decodeBody = d
		}
		if r := jsFn(s, "TableFixedRun"); r != "" {
			runBody = r
		}
	}
	if decodeBody == "" {
		t.Fatal("no RFixedDecode was generated")
	}
	if runBody == "" {
		t.Fatal("no TableFixedRun runtime was generated")
	}
	if faults := checkJsFixedLoadNeverClamps(decodeBody, runBody); len(faults) != 0 {
		t.Errorf("the strict read does not hold:\n  %s", strings.Join(faults, "\n  "))
	}
}

// TestJsFixedLoadRefusesNeverClampsNegativeControl generates the real artifact,
// verifies the clean shapes hold the strict read, then replants each regression
// into that same generated text — a decode that clamps a ranged scalar and
// counts report.oor, a decode that lands a bool byte above 1 as true, and a
// runtime text op that clamps the length and counts — and verifies the checker
// reports each one. It is the guard that TestJsFixedLoadRefusesNeverClamps would
// go green over code that still clamps.
func TestJsFixedLoadRefusesNeverClampsNegativeControl(t *testing.T) {
	t.Parallel()

	u := loadSchemaString(t, "neverclamps.schema", jsFixedNeverClampsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var decodeBody, runBody string
	for _, body := range files {
		s := string(body)
		if d := jsFn(s, "RFixedDecode"); d != "" {
			decodeBody = d
		}
		if r := jsFn(s, "TableFixedRun"); r != "" {
			runBody = r
		}
	}
	if decodeBody == "" || runBody == "" {
		t.Fatal("no real RFixedDecode/TableFixedRun was generated")
	}

	// hasFault reports whether a specific diagnostic is among the checker's.
	hasFault := func(faults []string, want string) bool {
		for _, f := range faults {
			if f == want {
				return true
			}
		}
		return false
	}

	// The clean generated artifact holds the strict read; each regression below
	// must be introduced by mutating it, never by feeding hand-written literals.
	if faults := checkJsFixedLoadNeverClamps(decodeBody, runBody); len(faults) != 0 {
		t.Fatalf("clean artifact should hold the strict read:\n  %s", strings.Join(faults, "\n  "))
	}

	// Plant a clamp in the real decode: the ranged-scalar refusal becomes a
	// clamp that lands an edge and counts report.oor.
	decRangeClamp := strings.Replace(decodeBody,
		"if (value.rng < -10 || value.rng > 10) { report.refused = TableFixedRefusal.OutOfRange; return; }",
		"if (value.rng < -10) { value.rng = -10; report.oor++; } else if (value.rng > 10) { value.rng = 10; report.oor++; }", 1)
	if decRangeClamp == decodeBody {
		t.Fatal("could not find the ranged-scalar refusal to mutate")
	}
	if faults := checkJsFixedLoadNeverClamps(decRangeClamp, runBody); !hasFault(faults, "decode still clamps and counts oor") {
		t.Fatalf("negative control did not flag the ranged-scalar clamp: %v", faults)
	}

	// Plant a bool byte above 1 landing true in the real decode: the
	// wire_damaged refusal becomes a bare nonzero-is-true assignment.
	decBoolLand := strings.Replace(decodeBody,
		"{ const b = view.getUint8(at + 4); if (b > 1) { report.refused = TableFixedRefusal.WireDamaged; return; } }",
		"value.flag = view.getUint8(at + 4) !== 0;", 1)
	if decBoolLand == decodeBody {
		t.Fatal("could not find the bool-byte refusal to mutate")
	}
	if faults := checkJsFixedLoadNeverClamps(decBoolLand, runBody); !hasFault(faults, "decode lacks TableFixedRefusal.WireDamaged") {
		t.Fatalf("negative control did not flag a bool byte above 1 landing true: %v", faults)
	}

	// Plant a clamp in the real runtime: the text-length refusal becomes a
	// clamp that counts report.oor.
	runTextClamp := strings.Replace(runBody,
		"if (v < 0 || v > cap) { report.refused = TableFixedRefusal.LengthOverBound; return; }",
		"if (v < 0) { v = 0; report.oor++; } else if (v > cap) { v = cap; report.oor++; }", 1)
	if runTextClamp == runBody {
		t.Fatal("could not find the text-length refusal to mutate")
	}
	if faults := checkJsFixedLoadNeverClamps(decodeBody, runTextClamp); !hasFault(faults, "runtime still clamps and counts oor") {
		t.Fatalf("negative control did not flag the runtime text clamp: %v", faults)
	}
}

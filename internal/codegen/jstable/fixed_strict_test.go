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
		"TableFixedRefusal.WireDamaged",  // a bool or present byte other than 0/1
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

// TestJsFixedLoadRefusesNeverClampsNegativeControl plants each regression — a
// decode that clamps a bool to true, a decode that clamps a range, and a
// runtime text op that clamps — into the real artifact's shapes and verifies
// the checker reports it. It is the guard that TestJsFixedLoadRefusesNeverClamps
// would go green over code that still clamps.
func TestJsFixedLoadRefusesNeverClampsNegativeControl(t *testing.T) {
	t.Parallel()

	// A decode that lands a bool byte above 1 as true and clamps a range.
	buggyDecode := `export function RFixedDecode(value, view, at, report) {
  value.lead = view.getUint32(at, true);
  value.flag = view.getUint8(at + 4) !== 0;
  value.rng = view.getInt32(at + 5, true);
  if (value.rng < -10) { value.rng = -10; report.oor++; } else if (value.rng > 10) { value.rng = 10; report.oor++; }
}`
	// A runtime text op that clamps the length and counts.
	buggyRun := `export function TableFixedRun(plan, entryCount, src, srcView, srcAt, dst, dstView, remap, report) {
  case TableFixedOpText: { const v = TableFixedGetU32(src, s) | 0; if (v < 0) { v = 0; report.oor++; } else if (v > cap) { v = cap; report.oor++; } }
}`
	if faults := checkJsFixedLoadNeverClamps(buggyDecode, buggyRun); len(faults) == 0 {
		t.Fatal("negative control failed to detect the clamped decode and runtime")
	}

	// A clean-shaped decode that merely omits every refusal must be flagged too:
	// four decode refusals and three runtime refusals, and no clamp counted.
	if faults := checkJsFixedLoadNeverClamps(
		`export function RFixedDecode(value, view, at, report) { value.lead = view.getUint32(at, true); }`,
		`export function TableFixedRun() {}`,
	); len(faults) != 7 {
		t.Fatalf("negative control did not flag every missing refusal: %v", faults)
	}
}

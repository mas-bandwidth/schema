package darttable

import (
	"strings"
	"testing"
)

// A FIXED ROOT IS FORM 3 ON THE TABLE WIRE, BOTH WAYS
// (docs/SPEC-TABLES.md §3.4; internal/tablewire/fixedroot_test.go): a declared
// `fixed table` writes form byte 3 and reads form byte 3, and a form-1 file at
// such a root is refused as previous_form. These are unit tests: they inspect
// the generated artifact — <Base>Fixed.dart and its runtime home — so they pin
// the rule without a Dart interpreter. The strict read is one read of the whole
// value or a refusal, never a form-1 resave.
const fixedRootThreeUnit = `package fixedpin
fixed table Root
{
    a     int32
    flag  bool
    small int8 | max = 5
    f     float32
}
`

// dartFixedRootArtifacts generates the fixed-root unit and returns the fixed
// root's surface (<Base>Fixed.dart) and the unit's fixed runtime home.
func dartFixedRootArtifacts(t *testing.T) (surface, home string) {
	t.Helper()
	u := unitFrom(t, fixedRootThreeUnit)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	homeBase := runtimeHome(u)
	for name, body := range files {
		if !strings.HasSuffix(name, "Fixed.dart") {
			continue
		}
		if strings.HasPrefix(name, homeBase) {
			home = string(body)
		} else {
			surface = string(body)
		}
	}
	if surface == "" {
		t.Fatal("the generator emitted no <Base>Fixed.dart for the fixed root")
	}
	if home == "" {
		t.Fatal("the generator emitted no fixed runtime home")
	}
	return surface, home
}

// fixedSurfaceHoldsFormThree is the positive check the two tests share: a
// fixed root's surface writes form 3, never form 1, and reads the form byte
// first, refusing form 1 as previous_form.
func fixedSurfaceHoldsFormThree(src string) bool {
	if !strings.Contains(src, "bytes[0] = tableFixedForm;") {
		return false
	}
	if strings.Contains(src, "bytes[0] = 1;") {
		return false
	}
	if !strings.Contains(src, "if (bytes[0] != tableFixedForm)") {
		return false
	}
	if !strings.Contains(src, "TableFixedLimits.variableForm => TableFixedRefusal.previousForm") {
		return false
	}
	return true
}

func TestDartFixedRootSavesFormThree(t *testing.T) {
	t.Parallel()
	surface, home := dartFixedRootArtifacts(t)
	if !strings.Contains(home, "const int tableFixedForm = 3;") {
		t.Error("the fixed runtime home does not pin tableFixedForm = 3")
	}
	if !fixedSurfaceHoldsFormThree(surface) {
		t.Error("the generated fixed root's surface does not hold form 3 both ways")
	}
}

func TestDartFixedRootSavesFormThreeNegativeControl(t *testing.T) {
	t.Parallel()
	surface, _ := dartFixedRootArtifacts(t)
	// PLANT THE FAULT IN THE REAL ARTIFACT: the save writes form byte 1
	// instead of 3 — the exact defect the card closes. The positive check must
	// catch it, proving it pins the real artifact and not a vacuous invariant.
	planted := strings.ReplaceAll(surface, "bytes[0] = tableFixedForm;", "bytes[0] = 1;")
	if planted == surface {
		t.Fatal("could not plant a form-1 save in the real artifact")
	}
	if fixedSurfaceHoldsFormThree(planted) {
		t.Error("the check accepted a form-1 save: the negative control shows nothing")
	}
}

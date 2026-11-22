package unitytable

import (
	"strings"
	"testing"
)

// The JSON integer read takes an integer token only (docs/SPEC-TABLES.md
// §16.2): the integer token rule refuses a fraction, an exponent and -0
// before the narrow integer kinds are reached, and the wide and fixed-point
// kinds read through ReadWide. So the double conversion that judged a
// fractional token for an integer field is unreachable and is not emitted,
// nor the comment that described integral floats landing (as CS-4 for C#
// (modern)).
func TestUnityJsonIntegerReadHasNoFractionalBranch(t *testing.T) {
	t.Parallel()
	for _, gone := range []string{"JSON HAS ONE NUMBER TYPE", "d != (double)(long)d", "d != (double)(ulong)d"} {
		if strings.Contains(unityTableJsonSource, gone) {
			t.Errorf("the JSON integer read still carries %q", gone)
		}
	}
	if !strings.Contains(unityTableJsonSource, "long value2 = TokenInteger(token, isSigned, out bool saturated);") {
		t.Error("the JSON integer read does not take the token's digits as the value")
	}
}

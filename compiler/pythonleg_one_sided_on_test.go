package compiler

import "testing"

func init() { nonGoCarriers["python"] = append(nonGoCarriers["python"], carryOneSidedRange) }

func TestPythonOneSidedRangePublicDriver(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{
		"package probe\ntable Root { lower int32 = 5 | min = 5 }\n",
		"package probe\ntable Root { upper uint16 | max = 9 }\n",
	} {
		if _, err := New().Generate(unitFromSource(t, schema), "python", nil); err != nil {
			t.Fatalf("Python refuses the one-sided range: %v", err)
		}
	}
}

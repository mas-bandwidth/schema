package cstable

import "testing"

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/SPEC-TABLES.md §22.6,
// docs/FIXED-FORM-ALGORITHM.md §5.3 step 9): a file whose header and layout are
// followed by no record holds no value to read, and every fixed read entry
// refuses it whatever capacity the caller offered.
func TestCSharpFixedLoadRefusesAnEmptyRoot(t *testing.T) {
	t.Parallel()
	csEmittedRulesTest(t, `package emptyroot

fixed table Root
{
    a int32
}
`, []csEmittedRule{{
		start: "public static long RootFixedLoad(\n",
		end:   "public static long RootFixedLoad(Root[] values",
		line:  "if (record_bytes <= 8 || rest == 0 || rest % record_bytes != 0)",
		why:   "a file whose header and layout are followed by no record is read as an empty batch",
	}})
}

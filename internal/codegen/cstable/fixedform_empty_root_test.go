package cstable

import "testing"

// A ROOT READ IS ONE RECORD (docs/SPEC-TABLES.md §3.4): handed room for one
// value, a fixed file of no record holds no value to read, and the engine
// answers it truncated (internal/tablewire/fixed.go). A batch read of more
// room may still read none.
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
		line:  "if (record_bytes <= 8 || rest % record_bytes != 0 || (rest == 0 && values.Length == 1))",
		why:   "a root read of a file with no record answers no refusal",
	}})
}

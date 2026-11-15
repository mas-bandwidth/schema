package lockfile_test

import "testing"

func TestLockLargeIntegerRangeNarrowingRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind string
		was  string
		now  string
	}{
		{
			name: "int64",
			kind: "int64",
			was:  "9223372036854775807",
			now:  "9223372036854775300",
		},
		{
			name: "uint64",
			kind: "uint64",
			was:  "18446744073709551615",
			now:  "18446744073709551500",
		},
		{
			name: "int128",
			kind: "int128",
			was:  "170141183460469231731687303715884105727",
			now:  "170141183460469231731687303715884105700",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := rowTable("    n " + tc.kind + " = 0 | min = 0, max = " + tc.was)
			after := rowTable("    n " + tc.kind + " = 0 | min = 0, max = " + tc.now)
			rowRefuses(t, before, after, "range narrowed")
		})
	}
}

func TestLockLargeIntegerRangeWideningAllows(t *testing.T) {
	t.Parallel()
	rowAllows(t,
		rowTable("    n int64 = 0 | min = 0, max = 9223372036854775300"),
		rowTable("    n int64 = 0 | min = 0, max = 9223372036854775807"),
		"Row")
}

func TestLockLargeIntegerRangeUnchangedPasses(t *testing.T) {
	t.Parallel()
	bound := "9223372036854775807"
	rowUnchanged(t,
		rowTable("    n int64 = 0 | min = 0, max = "+bound),
		rowTable("    n int64 = 0 | min = 0, max = "+bound))
}

func TestLockExponentFloatRangeWideningAllows(t *testing.T) {
	t.Parallel()
	rowAllows(t,
		rowTable("    n float32 | min = -1e30, max = 1e30, resolution = 1e25"),
		rowTable("    n float32 | min = -2e30, max = 2e30, resolution = 1e25"),
		"Row")
}

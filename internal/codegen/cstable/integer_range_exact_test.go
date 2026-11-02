package cstable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"testing"
)

// These are the same fourteen exact integer boundaries held by the neutral
// corpus. Each native read checks value, clamp events and canonical wire bytes.
func TestIntegerRangeExactCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	runCarryCS(t, `package probe
table Root {
 upper uint64 | max = 18446744073709551610
 lower uint64 = 9007199254740993 | min = 9007199254740993
 negative int64 = -9007199254740993 | max = -9007199254740993
 floor int64 | min = -9223372036854775807
 packed bits(63)
}
`, []string{
		`{"upper":18446744073709551609}`,
		`{"upper":18446744073709551610}`,
		`{"upper":18446744073709551611}`,
		`{"upper":18446744073709551615}`,
		`{"lower":9007199254740992}`,
		`{"lower":9007199254740993}`,
		`{"lower":9007199254740994}`,
		`{"negative":-9007199254740992}`,
		`{"negative":-9007199254740993}`,
		`{"negative":-9007199254740994}`,
		`{"floor":-9223372036854775808}`,
		`{"floor":-9223372036854775807}`,
		`{"packed":9223372036854775807}`,
		`{"packed":9223372036854775808}`,
	})
}

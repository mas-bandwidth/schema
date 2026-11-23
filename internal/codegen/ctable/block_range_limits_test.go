package ctable

import (
	"strings"
	"testing"
)

const cBlockRangeLimitsSchema = `package probe
fixed table Ranges {
 signed_span int64 | min = -9223372036854775808, max = 9223372036854775807
 signed_low int64 | min = -9223372036854775808, max = 9223372036854775806
 signed_high int64 | min = -9223372036854775807, max = 9223372036854775807
 unsigned_span uint64 | min = 0, max = 18446744073709551615
 unsigned_low uint64 | min = 0, max = 18446744073709551614
 unsigned_high uint64 = 1 | min = 1, max = 18446744073709551615
}
`

func TestCBlockRangeChecksElideStorageLimits(t *testing.T) {
	src := string(generate(t, cBlockRangeLimitsSchema)["ProbeBlock.h"])
	for _, forbidden := range []string{
		"-9223372036854775808ll",
		"18446744073709551615ull", "< 0ull",
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("block range check kept storage-limit expression %q", forbidden)
		}
	}
	for _, want := range []string{
		"signed_low > 9223372036854775806ll",
		"signed_high < -9223372036854775807ll",
		"unsigned_low > 18446744073709551614ull",
		"unsigned_high < 1ull",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("block range check is missing live end %q", want)
		}
	}
}

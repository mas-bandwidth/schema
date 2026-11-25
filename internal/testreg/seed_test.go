package testreg

import (
	"testing"
	"time"
)

// TestADateSeedIsTheUTCDate: the seed is the run's UTC date as YYYYMMDD,
// whatever zone the clock reads in.
func TestADateSeedIsTheUTCDate(t *testing.T) {
	t.Parallel()
	east := time.FixedZone("east", 10*3600)
	for at, want := range map[time.Time]uint64{
		time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC):   20261125,
		time.Date(2026, 1, 2, 3, 0, 0, 0, east):          20260101, // still the 1st in UTC
		time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC): 19991231,
	} {
		if got := DateSeed(at); got != want {
			t.Errorf("DateSeed(%v) = %d, want %d", at, got, want)
		}
	}
}

// TestASeedIsADecimalNumberAndNothingElse: a missing or malformed seed is an
// error naming the variable, never a default.
func TestASeedIsADecimalNumberAndNothingElse(t *testing.T) {
	t.Parallel()
	if n, err := ParseSeed("20261125"); err != nil || n != 20261125 {
		t.Errorf("ParseSeed(20261125) = %d, %v", n, err)
	}
	for _, bad := range []string{"", "x", "-1", "1.5", " 7", "0x10", "99999999999999999999999"} {
		if _, err := ParseSeed(bad); err == nil {
			t.Errorf("ParseSeed(%q) was accepted", bad)
		}
	}
}

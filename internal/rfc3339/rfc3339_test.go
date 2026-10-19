package rfc3339_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/rfc3339"
)

// TestStrictRFC3339 pins the one timestamp text parser (docs/SPEC-TABLES.md
// §2.11, §16.2): exactly RFC 3339 §5.6's date-time production, no more and no
// less. Everything accepted lands its UTC nanosecond count; everything refused
// answers false. No case reads the clock.
func TestStrictRFC3339(t *testing.T) {
	t.Parallel()

	utc := func(y int, mo time.Month, d, h, mi, s, ns int) int64 {
		return time.Date(y, mo, d, h, mi, s, ns, time.UTC).UnixNano()
	}

	accepted := []struct {
		name  string
		text  string
		nanos int64
	}{
		// the four-digit date, month 01-12 and a day valid for the month and year
		{"epoch", "1970-01-01T00:00:00Z", 0},
		{"leap day", "2024-02-29T12:34:05Z", utc(2024, time.February, 29, 12, 34, 5, 0)},
		{"400-year leap day", "2000-02-29T00:00:00Z", utc(2000, time.February, 29, 0, 0, 0, 0)},
		{"pre-epoch", "1969-12-31T23:59:59.999999999Z", -1},

		// T or t, and Z or z (§5.6's note)
		{"lowercase t and z", "2026-10-17t12:34:05z", utc(2026, time.October, 17, 12, 34, 5, 0)},
		{"lowercase with fraction", "2026-10-17t12:34:05.5z", utc(2026, time.October, 17, 12, 34, 5, 500000000)},

		// a fraction of one or more digits after a dot, truncated to nanoseconds
		{"one fraction digit", "2026-10-17T12:34:05.5Z", utc(2026, time.October, 17, 12, 34, 5, 500000000)},
		{"nine fraction digits", "2026-10-17T12:34:05.123456789Z", utc(2026, time.October, 17, 12, 34, 5, 123456789)},
		{"fraction truncated", "2026-10-17T12:34:05.1234567899Z", utc(2026, time.October, 17, 12, 34, 5, 123456789)},
		{"fraction longer than a scratch buffer", "2026-10-17T12:34:05." + strings.Repeat("1234567890", 10) + "Z", utc(2026, time.October, 17, 12, 34, 5, 123456789)},
		{"zero fraction", "2026-10-17T12:34:05.0Z", utc(2026, time.October, 17, 12, 34, 5, 0)},

		// an offset of hours 00-23 and minutes 00-59, both signs and the ends
		{"positive offset", "2026-10-17T18:04:05.123456789+05:30", utc(2026, time.October, 17, 12, 34, 5, 123456789)},
		{"negative offset", "2026-10-17T05:34:05-07:00", utc(2026, time.October, 17, 12, 34, 5, 0)},
		{"offset hour 23", "2026-10-18T12:33:05+23:59", utc(2026, time.October, 17, 12, 34, 5, 0)},
		{"negative offset hour 23", "2026-10-16T12:35:05-23:59", utc(2026, time.October, 17, 12, 34, 5, 0)},
		{"unknown local offset", "1970-01-01T00:00:00-00:00", 0},

		// the ends of Time.UnixNano's domain are counts and are accepted
		{"UnixNano minimum", "1677-09-21T00:12:43.145224192Z", math.MinInt64},
		{"UnixNano maximum", "2262-04-11T23:47:16.854775807Z", math.MaxInt64},
	}
	for _, tc := range accepted {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			t.Parallel()
			nanos, ok := rfc3339.Parse([]byte(tc.text))
			if !ok {
				t.Fatalf("refused %q", tc.text)
			}
			if nanos != tc.nanos {
				t.Fatalf("%q is %d, want %d", tc.text, nanos, tc.nanos)
			}
		})
	}

	refused := []struct{ name, text string }{
		// the four spellings of the red: time.Parse's hour 24, minute 60 and
		// comma fraction are not RFC 3339, and lowercase t/z are
		{"offset hour 24", "2026-10-17T12:34:05+24:00"},
		{"offset minute 60", "2026-10-17T12:34:05+05:60"},
		{"comma fraction", "2026-10-17T12:34:05,5Z"},
		{"negative offset hour 24", "2026-10-17T12:34:05-24:00"},
		{"negative offset minute 60", "2026-10-17T12:34:05-05:60"},

		// second 60 is a leap second this kind refuses (§2.11)
		{"leap second", "2016-12-31T23:59:60Z"},
		{"leap second with fraction", "2016-12-31T23:59:60.5Z"},

		// the date: month 01-12 and a day valid for that month and year
		{"month zero", "2026-00-17T12:34:05Z"},
		{"month thirteen", "2026-13-17T12:34:05Z"},
		{"day zero", "2026-10-00T12:34:05Z"},
		{"day thirty-two", "2026-10-32T12:34:05Z"},
		{"february thirty", "2026-02-30T12:34:05Z"},
		{"april thirty-one", "2026-04-31T12:34:05Z"},
		{"non-leap february twenty-nine", "2025-02-29T12:34:05Z"},
		{"century non-leap february twenty-nine", "1900-02-29T12:34:05Z"},

		// the time: hour 00-23, minute 00-59, second 00-59
		{"hour 24", "2026-10-17T24:00:00Z"},
		{"minute 60", "2026-10-17T12:60:05Z"},

		// the fraction is a dot and at least one digit
		{"empty fraction", "2026-10-17T12:34:05.Z"},
		{"two dots", "2026-10-17T12:34:05.1.2Z"},
		{"comma with digits", "2026-10-17T12:34:05,123Z"},

		// the offset is Z, z or sign HH:MM with both fields in range
		{"missing offset", "2026-10-17T12:34:05"},
		{"offset without colon", "2026-10-17T12:34:05+0530"},
		{"offset hour only", "2026-10-17T12:34:05+05"},
		{"offset minute only", "2026-10-17T12:34:05+05:30:00"},

		// nothing before or after
		{"empty", ""},
		{"date only", "2026-10-17"},
		{"missing seconds", "2026-10-17T12:34Z"},
		{"leading space", " 2026-10-17T12:34:05Z"},
		{"trailing space", "2026-10-17T12:34:05Z "},
		{"trailing newline", "2026-10-17T12:34:05Z\n"},
		{"trailing character", "2026-10-17T12:34:05Zx"},
		{"leading character", "x2026-10-17T12:34:05Z"},
		{"space for T", "2026-10-17 12:34:05Z"},
		{"non-digit year", "20x6-10-17T12:34:05Z"},
		{"non-digit hour", "2026-10-17T1x:34:05Z"},

		// an instant outside Time.UnixNano's domain names no count
		{"year zero outside the domain", "0000-01-01T00:00:00Z"},
		{"year 9999 outside the domain", "9999-12-31T23:59:59Z"},
		{"one nanosecond below the minimum", "1677-09-21T00:12:43.145224191Z"},
		{"one nanosecond above the maximum", "2262-04-11T23:47:16.854775808Z"},
	}
	for _, tc := range refused {
		t.Run("refuse/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if nanos, ok := rfc3339.Parse([]byte(tc.text)); ok {
				t.Fatalf("accepted %q as %d", tc.text, nanos)
			}
		})
	}
}

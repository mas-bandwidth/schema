package compiler

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// TestCPPTimestampTextStrictRFC3339 holds the C++ text form to
// docs/SPEC-TABLES.md §2.11 and §16.2: a timestamp's JSON text is exactly
// RFC 3339 §5.6's `date-time`, and anything else refuses the read by name
// (timestamp_not_rfc3339). Every accepted count and every refused spelling is
// the one internal/rfc3339/rfc3339_test.go's TestStrictRFC3339 pins for the
// same bytes, repeated in internal/codegen/gotable/timestampstrict_test.go's
// strictTimestampCases (TestGoTimestampStrictRFC3339, the oracle). The writer
// is held to a round trip through the same strict reader.
func TestCPPTimestampTextStrictRFC3339(t *testing.T) {
	t.Parallel()

	schema := "package probe\nfixed table Root\n{\n    at timestamp\n}\n"
	u := unitFromSource(t, schema)
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatalf("the cpp target refuses a timestamp: %v", err)
	}

	// the expected counts are the ones internal/rfc3339/rfc3339_test.go's
	// TestStrictRFC3339 pins: time.Date in UTC, exactly this test's utc helper
	utc := func(y int, mo time.Month, d, h, mi, s, ns int) int64 {
		return time.Date(y, mo, d, h, mi, s, ns, time.UTC).UnixNano()
	}

	type row struct {
		name  string
		text  string
		nanos int64
		ok    bool
	}
	rows := []row{
		// accepted, internal/rfc3339/rfc3339_test.go TestStrictRFC3339
		{"epoch", "1970-01-01T00:00:00Z", 0, true},
		{"leap day", "2024-02-29T12:34:05Z", utc(2024, time.February, 29, 12, 34, 5, 0), true},
		{"400-year leap day", "2000-02-29T00:00:00Z", utc(2000, time.February, 29, 0, 0, 0, 0), true},
		{"pre-epoch", "1969-12-31T23:59:59.999999999Z", -1, true},
		{"lowercase t and z", "2026-10-17t12:34:05z", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"lowercase with fraction", "2026-10-17t12:34:05.5z", utc(2026, time.October, 17, 12, 34, 5, 500000000), true},
		{"one fraction digit", "2026-10-17T12:34:05.5Z", utc(2026, time.October, 17, 12, 34, 5, 500000000), true},
		{"nine fraction digits", "2026-10-17T12:34:05.123456789Z", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"fraction truncated", "2026-10-17T12:34:05.1234567899Z", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"fraction longer than a scratch buffer", "2026-10-17T12:34:05." + strings.Repeat("1234567890", 10) + "Z", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"zero fraction", "2026-10-17T12:34:05.0Z", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"positive offset", "2026-10-17T18:04:05.123456789+05:30", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"negative offset", "2026-10-17T05:34:05-07:00", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"offset hour 23", "2026-10-18T12:33:05+23:59", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"negative offset hour 23", "2026-10-16T12:35:05-23:59", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"unknown local offset", "1970-01-01T00:00:00-00:00", 0, true},
		{"UnixNano minimum", "1677-09-21T00:12:43.145224192Z", math.MinInt64, true},
		{"UnixNano maximum", "2262-04-11T23:47:16.854775807Z", math.MaxInt64, true},

		// refused: the four spellings of the red
		{"offset hour 24", "2026-10-17T12:34:05+24:00", 0, false},
		{"offset minute 60", "2026-10-17T12:34:05+05:60", 0, false},
		{"comma fraction", "2026-10-17T12:34:05,5Z", 0, false},
		{"negative offset hour 24", "2026-10-17T12:34:05-24:00", 0, false},
		{"negative offset minute 60", "2026-10-17T12:34:05-05:60", 0, false},

		// refused: the leap second, the date and the time
		{"leap second", "2016-12-31T23:59:60Z", 0, false},
		{"leap second with fraction", "2016-12-31T23:59:60.5Z", 0, false},
		{"month zero", "2026-00-17T12:34:05Z", 0, false},
		{"month thirteen", "2026-13-17T12:34:05Z", 0, false},
		{"day zero", "2026-10-00T12:34:05Z", 0, false},
		{"day thirty-two", "2026-10-32T12:34:05Z", 0, false},
		{"february thirty", "2026-02-30T12:34:05Z", 0, false},
		{"april thirty-one", "2026-04-31T12:34:05Z", 0, false},
		{"non-leap february twenty-nine", "2025-02-29T12:34:05Z", 0, false},
		{"century non-leap february twenty-nine", "1900-02-29T12:34:05Z", 0, false},
		{"hour 24", "2026-10-17T24:00:00Z", 0, false},
		{"minute 60", "2026-10-17T12:60:05Z", 0, false},

		// refused: the fraction, the offset and anything before or after
		{"empty fraction", "2026-10-17T12:34:05.Z", 0, false},
		{"two dots", "2026-10-17T12:34:05.1.2Z", 0, false},
		{"comma with digits", "2026-10-17T12:34:05,123Z", 0, false},
		{"missing offset", "2026-10-17T12:34:05", 0, false},
		{"offset without colon", "2026-10-17T12:34:05+0530", 0, false},
		{"offset hour only", "2026-10-17T12:34:05+05", 0, false},
		{"offset minute only", "2026-10-17T12:34:05+05:30:00", 0, false},
		{"empty", "", 0, false},
		{"date only", "2026-10-17", 0, false},
		{"missing seconds", "2026-10-17T12:34Z", 0, false},
		{"leading space", " 2026-10-17T12:34:05Z", 0, false},
		{"trailing space", "2026-10-17T12:34:05Z ", 0, false},
		{"trailing newline", "2026-10-17T12:34:05Z\n", 0, false},
		{"trailing character", "2026-10-17T12:34:05Zx", 0, false},
		{"leading character", "x2026-10-17T12:34:05Z", 0, false},
		{"space for T", "2026-10-17 12:34:05Z", 0, false},
		{"non-digit year", "20x6-10-17T12:34:05Z", 0, false},
		{"non-digit hour", "2026-10-17T1x:34:05Z", 0, false},

		// refused: an instant outside Time.UnixNano's domain
		{"year zero outside the domain", "0000-01-01T00:00:00Z", 0, false},
		{"year 9999 outside the domain", "9999-12-31T23:59:59Z", 0, false},
		{"one nanosecond below the minimum", "1677-09-21T00:12:43.145224191Z", 0, false},
		{"one nanosecond above the maximum", "2262-04-11T23:47:16.854775808Z", 0, false},
	}

	var cases strings.Builder
	for _, r := range rows {
		nanos := fmt.Sprintf("%dll", r.nanos)
		if r.nanos == math.MinInt64 {
			nanos = "(-9223372036854775807ll - 1)"
		}
		canon := ""
		if r.ok {
			canon = time.Unix(0, r.nanos).UTC().Format(time.RFC3339Nano)
		}
		fmt.Fprintf(&cases, "    { %q, %q, %s, %t, %q },\n", r.name, `{"at":`+fmt.Sprintf("%q", r.text)+`}`, nanos, r.ok, canon)
	}

	driver := `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
#include <cstdint>
using namespace probe;

struct Case { const char * name; const char * json; int64_t nanos; bool ok; const char * canon; };
static const Case cases[] = {
` + cases.String() + `};

static char buffer[ 4096 ];

int main()
{
    for ( size_t i = 0; i < sizeof( cases ) / sizeof( cases[0] ); i++ )
    {
        const Case & c = cases[ i ];
        Root v;
        RootReset( v );
        TableReport report = {};
        const bool ok = RootFromJson( v, c.json, (int64_t) strlen( c.json ), &report );
        if ( ok != c.ok )
        {
            printf( "%s: accepted=%d want %d malformed=%d\n", c.name, (int) ok, (int) c.ok, (int) report.malformed );
            return 1;
        }
        if ( !c.ok )
        {
            // timestamp_not_rfc3339: the strict reader's named refusal (§2.11, §16.2)
            if ( !report.malformed ) { printf( "%s: refused without malformed\n", c.name ); return 1; }
            if ( v.at != 0 ) { printf( "%s: the refused spelling landed %lld\n", c.name, (long long) v.at ); return 1; }
            continue;
        }
        if ( report.malformed ) { printf( "%s: accepted with malformed\n", c.name ); return 1; }
        if ( v.at != c.nanos ) { printf( "%s: landed %lld, want %lld\n", c.name, (long long) v.at, (long long) c.nanos ); return 1; }

        // the writer emits the canonical UTC spelling ("Z"), and the strict
        // reader reads it back to the same count
        const int64_t need = RootToJsonMeasure( v );
        if ( need <= 0 ) { printf( "%s: measure %lld\n", c.name, (long long) need ); return 1; }
        const int64_t n = RootToJson( v, buffer, (int64_t) sizeof( buffer ) );
        if ( n != need ) { printf( "%s: write %lld measure %lld\n", c.name, (long long) n, (long long) need ); return 1; }
        Root back;
        RootReset( back );
        TableReport again = {};
        if ( !RootFromJson( back, buffer, n, &again ) || again.malformed || back.at != v.at )
        {
            printf( "%s: the written text does not read back: %s\n", c.name, buffer );
            return 1;
        }
        // the canonical UTC spelling is Go's time.RFC3339Nano, the oracle's
        // own writer, for this same count
        if ( c.canon[ 0 ] == 0 || strstr( buffer, c.canon ) == NULL )
        {
            printf( "%s: the written timestamp is not %s: %s\n", c.name, c.canon, buffer );
            return 1;
        }
    }
    return 0;
}
`
	runCppTableProbe(t, files, driver)
}

package gotable

import (
	"fmt"
	"go/format"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/rfc3339"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// strictTimestampSchema is the unit whose text form this test drives: one
// timestamp field, so a spec's reader is the generated RootFromJson and the
// engine's reader is tabletext.Model.Read.
const strictTimestampSchema = `package probe
fixed table Root
{
    at timestamp
}
`

type strictTimestampCase struct {
	name  string
	text  string // the timestamp spelling alone
	json  string // the document carrying it
	nanos int64
	ok    bool
}

// strictTimestampCases is the table TestStrictRFC3339 pins in
// internal/rfc3339, repeated here so the generated reader and the engine are
// held to every spelling and not to a sample (docs/SPEC-TABLES.md §2.11,
// §16.2).
func strictTimestampCases() []strictTimestampCase {
	utc := func(y int, mo time.Month, d, h, mi, s, ns int) int64 {
		return time.Date(y, mo, d, h, mi, s, ns, time.UTC).UnixNano()
	}
	cases := []strictTimestampCase{
		// accepted
		{"epoch", "1970-01-01T00:00:00Z", "", 0, true},
		{"leap day", "2024-02-29T12:34:05Z", "", utc(2024, time.February, 29, 12, 34, 5, 0), true},
		{"400-year leap day", "2000-02-29T00:00:00Z", "", utc(2000, time.February, 29, 0, 0, 0, 0), true},
		{"pre-epoch", "1969-12-31T23:59:59.999999999Z", "", -1, true},
		{"lowercase t and z", "2026-10-17t12:34:05z", "", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"lowercase with fraction", "2026-10-17t12:34:05.5z", "", utc(2026, time.October, 17, 12, 34, 5, 500000000), true},
		{"one fraction digit", "2026-10-17T12:34:05.5Z", "", utc(2026, time.October, 17, 12, 34, 5, 500000000), true},
		{"nine fraction digits", "2026-10-17T12:34:05.123456789Z", "", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"fraction truncated", "2026-10-17T12:34:05.1234567899Z", "", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"fraction longer than a scratch buffer", "2026-10-17T12:34:05." + strings.Repeat("1234567890", 10) + "Z", "", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"zero fraction", "2026-10-17T12:34:05.0Z", "", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"positive offset", "2026-10-17T18:04:05.123456789+05:30", "", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"negative offset", "2026-10-17T05:34:05-07:00", "", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"offset hour 23", "2026-10-18T12:33:05+23:59", "", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"negative offset hour 23", "2026-10-16T12:35:05-23:59", "", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"unknown local offset", "1970-01-01T00:00:00-00:00", "", 0, true},
		{"UnixNano minimum", "1677-09-21T00:12:43.145224192Z", "", -9223372036854775808, true},
		{"UnixNano maximum", "2262-04-11T23:47:16.854775807Z", "", 9223372036854775807, true},

		// refused: the four spellings of the red
		{"offset hour 24", "2026-10-17T12:34:05+24:00", "", 0, false},
		{"offset minute 60", "2026-10-17T12:34:05+05:60", "", 0, false},
		{"comma fraction", "2026-10-17T12:34:05,5Z", "", 0, false},
		{"negative offset hour 24", "2026-10-17T12:34:05-24:00", "", 0, false},
		{"negative offset minute 60", "2026-10-17T12:34:05-05:60", "", 0, false},

		// refused: the leap second, the date and the time
		{"leap second", "2016-12-31T23:59:60Z", "", 0, false},
		{"leap second with fraction", "2016-12-31T23:59:60.5Z", "", 0, false},
		{"month zero", "2026-00-17T12:34:05Z", "", 0, false},
		{"month thirteen", "2026-13-17T12:34:05Z", "", 0, false},
		{"day zero", "2026-10-00T12:34:05Z", "", 0, false},
		{"day thirty-two", "2026-10-32T12:34:05Z", "", 0, false},
		{"february thirty", "2026-02-30T12:34:05Z", "", 0, false},
		{"april thirty-one", "2026-04-31T12:34:05Z", "", 0, false},
		{"non-leap february twenty-nine", "2025-02-29T12:34:05Z", "", 0, false},
		{"century non-leap february twenty-nine", "1900-02-29T12:34:05Z", "", 0, false},
		{"hour 24", "2026-10-17T24:00:00Z", "", 0, false},
		{"minute 60", "2026-10-17T12:60:05Z", "", 0, false},

		// refused: the fraction, the offset and anything before or after
		{"empty fraction", "2026-10-17T12:34:05.Z", "", 0, false},
		{"two dots", "2026-10-17T12:34:05.1.2Z", "", 0, false},
		{"comma with digits", "2026-10-17T12:34:05,123Z", "", 0, false},
		{"missing offset", "2026-10-17T12:34:05", "", 0, false},
		{"offset without colon", "2026-10-17T12:34:05+0530", "", 0, false},
		{"offset hour only", "2026-10-17T12:34:05+05", "", 0, false},
		{"offset minute only", "2026-10-17T12:34:05+05:30:00", "", 0, false},
		{"empty", "", "", 0, false},
		{"date only", "2026-10-17", "", 0, false},
		{"missing seconds", "2026-10-17T12:34Z", "", 0, false},
		{"leading space", " 2026-10-17T12:34:05Z", "", 0, false},
		{"trailing space", "2026-10-17T12:34:05Z ", "", 0, false},
		{"trailing newline", "2026-10-17T12:34:05Z\n", "", 0, false},
		{"trailing character", "2026-10-17T12:34:05Zx", "", 0, false},
		{"leading character", "x2026-10-17T12:34:05Z", "", 0, false},
		{"space for T", "2026-10-17 12:34:05Z", "", 0, false},
		{"non-digit year", "20x6-10-17T12:34:05Z", "", 0, false},
		{"non-digit hour", "2026-10-17T1x:34:05Z", "", 0, false},

		// refused: an instant outside Time.UnixNano's domain
		{"year zero outside the domain", "0000-01-01T00:00:00Z", "", 0, false},
		{"year 9999 outside the domain", "9999-12-31T23:59:59Z", "", 0, false},
		{"one nanosecond below the minimum", "1677-09-21T00:12:43.145224191Z", "", 0, false},
		{"one nanosecond above the maximum", "2262-04-11T23:47:16.854775808Z", "", 0, false},
	}
	for i := range cases {
		cases[i].json = `{"at":` + strconv.Quote(cases[i].text) + `}`
	}
	return cases
}

// TestGoTimestampStrictRFC3339 drives the two Go readers over the same table:
// the generated text walk (runGenerated, the package's house pattern) and the
// engine internal/tabletext. They must land the same nanosecond count on every
// accepted spelling and the same named refusal on every refused one
// (docs/SPEC-TABLES.md §2.11, §16.2). It also fails if the parser source the
// emitter embeds is not the source internal/rfc3339 compiles.
func TestGoTimestampStrictRFC3339(t *testing.T) {
	t.Parallel()
	cases := strictTimestampCases()

	// THE PARSER THE EMITTER EMBEDS IS THE PARSER THE PACKAGE COMPILES.
	u := unitFrom(t, strictTimestampSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var jsonGo string
	for name, data := range files {
		if strings.HasSuffix(name, "TableJson.go") {
			jsonGo = string(data)
		}
	}
	if jsonGo == "" {
		t.Fatal("no generated TableJson.go")
	}
	fragment := rfc3339.Source[strings.Index(rfc3339.Source, "func tableRFC3339Parse"):]
	formatted, err := format.Source([]byte("package p\n\n" + fragment))
	if err != nil {
		t.Fatalf("the parser source does not format: %v", err)
	}
	want := strings.TrimPrefix(string(formatted), "package p\n\n")
	if !strings.Contains(jsonGo, want) {
		t.Fatalf("the emitted reader does not carry internal/rfc3339/rfc3339.go verbatim")
	}

	// THE ENGINE: tabletext reads the same table.
	m := tabletext.NewModel(u)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("no table Root")
	}
	for _, tc := range cases {
		t.Run("engine/"+tc.name, func(t *testing.T) {
			t.Parallel()
			inst := m.New(st)
			var report tabletext.Report
			ok := m.Read(inst, []byte(tc.json), &report)
			if ok != tc.ok {
				t.Fatalf("%s: accepted=%t want %t report=%+v", tc.json, ok, tc.ok, report)
			}
			field, found := inst.FieldByKey("at")
			if !found {
				t.Fatal("Root has no field keyed at")
			}
			if !tc.ok {
				if !report.Malformed || !strings.HasPrefix(report.Reason, "timestamp_not_rfc3339: at") {
					t.Fatalf("%s: refusal %+v", tc.json, report)
				}
				if field.Cell.I != 0 {
					t.Fatalf("%s: the refused spelling landed %d", tc.json, field.Cell.I)
				}
				return
			}
			if report.Malformed || report.Reason != "" {
				t.Fatalf("%s: report %+v", tc.json, report)
			}
			if field.Cell.I != tc.nanos {
				t.Fatalf("%s: landed %d, want %d", tc.json, field.Cell.I, tc.nanos)
			}
		})
	}

	// THE GENERATED LEG: the same table compiled and run in a t.TempDir().
	var source strings.Builder
	source.WriteString("package probe\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\n")
	source.WriteString("var strictCases = []struct {\n\tname  string\n\tjson  string\n\tnanos int64\n\tok    bool\n}{\n")
	for _, tc := range cases {
		fmt.Fprintf(&source, "\t{%q, %q, %d, %t},\n", tc.name, tc.json, tc.nanos, tc.ok)
	}
	source.WriteString(`}

func TestStrictTimestampSpellings(t *testing.T) {
	for _, c := range strictCases {
		t.Run(c.name, func(t *testing.T) {
			var got Root
			var report TableReport
			ok := RootFromJson(&got, []byte(c.json), &report)
			if ok != c.ok {
				t.Fatalf("%s: accepted=%t want %t report=%+v", c.json, ok, c.ok, report)
			}
			if !c.ok {
				if !report.Malformed || !strings.HasPrefix(report.Reason, "timestamp_not_rfc3339: at") {
					t.Fatalf("%s: refusal %+v", c.json, report)
				}
				if got.At != 0 {
					t.Fatalf("%s: the refused spelling landed %d", c.json, got.At)
				}
				return
			}
			if report.Malformed || report.Reason != "" {
				t.Fatalf("%s: report %+v", c.json, report)
			}
			if got.At != c.nanos {
				t.Fatalf("%s: landed %d, want %d", c.json, got.At, c.nanos)
			}
		})
	}
}
`)
	runGenerated(t, strictTimestampSchema, source.String())
}

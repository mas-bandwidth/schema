package ctable

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cTimestampSchema = `package probe
fixed table Root
{
    at  timestamp
    tag int64
}
`

const cTimestampVariableProbe = `
    static const int64_t nanos[] = {
        0ll,
        1740736445000000000ll,
        -1ll,
        (-9223372036854775807ll - 1),
        9223372036854775807ll
    };
    static uint8_t wire[ 256 ];
    int k, i;
    for ( k = 0; k < 5; ++k )
    {
        Root v, back;
        int64_t measured, n;
        TableReport rr;
        root_reset( &v );
        v.at = nanos[ k ];
        v.tag = k + 1;
        measured = root_measure( &v );
        n = root_save( &v, wire, (int64_t) sizeof( wire ) );
        if ( n <= 0 || n != measured ) { printf( "variable: save %lld measure %lld\n", (long long) n, (long long) measured ); return 1; }

        memset( &rr, 0, sizeof( rr ) );
        if ( !root_load( &back, wire, n, &rr ) ) { printf( "variable: load failed at case %d malformed=%d\n", k, (int) rr.refused ); return 1; }
        if ( rr.refused || rr.refuse_reason != SCHEMA_TABLE_REFUSE_OK || rr.widened != 0 ) { printf( "variable: counters at case %d x=%d rr=%d w=%d\n", k, (int) rr.refused, (int) rr.refuse_reason, rr.widened ); return 1; }
        if ( back.at != nanos[ k ] || back.tag != k + 1 ) { printf( "variable: value moved at case %d at=%lld tag=%lld\n", k, (long long) back.at, (long long) back.tag ); return 1; }

        if ( nanos[ k ] != 0 )
        {
            uint8_t pattern[8];
            const uint64_t bits = (uint64_t) nanos[ k ];
            int64_t found = -1, j;
            for ( i = 0; i < 8; ++i ) { pattern[ i ] = (uint8_t) ( bits >> ( 8 * i ) ); }
            for ( j = 1; j + 8 <= n; ++j )
                if ( memcmp( wire + j, pattern, 8 ) == 0 ) { if ( found != -1 ) { printf( "variable: the count is not unique\n" ); return 1; } found = j; }
            if ( found < 0 ) { printf( "variable: the count is not on the wire\n" ); return 1; }
            if ( wire[ found - 1 ] != 36 ) { printf( "variable: kind %u where 36 is the timestamp's\n", (unsigned) wire[ found - 1 ] ); return 1; }
        }
    }

    {
        const uint8_t payload[ 8 ] = { 0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70 };
        TableReport sr;
        TableReader r;
        memset( &sr, 0, sizeof( sr ) );
        r = table_reader_make( payload, (int64_t) sizeof( payload ), &sr );
        if ( !table_reader_skip( &r, 36 ) ) { printf( "variable: kind 36 is not skippable at eight bytes\n" ); return 1; }
        if ( r.offset != (int64_t) sizeof( payload ) ) { printf( "variable: kind 36 skipped %lld bytes, want 8\n", (long long) r.offset ); return 1; }
    }
`

const cTimestampFixedProbe = `
    static const int64_t nanos[] = { 0ll, 1740736445000000000ll, -1ll, (-9223372036854775807ll - 1), 9223372036854775807ll };
    static uint8_t file[ 512 ];
    int k;
    {
        TableFixedLayoutView view;
        int why = 0, saw_timestamp = 0, saw_tag = 0, i;
        const TableTypeInfo * info = root_table_type();
        uint64_t at_id = 0, tag_id = 0;
        if ( !table_fixed_parse_layout( root_fixed_known[ 0 ].layout, root_fixed_known[ 0 ].layout_bytes, &view, &why ) ) { printf( "fixed: the layout does not parse\n" ); return 1; }
        for ( i = 0; i < info->num_fields; ++i )
        {
            if ( strcmp( info->fields[ i ].name, "at" ) == 0 ) { at_id = info->fields[ i ].id; }
            if ( strcmp( info->fields[ i ].name, "tag" ) == 0 ) { tag_id = info->fields[ i ].id; }
        }
        for ( i = 0; i < view.count; ++i )
        {
            const TableFixedLayoutEntry e = table_fixed_entry_at( &view, i );
            if ( e.id == at_id ) { saw_timestamp = 1; if ( e.kind != 5 || e.size != 8u ) { printf( "fixed: the timestamp layout entry is kind %u size %u, want 5 / 8\n", (unsigned) e.kind, (unsigned) e.size ); return 1; } }
            if ( e.id == tag_id ) { saw_tag = 1; if ( e.kind != 5 || e.size != 8u ) { printf( "fixed: the int64 layout entry moved\n" ); return 1; } }
        }
        if ( !saw_timestamp || !saw_tag ) { printf( "fixed: the layout does not carry both fields\n" ); return 1; }
    }
    for ( k = 0; k < 5; ++k )
    {
        Root v, back;
        int64_t wrote;
        static TableFixedEntry plan[ 64 ];
        TableReport rr;
        root_reset( &v );
        v.at = nanos[ k ];
        v.tag = k + 1;
        wrote = root_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
        if ( wrote != root_fixed_measure( 1 ) ) { printf( "fixed: save %lld measure %lld\n", (long long) wrote, (long long) root_fixed_measure( 1 ) ); return 1; }
        root_reset( &back );
        memset( &rr, 0, sizeof( rr ) );
        if ( root_fixed_load( &back, 1, file, wrote, plan, 64, NULL, &rr ) != 1 ) { printf( "fixed: load refused at case %d reason=%d\n", k, (int) rr.reason ); return 1; }
        if ( rr.refused || rr.refuse_reason != SCHEMA_TABLE_REFUSE_OK || rr.widened != 0 ) { printf( "fixed: counters at case %d\n", k ); return 1; }
        if ( back.at != nanos[ k ] || back.tag != k + 1 ) { printf( "fixed: value moved at case %d\n", k ); return 1; }
    }
`

const cTimestampMessageProbe = `
    uint8_t ann[ 1 << 16 ];
    TableMessageEntry entries[ kTableMessageEntriesHere ];
    TableVocabulary vocabulary = table_vocabulary( entries, kTableMessageEntriesHere );
    TableReport ar;
    static const int64_t nanos[] = { 0ll, 1740736445000000000ll, -1ll, (-9223372036854775807ll - 1), 9223372036854775807ll };
    static uint8_t wire[ 512 ];
    int k;
    if ( table_message_value_bits( 36, 0, 0 ) != 64 ) { printf( "message: kind 36 is %lld bits, want 64\n", (long long) table_message_value_bits( 36, 0, 0 ) ); return 1; }
    if ( announce( ann, (int64_t) sizeof( ann ) ) != announce_measure() ) { printf( "message: the announcement did not write its measure\n" ); return 1; }
    memset( &ar, 0, sizeof( ar ) );
    if ( !announce_read( &vocabulary, ann, announce_measure(), &ar ) ) { printf( "message: the announcement was refused: reason=%d\n", (int) ar.reason ); return 1; }

    for ( k = 0; k < 5; ++k )
    {
        Root v, back[ 1 ];
        TableReport wr, rr;
        int64_t n, count = 1;
        root_reset( &v );
        v.at = nanos[ k ];
        v.tag = k + 1;
        memset( &wr, 0, sizeof( wr ) );
        n = root_save_messages( &v, 1, wire, (int64_t) sizeof( wire ), &wr );
        if ( n <= 0 ) { printf( "message: save failed at case %d reason=%d\n", k, (int) wr.reason ); return 1; }
        memset( &rr, 0, sizeof( rr ) );
        if ( !root_load_messages( back, &count, &vocabulary, wire, n, &rr ) || count != 1 ) { printf( "message: load failed at case %d count=%lld reason=%d\n", k, (long long) count, (int) rr.reason ); return 1; }
        if ( rr.refused || rr.refused ) { printf( "message: counters at case %d\n", k ); return 1; }
        if ( back[ 0 ].at != nanos[ k ] || back[ 0 ].tag != k + 1 ) { printf( "message: value moved at case %d at=%lld\n", k, (long long) back[ 0 ].at ); return 1; }
    }
`

func TestCTimestampBinaryForms(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	u := unitFrom(t, cTimestampSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	source := string(files["ProbeTable.c"]) + "\n" + string(files["ProbeTable.h"])
	for _, want := range []string{`"at", "at", "timestamp", 0x`, `if ( kind != 36 )`, `!( value->at == 0 )`} {
		if !strings.Contains(source, want) {
			t.Fatalf("the generated C does not carry %q — the descriptor or the wire kind is not the timestamp's", want)
		}
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{"variable", cTimestampVariableProbe},
		{"fixed", cTimestampFixedProbe},
		{"message", cTimestampMessageProbe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := cHeldRun(t, unitFrom(t, cTimestampSchema), tc.body)
			if err != nil {
				t.Fatalf("the %s probe: %v\n%s", tc.name, err, out)
			}
		})
	}
}

func TestCTimestampTextStrictRFC3339(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")

	schema := "package probe\nfixed table Root\n{\n    at timestamp\n}\n"
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

		{"offset hour 24", "2026-10-17T12:34:05+24:00", 0, false},
		{"offset minute 60", "2026-10-17T12:34:05+05:60", 0, false},
		{"comma fraction", "2026-10-17T12:34:05,5Z", 0, false},
		{"negative offset hour 24", "2026-10-17T12:34:05-24:00", 0, false},
		{"negative offset minute 60", "2026-10-17T12:34:05-05:60", 0, false},
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
		okInt := 0
		if r.ok {
			okInt = 1
		}
		fmt.Fprintf(&cases, "    { %q, %q, %s, %d, %q },\n", r.name, `{"at":`+fmt.Sprintf("%q", r.text)+`}`, nanos, okInt, canon)
	}

	probe := `
    struct Case { const char * name; const char * json; int64_t nanos; int ok; const char * canon; };
    static const struct Case cases[] = {
` + cases.String() + `    };
    static char buffer[ 4096 ];
    size_t i;
    for ( i = 0; i < sizeof( cases ) / sizeof( cases[0] ); i++ )
    {
        const struct Case * c = &cases[ i ];
        Root v, back;
        TableReport report = {0}, again = {0};
        int ok;
        int64_t need, n;
        root_reset( &v );
        ok = root_from_json( &v, c->json, (int64_t) strlen( c->json ), &report );
        if ( ok != c->ok )
        {
            printf( "%s: accepted=%d want %d malformed=%d\n", c->name, ok, c->ok, (int) report.refused );
            return 1;
        }
        if ( !c->ok )
        {
            if ( !report.refused ) { printf( "%s: refused without malformed\n", c->name ); return 1; }
            if ( v.at != 0 ) { printf( "%s: the refused spelling landed %lld\n", c->name, (long long) v.at ); return 1; }
            continue;
        }
        if ( report.refused ) { printf( "%s: accepted with malformed\n", c->name ); return 1; }
        if ( v.at != c->nanos ) { printf( "%s: landed %lld, want %lld\n", c->name, (long long) v.at, (long long) c->nanos ); return 1; }

        need = root_to_json_measure( &v );
        if ( need <= 0 ) { printf( "%s: measure %lld\n", c->name, (long long) need ); return 1; }
        n = root_to_json( &v, buffer, (int64_t) sizeof( buffer ) );
        if ( n != need ) { printf( "%s: write %lld measure %lld\n", c->name, (long long) n, (long long) need ); return 1; }
        root_reset( &back );
        if ( !root_from_json( &back, buffer, n, &again ) || again.refused || back.at != v.at )
        {
            printf( "%s: the written text does not read back: %s\n", c->name, buffer );
            return 1;
        }
        if ( c->canon[ 0 ] == 0 || strstr( buffer, c->canon ) == NULL )
        {
            printf( "%s: the written timestamp is not %s: %s\n", c->name, c->canon, buffer );
            return 1;
        }
    }
`
	if out, err := cHeldRun(t, unitFrom(t, schema), probe); err != nil {
		t.Fatalf("strict RFC3339 probe failed: %v\n%s", err, out)
	}
}

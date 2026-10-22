package cpptable

// TestCPPTimestampBinaryForms carries the `timestamp` kind (docs/SPEC-TABLES.md
// §2.11, §3 kind 36) on the C++ binary forms: the variable wire, the fixed
// form's eight-byte slot and layout entry, the message form's sixty-four raw
// bits, and the descriptor's kind.
//
// THE ORACLE IS GO. The nanosecond counts below are the ones
// internal/codegen/gotable/timestampstrict_test.go's TestGoTimestampStrictRFC3339
// pins for the same instants (its `epoch`, `leap day`, `pre-epoch`,
// `UnixNano minimum` and `UnixNano maximum` rows), and the kind-36 skip is the
// rule internal/tablewire/timestamp_skip_test.go
// TestTimestampKindIsSkippedAtEightBytes pins. Nothing here is a value this
// port computed for itself.
import (
	"strings"
	"testing"
)

const cppTimestampSchema = `package probe
fixed table Root
{
    at  timestamp
    tag int64
}
`

// cppTimestampVariableProbe drives the VARIABLE wire: the kind byte is 36, the
// payload is the int64 count's eight bytes, a kind-36 payload under the int64
// field is a kind mismatch and never a widening, and an unnamed kind-36 field
// skips cleanly at eight bytes (docs/SPEC-TABLES.md §2.11, §3, §4).
const cppTimestampVariableProbe = `
    // internal/codegen/gotable/timestampstrict_test.go, TestGoTimestampStrictRFC3339
    static const int64_t nanos[] = {
        0ll,                          // "epoch": 1970-01-01T00:00:00Z
        1740736445000000000ll,        // "leap day": 2024-02-29T12:34:05Z
        -1ll,                         // "pre-epoch": 1969-12-31T23:59:59.999999999Z
        (-9223372036854775807ll - 1), // "UnixNano minimum": 1677-09-21T00:12:43.145224192Z
        9223372036854775807ll         // "UnixNano maximum": 2262-04-11T23:47:16.854775807Z
    };
    static uint8_t wire[ 256 ];
    for ( int k = 0; k < 5; ++k )
    {
        Root v;
        RootReset( v );
        v.at = nanos[ k ];
        v.tag = k + 1;
        const int64_t measured = RootMeasure( v );
        TableReport wr;
        memset( &wr, 0, sizeof( wr ) );
        const int64_t n = RootSave( v, wire, (int64_t) sizeof( wire ) );
        if ( n <= 0 || n != measured ) { printf( "variable: save %lld measure %lld\n", (long long) n, (long long) measured ); return 1; }

        Root back;
        TableReport rr;
        memset( &rr, 0, sizeof( rr ) );
        if ( !RootLoad( back, wire, n, &rr ) ) { printf( "variable: load failed at case %d malformed=%d\n", k, (int) rr.malformed ); return 1; }
        if ( rr.malformed || rr.refused || rr.kind_mismatch != 0 || rr.widened != 0 ) { printf( "variable: counters at case %d m=%d x=%d km=%d w=%d\n", k, (int) rr.malformed, (int) rr.refused, rr.kind_mismatch, rr.widened ); return 1; }
        if ( back.at != nanos[ k ] || back.tag != k + 1 ) { printf( "variable: value moved at case %d at=%lld tag=%d\n", k, (long long) back.at, back.tag ); return 1; }

        // THE FIELD HEADER'S KIND IS 36 AND THE PAYLOAD IS THE EIGHT-BYTE
        // COUNT. Find the unique int64 payload, then read the kind byte in
        // front of it and compare the payload's own little-endian bytes.
        if ( nanos[ k ] != 0 ) // the zero time is the field's default and elides
        {
            uint8_t pattern[8];
            const uint64_t bits = (uint64_t) nanos[ k ];
            for ( int i = 0; i < 8; ++i ) { pattern[ i ] = (uint8_t) ( bits >> ( 8 * i ) ); }
            int64_t found = -1;
            for ( int64_t i = 1; i + 8 <= n; ++i )
                if ( memcmp( wire + i, pattern, 8 ) == 0 ) { if ( found != -1 ) { printf( "variable: the count is not unique\n" ); return 1; } found = i; }
            if ( found < 0 ) { printf( "variable: the count is not on the wire\n" ); return 1; }
            if ( wire[ found - 1 ] != 36 ) { printf( "variable: kind %u where 36 is the timestamp's\n", (unsigned) wire[ found - 1 ] ); return 1; }
        }
    }

    // AN UNNAMED KIND-36 FIELD SKIPS AT ITS EIGHT BYTES, never as damage
    // (internal/tablewire/timestamp_skip_test.go,
    // TestTimestampKindIsSkippedAtEightBytes).
    {
        const uint8_t payload[ 8 ] = { 0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70 };
        TableIdTable ids;
        TableReport sr;
        memset( &sr, 0, sizeof( sr ) );
        TableReader r( payload, (int64_t) sizeof( payload ), &sr, &ids );
        if ( !r.skip( 36 ) ) { printf( "variable: kind 36 is not skippable at eight bytes\n" ); return 1; }
        if ( r.offset != (int64_t) sizeof( payload ) ) { printf( "variable: kind 36 skipped %lld bytes, want 8\n", (long long) r.offset ); return 1; }
    }

    // A KINDS-MISMATCHED TIMESTAMP IS SKIPPED AND COUNTED, AND NEITHER
    // SPELLING WIDENS INTO THE OTHER (§2.11, §4). Retype the timestamp's own
    // payload as i64, and the int64 field's payload as timestamp.
    {
        Root v;
        RootReset( v );
        v.at = 1234567890123ll;
        v.tag = 0x11223344;
        uint8_t bytes[ 256 ];
        const int64_t n = RootSave( v, bytes, (int64_t) sizeof( bytes ) );
        if ( n <= 0 ) { printf( "mismatch: save failed\n" ); return 1; }

        uint8_t at_pattern[ 8 ], tag_pattern[ 8 ];
        const uint64_t a = (uint64_t) v.at, g = (uint64_t) v.tag;
        for ( int i = 0; i < 8; ++i ) { at_pattern[ i ] = (uint8_t) ( a >> ( 8 * i ) ); tag_pattern[ i ] = (uint8_t) ( g >> ( 8 * i ) ); }
        int64_t at_at = -1, tag_at = -1;
        for ( int64_t i = 1; i + 8 <= n; ++i )
            if ( memcmp( bytes + i, at_pattern, 8 ) == 0 ) { at_at = i; break; }
        if ( at_at >= 1 )
            for ( int64_t i = at_at + 8; i + 8 <= n; ++i )
                if ( memcmp( bytes + i, tag_pattern, 8 ) == 0 ) { tag_at = i; break; }
        if ( at_at < 1 || tag_at < 1 ) { printf( "mismatch: a payload is not on the wire at=%lld tag=%lld\n", (long long) at_at, (long long) tag_at ); return 1; }
        if ( bytes[ at_at - 1 ] != 36 || bytes[ tag_at - 1 ] != 5 ) { printf( "mismatch: kinds %u and %u before the payloads at=%lld tag=%lld\n", (unsigned) bytes[ at_at - 1 ], (unsigned) bytes[ tag_at - 1 ], (long long) at_at, (long long) tag_at ); return 1; }
        bytes[ at_at - 1 ] = 5;   // the timestamp read as i64
        bytes[ tag_at - 1 ] = 36; // the int64 read as a timestamp
        Root back;
        TableReport rr;
        memset( &rr, 0, sizeof( rr ) );
        if ( !RootLoadTolerant( back, bytes, n, &rr ) ) { printf( "mismatch: load stopped\n" ); return 1; }
        if ( rr.kind_mismatch != 2 || rr.widened != 0 ) { printf( "mismatch: kind_mismatch=%d widened=%d, want 2 / 0\n", rr.kind_mismatch, rr.widened ); return 1; }
        if ( back.at != 0 || back.tag != 0 ) { printf( "mismatch: a skipped field landed a value at=%lld tag=%d\n", (long long) back.at, back.tag ); return 1; }
    }
`

// cppTimestampFixedProbe drives the FIXED form: the slot is the int64's eight
// bytes, the layout entry names kind 5 (the fixed layout has no member for a
// timestamp, docs/SPEC-TABLES.md §2.11, §3.4), and every count round-trips.
const cppTimestampFixedProbe = `
    static const int64_t nanos[] = {
        0ll, 1740736445000000000ll, -1ll, (-9223372036854775807ll - 1), 9223372036854775807ll
    };
    static uint8_t file[ 256 ];
    TableFixedLayoutView view;
    TableMessageReason why;
    if ( !TableFixedParseLayout( RootFixedKnown[ 0 ].layout, (int64_t) RootFixedKnown[ 0 ].layout_bytes, view, why ) ) { printf( "fixed: the layout does not parse\n" ); return 1; }
    bool saw_timestamp = false, saw_tag = false;
    const TableTypeInfo * info = RootTableType();
    uint64_t at_id = 0, tag_id = 0;
    for ( int i = 0; i < info->num_fields; ++i )
    {
        if ( strcmp( info->fields[ i ].name, "at" ) == 0 ) { at_id = info->fields[ i ].id; }
        if ( strcmp( info->fields[ i ].name, "tag" ) == 0 ) { tag_id = info->fields[ i ].id; }
    }
    for ( int i = 0; i < view.count; ++i )
    {
        const TableFixedLayoutEntry e = TableFixedEntryAt( view, i );
        if ( e.id == at_id ) { saw_timestamp = true; if ( e.kind != 5 || e.size != 8u ) { printf( "fixed: the timestamp layout entry is kind %u size %u, want 5 / 8\n", (unsigned) e.kind, (unsigned) e.size ); return 1; } }
        if ( e.id == tag_id ) { saw_tag = true; if ( e.kind != 5 || e.size != 8u ) { printf( "fixed: the int64 layout entry moved\n" ); return 1; } }
    }
    if ( !saw_timestamp || !saw_tag ) { printf( "fixed: the layout does not carry both fields\n" ); return 1; }
    for ( int k = 0; k < 5; ++k )
    {
        Root v;
        RootReset( v );
        v.at = nanos[ k ];
        v.tag = k + 1;
        const int64_t wrote = RootFixedSave( &v, 1, file, (int64_t) sizeof( file ) );
        if ( wrote != RootFixedMeasure( 1 ) ) { printf( "fixed: save %lld measure %lld\n", (long long) wrote, (long long) RootFixedMeasure( 1 ) ); return 1; }
        Root back;
        RootReset( back );
        static TableFixedEntry plan[ 64 ];
        TableReport rr;
        memset( &rr, 0, sizeof( rr ) );
        if ( RootFixedLoad( &back, 1, file, wrote, plan, 64, NULL, &rr ) != 1 ) { printf( "fixed: load refused at case %d reason=%d\n", k, (int) rr.reason ); return 1; }
        if ( rr.malformed || rr.refused || rr.kind_mismatch != 0 || rr.widened != 0 ) { printf( "fixed: counters at case %d\n", k ); return 1; }
        if ( back.at != nanos[ k ] || back.tag != k + 1 ) { printf( "fixed: value moved at case %d\n", k ); return 1; }
    }
`

// cppTimestampMessageProbe drives the MESSAGE form: kind 36 is a sixty-four
// bit raw payload with no shape bytes (docs/SPEC-TABLES.md §3.3's entry shape
// table), so the announced collection names it and a batch round-trips.
const cppTimestampMessageProbe = `
    if ( TableMessageValueBits( 36, 0, 0 ) != 64 ) { printf( "message: kind 36 is %lld bits, want 64\n", (long long) TableMessageValueBits( 36, 0, 0 ) ); return 1; }
    uint8_t ann[ 1 << 16 ];
    if ( Announce( ann, (int64_t) sizeof( ann ) ) != AnnounceMeasure() ) { printf( "message: the announcement did not write its measure\n" ); return 1; }
    TableMessageEntry entries[ kTableMessageEntriesHere ];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    TableReport ar;
    memset( &ar, 0, sizeof( ar ) );
    if ( !AnnounceRead( vocabulary, ann, AnnounceMeasure(), &ar ) ) { printf( "message: the announcement was refused: reason=%d\n", (int) ar.reason ); return 1; }

    static const int64_t nanos[] = { 0ll, 1740736445000000000ll, -1ll, (-9223372036854775807ll - 1), 9223372036854775807ll };
    static uint8_t wire[ 512 ];
    for ( int k = 0; k < 5; ++k )
    {
        Root v;
        RootReset( v );
        v.at = nanos[ k ];
        v.tag = k + 1;
        TableReport wr;
        memset( &wr, 0, sizeof( wr ) );
        const int64_t n = RootSaveMessages( &v, 1, wire, (int64_t) sizeof( wire ), &wr );
        if ( n <= 0 ) { printf( "message: save failed at case %d reason=%d\n", k, (int) wr.reason ); return 1; }
        Root back[ 1 ];
        int64_t count = 1;
        TableReport rr;
        memset( &rr, 0, sizeof( rr ) );
        if ( !RootLoadMessages( back, &count, vocabulary, wire, n, &rr ) || count != 1 ) { printf( "message: load failed at case %d count=%lld reason=%d\n", k, (long long) count, (int) rr.reason ); return 1; }
        if ( rr.malformed || rr.refused ) { printf( "message: counters at case %d\n", k ); return 1; }
        if ( back[ 0 ].at != nanos[ k ] || back[ 0 ].tag != k + 1 ) { printf( "message: value moved at case %d at=%lld\n", k, (long long) back[ 0 ].at ); return 1; }
    }
`

func TestCPPTimestampBinaryForms(t *testing.T) {
	t.Parallel()
	u := cppUnitFrom(t, cppTimestampSchema)

	// THE DESCRIPTOR STATES THE WIRE'S KIND: 36, and the type name the
	// declaration spells (docs/SPEC-TABLES.md §8.1).
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["ProbeTable.h"])
	for _, want := range []string{`"at", "at", "timestamp", 0x`, `if ( kind != 36 )`, `value.at != 0`} {
		if !strings.Contains(header, want) {
			t.Fatalf("the generated header does not carry %q — the descriptor or the wire kind is not the timestamp's", want)
		}
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"variable", cppTimestampVariableProbe},
		{"fixed", cppTimestampFixedProbe},
		{"message", cppTimestampMessageProbe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := cppHeldRun(t, cppUnitFrom(t, cppTimestampSchema), tc.body)
			if err != nil {
				t.Fatalf("the %s probe: %v\n%s", tc.name, err, out)
			}
		})
	}
}

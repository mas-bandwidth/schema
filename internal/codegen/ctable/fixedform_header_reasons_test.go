package ctable

// TestCFixedHeaderRefusalReasons holds the C fixed-form load's two header
// refusals to their names (docs/FIXED-FORM-ALGORITHM.md §5.3 step 2,
// docs/SPEC-TABLES.md §3.4): a form-3 file shorter than the twenty-byte header
// is truncated, and a nonzero reserved byte is reserved_not_zero. The C++
// reference and the Go oracle carry both names on the report; this drives the
// GENERATED C reader over a ten-byte form-3 file and a twenty-byte form-3 file
// with reserved byte 3 set, so the emitted path is what answers. Each refusal
// is held to the WHOLE report, not just the name: the named reason rides beside
// wire_damaged and no other field moves.

import (
	"strings"
	"testing"
)

const cFixedHeaderReasonsSrc = `package hdr

fixed table HeaderReport
{
    value uint32 = 1
}
`

func TestCFixedHeaderRefusalReasons(t *testing.T) {
	t.Parallel()
	body := `
    {
        uint8_t file[64];
        @ROOT@ back;
        TableReport r, want;
        static TableFixedEntry plan[4096];
        int64_t n;
        int bad = 0;

        /* A 10-byte form-3 file: shorter than the twenty-byte header. */
        memset( file, 0, sizeof( file ) );
        file[0] = kTableFixedForm;
        memset( &back, 0, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, file, 10, plan, 4096, NULL, &r );
        memset( &want, 0, sizeof( want ) );
        want.refused = 1;
        want.reason = SCHEMA_TABLE_REFUSE_TRUNCATED;
        want.refuse_reason = SCHEMA_TABLE_REFUSE_WIRE_DAMAGED;
        if ( n != -1 || memcmp( &r, &want, sizeof( want ) ) != 0 )
        {
            printf( "truncated: n=%lld refused=%d reason=%d refuse_reason=%d, want -1 and exactly refused+truncated+wire_damaged\n",
                    (long long) n, r.refused, r.reason, r.refuse_reason );
            bad = 1;
        }

        /* A 20-byte form-3 file whose reserved byte 3 is nonzero. */
        memset( file, 0, sizeof( file ) );
        file[0] = kTableFixedForm;
        file[3] = 1;
        memset( &back, 0, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, file, 20, plan, 4096, NULL, &r );
        memset( &want, 0, sizeof( want ) );
        want.refused = 1;
        want.reason = SCHEMA_TABLE_REFUSE_RESERVED_NOT_ZERO;
        want.refuse_reason = SCHEMA_TABLE_REFUSE_WIRE_DAMAGED;
        if ( n != -1 || memcmp( &r, &want, sizeof( want ) ) != 0 )
        {
            printf( "reserved: n=%lld refused=%d reason=%d refuse_reason=%d, want -1 and exactly refused+reserved_not_zero+wire_damaged\n",
                    (long long) n, r.refused, r.reason, r.refuse_reason );
            bad = 1;
        }

        if ( bad ) { return 1; }
        printf( "fixed header refusals named\n" );
        return 0;
    }
`
	out, err := cT11Run(t, cFixedHeaderReasonsSrc, body)
	if err != nil {
		t.Fatalf("fixed header refusal reasons: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "fixed header refusals named") {
		t.Fatalf("the probe did not name both header refusals:\n%s", out)
	}
}

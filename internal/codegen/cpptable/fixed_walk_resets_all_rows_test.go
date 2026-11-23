package cpptable

// TestCppFixedLoadWalkResetsEveryRowOnRefusal pins ruling R4 on the public inline
// <T>FixedLoadWalk: called DIRECTLY, a refusal leaves every row handed in, all
// `capacity` of them, at its declared default. The old walk returned -1 and left
// whatever the caller had in the rows, so rows 0..k (or all of them, for a
// refusal before the loop) kept their values.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestCppFixedLoadWalkResetsEveryRowOnRefusal(t *testing.T) {
	slowtest.Gate(t, "the c++ compiler (the leg's own toolchain)")
	t.Parallel()

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema")
	noRecord, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "fixed-empty-batch", "no-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	body := `
    static const uint8_t none[] = { ` + cppByteList(noRecord) + ` };
    static TableFixedEntry plan[4096];
    TableReport r;
    Batch rows[4];
    for ( int i = 0; i < 4; ++i ) { BatchReset( rows[i] ); rows[i].a = 99; rows[i].name_length = 3; memcpy( rows[i].name, "xyz", 3 ); }
    memset( &r, 0, sizeof( r ) );
    // a file with no record is refused; the caller's four rows must all be back at defaults
    int64_t n = BatchFixedLoadWalk( rows, 4, none, (int64_t) sizeof( none ), plan, 4096, NULL, &r );
    if ( n != -1 ) { printf( "walk: no-record file read as n=%lld\n", (long long) n ); return 1; }
    for ( int i = 0; i < 4; ++i )
    {
        if ( rows[i].a != 0 || rows[i].name_length != 0 )
            { printf( "walk: row %d kept a=%d length=%d after a refusal\n", i, (int) rows[i].a, (int) rows[i].name_length ); return 1; }
    }
`
	out, err := cppRunUnit(t, schema, "fixedemptybatch", body, nil)
	if err != nil {
		t.Fatalf("FixedLoadWalk left a partial value after a refusal: %v\n%s", err, out)
	}
}

package cpptable

// TestCppFixedBatchHoldsARecord pins docs/rulings.json fixed-form-edges on the
// C++ leg: a fixed-form file holds at least one record. The vector under
// testdata/wire/fixed-empty-batch carries one file with one record and the same
// header, layout and integrity trailer with the record removed.
//
// EVERY FIXED READ ENTRY refuses the file with no record — FixedLoadWalk and
// the strict FixedLoad beside it — whatever the caller's capacity, and a
// one-record file still reads whole through both. A save of zero records is
// refused by the leg's own write contract, a debug assert (docs/rulings.json
// write-refusal): the probe is a debug build, so the assert is live; a release
// build carries no check and is not tested. The verdict is the contract; this
// test compares no reason name.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestCppFixedBatchHoldsARecord(t *testing.T) {
	slowtest.Gate(t, "the c++ compiler (the leg's own toolchain)")
	t.Parallel()

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema")
	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")
	oneRecord, err := os.ReadFile(filepath.Join(vector, "one-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	noRecord, err := os.ReadFile(filepath.Join(vector, "no-record.bin"))
	if err != nil {
		t.Fatal(err)
	}

	load := fmt.Sprintf(`
    static const uint8_t one[] = { %s };
    static const uint8_t none[] = { %s };
    static TableFixedEntry plan[4096];
    TableReport r;
    Batch back;

    // A FILE WITH NO RECORD IS REFUSED BY EVERY FIXED READ ENTRY, whatever the
    // caller's capacity: a fixed-form file holds at least one record.
    memset( &r, 0, sizeof( r ) );
    BatchReset( back );
    int64_t n = BatchFixedLoadWalk( &back, 8, none, (int64_t) sizeof( none ), plan, 4096, NULL, &r );
    if ( n != -1 ) { printf( "batch: FixedLoadWalk read a file with no record as n=%%lld\n", (long long) n ); return 1; }

    memset( &r, 0, sizeof( r ) );
    BatchReset( back );
    n = BatchFixedLoad( &back, 8, none, (int64_t) sizeof( none ), plan, 4096, NULL, &r );
    if ( n != -1 ) { printf( "batch: FixedLoad read a file with no record as n=%%lld\n", (long long) n ); return 1; }

    // A ONE-RECORD FILE STILL READS, through both entries.
    memset( &r, 0, sizeof( r ) );
    BatchReset( back );
    n = BatchFixedLoadWalk( &back, 8, one, (int64_t) sizeof( one ), plan, 4096, NULL, &r );
    if ( n != 1 || back.a != 7 || back.name_length != 5 || memcmp( back.name, "hello", 5 ) != 0 )
        { printf( "batch: FixedLoadWalk did not read the one-record file: n=%%lld a=%%d length=%%d\n", (long long) n, (int) back.a, (int) back.name_length ); return 1; }

    memset( &r, 0, sizeof( r ) );
    BatchReset( back );
    n = BatchFixedLoad( &back, 8, one, (int64_t) sizeof( one ), plan, 4096, NULL, &r );
    if ( n != 1 || back.a != 7 || back.name_length != 5 || memcmp( back.name, "hello", 5 ) != 0 )
        { printf( "batch: FixedLoad did not read the one-record file: n=%%lld a=%%d length=%%d\n", (long long) n, (int) back.a, (int) back.name_length ); return 1; }
`, cppByteList(oneRecord), cppByteList(noRecord))

	out, err := cppRunUnit(t, schema, "fixedemptybatch", load, nil)
	if err != nil {
		t.Fatalf("the fixed read entries: %v\n%s", err, out)
	}

	// A SAVE OF ZERO RECORDS IS REFUSED: the debug assert is the leg's write
	// contract, and the process aborts on it. A save that returns normally —
	// accepted, or refused some other way — is not the debug assert and fails.
	save := `
    static uint8_t empty[4096];
    Batch one_value;
    BatchReset( one_value );
    const int64_t n = BatchFixedSave( &one_value, 0, empty, (int64_t) sizeof( empty ) );
    printf( "batch: a save of zero records returned %lld, not the debug assert\n", (long long) n );
    return 0;
`
	out, err = cppRunUnit(t, schema, "fixedemptybatch", save, nil)
	if err == nil {
		t.Fatalf("a save of zero records was accepted\n%s", out)
	}
	if strings.Contains(err.Error(), "did not compile") {
		t.Fatalf("the save probe did not compile: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "count > 0") {
		t.Fatalf("a save of zero records did not trip the debug assert: %v\n%s", err, out)
	}
}

// cppByteList is a C++ initializer list for a byte slice.
func cppByteList(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprintf("0x%02x", b)
	}
	return strings.Join(parts, ", ")
}

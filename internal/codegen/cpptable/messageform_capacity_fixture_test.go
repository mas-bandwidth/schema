package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestCppMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of
// tables/backend, client_build 40, written by the Go engine and pinned to it
// by test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated C++'s LoginRequestLoadMessages. Storage for one body
// whose client_build is the sentinel 17 owes "A refused batch returns a count
// of `0`" (docs/SPEC-TABLES.md §3.3) and ruling R4: a refusal, count 0, and
// client_build at its declared default. Storage for THREE values, all at the
// sentinel, owes the same on a framing refusal (the form byte changed) and on
// a refusal after the bodies (the last byte dropped): every value handed in
// at its declared default, slot 2 included though it lies past the batch's
// count of 2. Storage for two owes 2 and 40. A caller's capacity is a
// resource limit (§6.5), so this row runs here, in the leg's own tests, never
// in the shared suites.
func TestCppMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadGraphDirUnit(t, filepath.Join("..", "..", "..", "tables", "backend"))
	out, err := cppHeldRun(t, u, fmt.Sprintf(cppCapacityFixtureProbe, cppBytes(fixture)))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

const cppCapacityFixtureProbe = `
    const uint8_t fixture[] = { %s };
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    TableReport ar;
    if ( !AnnounceRead( vocabulary, kTableAnnounce, kTableAnnounceBytes, &ar ) ) { printf( "announce refused\n" ); return 1; }
    LoginRequest defaults; LoginRequestReset( defaults );
    if ( defaults.client_build == 17 ) { printf( "the sentinel is the declared default\n" ); return 1; }
    LoginRequest one[1]; LoginRequestReset( one[0] ); one[0].client_build = 17;
    int64_t count = 1; TableReport r1;
    if ( LoginRequestLoadMessages( one, &count, vocabulary, fixture, (int64_t) sizeof( fixture ), &r1 ) || !r1.refused || count != 0 )
        { printf( "two bodies into storage for one: count=%%lld, want a refusal with count 0\n", (long long) count ); return 1; }
    if ( one[0].client_build != defaults.client_build )
        { printf( "batch_too_large: client_build %%u, not its declared default\n", (unsigned) one[0].client_build ); return 1; }
    LoginRequest three[3];
    std::vector<uint8_t> framing( fixture, fixture + sizeof( fixture ) ); framing[0] = 3;
    for ( int i = 0; i < 3; i++ ) { LoginRequestReset( three[i] ); three[i].client_build = 17; }
    count = 3; TableReport r2;
    if ( LoginRequestLoadMessages( three, &count, vocabulary, framing.data(), (int64_t) framing.size(), &r2 ) || count != 0 || !( r2.refused || r2.damaged ) )
        { printf( "framing: the changed form byte did not refuse with count 0\n" ); return 1; }
    for ( int i = 0; i < 3; i++ ) if ( three[i].client_build != defaults.client_build )
        { printf( "framing refusal: value %%d kept client_build %%u, not its declared default\n", i, (unsigned) three[i].client_build ); return 1; }
    for ( int i = 0; i < 3; i++ ) { LoginRequestReset( three[i] ); three[i].client_build = 17; }
    count = 3; TableReport r3;
    if ( LoginRequestLoadMessages( three, &count, vocabulary, fixture, (int64_t) sizeof( fixture ) - 1, &r3 ) || count != 0 || !( r3.refused || r3.damaged ) )
        { printf( "short batch: the dropped last byte did not refuse with count 0\n" ); return 1; }
    for ( int i = 0; i < 3; i++ ) if ( three[i].client_build != defaults.client_build )
        { printf( "short batch refusal: value %%d kept client_build %%u, not its declared default\n", i, (unsigned) three[i].client_build ); return 1; }
    LoginRequest two[2]; LoginRequestReset( two[0] ); LoginRequestReset( two[1] );
    count = 2; TableReport r4;
    if ( !LoginRequestLoadMessages( two, &count, vocabulary, fixture, (int64_t) sizeof( fixture ), &r4 ) || count != 2 || two[0].client_build != 40 || two[1].client_build != 40 )
        { printf( "two bodies into storage for two: count=%%lld\n", (long long) count ); return 1; }
`

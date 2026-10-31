package cpptable

// TestMessageFormHeldCPP proves, one subtest per clause and per manifest row,
// the MESSAGE FORM (form byte 2, docs/SPEC-TABLES.md §3.3) on the generated
// C++ surface, to the same bar the Go leg's cards schema-go-msg-refusals and
// schema-go-msg-goldens set. Each subtest generates the C++ for a unit,
// compiles a probe against it, and runs the probe, which asserts the clause or
// the row on the generated verbs and reports the red line by name.
//
// The clauses are the page's own: TableMessageReason::no_vocabulary, TableMessageReason::second_announcement and its
// terminality, TableMessageReason::vocabulary_too_large on both bounds, the two strict announcement
// checks, TableMessageReason::batch_too_large on both sides, damage anywhere delivering no body,
// bytes after the pad, and TableMessageReason::newer_form with TableMessageReason::message_form_as_file.
//
// The rows are the manifest's own, read here rather than copied: every
// `connection` row's announcement is byte-identical to the reference file and
// every `message` row round-trips — loading either form and saving the other
// reproduces the other's pinned bytes (§3.3).

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppHeldRun generates the leg's C++ for the unit, compiles a probe against
// every generated Table header, links the generated translation units beside
// it, and runs `body` (C++ that returns nonzero and prints its red line on a
// failed assertion). The probe reads golden files by path, so the manifest
// rows pass those paths through; the include roots a generated header can
// reach — the repo's native-type headers and the sibling serialize runtime —
// are on the compile line so a unit that names either still builds.
func cppHeldRun(t *testing.T, u *ir.Unit, body string) ([]byte, error) {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	outDir := t.TempDir()
	var headers []string
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			headers = append(headers, name)
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, filepath.Join(outDir, name))
		}
	}
	if len(headers) == 0 {
		t.Fatal("no Table.h was generated")
	}
	sort.Strings(headers)
	sort.Strings(sources)

	// Every generated Table.h is included, not just one: a multi-file unit
	// (the pointer corpus's graphdemo) declares its roots across files, and a
	// probe that names one root needs that file's header. The runtimes are
	// per-package guarded, so the whole set is safe in one translation unit.
	var includes strings.Builder
	for _, h := range headers {
		fmt.Fprintf(&includes, "#include \"%s\"\n", h)
	}

	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>
%s
using namespace %s;

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    fseek( f, 0, SEEK_END );
    const long n = ftell( f );
    fseek( f, 0, SEEK_SET );
    out.resize( (size_t) n );
    if ( n > 0 && fread( out.data(), 1, (size_t) n, f ) != (size_t) n ) { fclose( f ); return false; }
    fclose( f );
    return true;
}

int main( void )
{
%s
    return 0;
}
`, includes.String(), u.Package, body)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	// The two include roots a generated header can reach beyond outDir: the
	// repo's own native-type headers (test/tables, a `cpp_include` like the
	// pointer corpus's graph_colour.h, docs/SPEC-TABLES.md §4.2) and the
	// documented sibling serialize runtime (../serialize, the 128-bit storage
	// types a Table header names only when its closure declares one,
	// docs/SPEC-TABLES.md §3). Supplying both lets a manifest unit that names
	// either still compile, and the generated variable-unit sources below link
	// beside the probe.
	native, err := filepath.Abs("../../../test/tables")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + native, "-I" + serialize, filepath.Join(outDir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(outDir, "probe"))
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

// cppHeldProbe is cppHeldRun over a single-file unit.
func cppHeldProbe(t *testing.T, unitPath, body string) ([]byte, error) {
	t.Helper()
	return cppHeldRun(t, loadUnit(t, unitPath), body)
}

// "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
// decoded, no counter moves and `malformed` does not fire." (docs/SPEC-TABLES.md §3.3).
const cppHeldNoVocabulary = `
    LoginRequest v; LoginRequestReset( v );
    uint8_t buf[512];
    TableReport save; memset( &save, 0, sizeof( save ) );
    int64_t n = LoginRequestSaveMessages( &v, 1, buf, (int64_t) sizeof( buf ), &save );
    if ( n < 1 ) { printf( "TableMessageReason::no_vocabulary: the save failed: n=%lld\n", (long long) n ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    LoginRequest out[1];
    int64_t count = 1;
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( LoginRequestLoadMessages( out, &count, vocabulary, buf, n, &r ) )
        { printf( "TableMessageReason::no_vocabulary: a body from an endpoint that never announced was not refused\n" ); return 1; }
    if ( !r.refused || r.reason != TableMessageReason::no_vocabulary || r.malformed )
        { printf( "TableMessageReason::no_vocabulary: refused=%d reason=%d malformed=%d\n", (int) r.refused, (int) r.reason, (int) r.malformed ); return 1; }
    if ( count != 0 ) { printf( "TableMessageReason::no_vocabulary moved the returned count: %lld\n", (long long) count ); return 1; }
`

// "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does not
// replace or amend anything." and "REFUSAL IS TERMINAL, and a refused FIRST
// announcement is the one the connection gets ... every announcement after it
// is refused as `TableMessageReason::second_announcement` ... every body after it is refused for
// want of one." (docs/SPEC-TABLES.md §3.3).
const cppHeldSecondAnnouncement = `
    uint8_t ann[1 << 16];
    if ( Announce( ann, (int64_t) sizeof( ann ) ) != AnnounceMeasure() )
        { printf( "TableMessageReason::second_announcement: the announcement did not write its own measure\n" ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v( entries, kTableMessageEntriesHere );
    TableReport first; memset( &first, 0, sizeof( first ) );
    if ( !AnnounceRead( v, ann, AnnounceMeasure(), &first ) || v.count != kTableMessageEntriesHere )
        { printf( "TableMessageReason::second_announcement: the first announcement was not accepted: refused=%d reason=%d\n", (int) first.refused, (int) first.reason ); return 1; }
    const int64_t before = v.count;
    TableReport second; memset( &second, 0, sizeof( second ) );
    if ( AnnounceRead( v, ann, AnnounceMeasure(), &second ) || !second.refused || second.reason != TableMessageReason::second_announcement || second.malformed )
        { printf( "TableMessageReason::second_announcement: a second announcement was not refused by name: refused=%d reason=%d malformed=%d\n", (int) second.refused, (int) second.reason, (int) second.malformed ); return 1; }
    if ( v.count != before ) { printf( "TableMessageReason::second_announcement: a second announcement replaced the vocabulary\n" ); return 1; }
    TableMessageEntry wentries[kTableMessageEntriesHere];
    TableVocabulary w( wentries, kTableMessageEntriesHere );
    const uint8_t junk[3] = { 9, 9, 9 };
    TableReport rejected; memset( &rejected, 0, sizeof( rejected ) );
    if ( AnnounceRead( w, junk, 3, &rejected ) || !rejected.refused || rejected.reason != TableMessageReason::newer_form || w.announced )
        { printf( "TableMessageReason::second_announcement: a refused first announcement was not refused by name: refused=%d reason=%d announced=%d\n", (int) rejected.refused, (int) rejected.reason, (int) w.announced ); return 1; }
    TableReport later; memset( &later, 0, sizeof( later ) );
    if ( AnnounceRead( w, ann, AnnounceMeasure(), &later ) || later.reason != TableMessageReason::second_announcement || w.announced )
        { printf( "TableMessageReason::second_announcement: an announcement after a refused first is not TableMessageReason::second_announcement: reason=%d announced=%d\n", (int) later.reason, (int) w.announced ); return 1; }
    LoginRequest root; LoginRequestReset( root );
    uint8_t buf[512];
    TableReport wrote; memset( &wrote, 0, sizeof( wrote ) );
    int64_t n = LoginRequestSaveMessages( &root, 1, buf, (int64_t) sizeof( buf ), &wrote );
    if ( n < 1 ) { printf( "TableMessageReason::second_announcement: the save failed\n" ); return 1; }
    LoginRequest storage[1];
    int64_t count = 1;
    TableReport body; memset( &body, 0, sizeof( body ) );
    if ( LoginRequestLoadMessages( storage, &count, w, buf, n, &body ) || count != 0 || body.reason != TableMessageReason::no_vocabulary )
        { printf( "TableMessageReason::second_announcement: a batch on the refused connection: refused=%d count=%lld reason=%d\n", (int) body.refused, (long long) count, (int) body.reason ); return 1; }
`

// "`TableMessageReason::vocabulary_too_large` covers both bounds, the entry count and the
// vocabulary's bytes." (docs/SPEC-TABLES.md §3.3).
const cppHeldVocabularyTooLarge = `
    uint8_t ann[1 << 16];
    Announce( ann, (int64_t) sizeof( ann ) );
    TableMessageEntry one[1];
    TableVocabulary entries( one, 1 );
    TableReport entryReport; memset( &entryReport, 0, sizeof( entryReport ) );
    if ( AnnounceRead( entries, ann, AnnounceMeasure(), &entryReport ) || !entryReport.refused || entryReport.reason != TableMessageReason::vocabulary_too_large || entryReport.malformed || entries.announced )
        { printf( "TableMessageReason::vocabulary_too_large entry bound: refused=%d reason=%d malformed=%d announced=%d\n", (int) entryReport.refused, (int) entryReport.reason, (int) entryReport.malformed, (int) entries.announced ); return 1; }
    TableMessageEntry all[kTableMessageEntriesHere];
    TableVocabulary bytes( all, kTableMessageEntriesHere );
    bytes.max_bytes = 4;
    TableReport byteReport; memset( &byteReport, 0, sizeof( byteReport ) );
    if ( AnnounceRead( bytes, ann, AnnounceMeasure(), &byteReport ) || !byteReport.refused || byteReport.reason != TableMessageReason::vocabulary_too_large || byteReport.malformed || bytes.announced )
        { printf( "TableMessageReason::vocabulary_too_large byte bound: refused=%d reason=%d malformed=%d announced=%d\n", (int) byteReport.refused, (int) byteReport.reason, (int) byteReport.malformed, (int) bytes.announced ); return 1; }
`

// "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS: the build
// version present, exactly once, kind `9`, eight bytes wide, and the vocabulary
// present, exactly once, kind `14`, element kind `6` ... A FAILED STRICT CHECK
// IS MALFORMED, not a refusal". (docs/SPEC-TABLES.md §3.3).
const cppHeldStrictChecks = `
    // Assemble a valid announcement around the given field bytes — form byte,
    // fields, the ref-0 terminator, and the two-entry id table (build version
    // at ref 1, vocabulary at ref 2, count 2) — the Go leg's announcement().
    auto heldAnnounce = []( const std::vector<uint8_t> & fields )
    {
        std::vector<uint8_t> a;
        a.push_back( 1 );
        a.insert( a.end(), fields.begin(), fields.end() );
        a.push_back( 0x00 );
        const uint64_t tail[3] = { 0xFFFFFFFFFFFFFFFEull, 0xFFFFFFFFFFFFFFFDull, 2ull };
        for ( int t = 0; t < 3; ++t )
            for ( int i = 0; i < 8; ++i )
                a.push_back( (uint8_t)( tail[t] >> ( 8 * i ) ) );
        return a;
    };
    // The build version field (ref 1, kind 9, eight bytes) and the vocabulary
    // field (ref 2, kind 14, an empty element-kind-6 run): the Go leg's
    // versionField and vocabField(6, nil). Each build-version row plants one
    // of the strict check's four facts — absent, twice, a kind other than 9,
    // and fewer than eight bytes — and the reader must answer malformed, not a
    // refusal, and set no vocabulary.
    const uint8_t heldVersion[10] = { 1, 9, 8, 7, 6, 5, 4, 3, 2, 1 };
    const uint8_t heldVocab[5] = { 2, 14, 2, 6, 0 };
    const char * buildRowNames[4] = { "absent", "twice", "not kind 9", "fewer than eight bytes" };
    std::vector<uint8_t> buildRows[4];
    buildRows[0] = heldAnnounce( std::vector<uint8_t>( heldVocab, heldVocab + 5 ) );
    {
        std::vector<uint8_t> f( heldVersion, heldVersion + 10 );
        f.insert( f.end(), heldVersion, heldVersion + 10 );
        f.insert( f.end(), heldVocab, heldVocab + 5 );
        buildRows[1] = heldAnnounce( f );
    }
    {
        const uint8_t notKind9[10] = { 1, 5, 0, 0, 0, 0, 0, 0, 0, 0 };
        std::vector<uint8_t> f( notKind9, notKind9 + 10 );
        f.insert( f.end(), heldVocab, heldVocab + 5 );
        buildRows[2] = heldAnnounce( f );
    }
    {
        const uint8_t shortVersion[6] = { 1, 9, 0, 0, 0, 0 };
        buildRows[3] = heldAnnounce( std::vector<uint8_t>( shortVersion, shortVersion + 6 ) );
    }
    for ( int i = 0; i < 4; ++i )
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary v( entries, kTableMessageEntriesHere );
        TableReport r; memset( &r, 0, sizeof( r ) );
        if ( AnnounceRead( v, buildRows[i].data(), (int64_t) buildRows[i].size(), &r ) || !r.malformed || r.refused || v.announced )
            { printf( "strict: the build version %s was not malformed: refused=%d malformed=%d announced=%d\n", buildRowNames[i], (int) r.refused, (int) r.malformed, (int) v.announced ); return 1; }
    }
    // The vocabulary's element-kind row, over the announcement the leg itself
    // wrote: find the vocabulary field (ref 2, kind 14), an LEB length, then
    // the element kind, and corrupt 6 to 9.
    uint8_t ann[1 << 16];
    const int64_t bytes = Announce( ann, (int64_t) sizeof( ann ) );
    uint8_t bad[1 << 16];
    memcpy( bad, ann, (size_t) bytes );
    int64_t at = -1;
    for ( int64_t i = 0; i + 2 < bytes; ++i )
        if ( bad[i] == 2 && bad[i+1] == 14 ) { at = i + 2; break; }
    if ( at < 0 ) { printf( "strict: the announcement carries no vocabulary field\n" ); return 1; }
    while ( bad[at] & 0x80 ) { at++; }
    at++; // past the L
    if ( bad[at] != 6 ) { printf( "strict: the vocabulary field's element kind is not 6: %u\n", bad[at] ); return 1; }
    bad[at] = 9;
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v( entries, kTableMessageEntriesHere );
    TableReport r; memset( &r, 0, sizeof( r ) );
    // The build version is kept the moment it is read, refusal or not.
    if ( AnnounceRead( v, bad, bytes, &r ) || !r.malformed || r.refused || v.announced )
        { printf( "strict: the bad element kind was not malformed: refused=%d malformed=%d announced=%d\n", (int) r.refused, (int) r.malformed, (int) v.announced ); return 1; }
    if ( v.build_version == 0 ) { printf( "strict: the refused announcement did not keep the build version\n" ); return 1; }
`

// "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... `MeasureMessages`
// and `SaveMessages` both refuse and return `-1`" and "`M` ABOVE THE CALLER'S
// CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ... The reader sets the
// returned count to the wire's `M` before it returns the refusal."
// (docs/SPEC-TABLES.md §3.3).
const cppHeldBatchTooLarge = `
    LoginRequest huge[kTableMessageBatchMax + 1];
    for ( int64_t i = 0; i < kTableMessageBatchMax + 1; ++i ) LoginRequestReset( huge[i] );
    TableReport measure; memset( &measure, 0, sizeof( measure ) );
    if ( LoginRequestMeasureMessages( huge, kTableMessageBatchMax + 1, &measure ) != -1 || !measure.refused || measure.reason != TableMessageReason::batch_too_large )
        { printf( "TableMessageReason::batch_too_large write measure: n!= -1 or no refusal: refused=%d reason=%d\n", (int) measure.refused, (int) measure.reason ); return 1; }
    uint8_t out[64];
    TableReport save; memset( &save, 0, sizeof( save ) );
    if ( LoginRequestSaveMessages( huge, kTableMessageBatchMax + 1, out, (int64_t) sizeof( out ), &save ) != -1 || !save.refused || save.reason != TableMessageReason::batch_too_large )
        { printf( "TableMessageReason::batch_too_large write save: no refusal by name: refused=%d reason=%d\n", (int) save.refused, (int) save.reason ); return 1; }
    // the read side: two bodies into storage for one
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    uint8_t ann[1 << 16];
    Announce( ann, (int64_t) sizeof( ann ) );
    TableReport announced; memset( &announced, 0, sizeof( announced ) );
    if ( !AnnounceRead( vocabulary, ann, AnnounceMeasure(), &announced ) ) { printf( "TableMessageReason::batch_too_large: the announcement failed\n" ); return 1; }
    LoginRequest pair[2]; LoginRequestReset( pair[0] ); LoginRequestReset( pair[1] );
    uint8_t buf[512];
    TableReport wrote; memset( &wrote, 0, sizeof( wrote ) );
    int64_t n = LoginRequestSaveMessages( pair, 2, buf, (int64_t) sizeof( buf ), &wrote );
    if ( n < 1 ) { printf( "TableMessageReason::batch_too_large: the two-body save failed\n" ); return 1; }
    LoginRequest storage[1];
    int64_t count = 1;
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( LoginRequestLoadMessages( storage, &count, vocabulary, buf, n, &r ) || count != 2 || !r.refused || r.reason != TableMessageReason::batch_too_large || r.malformed )
        { printf( "TableMessageReason::batch_too_large read: refused=%d count=%lld reason=%d malformed=%d\n", (int) r.refused, (long long) count, (int) r.reason, (int) r.malformed ); return 1; }
`

// "DAMAGE ANYWHERE IN THE BATCH DISCARDS THE WHOLE BATCH" (docs/SPEC-TABLES.md
// §3.3, §4): no body is delivered, the returned count is 0, and the report
// says malformed.
const cppHeldDamageInsideBody = `
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    uint8_t ann[1 << 16];
    Announce( ann, (int64_t) sizeof( ann ) );
    TableReport announced; memset( &announced, 0, sizeof( announced ) );
    if ( !AnnounceRead( vocabulary, ann, AnnounceMeasure(), &announced ) ) { printf( "damage: the announcement failed\n" ); return 1; }
    // M is three: body 1 is empty, body 2's first reference is 63, above the
    // announced entry count E = 33, and body 3 is empty. A reference is six
    // bits, low bit first, so the terminator then 63 is 0xC0 0x0F.
    const uint8_t wire[5] = { 2, 2, 0xC0, 0x0F, 0x00 };
    LoginRequest storage[3];
    int64_t count = 3;
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( LoginRequestLoadMessages( storage, &count, vocabulary, wire, 5, &r ) || count != 0 )
        { printf( "damage: body 2's damage delivered a body: count=%lld refused=%d\n", (long long) count, (int) r.refused ); return 1; }
    if ( !r.malformed || r.refused ) { printf( "damage: one malformed did not count or it was refused: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
`

// "BYTES AFTER THE PAD ARE MALFORMED ... the batch ends at the pad to the byte
// boundary, and a buffer with bytes left over describes no batch this reader
// can name." (docs/SPEC-TABLES.md §3.3).
const cppHeldBytesAfterPad = `
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    uint8_t ann[1 << 16];
    Announce( ann, (int64_t) sizeof( ann ) );
    TableReport announced; memset( &announced, 0, sizeof( announced ) );
    if ( !AnnounceRead( vocabulary, ann, AnnounceMeasure(), &announced ) ) { printf( "pad: the announcement failed\n" ); return 1; }
    LoginRequest v; LoginRequestReset( v );
    uint8_t buf[512];
    TableReport wrote; memset( &wrote, 0, sizeof( wrote ) );
    int64_t n = LoginRequestSaveMessages( &v, 1, buf, (int64_t) sizeof( buf ), &wrote );
    if ( n < 1 ) { printf( "pad: the save failed\n" ); return 1; }
    buf[n] = 0x00;
    LoginRequest storage[1];
    int64_t count = 1;
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( LoginRequestLoadMessages( storage, &count, vocabulary, buf, n + 1, &r ) || count != 0 || !r.malformed || r.refused )
        { printf( "pad: bytes after the pad were not malformed: count=%lld malformed=%d refused=%d\n", (long long) count, (int) r.malformed, (int) r.refused ); return 1; }
`

// "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does not
// know is a REFUSAL by name and never damage." and "a reader handed a form-`2`
// wire where a file was expected refuses by name." (docs/SPEC-TABLES.md §3.3).
const cppHeldFormByte = `
    LoginRequest storage[1];
    for ( uint8_t form = 1; form <= 6; form += 5 )
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
        const uint8_t wire[1] = { form };
        int64_t count = 1;
        TableReport r; memset( &r, 0, sizeof( r ) );
        if ( LoginRequestLoadMessages( storage, &count, vocabulary, wire, 1, &r ) || count != 0 || !r.refused || r.reason != TableMessageReason::newer_form )
            { printf( "form byte %u: refused=%d reason=%d count=%lld\n", form, (int) r.refused, (int) r.reason, (long long) count ); return 1; }
    }
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
        const uint8_t batch[4] = { 2, 0, 0, 0 };
        TableReport r; memset( &r, 0, sizeof( r ) );
        if ( AnnounceRead( vocabulary, batch, 4, &r ) || !r.refused || r.reason != TableMessageReason::message_form_as_file )
            { printf( "TableMessageReason::message_form_as_file: refused=%d reason=%d\n", (int) r.refused, (int) r.reason ); return 1; }
    }
`

// ---- the manifest rows -------------------------------------------------------

type heldManifestConnection struct {
	unit, path string
}

type heldManifestRow struct {
	name, connection string
	root             string
	file, batch      string
	connectionRow    bool
}

// parseHeldManifest reads the manifest itself, never a copied list. Only the
// `connection` and `message` line kinds are returned; every other kind is
// another surface's. A `connection` line is one row (the announcement), and a
// `message` line is one row (the round trip).
func parseHeldManifest(t *testing.T, path string) (map[string][]string, map[string]heldManifestConnection, []heldManifestRow) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	connections := map[string]heldManifestConnection{}
	var rows []heldManifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "unit":
			if len(fields) >= 3 {
				units[fields[1]] = fields[2:]
			}
		case "connection":
			if len(fields) >= 5 {
				connections[fields[1]] = heldManifestConnection{unit: fields[2], path: fields[4]}
				rows = append(rows, heldManifestRow{name: "connection_" + fields[1], connection: fields[1], connectionRow: true})
			}
		case "message":
			if len(fields) >= 6 {
				rows = append(rows, heldManifestRow{name: fields[1], connection: fields[2], root: fields[3], file: fields[4], batch: fields[5]})
			}
		}
	}
	if len(connections) == 0 || len(rows) == 0 {
		t.Fatalf("%s carries no connection or message rows", path)
	}
	return units, connections, rows
}

// loadHeldUnitPaths parses one unit's source files. A directory contributes its
// *.schema files; a named file contributes itself (compiler.GatherPaths' rule).
func loadHeldUnitPaths(t *testing.T, root string, paths []string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		abs := filepath.Join(root, p)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(abs)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(abs, e.Name()))
				}
			}
		} else {
			names = append(names, abs)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			ast, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path:  name,
				Name:  filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data,
				AST:   ast,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// cppConnectionBody asserts a `connection` row: the generated announcement is
// the reference's file byte for byte and the announcement reader accepts it.
func cppConnectionBody(announcement string) string {
	return fmt.Sprintf(`
    std::vector<uint8_t> want;
    if ( !slurp( %q, want ) ) { printf( "the announcement golden is missing\n" ); return 1; }
    if ( AnnounceMeasure() != (int64_t) want.size() )
        { printf( "AnnounceMeasure is %%lld, the golden is %%zu bytes\n", (long long) AnnounceMeasure(), want.size() ); return 1; }
    std::vector<uint8_t> got( want.size() );
    if ( Announce( got.data(), (int64_t) got.size() ) != (int64_t) got.size() )
        { printf( "Announce wrote the wrong length\n" ); return 1; }
    if ( memcmp( got.data(), want.data(), want.size() ) != 0 )
        { printf( "the announcement bytes differ from the reference\n" ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( !AnnounceRead( vocabulary, want.data(), (int64_t) want.size(), &r ) )
        { printf( "AnnounceRead refused the reference announcement: refused=%%d reason=%%d malformed=%%d\n", (int) r.refused, (int) r.reason, (int) r.malformed ); return 1; }
    if ( vocabulary.count != kTableMessageEntriesHere )
        { printf( "announced %%lld entries, want %%lld\n", (long long) vocabulary.count, (long long) kTableMessageEntriesHere ); return 1; }
`, announcement)
}

// cppMessageBody asserts a `message` row for a fixed root: the file-form
// instance decodes to the value the batch gives, and saving that batch
// reproduces the batch wire exactly (§3.3).
func cppMessageBody(root, announcement, fileWire, batchWire string) string {
	const tmpl = `
    std::vector<uint8_t> announcement, fileWire, batchWire;
    if ( !slurp( @ANNOUNCE@, announcement ) || !slurp( @FILE@, fileWire ) || !slurp( @BATCH@, batchWire ) )
        { printf( "a golden is missing\n" ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    TableReport announced; memset( &announced, 0, sizeof( announced ) );
    if ( !AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &announced ) )
        { printf( "AnnounceRead refused the reference announcement\n" ); return 1; }
    @ROOT@ want; memset( &want, 0, sizeof( want ) ); @ROOT@Reset( want );
    TableReport fileReport; memset( &fileReport, 0, sizeof( fileReport ) );
    if ( !@ROOT@Load( want, fileWire.data(), (int64_t) fileWire.size(), &fileReport ) )
        { printf( "@ROOT@Load refused the file-form golden\n" ); return 1; }
    @ROOT@ storage[1]; memset( storage, 0, sizeof( storage ) ); @ROOT@Reset( storage[0] );
    int64_t count = 1;
    TableReport loadReport; memset( &loadReport, 0, sizeof( loadReport ) );
    if ( !@ROOT@LoadMessages( storage, &count, vocabulary, batchWire.data(), (int64_t) batchWire.size(), &loadReport ) || count != 1 )
        { printf( "@ROOT@LoadMessages: count=%lld\n", (long long) count ); return 1; }
    if ( memcmp( &storage[0], &want, sizeof( want ) ) != 0 )
        { printf( "the batch's value is not the file-form instance's value\n" ); return 1; }
    TableReport saveReport; memset( &saveReport, 0, sizeof( saveReport ) );
    const int64_t n = @ROOT@MeasureMessages( storage, 1, &saveReport );
    if ( n < 0 ) { printf( "MeasureMessages failed\n" ); return 1; }
    std::vector<uint8_t> out( (size_t) n );
    if ( @ROOT@SaveMessages( storage, 1, out.data(), n, &saveReport ) != n )
        { printf( "SaveMessages failed\n" ); return 1; }
    if ( memcmp( out.data(), batchWire.data(), batchWire.size() ) != 0 )
        { printf( "the batch bytes differ from the reference\n" ); return 1; }
`
	return strings.NewReplacer(
		"@ROOT@", root,
		"@ANNOUNCE@", fmt.Sprintf("%q", announcement),
		"@FILE@", fmt.Sprintf("%q", fileWire),
		"@BATCH@", fmt.Sprintf("%q", batchWire),
	).Replace(tmpl)
}

// cppVariableMessageBody asserts a `message` row whose root is a VARIABLE
// table (a pointered root, read through a region): loading either form and
// saving the other reproduces the other's pinned bytes, the §3.3 round trip
// the manifest's `message` line pins. A variable root is held by reference,
// so each form is read into its own region and re-saved as the other.
func cppVariableMessageBody(root, announcement, fileWire, batchWire string) string {
	const tmpl = `
    std::vector<uint8_t> announcement, fileWire, batchWire;
    if ( !slurp( @ANNOUNCE@, announcement ) || !slurp( @FILE@, fileWire ) || !slurp( @BATCH@, batchWire ) )
        { printf( "a golden is missing\n" ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    TableReport announced; memset( &announced, 0, sizeof( announced ) );
    if ( !AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &announced ) )
        { printf( "AnnounceRead refused the reference announcement\n" ); return 1; }
    // the FILE form, read into its own region (§3.3): a pointered root is held
    // by reference, and the caller owns the region.
    TableReport fileReport; memset( &fileReport, 0, sizeof( fileReport ) );
    const int64_t fileNeed = @ROOT@LoadMeasure( fileWire.data(), (int64_t) fileWire.size() );
    if ( fileNeed < 0 ) { printf( "@ROOT@LoadMeasure refused the file-form golden\n" ); return 1; }
    std::vector<uint8_t> fileRegion( (size_t) fileNeed );
    const @ROOT@ * fileRoot = @ROOT@Load( fileRegion.data(), fileNeed, fileWire.data(), (int64_t) fileWire.size(), &fileReport );
    if ( fileRoot == NULL ) { printf( "@ROOT@Load refused the file-form golden\n" ); return 1; }
    // loading the FILE form and saving the MESSAGE form reproduces the message
    // bytes (§3.3).
    const @ROOT@ * fileRoots[1] = { fileRoot };
    TableReport fileSaveReport; memset( &fileSaveReport, 0, sizeof( fileSaveReport ) );
    const int64_t fileMsgSize = @ROOT@MeasureMessages( fileRoots, 1, &fileSaveReport );
    if ( fileMsgSize < 0 ) { printf( "MeasureMessages failed on the file-form value\n" ); return 1; }
    std::vector<uint8_t> fileMsg( (size_t) fileMsgSize );
    if ( @ROOT@SaveMessages( fileRoots, 1, fileMsg.data(), fileMsgSize, &fileSaveReport ) != fileMsgSize )
        { printf( "SaveMessages failed on the file-form value\n" ); return 1; }
    if ( memcmp( fileMsg.data(), batchWire.data(), batchWire.size() ) != 0 )
        { printf( "the file form saved as a message differs from the message reference\n" ); return 1; }
    // loading the MESSAGE form and saving the FILE form reproduces the file
    // bytes (§3.3).
    TableReport msgReport; memset( &msgReport, 0, sizeof( msgReport ) );
    const int64_t msgNeed = @ROOT@LoadMeasure( vocabulary, batchWire.data(), (int64_t) batchWire.size() );
    if ( msgNeed < 0 ) { printf( "@ROOT@LoadMeasure refused the message-form golden\n" ); return 1; }
    std::vector<uint8_t> msgRegion( (size_t) msgNeed );
    const @ROOT@ * msgRoots[1] = { NULL };
    int64_t count = 1;
    if ( !@ROOT@LoadMessages( msgRoots, &count, msgRegion.data(), msgNeed, vocabulary, batchWire.data(), (int64_t) batchWire.size(), &msgReport ) || count != 1 )
        { printf( "@ROOT@LoadMessages: count=%lld\n", (long long) count ); return 1; }
    const int64_t msgFileSize = @ROOT@Measure( msgRoots[0] );
    if ( msgFileSize < 0 ) { printf( "@ROOT@Measure failed on the message-form value\n" ); return 1; }
    std::vector<uint8_t> msgFile( (size_t) msgFileSize );
    if ( @ROOT@Save( msgRoots[0], msgFile.data(), msgFileSize ) != msgFileSize )
        { printf( "@ROOT@Save failed on the message-form value\n" ); return 1; }
    if ( memcmp( msgFile.data(), fileWire.data(), fileWire.size() ) != 0 )
        { printf( "the message form saved as a file differs from the file reference\n" ); return 1; }
`
	return strings.NewReplacer(
		"@ROOT@", root,
		"@ANNOUNCE@", fmt.Sprintf("%q", announcement),
		"@FILE@", fmt.Sprintf("%q", fileWire),
		"@BATCH@", fmt.Sprintf("%q", batchWire),
	).Replace(tmpl)
}

// TestMessageFormHeldCPP is table-driven: one subtest per §3.3 refusal clause
// and one per manifest `connection` and `message` row, and every subtest opens
// with t.Parallel().
func TestMessageFormHeldCPP(t *testing.T) {
	t.Parallel()
	const unit = "tables/backend/Backend.schema"
	unitPath := filepath.Join("..", "..", "..", unit)
	clauses := []struct {
		name string
		body string
	}{
		{"TableMessageReason::no_vocabulary", cppHeldNoVocabulary},
		{"TableMessageReason::second_announcement", cppHeldSecondAnnouncement},
		{"TableMessageReason::vocabulary_too_large", cppHeldVocabularyTooLarge},
		{"announcement_strict_checks", cppHeldStrictChecks},
		{"TableMessageReason::batch_too_large", cppHeldBatchTooLarge},
		{"damage_inside_body", cppHeldDamageInsideBody},
		{"bytes_after_pad", cppHeldBytesAfterPad},
		{"form_byte", cppHeldFormByte},
	}
	for _, clause := range clauses {
		t.Run(clause.name, func(t *testing.T) {
			t.Parallel()
			out, err := cppHeldProbe(t, unitPath, clause.body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", clause.name, err, out)
			}
		})
	}

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units, connections, rows := parseHeldManifest(t, manifest)

	// Every unit a connection names is parsed once, here, before the parallel
	// subtests: check.Unit is not concurrency-safe and the subtests must not
	// share a written cache.
	loaded := map[string]*ir.Unit{}
	for _, conn := range connections {
		if _, ok := loaded[conn.unit]; ok {
			continue
		}
		paths, ok := units[conn.unit]
		if !ok {
			t.Fatalf("manifest names unit %q with no unit line", conn.unit)
		}
		loaded[conn.unit] = loadHeldUnitPaths(t, root, paths)
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			conn, ok := connections[row.connection]
			if !ok {
				t.Fatalf("manifest row %q names connection %q with no connection line", row.name, row.connection)
			}
			u, ok := loaded[conn.unit]
			if !ok {
				t.Fatalf("manifest row %q names unit %q with no unit line", row.name, conn.unit)
			}
			var body string
			switch {
			case row.connectionRow:
				body = cppConnectionBody(filepath.Join(root, conn.path))
			case ir.VariableTables(u)[row.root]:
				body = cppVariableMessageBody(row.root, filepath.Join(root, conn.path), filepath.Join(root, row.file), filepath.Join(root, row.batch))
			default:
				body = cppMessageBody(row.root, filepath.Join(root, conn.path), filepath.Join(root, row.file), filepath.Join(root, row.batch))
			}
			out, err := cppHeldRun(t, u, body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", row.name, err, out)
			}
		})
	}
}

// TestMessageFormSkipDepthCPP is the message_skip_depth boundary on the
// generated C++ reader (docs/SPEC-TABLES.md, limit value 128). Nested bodies,
// union arms and walked array elements succeed at 127 and at 128. The named
// above-limit rows are complete zero-padded batches whose only fault is depth
// past 128. Verdicts are the spec's. TableMessageEntry keeps its default
// value_bits of -1; zeroing the entry would take the arithmetic element path.
func TestMessageFormSkipDepthCPP(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
 a int32
 b bool
 c uint8
}
`
	out, err := runCppGeneratedEdited(t, schema, cppSkipDepthMain, nil)
	if err != nil {
		t.Fatalf("skip_depth_above_128: %v\n%s", err, out)
	}
	_, err = runCppGeneratedEdited(t, schema, cppSkipDepthMain, func(files map[string][]byte) {
		files["ProbeTable.h"] = []byte(strings.ReplaceAll(string(files["ProbeTable.h"]), "if ( depth > 128 )", "if ( depth > 256 )"))
	})
	if err == nil {
		t.Fatalf("skip_depth_above_128: raising the depth bound from 128 to 256 did not fail the above-limit rows")
	}
	u := cppUnitFrom(t, schema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.cpp"), []byte(cppSkipDepthMain), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe_asan")
	args := []string{"-std=c++17", "-O2", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-I", dir,
		filepath.Join(dir, "main.cpp"), filepath.Join(dir, "ProbeBlock.cpp"), filepath.Join(dir, "ProbeTable.cpp"), "-o", bin}
	if out, err := exec.Command("c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile asan: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run asan: %v\n%s", err, out)
	}
}

const cppSkipDepthMain = `#include "ProbeTable.h"
#include <cstdio>
#include <cstdint>
#include <cstring>
using namespace probe;

static void fill_entry(TableMessageEntry & e, uint8_t kind)
{
    e.id = 1;
    e.kind = kind;
}
static int64_t pack_refs(uint8_t * buf, int cap, int width, int nlead, uint64_t lead, int nmid, uint64_t mid, int ntail)
{
    int at = 16, i, b, n;
    if (cap < 2) return -1;
    memset(buf, 0, (size_t)cap);
    buf[0] = 2;
    buf[1] = 0;
    n = nlead + nmid + ntail;
    for (i = 0; i < n; i++) {
        uint64_t ref = i < nlead ? lead : (i < nlead + nmid ? mid : 0);
        for (b = 0; b < width; b++) {
            if ((at >> 3) >= cap) return -1;
            if ((ref >> b) & 1) buf[at >> 3] |= (uint8_t)(1u << (at & 7));
            at++;
        }
    }
    return (int64_t)(at + 7) / 8;
}
static int load_one(const TableVocabulary & vocabulary, const uint8_t * buf, int64_t n, Root & storage, int64_t & count, TableReport & report)
{
    RootReset(storage);
    count = 1;
    memset(&report, 0, sizeof(report));
    return RootLoadMessages(&storage, &count, vocabulary, buf, n, &report) ? 1 : 0;
}
static int expect_ok(const char * row, const TableVocabulary & vocabulary, const uint8_t * buf, int64_t n)
{
    Root storage; int64_t count = 0; TableReport report; int ok;
    ok = load_one(vocabulary, buf, n, storage, count, report);
    if (!(ok && !report.malformed && report.unknown == 1 && storage.a == 0 && count == 1)) {
        printf("%s: ok=%d malformed=%d unknown=%d a=%d count=%lld\n",
            row, ok, (int)report.malformed, (int)report.unknown, (int)storage.a, (long long)count);
        return 1;
    }
    return 0;
}
static int expect_bad(const char * row, const TableVocabulary & vocabulary, const uint8_t * buf, int64_t n)
{
    Root storage; int64_t count = 0; TableReport report; int ok;
    ok = load_one(vocabulary, buf, n, storage, count, report);
    if (!(!ok && report.malformed && count == 0)) {
        printf("%s: ok=%d malformed=%d count=%lld\n", row, ok, (int)report.malformed, (long long)count);
        return 1;
    }
    return 0;
}
int main()
{
    uint8_t buf[128];
    int64_t n;
    TableMessageEntry body, arm, arr[2], keyed6[2], keyed32[2], keyed13[2];
    fill_entry(body, 13);
    fill_entry(arm, 15);
    fill_entry(arr[0], 13);
    fill_entry(arr[1], 14);
    arr[1].id = 2;
    arr[1].min = 1;
    arr[1].max = 1;
    arr[1].elem_kind = 13;
    fill_entry(keyed6[0], 13);
    fill_entry(keyed6[1], 16);
    keyed6[1].id = 2;
    keyed6[1].max = 3;
    keyed6[1].elem_kind = 6;
    keyed6[1].elem_value_bits = 8;
    fill_entry(keyed32[0], 13);
    fill_entry(keyed32[1], 16);
    keyed32[1].id = 2;
    keyed32[1].max = 3;
    keyed32[1].elem_kind = 32;
    fill_entry(keyed13[0], 13);
    fill_entry(keyed13[1], 16);
    keyed13[1].id = 2;
    keyed13[1].max = 3;
    keyed13[1].elem_kind = 13;
    TableVocabulary body_v(&body, 1);
    body_v.count = 1;
    body_v.ref_bits = 1;
    body_v.announced = true;
    TableVocabulary arm_v(&arm, 1);
    arm_v.count = 1;
    arm_v.ref_bits = 1;
    arm_v.announced = true;
    TableVocabulary arr_v(arr, 2);
    arr_v.count = 2;
    arr_v.ref_bits = 2;
    arr_v.announced = true;
    TableVocabulary keyed6_v(keyed6, 2);
    keyed6_v.count = 2;
    keyed6_v.ref_bits = 2;
    keyed6_v.announced = true;
    TableVocabulary keyed32_v(keyed32, 2);
    keyed32_v.count = 2;
    keyed32_v.ref_bits = 2;
    keyed32_v.announced = true;
    TableVocabulary keyed13_v(keyed13, 2);
    keyed13_v.count = 2;
    keyed13_v.ref_bits = 2;
    keyed13_v.announced = true;
    n = pack_refs(buf, 128, 1, 127, 1, 0, 0, 128);
    if (n < 0 || expect_ok("skip_depth_body_below_128", body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 128, 1, 0, 0, 129);
    if (n < 0 || expect_ok("skip_depth_body_at_128", body_v, buf, n)) return 1;
    if (expect_ok("skip_depth_continuation", body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 128, 1, 0, 0, 128);
    if (n < 0 || n * 8 != 16 + 256) { printf("skip_depth_truncation alignment\n"); return 1; }
    if (expect_bad("skip_depth_truncation", body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 129, 1, 0, 0, 130);
    if (n < 0 || expect_bad("skip_depth_above_128 body", body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 128, 1, 0, 0, 2);
    if (n < 0 || expect_ok("skip_depth_union_below_128", arm_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 129, 1, 0, 0, 2);
    if (n < 0 || expect_ok("skip_depth_union_at_128", arm_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 130, 1, 0, 0, 2);
    if (n < 0 || expect_bad("skip_depth_above_128 union", arm_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 125, 1, 1, 2, 127);
    if (n < 0 || expect_ok("skip_depth_array_below_128", arr_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 126, 1, 1, 2, 128);
    if (n < 0 || expect_ok("skip_depth_array_at_128", arr_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 127, 1, 1, 2, 129);
    if (n < 0 || expect_bad("skip_depth_above_128 array", arr_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 128, 1, 2, 2, 139);
    if (n < 0 || expect_ok("skip_depth_keyed_fixed_at_128", keyed6_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 128, 1, 2, 2, 131);
    if (n < 0 || expect_ok("skip_depth_keyed_nopayload_at_128", keyed32_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 127, 1, 2, 2, 132);
    if (n < 0 || expect_bad("skip_depth_above_128 keyed", keyed13_v, buf, n)) return 1;
    return 0;
}
`

package cpptable

// TestMessageFormHeldCPP proves, one subtest per clause, the refusals and rows
// the MESSAGE FORM (form byte 2, docs/SPEC-TABLES.md §3.3) names on the
// generated C++ surface, to the same bar the Go leg's cards set. Each subtest
// generates the C++ for a unit, compiles a probe against it, and runs the
// probe, which asserts the clause on the generated verbs and reports the red
// line by name.
//
// The clauses are the page's own: no_vocabulary, second_announcement and its
// terminality, vocabulary_too_large on both bounds, the two strict announcement
// checks, batch_too_large on both sides, damage inside body k delivering k-1,
// bytes after the pad, and newer_form with message_form_as_file.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
)

// cppHeldProbe generates the leg's C++ headers for the unit at `unitPath` and
// compiles a probe that includes them and runs `body` (C++ that returns nonzero
// and prints its red line on a failed assertion). The unit is a fixed table's,
// so the generated headers are self-contained and need no sibling runtime.
func cppHeldProbe(t *testing.T, unitPath, body string) ([]byte, error) {
	t.Helper()
	u := loadUnit(t, unitPath)
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
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}

	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <cstdint>
#include "%s"

using namespace %s;

int main( void )
{
%s
    return 0;
}
`, header, u.Package, body)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	build := exec.Command(cxx, "-std=c++17", "-O1", "-I"+outDir, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe"))
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

// "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
// decoded, no counter moves and `malformed` does not fire." (docs/SPEC-TABLES.md §3.3)
const cppHeldNoVocabulary = `
    LoginRequest v; LoginRequestReset( v );
    uint8_t buf[512];
    TableReport save; memset( &save, 0, sizeof( save ) );
    int64_t n = LoginRequestSaveMessages( &v, 1, buf, (int64_t) sizeof( buf ), &save );
    if ( n < 1 ) { printf( "no_vocabulary: the save failed: n=%lld\n", (long long) n ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    LoginRequest out[1];
    int64_t count = 1;
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( LoginRequestLoadMessages( out, &count, vocabulary, buf, n, &r ) )
        { printf( "no_vocabulary: a body from a peer that never announced was not refused\n" ); return 1; }
    if ( !r.refused || r.reason != no_vocabulary || r.malformed )
        { printf( "no_vocabulary: refused=%d reason=%d malformed=%d\n", (int) r.refused, (int) r.reason, (int) r.malformed ); return 1; }
    if ( count != 0 ) { printf( "no_vocabulary moved the returned count: %lld\n", (long long) count ); return 1; }
`

// "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does not
// replace or amend anything." and "REFUSAL IS TERMINAL, and a refused FIRST
// announcement is the one the connection gets ... every announcement after it
// is refused as `second_announcement` ... every body after it is refused for
// want of one." (docs/SPEC-TABLES.md §3.3)
const cppHeldSecondAnnouncement = `
    uint8_t ann[1 << 16];
    if ( Announce( ann, (int64_t) sizeof( ann ) ) != AnnounceMeasure() )
        { printf( "second_announcement: the announcement did not write its own measure\n" ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v( entries, kTableMessageEntriesHere );
    TableReport first; memset( &first, 0, sizeof( first ) );
    if ( !AnnounceRead( v, ann, AnnounceMeasure(), &first ) || v.count != kTableMessageEntriesHere )
        { printf( "second_announcement: the first announcement was not accepted: refused=%d reason=%d\n", (int) first.refused, (int) first.reason ); return 1; }
    const int64_t before = v.count;
    TableReport second; memset( &second, 0, sizeof( second ) );
    if ( AnnounceRead( v, ann, AnnounceMeasure(), &second ) || !second.refused || second.reason != second_announcement || second.malformed )
        { printf( "second_announcement: a second announcement was not refused by name: refused=%d reason=%d malformed=%d\n", (int) second.refused, (int) second.reason, (int) second.malformed ); return 1; }
    if ( v.count != before ) { printf( "second_announcement: a second announcement replaced the vocabulary\n" ); return 1; }
    TableMessageEntry wentries[kTableMessageEntriesHere];
    TableVocabulary w( wentries, kTableMessageEntriesHere );
    const uint8_t junk[3] = { 9, 9, 9 };
    TableReport rejected; memset( &rejected, 0, sizeof( rejected ) );
    if ( AnnounceRead( w, junk, 3, &rejected ) || !rejected.refused || rejected.reason != newer_form || w.announced )
        { printf( "second_announcement: a refused first announcement was not refused by name: refused=%d reason=%d announced=%d\n", (int) rejected.refused, (int) rejected.reason, (int) w.announced ); return 1; }
    TableReport later; memset( &later, 0, sizeof( later ) );
    if ( AnnounceRead( w, ann, AnnounceMeasure(), &later ) || later.reason != second_announcement || w.announced )
        { printf( "second_announcement: an announcement after a refused first is not second_announcement: reason=%d announced=%d\n", (int) later.reason, (int) w.announced ); return 1; }
    LoginRequest root; LoginRequestReset( root );
    uint8_t buf[512];
    TableReport wrote; memset( &wrote, 0, sizeof( wrote ) );
    int64_t n = LoginRequestSaveMessages( &root, 1, buf, (int64_t) sizeof( buf ), &wrote );
    if ( n < 1 ) { printf( "second_announcement: the save failed\n" ); return 1; }
    LoginRequest storage[1];
    int64_t count = 1;
    TableReport body; memset( &body, 0, sizeof( body ) );
    if ( LoginRequestLoadMessages( storage, &count, w, buf, n, &body ) || count != 0 || body.reason != no_vocabulary )
        { printf( "second_announcement: a batch on the refused connection: refused=%d count=%lld reason=%d\n", (int) body.refused, (long long) count, (int) body.reason ); return 1; }
`

// "`vocabulary_too_large` covers both bounds, the entry count and the
// vocabulary's bytes." (docs/SPEC-TABLES.md §3.3)
const cppHeldVocabularyTooLarge = `
    uint8_t ann[1 << 16];
    Announce( ann, (int64_t) sizeof( ann ) );
    TableMessageEntry one[1];
    TableVocabulary entries( one, 1 );
    TableReport entryReport; memset( &entryReport, 0, sizeof( entryReport ) );
    if ( AnnounceRead( entries, ann, AnnounceMeasure(), &entryReport ) || !entryReport.refused || entryReport.reason != vocabulary_too_large || entryReport.malformed || entries.announced )
        { printf( "vocabulary_too_large entry bound: refused=%d reason=%d malformed=%d announced=%d\n", (int) entryReport.refused, (int) entryReport.reason, (int) entryReport.malformed, (int) entries.announced ); return 1; }
    TableMessageEntry all[kTableMessageEntriesHere];
    TableVocabulary bytes( all, kTableMessageEntriesHere );
    bytes.max_bytes = 4;
    TableReport byteReport; memset( &byteReport, 0, sizeof( byteReport ) );
    if ( AnnounceRead( bytes, ann, AnnounceMeasure(), &byteReport ) || !byteReport.refused || byteReport.reason != vocabulary_too_large || byteReport.malformed || bytes.announced )
        { printf( "vocabulary_too_large byte bound: refused=%d reason=%d malformed=%d announced=%d\n", (int) byteReport.refused, (int) byteReport.reason, (int) byteReport.malformed, (int) bytes.announced ); return 1; }
`

// "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS: the build
// version present, exactly once, kind `9`, eight bytes wide, and the vocabulary
// present, exactly once, kind `14`, element kind `6` ... A FAILED STRICT CHECK
// IS MALFORMED, not a refusal". (docs/SPEC-TABLES.md §3.3)
const cppHeldStrictChecks = `
    uint8_t ann[1 << 16];
    const int64_t bytes = Announce( ann, (int64_t) sizeof( ann ) );
    // Find the vocabulary field's element-kind byte: the field is ref 2, kind
    // 14, an LEB length, then the element kind. Corrupting 6 to 9 is the
    // element-kind strict check's own row.
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
// (docs/SPEC-TABLES.md §3.3)
const cppHeldBatchTooLarge = `
    LoginRequest huge[kTableMessageBatchMax + 1];
    for ( int64_t i = 0; i < kTableMessageBatchMax + 1; ++i ) LoginRequestReset( huge[i] );
    TableReport measure; memset( &measure, 0, sizeof( measure ) );
    if ( LoginRequestMeasureMessages( huge, kTableMessageBatchMax + 1, &measure ) != -1 || !measure.refused || measure.reason != batch_too_large )
        { printf( "batch_too_large write measure: n!= -1 or no refusal: refused=%d reason=%d\n", (int) measure.refused, (int) measure.reason ); return 1; }
    uint8_t out[64];
    TableReport save; memset( &save, 0, sizeof( save ) );
    if ( LoginRequestSaveMessages( huge, kTableMessageBatchMax + 1, out, (int64_t) sizeof( out ), &save ) != -1 || !save.refused || save.reason != batch_too_large )
        { printf( "batch_too_large write save: no refusal by name: refused=%d reason=%d\n", (int) save.refused, (int) save.reason ); return 1; }
    // the read side: two bodies into storage for one
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    uint8_t ann[1 << 16];
    Announce( ann, (int64_t) sizeof( ann ) );
    TableReport announced; memset( &announced, 0, sizeof( announced ) );
    if ( !AnnounceRead( vocabulary, ann, AnnounceMeasure(), &announced ) ) { printf( "batch_too_large: the announcement failed\n" ); return 1; }
    LoginRequest pair[2]; LoginRequestReset( pair[0] ); LoginRequestReset( pair[1] );
    uint8_t buf[512];
    TableReport wrote; memset( &wrote, 0, sizeof( wrote ) );
    int64_t n = LoginRequestSaveMessages( pair, 2, buf, (int64_t) sizeof( buf ), &wrote );
    if ( n < 1 ) { printf( "batch_too_large: the two-body save failed\n" ); return 1; }
    LoginRequest storage[1];
    int64_t count = 1;
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( LoginRequestLoadMessages( storage, &count, vocabulary, buf, n, &r ) || count != 2 || !r.refused || r.reason != batch_too_large || r.malformed )
        { printf( "batch_too_large read: refused=%d count=%lld reason=%d malformed=%d\n", (int) r.refused, (long long) count, (int) r.reason, (int) r.malformed ); return 1; }
`

// "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`. The returned count
// states `k - 1`, ONE `malformed` counts, and nothing at or after body `k` is
// read." (docs/SPEC-TABLES.md §3.3)
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
    if ( LoginRequestLoadMessages( storage, &count, vocabulary, wire, 5, &r ) || count != 1 )
        { printf( "damage: body 2's damage did not deliver one body: count=%lld refused=%d\n", (long long) count, (int) r.refused ); return 1; }
    if ( !r.malformed ) { printf( "damage: one malformed did not count: malformed=%d\n", (int) r.malformed ); return 1; }
`

// "BYTES AFTER THE PAD ARE MALFORMED ... the batch ends at the pad to the byte
// boundary, and a buffer with bytes left over describes no batch this reader
// can name." (docs/SPEC-TABLES.md §3.3)
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
    if ( LoginRequestLoadMessages( storage, &count, vocabulary, buf, n + 1, &r ) || count != 1 || !r.malformed || r.refused )
        { printf( "pad: bytes after the pad were not malformed: count=%lld malformed=%d refused=%d\n", (long long) count, (int) r.malformed, (int) r.refused ); return 1; }
`

// "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does not
// know is a REFUSAL by name and never damage." and "a reader handed a form-`2`
// wire where a file was expected refuses by name." (docs/SPEC-TABLES.md §3.3)
const cppHeldFormByte = `
    LoginRequest storage[1];
    for ( uint8_t form = 1; form <= 6; form += 5 )
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
        const uint8_t wire[1] = { form };
        int64_t count = 1;
        TableReport r; memset( &r, 0, sizeof( r ) );
        if ( LoginRequestLoadMessages( storage, &count, vocabulary, wire, 1, &r ) || count != 0 || !r.refused || r.reason != newer_form )
            { printf( "form byte %u: refused=%d reason=%d count=%lld\n", form, (int) r.refused, (int) r.reason, (long long) count ); return 1; }
    }
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
        const uint8_t batch[4] = { 2, 0, 0, 0 };
        TableReport r; memset( &r, 0, sizeof( r ) );
        if ( AnnounceRead( vocabulary, batch, 4, &r ) || !r.refused || r.reason != message_form_as_file )
            { printf( "message_form_as_file: refused=%d reason=%d\n", (int) r.refused, (int) r.reason ); return 1; }
    }
`

// TestMessageFormHeldCPP is table-driven, one subtest per clause, and every
// subtest opens with t.Parallel().
func TestMessageFormHeldCPP(t *testing.T) {
	t.Parallel()
	const unit = "tables/backend/Backend.schema"
	unitPath := filepath.Join("..", "..", "..", unit)
	clauses := []struct {
		name string
		body string
	}{
		{"no_vocabulary", cppHeldNoVocabulary},
		{"second_announcement", cppHeldSecondAnnouncement},
		{"vocabulary_too_large", cppHeldVocabularyTooLarge},
		{"announcement_strict_checks", cppHeldStrictChecks},
		{"batch_too_large", cppHeldBatchTooLarge},
		{"damage_inside_body", cppHeldDamageInsideBody},
		{"bytes_after_pad", cppHeldBytesAfterPad},
		{"form_byte", cppHeldFormByte},
	}
	for _, clause := range clauses {
		clause := clause
		t.Run(clause.name, func(t *testing.T) {
			t.Parallel()
			out, err := cppHeldProbe(t, unitPath, clause.body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", clause.name, err, out)
			}
		})
	}
}

// THE MESSAGE FORM CARRIES THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14,
// §3.3): a fixed table that declares `integrity = "crc32c"` saves a batch with
// four CRC-32C bytes after the padded bit stream, and the reader checks them
// before it decodes the first body, so no single-bit flip of a saved batch
// decodes as a value. A table without the key writes the bytes it wrote before.
//
// Every expected answer is the one the Go oracle pins for the same schema text
// and the same input bytes (internal/codegen/gotable/integritymessage_test.go
// TestGoWireIntegrityMessageForm), transcribed into this C++ probe.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppIntegrityMessageSchema = `package probe
fixed table Ledger | integrity = "crc32c"
{
    n int32
    count uint16
}
fixed table Plain
{
    n int32
    count uint16
}
`

const cppIntegrityMessageDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { printf( "FAIL %s\n", what ); failures++; }
}

int main()
{
    // THE ORACLE GOLDENS (internal/codegen/gotable/integritymessage_test.go):
    // the keyless batch does not move, and the declaring batch is it plus its
    // CRC-32C trailer.
    static const char plain_golden[] = "0200813e000020070000";
    static const char ledger_golden[] = "0200813e00002007000078f2be34";

    TableReport report;
    Ledger l; l.n = 1000; l.count = 7;
    Plain p; p.n = 1000; p.count = 7;
    Ledger * lv = &l;
    Plain * pv = &p;

    const int64_t ln = LedgerMeasureMessages( lv, 1, &report );
    const int64_t pn = PlainMeasureMessages( pv, 1, &report );
    check( ln >= 0 && pn >= 0, "measure < 0" );
    check( ln == pn + 4, "the declaring batch does not measure four more" );

    uint8_t lb[64], pb[64];
    check( LedgerSaveMessages( lv, 1, lb, (int64_t) sizeof( lb ), &report ) == ln, "LedgerSaveMessages length" );
    check( PlainSaveMessages( pv, 1, pb, (int64_t) sizeof( pb ), &report ) == pn, "PlainSaveMessages length" );
    check( LedgerSaveMessages( lv, 1, lb, ln - 1, &report ) < 0, "a buffer one byte short did not refuse" );

    char hex[64];
    int pos = 0;
    for ( int64_t i = 0; i < pn; i++ ) { pos += sprintf( hex + pos, "%02x", pb[i] ); }
    hex[pos] = 0;
    check( strcmp( hex, plain_golden ) == 0, "Plain batch moved from BASE" );
    pos = 0;
    for ( int64_t i = 0; i < ln; i++ ) { pos += sprintf( hex + pos, "%02x", lb[i] ); }
    hex[pos] = 0;
    check( strcmp( hex, ledger_golden ) == 0, "Ledger batch golden" );

    // the reader half needs the unit's announced vocabulary
    static TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocab( entries, kTableMessageEntriesHere );
    static uint8_t ann[65536];
    check( Announce( ann, (int64_t) sizeof( ann ) ) == AnnounceMeasure(), "Announce" );
    check( AnnounceRead( vocab, ann, AnnounceMeasure(), &report ), "AnnounceRead" );

    // clean round trips
    {
        Ledger out[1];
        int64_t got = 1;
        check( LedgerLoadMessages( out, &got, vocab, lb, ln, &report ) && got == 1 && out[0].n == 1000 && out[0].count == 7, "Ledger clean round trip" );
    }
    {
        Plain out[1];
        int64_t got = 1;
        check( PlainLoadMessages( out, &got, vocab, pb, pn, &report ) && got == 1 && out[0].n == 1000 && out[0].count == 7, "Plain clean round trip" );
    }

    // every single-bit flip and every 0xff of every byte is refused by name,
    // and nothing is decoded. The form byte is read first, so its flip is
    // §3.3's newer_form; every other byte is the batch trailer's
    // integrity_mismatch (docs/SPEC-TABLES.md §2.14).
    static const uint8_t flips[] = { 1, 2, 4, 8, 16, 32, 64, 128, 255 };
    for ( int64_t i = 0; i < ln; i++ )
    {
        for ( unsigned k = 0; k < sizeof( flips ); k++ )
        {
            uint8_t bad[64];
            memcpy( bad, lb, (size_t) ln );
            bad[i] ^= flips[k];
            Ledger out[1];
            int64_t got = 1;
            TableReport r;
            const bool ok = LedgerLoadMessages( out, &got, vocab, bad, ln, &r );
            if ( i == 0 )
            {
                check( !ok && got == 0 && r.refused && r.reason == TableMessageReason::newer_form, "flip of the form byte is not newer_form" );
            }
            else
            {
                check( !ok && got == 0 && r.refused && r.refuse_reason == TableRefuseReason::integrity_mismatch, "flip is not integrity_mismatch" );
            }
        }
    }

    return failures == 0 ? 0 : 1;
}
`

// TestCPPWireIntegrityMessageForm holds the generated C++ save path to the Go
// oracle's bytes and refusals for the message form's integrity trailer.
func TestCPPWireIntegrityMessageForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated message surface is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppIntegrityMessageSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// the header the emitter grew for THIS schema has to carry the trailer's
	// helpers at all, or the comparison below would pass by measuring nothing.
	header := string(files["ProbeTable.h"])
	for _, want := range []string{
		"schema_crc32c(",
		"TableMessageBatchSeal(",
		"TableMessageBatchCheckIntegrity(",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("the generated message surface does not carry %q — this test would prove nothing", want)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppIntegrityMessageDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "cppintegritymessage")
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main, "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

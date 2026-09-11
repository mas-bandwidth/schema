package compiler

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// announcementWith frames one vocabulary the way ir.TableAnnouncement frames
// the unit's own (docs/SPEC-TABLES.md §3.3): a form-1 file whose body is the
// build version under the reserved id at kind 9 and the vocabulary bytes
// under the reserved id at kind 14 over element kind 6. The test needs it to
// put an entry on the wire TWICE, which no compiler produces.
func announcementWith(u *ir.Unit, vocabulary []byte) []byte {
	leb := func(out []byte, v uint64) []byte {
		for v >= 0x80 {
			out = append(out, uint8(v)|0x80)
			v >>= 7
		}
		return append(out, uint8(v))
	}
	body := append([]byte{uint8(ir.TableKindU8)}, leb(nil, uint64(len(vocabulary)))...)
	body = append(body, vocabulary...)
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, ir.BuildVersion(u))
	out = append(out, 2, uint8(ir.TableKindArray))
	out = leb(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// Two declared quantized shapes that DERIVE one quantization are two entries
// (docs/SPEC-TABLES.md §3.3: the same name at a second shape takes a second
// slot), and the C++ reference's AnnounceRead must take the compiler's own
// announcement that carries them. Its duplicate scan compared the resolved
// entries, which keep only §4.3's derivation and not the declared max and
// res, so 0.1 and 0.1001 over [0, 1] collapsed into one entry and the unit's
// own announcement read as malformed (#722). The scan now compares the
// declared bytes, and a genuinely repeated entry is still malformed.
func TestCppTableAnnouncementDistinctQuantizedShapes(t *testing.T) {
	u := unitFromSource(t, `package probe
fixed table First { rate float32 | min = 0, max = 1, resolution = 0.1 }
fixed table Second { rate float32 | min = 0, max = 1, resolution = 0.1001 }
`)
	entries := ir.TableVocabulary(u)
	rate := -1
	for i, e := range entries {
		if e.Kind != uint8(ir.TableKindF32) {
			continue
		}
		if rate < 0 {
			rate = i
			continue
		}
		if entries[rate].Id != e.Id || entries[rate].Key() == e.Key() {
			t.Fatalf("the two rates must share an id and differ in declared shape: %+v %+v", entries[rate], e)
		}
	}
	if rate < 0 {
		t.Fatal("the unit announces no quantized entry")
	}
	own := tablewire.Announce(u)
	repeated := announcementWith(u, append(ir.TableVocabularyBytes(entries), entries[rate].Encode(nil)...))

	// THE GO ENGINE IS THE CONTROL: it takes the compiler's own announcement
	// and refuses the one carrying a byte-identical repeat.
	var vocabulary tablewire.Vocabulary
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(own, &report); err != nil || !vocabulary.Announced() || !report.Silent() {
		t.Fatalf("go: the unit's own announcement was refused: %v %+v", err, report)
	}
	var again tablewire.Vocabulary
	var refused tabletext.Report
	// damage is an answer, not an error: the report says malformed and the
	// vocabulary stays unset
	if err := again.AnnounceRead(repeated, &refused); err != nil || again.Announced() || !refused.Malformed || refused.Refused {
		t.Fatalf("go: a repeated entry must be malformed: %v %+v", err, refused)
	}

	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`#include "ProbeTable.h"
#include <cstdio>
using namespace probe;
#define REQUIRE(x) do { if (!(x)) { fprintf(stderr,"announcement failed at line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
int main()
{
    static_assert( sizeof( TableMessageEntry ) == 96, "the resolved entry is pinned at 96 bytes" );
    // the compiler's own announcement, read back through its own AnnounceRead
    uint8_t own[ kTableAnnounceBytes ];
    REQUIRE( Announce( own, (int64_t) sizeof( own ) ) == (int64_t) sizeof( own ) );
    TableMessageEntry entries[ kTableMessageEntriesHere ];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    TableReport report;
    REQUIRE( AnnounceRead( vocabulary, own, (int64_t) sizeof( own ), &report ) );
    REQUIRE( vocabulary.announced && !report.malformed && !report.refused );
    REQUIRE( vocabulary.count == %d );
    // the two rates: one id, kind 10, one derivation, TWO slots
    const TableMessageEntry & first = TableVocabularyEntryAt( vocabulary, %d );
    const TableMessageEntry & second = TableVocabularyEntryAt( vocabulary, %d );
    REQUIRE( first.id == second.id && first.kind == 10 && second.kind == 10 );
    REQUIRE( first.qmin == second.qmin && first.qdelta == second.qdelta && first.qcount == second.qcount );
    REQUIRE( first.qcount == 10 && first.value_bits == second.value_bits );
    // the control: an entry the wire spells twice, byte for byte, is malformed
    const uint8_t repeated[] = { %s };
    TableMessageEntry room[ kTableMessageEntriesHere + 1 ];
    TableVocabulary twice( room, kTableMessageEntriesHere + 1 );
    TableReport refused;
    REQUIRE( !AnnounceRead( twice, repeated, (int64_t) sizeof( repeated ), &refused ) );
    REQUIRE( !twice.announced && twice.refused && refused.malformed && !refused.refused );
    return 0;
}
`, len(entries), rate+1, rate+2, cppWire(repeated))
	referenceCompileRun(t, files, probe)
}

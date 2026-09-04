// THE MESSAGE FORM's C++ runtime (docs/SPEC-TABLES.md §3.3): the unit's
// announcement as a compile-time constant, the connection table a receiver
// reads it into, and the tolerant read with its one strict check.
//
// A file carries its own id table and a MESSAGE STREAM announces one and then
// carries none, so everything here is about WHERE the table lives. The body's
// framing, its elision rules and every malformed rule of §3 are untouched.
package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableMessageForm emits the shared, per-package half of the message form: the
// form byte, the reserved build-version id, the ANNOUNCEMENT as a constant
// byte array and its length, the connection table, and the three unit-scope
// entry points Announce, AnnounceMeasure and AnnounceRead.
//
// THE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT and the C++
// reference emits it as one, which §3.3 licenses in so many words: every byte
// of it is settled by the compiler, so a walk would compute at run time what
// the emitter already knows. Announce is a copy and AnnounceMeasure is a
// constant.
func tableMessageForm(u *ir.Unit, forceInline string, anyVariable bool) string {
	announcement := ir.TableAnnouncement(u)
	vocabulary := ir.TableVocabulary(u)

	var b strings.Builder
	b.WriteString(`// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): a FILE carries its own id
// table and a MESSAGE STREAM announces one and then carries none.
//
// A form 2 wire is TWO PARTS, the form byte and the root body: the body ends
// at its own zero reference as it does in a file, there is no trailer, and the
// message's last byte is the body's terminator. Its references resolve against
// the CONNECTION's table, which is the unit's whole vocabulary in the order
// the compiler settled.
const uint8_t kTableWireMessageForm = 2;

// The RESERVED build-version id, the second id the language holds back (§5,
// §11), beside the node table's. It is the announcement's one required field,
// and a reserved id in any body but the one whose transport it is, is
// malformed (§3.1).
static const uint64_t kTableBuildVersionFieldId = 0xFFFFFFFFFFFFFFFEull;

`)
	if anyVariable {
		// THE NODE TABLE's OWN SLOT, in a unit that HAS a node table and in no
		// other. The reserved node-table ID rides in every unit, because every
		// reader owes §3.1's refusal of it inside a nested body; the SLOT is
		// the writer's half and only a pointered message ever names it, so a
		// value-only unit carries none of it and the zero-cost gate holds
		// (docs/SPEC-TABLES.md §2, §3.1, §3.3).
		fmt.Fprintf(&b, `// The reserved NODE-TABLE id's own slot in this unit's vocabulary (§3.3). A
// pointered message names the node table through it, exactly as every other
// field header names its id through a slot.
static const uint64_t kTableNodeTableFieldSlot = %d;

`, slotOfIn(vocabulary, ir.TableNodeWireId))
	}

	fmt.Fprintf(&b, `// THE UNIT'S ANNOUNCEMENT, byte for byte: %d entries and %d bytes. It is an
// ordinary form 1 FILE — the form byte, a body carrying the BUILD VERSION
// under the reserved id at kind 9, and the trailer that IS the connection's
// table, slot 1 the reserved id and slots 2 and up the vocabulary under one
// numbering.
//
// The vocabulary is the unit's whole closure in the COOK PROJECTION's order
// (§20.2) — each record in the order the projection renders it and each
// record's fields in the order the projection renders them, then each enum's
// variants and each union's arms — followed by the tail the projection does
// not name: the reserved node-table id, the three blob type ids as bytes,
// string and wstring, and every table's own name id in the projection's sorted
// record order. The tail is UNCONDITIONAL, so an ordinary edit only ever grows
// it at its end and never moves a slot a generated field header carries as a
// literal.
static const int64_t kTableAnnounceBytes = %d;
static const uint8_t kTableAnnounce[ kTableAnnounceBytes ] = {
`, len(vocabulary), len(announcement), len(announcement))
	for i, by := range announcement {
		if i%12 == 0 {
			b.WriteString("    ")
		}
		fmt.Fprintf(&b, "0x%02x,", by)
		if i%12 == 11 || i == len(announcement)-1 {
			b.WriteString("\n")
		} else {
			b.WriteString(" ")
		}
	}
	b.WriteString(`};

// AnnounceMeasure is the announcement's byte count, which is a constant of the
// unit and not a walk.
inline int64_t AnnounceMeasure() { return kTableAnnounceBytes; }

// Announce writes the announcement into the caller's buffer and answers the
// bytes written — exactly AnnounceMeasure's answer — or -1 when the buffer is
// too small. It allocates nothing and walks nothing.
inline int64_t Announce( uint8_t * buffer, int64_t capacity )
{
    if ( buffer == NULL || capacity < kTableAnnounceBytes ) { return -1; }
    memcpy( buffer, kTableAnnounce, (size_t) kTableAnnounceBytes );
    return kTableAnnounceBytes;
}

// TableVocabulary is ONE DIRECTION of ONE CONNECTION's id table (§3.3): the
// entries an announcement carried, whole, under one numbering with slot 1 the
// reserved build-version id.
//
// A peer holds TWO of these for a connection, the one it writes with and the
// one it reads with, and neither is the other's. A restart opens a fresh
// connection with empty tables and nothing is cached across connections, so
// its whole life is one connection's. It BORROWS the announcement's bytes rather than
// copying them, so a receiver holds one table a direction and its memory is
// the bound below and nothing else.
struct TableVocabulary
{
    // THE CONFORMING DEFAULT BOUND (§3.3): 32 KiB a direction, eight times the
    // 500-id unit that is already a large one. A connection's table is bounded
    // by nothing the wire carries, so the receiver declares the maximum and an
    // announcement above it is refused by name before an entry is touched.
    static const int64_t kDefaultMaxEntries = 4096;

    TableIdTable table;
    uint64_t build_version = 0;
    bool announced = false;
    int64_t max_entries = kDefaultMaxEntries;
};

// AnnounceRead reads an announcement into one direction's table (§3.3).
//
// THE BOUND IS CHECKED BEFORE ANYTHING IS ALLOCATED: the entry count is a
// fixed little-endian u64 at the end, so a receiver reads it, compares it and
// refuses without touching an entry. After that it is §3's ordinary FILE read,
// because the announcement IS a file, with EXACTLY ONE STRICT CHECK over its
// body: the reserved build-version field present, exactly once, under kind 9,
// eight bytes wide. Everything else is an ordinary field under §4's tolerance,
// so an unknown one is skipped and counted and the announcement can GAIN a
// field in a later minor without a lockstep redeploy.
//
// The FIRST announcement sets the table and it is the only one that can. A
// SECOND is refused by name: it does not replace the table, it does not amend
// it and it changes nothing. A refused announcement sets NO TABLE.
inline bool AnnounceRead( TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )
{
    TableReport ignored;
    TableReport * to = report != NULL ? report : &ignored;
    if ( vocabulary.announced )
    {
        to->refused = true;
        to->reason = second_announcement;
        return false;
    }
    if ( bytes < 1 ) { to->malformed = true; return false; }
    if ( buffer[0] != kTableWireForm )
    {
        to->refused = true;
        to->reason = buffer[0] == kTableWireMessageForm ? message_form_as_file : newer_form;
        return false;
    }
    if ( bytes < 9 ) { to->malformed = true; return false; }
    const uint8_t * tail = buffer + bytes - 8;
    uint64_t lo = uint64_t( tail[0] ) | uint64_t( tail[1] ) << 8 | uint64_t( tail[2] ) << 16 | uint64_t( tail[3] ) << 24;
    uint64_t hi = uint64_t( tail[4] ) | uint64_t( tail[5] ) << 8 | uint64_t( tail[6] ) << 16 | uint64_t( tail[7] ) << 24;
    if ( ( lo | ( hi << 32 ) ) > (uint64_t) vocabulary.max_entries )
    {
        to->refused = true;
        to->reason = vocabulary_too_large;
        return false;
    }
    TableIdTable table;
    int64_t body_bytes = 0;
    const TableOpenVerdict verdict = TableOpen( buffer, bytes, table, body_bytes );
    if ( verdict != TableOpenOk )
    {
        if ( verdict == TableOpenDamaged ) { to->malformed = true; }
        else { to->refused = true; to->reason = newer_form; }
        return false;
    }
    if ( TableBodyEndsEarly( buffer + 1, body_bytes, table ) ) { to->malformed = true; return false; }
    // the body, under §4's tolerance and this form's one strict check
    TableReader r( buffer + 1, body_bytes, to, &table );
    uint64_t version = 0;
    int32_t seen = 0;
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.getleb( ref ) ) { to->malformed = true; return false; }
        if ( ref == 0 ) { break; }
        if ( ref > (uint64_t) table.count || !r.has( 1 ) ) { to->malformed = true; return false; }
        const uint64_t id = table.at( ref );
        const uint8_t kind = r.get8();
        if ( id != kTableBuildVersionFieldId )
        {
            to->unknown++;
            if ( !r.skip( kind ) ) { to->malformed = true; return false; }
            continue;
        }
        if ( kind != 9 || !r.has( 8 ) ) { to->refused = true; to->reason = no_vocabulary; return false; }
        version = r.get64();
        seen++;
    }
    if ( seen != 1 ) { to->refused = true; to->reason = no_vocabulary; return false; }
    vocabulary.table = table;
    vocabulary.build_version = version;
    vocabulary.announced = true;
    return true;
}

`)
	_ = forceInline
	return b.String()
}

// slotOfIn is one id's slot in a vocabulary, counted from 1.
func slotOfIn(vocabulary []uint64, id uint64) uint64 {
	for i, have := range vocabulary {
		if have == id {
			return uint64(i + 1)
		}
	}
	return 0
}

// emitMessageEntries emits one ROOT's message-form surface (§3.3): the three
// suffixes MeasureMessage, SaveMessage and LoadMessage, beside the file form's
// Measure, Save and Load.
//
// A message body is measured and written by the SAME walk a file's is — the
// only difference is where a reference comes from, and the walk carries both
// the id and its compile-time slot at every header — so nothing here duplicates
// a codec.
func (g *tableGen) emitMessageEntries(st *ir.Struct) {
	if g.isVar(st.Name) || st.IsMapEntry() {
		// a variable-length root's message surface takes a region root rather
		// than a value, and is emitted with the rest of that surface
		return
	}
	n := st.Name
	g.pf("// The MESSAGE FORM (docs/SPEC-TABLES.md §3.3): the form byte and the root\n")
	g.pf("// body, and no trailer at all — the connection's announced table is where\n")
	g.pf("// the ids live. Every reference is a compile-time SLOT, so this walk does\n")
	g.pf("// no lookup and a save costs what a save costs.\n")
	g.pf("inline int64_t %sMeasureMessage( const %s & value )\n{\n", n, n)
	g.pf("    TableIds ids;\n")
	g.pf("    ids.vocabulary = true;\n")
	g.pf("    const int64_t body = %sMeasureBody( ids, value );\n", n)
	g.pf("    if ( body < 0 ) { return -1; }\n")
	g.pf("    return 1 + body;\n}\n\n")
	g.pf("inline int64_t %sSaveMessage( const %s & value, uint8_t * buffer, int64_t capacity )\n{\n", n, n)
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    TableIds ids;\n")
	g.pf("    ids.vocabulary = true;\n")
	g.pf("    w.put8( kTableWireMessageForm );\n")
	g.pf("    if ( !%sSaveBody( w, ids, value ) || w.overflow ) { return -1; }\n", n)
	g.pf("    return w.offset; // == %sMeasureMessage( value )\n}\n\n", n)
	g.pf("// A form 2 message with NO TABLE for the connection is REFUSED BY NAME:\n")
	g.pf("// nothing is decoded, the reader says it holds no table, no counter moves\n")
	g.pf("// and malformed does not fire. A reader does not fall back to the file\n")
	g.pf("// form on its own and does not guess a table, because a guessed table\n")
	g.pf("// decodes a body under the wrong names in silence.\n")
	g.pf("inline bool %sLoadMessage( %s & value, const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * to = report != NULL ? report : &ignored;\n")
	g.pf("    %sReset( value );\n", n)
	g.pf("    if ( bytes < 1 ) { to->malformed = true; return false; }\n")
	g.pf("    if ( buffer[0] != kTableWireMessageForm ) { to->refused = true; to->reason = newer_form; return false; }\n")
	g.pf("    if ( !vocabulary.announced ) { to->refused = true; to->reason = no_vocabulary; return false; }\n")
	g.pf("    // a form 2 wire has NO TRAILER, so the body runs to the last byte and\n")
	g.pf("    // there is no stray-byte rule to apply between one and a first entry\n")
	g.pf("    TableReader r( buffer + 1, bytes - 1, to, &vocabulary.table );\n")
	g.pf("    r.nested = false; // the ROOT body, the one that may carry a node table\n")
	g.pf("    return %sLoadBody( r, value );\n}\n\n", n)
}

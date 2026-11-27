// CELL cpp/W7 — "arg and meta are two lanes" (docs/roadmap.sexp, row
// union-guards, audit schema#898 / matrix schema#876).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:240): "`arg` and `meta` ARE TWO LANES
// BECAUSE THEY ARE TWO FACTS, and must never share one. (fix 12) A `string(N)`
// under a union arm needs both, and one lane gives whichever was stamped
// last: a text string under arm `3` read as BYTES, or an entry that runs only
// when the tag equals the flavour — under the wrong arm. Neither is visible to
// this build's own records; only to a peer's." The plan entry spells the two
// facts apart: `arg` IS THE GUARD'S ORDINAL and nothing else; `meta` IS THE
// OP'S OWN ARGUMENT (a text flavour: 1 utf8, 3 bytes) — the same
// paragraph's struct, docs/FIXED-FORM-ALGORITHM.md §4.1.
//
// WHY THIS FIXTURE (tables/backend/Backend.schema, the Envelope root): a
// union of three fixed tables whose arms carry a `bytes(32)` (LoginRequest.
// session_token) and a `string(32)` (StorePurchase.sku) as their text. The
// ordinal and the flavour DISAGREE at both text arms, which is the exact shape
// one lane cannot hold (the FM1/FU1 lesson, stated at test/tables/FM1.schema
// and test/tables/FU1.schema):
//
//   session_token  arm 1 (Login),    flavour 3 (bytes)  — arg 1, meta 3
//   sku            arm 3 (Purchase), flavour 1 (utf8)   — arg 3, meta 1
//
// Stamp one lane with "whichever was last" and either the guard becomes the
// flavour (session_token runs under arm 3 — under the wrong arm, and the login
// record reads empty) or the flavour becomes the guard's ordinal (session_token
// reads as utf8 and terminates the byte string, sku reads as bytes and leaves
// the C string unterminated). Both are visible on THIS build's own records the
// moment the baked identity plan is read back and a record is round-tripped
// through the production reader — the law says "only to a peer's" of the
// BUG, not of the assertion.
//
// WHAT THIS TEST DOES:
//   1. Holds EnvelopeFixedPlan's two kTableFixedText entries to the law: the
//      login arm's entry carries arg=Login and meta=kTableFixedTextBytes, the
//      purchase arm's carries arg=Purchase and meta=kTableFixedTextUtf8 — the
//      two facts disagree at both entries, and arg (u64) and meta (u8) are two
//      fields, never one. This is the generated-code constant the law governs.
//   2. Round-trips one record per text arm through the production writer and
//      the production reader (EnvelopeFixedSave / EnvelopeFixedLoad), each
//      text at its FULL used length (32 of 32), so the flavour's terminator
//      write lands one byte past the payload and is observable:
//        - login / session_token (meta=bytes, uint8_t[32]): NO terminator is
//          written. If meta were utf8 the terminator would land at
//          session_token + 32 — the low byte of session_token_length, which
//          the op stored first — and the length would read 0, not 32.
//        - purchase / sku (meta=utf8, char[33]): the terminator IS written at
//          sku[32], which the destination carries poisoned 0x7F before the
//          read. If meta were bytes the poison would stand.
//
//      THE LAW MOVED for the old vector (docs/FIXED-FORM-ALGORITHM.md:449;
//      docs/rulings.json strict-only-read): the row once STAINED the text's
//      slack on the wire 0xFF after the save to observe the terminator. Those
//      bytes are DEAD, and a non-zero dead byte now refuses the whole load
//      wire_damaged (W4's row), so the observation moved to the full-length
//      text above. The stained wire is kept as its own case: it must refuse.
//
// THE CONTROL (STEP 4, performed against build/tables-generated/, never this
// file): flip the ONE generated-code constant the law governs — session_token's
// plan-entry meta, 3 -> 1, the one-lane failure "a byte string read as utf8" —
// and this binary goes RED on both the plan assertion and the round-trip byte.
// Restore the constant: GREEN. (Breaking arg 1 -> 3 reddens the arg assertion
// and leaves the login record's session_token at its prefill: the entry runs
// under the wrong arm.)
//
// The wire is built by the generated writer (the conformance corpus has no
// fixed-form rows — W4's note holds here too). The text run sits at the
// fixed-form body's body + src + 4, size 32 (the stained case writes its dead
// bytes there). src 9 / payload 13 / size 32 is
// EnvelopeFixedPlan's own text row for both arms (they share the wire offsets
// and differ in guard ordinal and flavour — that is the point).
#include <cstddef>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "BackendTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) failures++;
}

using backenddemo::Envelope;
using backenddemo::EnvelopeFixedLoad;
using backenddemo::EnvelopeFixedMeasure;
using backenddemo::EnvelopeFixedPlan;
using backenddemo::EnvelopeFixedPlanCount;
using backenddemo::EnvelopeFixedSave;
using backenddemo::EnvelopeFixedLayoutBytes;
using backenddemo::LoginRequest;
using backenddemo::PayloadType;
using backenddemo::StorePurchase;
using backenddemo::TableFixedEntry;
using backenddemo::TableReport;
using backenddemo::kTableFixedHeaderBytes;
using backenddemo::kTableFixedText;
using backenddemo::kTableFixedTextBytes;
using backenddemo::kTableFixedTextUtf8;

// one record's body, after the fixed-form framing: header + 4 + layout + hash
static const uint8_t * body_of( const std::vector<uint8_t> & wire )
{
    return wire.data() + kTableFixedHeaderBytes + 4 + EnvelopeFixedLayoutBytes + 8;
}

// read one record off `wire` into `out`; returns the load's verdict. sku[32],
// the one byte past a full sku, is poisoned 0x7F so the utf8 terminator's
// store is visible.
static int64_t read_one( const std::vector<uint8_t> & wire, Envelope & out, TableReport & r )
{
    backenddemo::EnvelopeReset( out );
    out.payload.purchase.sku[32] = (char) 0x7F;
    std::vector<TableFixedEntry> plan( 8192 );
    return EnvelopeFixedLoad( &out, 1, wire.data(), (int64_t) wire.size(),
                              plan.data(), (int32_t) plan.size(), NULL, &r );
}

// one Envelope record on the wire, its text at the FULL used length (32 of 32)
// so a terminator store lands one byte past the payload. `login` selects the
// arm: login/session_token (bytes flavour) or purchase/sku (utf8 flavour).
// `stain` writes 0xFF over a SHORT text's dead bytes on the wire instead (the
// old vector, now damage).
static std::vector<uint8_t> build_wire( bool login, bool stain = false )
{
    const int32_t used = stain ? 4 : 32;
    Envelope value;
    backenddemo::EnvelopeReset( value );
    if ( login )
    {
        value.payload.type = PayloadType::Login;
        value.payload.login = LoginRequest();
        value.payload.login.player_id = 0x1111111111111111ull;
        value.payload.login.session_token_length = used;
        for ( int i = 0; i < 32; ++i ) { value.payload.login.session_token[i] = 0xFF; }
        value.payload.login.client_build = 0x22222222u;
    }
    else
    {
        value.payload.type = PayloadType::Purchase;
        value.payload.purchase = StorePurchase();
        value.payload.purchase.player_id = 0x3333333333333333ull;
        value.payload.purchase.sku_length = used;
        for ( int i = 0; i < 32; ++i ) { value.payload.purchase.sku[i] = 'k'; }
        value.payload.purchase.sku[0] = 's';
        value.payload.purchase.sku[1] = 'k';
        value.payload.purchase.sku[2] = 'u';
        value.payload.purchase.sku[3] = '!';
        value.payload.purchase.sku[32] = 0;
        value.payload.purchase.quantity = 5;
        value.payload.purchase.price_minor = 0x44444444u;
    }
    std::vector<uint8_t> wire( (size_t) EnvelopeFixedMeasure( 1 ) );
    if ( EnvelopeFixedSave( &value, 1, wire.data(), (int64_t) wire.size() ) != (int64_t) wire.size() )
    {
        std::printf( "FAIL W7: the record does not save\n" );
        failures++;
        return wire;
    }
    if ( stain )
    {
        // THE OLD VECTOR: 0xFF over the text's dead bytes 4..31 at body + 13.
        std::memset( (uint8_t *) body_of( wire ) + 13 + 4, 0xFF, 32 - 4 );
    }
    return wire;
}
int main()
{
    // ---- 1. THE PLAN CONSTANTS: arg and meta are two lanes ------------------
    //
    // TableFixedEntry lays arg (u64, the guard's ordinal) and meta (u8, the
    // text flavour) in two separate fields — the two lanes the law names. A
    // ten-value aggregate that shared one would have to choose between "which
    // arm am I guarded by" and "which flavour of text am I" for a string(N)
    // under an arm, and the FM1/FU1 fixtures record what that choice cost.
    check( offsetof( TableFixedEntry, arg ) != offsetof( TableFixedEntry, meta ),
           "W7: arg and meta are two lanes (two fields, never one)" );
    check( sizeof( ( (TableFixedEntry *) nullptr )->arg ) == 8 && sizeof( ( (TableFixedEntry *) nullptr )->meta ) == 1,
           "W7: arg is full width (bill §12.7) and meta is the op's own byte" );

    int text_entries = 0;
    bool saw_login_text = false;
    bool saw_purchase_text = false;
    for ( int32_t i = 0; i < EnvelopeFixedPlanCount; ++i )
    {
        const TableFixedEntry & p = EnvelopeFixedPlan[i];
        if ( p.op != kTableFixedText ) { continue; }
        text_entries++;
        check( p.guard == 0, "W7: a text entry under an arm guards on the payload tag (offset 0)" );
        if ( p.arg == (uint64_t) PayloadType::Login )
        {
            saw_login_text = true;
            // arm 1 (Login), flavour 3 (bytes): the ordinal and the flavour
            // DISAGREE — one lane cannot hold both facts.
            check( p.meta == (uint8_t) kTableFixedTextBytes,
                   "W7: session_token carries the Login ordinal in arg AND the bytes flavour in meta (two lanes)" );
            check( p.arg != (uint64_t) p.meta,
                   "W7: session_token's guard ordinal and flavour disagree (one lane could not hold both)" );
        }
        else if ( p.arg == (uint64_t) PayloadType::Purchase )
        {
            saw_purchase_text = true;
            // arm 3 (Purchase), flavour 1 (utf8): the ordinal and the flavour
            // DISAGREE at this arm too.
            check( p.meta == (uint8_t) kTableFixedTextUtf8,
                   "W7: sku carries the Purchase ordinal in arg AND the utf8 flavour in meta (two lanes)" );
            check( p.arg != (uint64_t) p.meta,
                   "W7: sku's guard ordinal and flavour disagree (one lane could not hold both)" );
        }
    }
    check( text_entries == 2, "W7: EnvelopeFixedPlan has exactly two text entries (one per text arm)" );
    check( saw_login_text && saw_purchase_text, "W7: both text arms' entries are on the plan" );

    // ---- 2. THE ROUND TRIP: each lane does its own job ----------------------
    //
    // Login / session_token — meta=bytes: no terminator. At the full used
    // length a utf8 terminator would land on session_token_length's low byte,
    // which the op stored first, and the length would read 0. The arg lane is
    // what runs this entry under tag=Login only.
    {
        const std::vector<uint8_t> wire = build_wire( true );
        Envelope out;
        TableReport r;
        const int64_t n = read_one( wire, out, r );
        // strict-only-read: a kind mismatch, an out-of-range value and a repeat now refuse the
        // whole load (no counter), so the retired zero counts read as not refused, not damaged.
        check( n == 1 && !r.refused && !r.damaged,
               "W7: the login record reads clean" );
        check( out.payload.type == PayloadType::Login, "W7: the login tag lands (arg's guard matched)" );
        check( out.payload.login.player_id == 0x1111111111111111ull, "W7: login.player_id lands" );
        check( out.payload.login.session_token[0] == 0xFF && out.payload.login.session_token[31] == 0xFF,
               "W7: session_token's used units land" );
        // THE FLAVOUR'S BITE: was "the stained slack byte at the used length
        // stays 0xFF"; now the byte past a full token is the length's own.
        check( out.payload.login.session_token_length == 32,
               "W7: bytes flavour writes no terminator — session_token_length stands at 32 (meta=bytes, two lanes)" );
        check( out.payload.login.client_build == 0x22222222u, "W7: the login arm's neighbour after the text lands" );
    }

    // Purchase / sku — meta=utf8: the terminator IS written at the used length,
    // sku[32], over the destination's 0x7F poison. meta=bytes would leave it.
    {
        const std::vector<uint8_t> wire = build_wire( false );
        Envelope out;
        TableReport r;
        const int64_t n = read_one( wire, out, r );
        // strict-only-read: a kind mismatch, an out-of-range value and a repeat now refuse the
        // whole load (no counter), so the retired zero counts read as not refused, not damaged.
        check( n == 1 && !r.refused && !r.damaged,
               "W7: the purchase record reads clean" );
        check( out.payload.type == PayloadType::Purchase, "W7: the purchase tag lands (arg's guard matched)" );
        check( out.payload.purchase.player_id == 0x3333333333333333ull, "W7: purchase.player_id lands" );
        check( out.payload.purchase.sku_length == 32, "W7: sku_length lands under the Purchase arm (arg=3)" );
        check( out.payload.purchase.sku[0] == 's' && out.payload.purchase.sku[1] == 'k' &&
               out.payload.purchase.sku[2] == 'u' && out.payload.purchase.sku[3] == '!' &&
               out.payload.purchase.sku[31] == 'k',
               "W7: sku's used units land" );
        // THE FLAVOUR'S BITE: meta=utf8 terminates at the used length.
        check( out.payload.purchase.sku[32] == 0,
               "W7: utf8 flavour writes its terminator (meta=utf8, arg and meta are two lanes)" );
        check( out.payload.purchase.quantity == 5, "W7: the purchase arm's neighbour after the text lands" );
    }

    // THE OLD VECTOR: a short text with its dead bytes stained 0xFF on the wire.
    // strict-only-read: was "reads clean, the stain is copied and not acted on";
    // a non-zero dead byte now refuses the whole load wire_damaged
    // (FIXED-FORM-ALGORITHM.md:449, §5.3 step 11), on both arms.
    for ( int arm = 0; arm < 2; ++arm )
    {
        const std::vector<uint8_t> wire = build_wire( arm == 0, true );
        Envelope out;
        TableReport r;
        const int64_t n = read_one( wire, out, r );
        check( n == -1 && ( r.refused || r.damaged ) && r.refuse_reason == backenddemo::wire_damaged,
               arm == 0 ? "W7: the login record with stained dead bytes refuses wire_damaged"
                        : "W7: the purchase record with stained dead bytes refuses wire_damaged" );
    }

    return failures == 0 ? 0 : 1;
}

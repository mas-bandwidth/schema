// cards.hpp — the owning facade for the generated Card unit (tables/cards).
// A port of the Go facade in tables/cards/cards.go (docs/SPEC-TABLES.md
// §2.5, §2.9, §6.2): a Record is a value type owning a defensive copy of its
// wire bytes and the measured, aligned region that backs the loaded card. No
// generated Card* and no shallow copy of a relative pointer escapes.
#ifndef CARDS_HPP
#define CARDS_HPP

#include <cstdint>
#include <cstddef>
#include <string>
#include <vector>

#include "CardTable.h"

namespace cards {

// The customer ceilings (docs/SPEC-TABLES.md §2.5, §2.9), declared in the Go
// facade at tables/cards/cards.go:
//   MaxBodyBytes = 64 << 10
//   MaxListEntries = 512
inline constexpr int64_t kMaxBodyBytes = 64 << 10;
inline constexpr int64_t kMaxListEntries = 512;

struct Record {
    std::vector<uint8_t> wire;   // the caller's bytes, copied
    std::vector<uint8_t> region; // measured, aligned temporary region
    const Card * root = nullptr; // into region; never escapes the Record
    TableReport report;
};

// Load reads wire into a Record using the generated measured/aligned region
// load (§6.2). Failures and malformed wire are errors; tolerant events ride in
// the returned report. The ORIGINAL wire is copied, never rewritten.
bool CardLoad(const uint8_t * wire, size_t len, Record & out, TableReport & report);

// FromJSON builds a Record from JSON text through the generated CardBuilder and
// CardFromJson (§16), then the generated save. Unknown, mismatched, clamped and
// malformed inputs are refused, as are the customer bounds. Bytes are published
// only on complete success; a refused write mutates nothing.
bool CardFromJSON(const char * text, size_t len, Record & out, TableReport & report);

// CardFromJSON is the same write, and refuse_class is the conformance reason
// class (tables/cards/conformance/FORMAT.md) on refusal, empty on success
// (docs/SPEC-TABLES.md §2.5, §2.9, §16).
bool CardFromJSON(const char * text, size_t len, Record & out, TableReport & report, std::string & refuse_class);

// ToJSON writes the record's known-field JSON text. It is the known-field view,
// not a lossless archival rewrite (§16).
bool CardToJSON(const Record & in, std::vector<char> & out, TableReport & report);

// Wire returns a defensive copy of the record's wire bytes.
std::vector<uint8_t> CardWire(const Record & in);

// CheckCard enforces the customer bounds and the well-formedness of the opaque
// body and every collection element before bytes are published (§2.5, §2.9).
bool CheckCard(const Card * root, bool & over_body, std::string & over_list);

// Parse reads the admitted brief text and publishes a Record only on complete
// checked success (tables/cards/parse.go; docs/SPEC-TABLES.md §2, §6.2). The
// body is the exact suffix after the last header line's terminator. On refusal
// refuse_class names the conformance reason class (FORMAT.md), empty on success.
bool Parse(const char * text, size_t len, Record & out, std::string & refuse_class);

// Render reads the record's wire into the informational text view: RESULT first,
// the mapped keys in the fixed documented order, extras in stored order, then
// the body bytes verbatim (tables/cards/render.go; docs/SPEC-TABLES.md §6.2, §16).
bool Render(const Record & in, std::vector<char> & out, TableReport & report);

// MaxRetainBytes and MaxRetainIDs are the customer ceilings on the two
// caller-owned retention stores (docs/SPEC-TABLES.md §6.6). They never grow.
inline constexpr int64_t kMaxRetainBytes = 1 << 20;
inline constexpr int32_t kMaxRetainIDs = 4096;

// Upgrade rewrites wire through the generated retaining path so every field
// the current declaration cannot name is kept (tables/cards/upgrade.go;
// docs/SPEC-TABLES.md §6.6; tla/CardVersioning.tla NoFieldLost, BodyUnchanged,
// RefusalKeepsOriginal). On loss or a refused wire it returns false, leaves
// out empty, and names the conformance class. The input bytes are never written.
// UpgradeRetain is the same path with caller-owned store sizes, so a probe can
// show a short id list names retain-lost.
bool Upgrade(const uint8_t * wire, size_t len, std::vector<uint8_t> & out, TableReport & report, std::string & refuse_class);
bool UpgradeRetain(const uint8_t * wire, size_t len, int64_t retain_bytes, int32_t retain_ids,
                   std::vector<uint8_t> & out, TableReport & report, std::string & refuse_class);

// ReadStrict loads wire and refuses any data the current declaration cannot
// support (docs/SPEC-TABLES.md §4). A safe Widened event does not refuse. On
// rejection out holds no usable record.
bool ReadStrict(const uint8_t * wire, size_t len, Record & out, std::string & refuse_class);

// Sha256 is FIPS 180-4. out is 32 bytes.
void Sha256(const uint8_t * data, size_t len, uint8_t out[32]);

// TaskHash digests the task fields — body, paths, new, shared, test, done —
// length-prefixed so boundaries cannot collide (tables/cards/edit.go). A nil
// or lossy record is refused and out is zero. CARDS_CPP_HASH_NEEDS=1 appends
// the needs list so the conformance hash surface alone goes red.
bool TaskHash(const Record & rec, uint8_t out[32], std::string & refuse_class);

// Field edits validate, rebuild through a fresh builder, and publish only
// after lock, save, reload and CheckCard succeed (docs/SPEC-TABLES.md §2.9,
// §6.2; tla/CardHops.tla EditDropsUnknown). A refusal leaves rec.wire unchanged
// and names the conformance class.
bool SetNeeds(Record & rec, const std::vector<std::string> & needs, std::string & refuse_class);
bool SetPaths(Record & rec, const std::vector<std::string> & paths, std::string & refuse_class);
bool SetTier(Record & rec, Tier tier, std::string & refuse_class);
bool SetPriority(Record & rec, Priority priority, std::string & refuse_class);
bool SetColumn(Record & rec, Column column, std::string & refuse_class);
bool SetHolder(Record & rec, const std::string & holder, std::string & refuse_class);

// WriteView walks UnitView and spells every field of every table
// (tables/cards/conformance/FORMAT.md; docs/SPEC-TABLES.md §8).
std::string WriteView();

} // namespace cards

#endif

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

} // namespace cards

#endif

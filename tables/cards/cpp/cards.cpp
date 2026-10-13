// cards.cpp — the owning facade (tables/cards/cards.go, docs/SPEC-TABLES.md
// §2.5, §2.9, §6.2). A port of the Go facade: Load, FromJSON, ToJSON and Wire
// over the generated Card table unit, with the customer bounds enforced before
// any record is published.
#include "cards.hpp"

#include <cstring>
#include <string>
#include <vector>

namespace cards {

namespace {

bool isUtf8WithoutNul(const char * s, size_t len) {
    size_t i = 0;
    while (i < len) {
        unsigned char c = (unsigned char)s[i];
        if (c == 0) return false;
        if (c < 0x80) { i += 1; continue; }
        if ((c & 0xE0) == 0xC0) {
            if (i + 1 >= len) return false;
            if (((unsigned char)s[i+1] & 0xC0) != 0x80) return false;
            i += 2; continue;
        }
        if ((c & 0xF0) == 0xE0) {
            if (i + 2 >= len) return false;
            for (int k = 1; k <= 2; k++)
                if (((unsigned char)s[i+k] & 0xC0) != 0x80) return false;
            i += 3; continue;
        }
        if ((c & 0xF8) == 0xF0) {
            if (i + 3 >= len) return false;
            for (int k = 1; k <= 3; k++)
                if (((unsigned char)s[i+k] & 0xC0) != 0x80) return false;
            i += 4; continue;
        }
        return false;
    }
    return true;
}

bool validString(const char * s, size_t len) {
    return isUtf8WithoutNul(s, len);
}

// CheckCard (docs/SPEC-TABLES.md §2.5, §2.9): the customer ceilings and the
// well-formedness of the opaque body and every collection element, enforced
// before bytes are published.
bool fieldListOk(const TableList<PathText> & list, int64_t ceiling) {
    if (list.count < 0 || (int64_t)list.count > ceiling) return false;
    for (int32_t i = 0; i < list.count; i++) {
        const PathText & e = list[i];
        if (!validString(e.s, (size_t)(e.s_length < 0 ? 0 : e.s_length))) return false;
    }
    return true;
}
bool idListOk(const TableList<CardID> & list, int64_t ceiling) {
    if (list.count < 0 || (int64_t)list.count > ceiling) return false;
    for (int32_t i = 0; i < list.count; i++) {
        const CardID & e = list[i];
        if (!validString(e.s, (size_t)(e.s_length < 0 ? 0 : e.s_length))) return false;
    }
    return true;
}
bool extraListOk(const TableList<KeyValue> & list, int64_t ceiling) {
    if (list.count < 0 || (int64_t)list.count > ceiling) return false;
    for (int32_t i = 0; i < list.count; i++) {
        const KeyValue & e = list[i];
        if (!validString(e.key, (size_t)(e.key_length < 0 ? 0 : e.key_length))) return false;
        if (!e.value.null()) {
            TableStringView v = TableStringAt(e.value);
            if (!validString(v.data, v.length)) return false;
        }
    }
    return true;
}
bool consumerListOk(const TableList<Consumer> & list, int64_t ceiling) {
    if (list.count < 0 || (int64_t)list.count > ceiling) return false;
    for (int32_t i = 0; i < list.count; i++) {
        const Consumer & e = list[i];
        if (!validString(e.kind, (size_t)(e.kind_length < 0 ? 0 : e.kind_length))) return false;
        if (!validString(e.who, (size_t)(e.who_length < 0 ? 0 : e.who_length))) return false;
        if (!validString(e.route, (size_t)(e.route_length < 0 ? 0 : e.route_length))) return false;
        if (!validString(e.model, (size_t)(e.model_length < 0 ? 0 : e.model_length))) return false;
        if (!validString(e.outcome, (size_t)(e.outcome_length < 0 ? 0 : e.outcome_length))) return false;
        if (!validString(e.line, (size_t)(e.line_length < 0 ? 0 : e.line_length))) return false;
    }
    return true;
}

} // namespace

bool CheckCard(const Card * root, bool & over_body, std::string & over_list) {
    over_body = false;
    over_list.clear();
    if (root == nullptr) return false;
    if (!root->body.null()) {
        TableStringView v = TableStringAt(root->body);
        if (v.length > kMaxBodyBytes) { over_body = true; return false; }
        if (!validString(v.data, v.length)) return false;
    }
    if (!fieldListOk(root->paths, kMaxListEntries)) { over_list = "paths"; return false; }
    if (!fieldListOk(root->_new, kMaxListEntries)) { over_list = "_new"; return false; }
    if (!fieldListOk(root->shared, kMaxListEntries)) { over_list = "shared"; return false; }
    if (!idListOk(root->needs, kMaxListEntries)) { over_list = "needs"; return false; }
    if (!extraListOk(root->extra, kMaxListEntries)) { over_list = "extra"; return false; }
    if (!consumerListOk(root->consumers, kMaxListEntries)) { over_list = "consumers"; return false; }
    return true;
}

bool CardLoad(const uint8_t * wire, size_t len, Record & out, TableReport & report) {
    int64_t n = CardLoadMeasure(wire, (int64_t)len);
    if (n < 0) {
        int64_t attribution = 0;
        TableRefuseReason reason;
        int64_t m = CardLoadMeasure(wire, (int64_t)len, &attribution, &reason);
        (void)m; (void)reason;
        return false;
    }
    std::vector<uint8_t> raw((size_t)n + 63);
    uintptr_t addr = reinterpret_cast<uintptr_t>(raw.data());
    size_t off = (64 - (addr & 63)) & 63;
    uint8_t * aligned = raw.data() + off;
    const Card * root = CardLoad(aligned, n, wire, (int64_t)len, &report);
    if (root == nullptr) return false;
    bool over_body = false;
    std::string over_list;
    if (!CheckCard(root, over_body, over_list)) return false;
    out.wire.assign(wire, wire + len);
    out.region.swap(raw);
    out.root = root;
    return true;
}

bool CardFromJSON(const char * text, size_t len, Record & out, TableReport & report) {
    CardBuilder b;
    if (!CardFromJson(b, text, (int64_t)len, &report)) return false;
    if (report.malformed) return false;
    if (report.unknown > 0 || report.kind_mismatch > 0 || report.clamped > 0) return false;
    if (!b.Lock()) return false;
    const Card * root = b.AsConst();
    int64_t size = CardMeasure(root);
    if (size < 0) return false;
    std::vector<uint8_t> wire((size_t)size);
    if (CardSave(root, wire.data(), size) != size) return false;
    // Re-load the saved wire into a region form so the bounds check walks the
    // same self-relative layout the facade owns (docs/SPEC-TABLES.md §6.2).
    int64_t n = CardLoadMeasure(wire.data(), size);
    if (n < 0) return false;
    std::vector<uint8_t> raw((size_t)n + 63);
    uintptr_t addr = reinterpret_cast<uintptr_t>(raw.data());
    size_t off = (64 - (addr & 63)) & 63;
    uint8_t * aligned = raw.data() + off;
    TableReport loadReport;
    const Card * own = CardLoad(aligned, n, wire.data(), size, &loadReport);
    if (own == nullptr) return false;
    bool over_body = false;
    std::string over_list;
    if (!CheckCard(own, over_body, over_list)) return false;
    out.wire.swap(wire);
    out.region.swap(raw);
    out.root = own;
    return true;
}

bool CardToJSON(const Record & in, std::vector<char> & out, TableReport & report) {
    (void)report;
    if (in.root == nullptr) return false;
    int64_t size = CardToJsonMeasure(in.root);
    if (size < 0) return false;
    out.resize((size_t)size);
    if (CardToJson(in.root, out.data(), size) != size) return false;
    return true;
}

std::vector<uint8_t> CardWire(const Record & in) {
    return in.wire;
}

} // namespace cards

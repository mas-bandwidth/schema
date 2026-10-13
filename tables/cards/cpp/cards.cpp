// cards.cpp — the owning facade (tables/cards/cards.go, docs/SPEC-TABLES.md
// §2.5, §2.9, §6.2). A port of the Go facade: Load, FromJSON, ToJSON and Wire
// over the generated Card table unit, with the customer bounds enforced before
// any record is published.
#include "cards.hpp"

#include <cstring>
#include <map>
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

bool CardFromJSON(const char * text, size_t len, Record & out, TableReport & report, std::string & refuse_class) {
    refuse_class.clear();
    CardBuilder b;
    if (!CardFromJson(b, text, (int64_t)len, &report)) { refuse_class = "malformed"; return false; }
    if (report.malformed) { refuse_class = "malformed"; return false; }
    if (report.unknown > 0 || report.kind_mismatch > 0 || report.clamped > 0) { refuse_class = "malformed"; return false; }
    if (!b.Lock()) { refuse_class = "malformed"; return false; }
    const Card * root = b.AsConst();
    int64_t size = CardMeasure(root);
    if (size < 0) { refuse_class = "malformed"; return false; }
    std::vector<uint8_t> wire((size_t)size);
    if (CardSave(root, wire.data(), size) != size) { refuse_class = "malformed"; return false; }
    // Re-load the saved wire into a region form so the bounds check walks the
    // same self-relative layout the facade owns (docs/SPEC-TABLES.md §6.2).
    int64_t n = CardLoadMeasure(wire.data(), size);
    if (n < 0) { refuse_class = "malformed"; return false; }
    std::vector<uint8_t> raw((size_t)n + 63);
    uintptr_t addr = reinterpret_cast<uintptr_t>(raw.data());
    size_t off = (64 - (addr & 63)) & 63;
    uint8_t * aligned = raw.data() + off;
    TableReport loadReport;
    const Card * own = CardLoad(aligned, n, wire.data(), size, &loadReport);
    if (own == nullptr) { refuse_class = "malformed"; return false; }
    bool over_body = false;
    std::string over_list;
    if (!CheckCard(own, over_body, over_list)) {
        if (over_body) refuse_class = "body-bound 65536";
        else if (!over_list.empty()) refuse_class = "list-bound " + over_list + " 512";
        else refuse_class = "malformed";
        return false;
    }
    out.wire.swap(wire);
    out.region.swap(raw);
    out.root = own;
    out.report = report;
    return true;
}

bool CardFromJSON(const char * text, size_t len, Record & out, TableReport & report) {
    std::string unused;
    return CardFromJSON(text, len, out, report, unused);
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

// ---------------------------------------------------------------------------
// L2 — parse once, render many (tables/cards/parse.go, render.go; DESIGN layer
// L2, docs/SPEC-TABLES.md §2, §6.2, §16). Parse turns the customer's readable
// brief into the generated wire; Render projects the record back to that text.
// ---------------------------------------------------------------------------

namespace {

std::string rowString(const char * s, int32_t n) {
    if (n < 0) n = 0;
    return std::string(s, (size_t)n);
}

std::string trimWs(const std::string & s) {
    size_t a = 0, b = s.size();
    while (a < b && (s[a] == ' ' || s[a] == '\t' || s[a] == '\n' || s[a] == '\r')) a++;
    while (b > a && (s[b-1] == ' ' || s[b-1] == '\t' || s[b-1] == '\n' || s[b-1] == '\r')) b--;
    return s.substr(a, b - a);
}

bool isHexStr(const std::string & s) {
    if (s.empty()) return false;
    for (char c : s) {
        if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) return false;
    }
    return true;
}

bool isBlankLine(const std::string & s) {
    for (char c : s) if (c != ' ' && c != '\t' && c != '\r') return false;
    return true;
}

// headerMatch is parse.go's headerLineRE: a key of one letter then
// letters/digits/hyphens, a colon, optional whitespace, the rest as value.
bool headerMatch(const std::string & line, std::string & key, std::string & value) {
    if (line.empty()) return false;
    char c0 = line[0];
    if (!((c0 >= 'A' && c0 <= 'Z') || (c0 >= 'a' && c0 <= 'z'))) return false;
    size_t i = 1;
    while (i < line.size()) {
        char c = line[i];
        if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') { i++; continue; }
        break;
    }
    if (i >= line.size() || line[i] != ':') return false;
    key = line.substr(0, i);
    size_t v = i + 1;
    while (v < line.size() && (line[v] == ' ' || line[v] == '\t' || line[v] == '\n' || line[v] == '\r' || line[v] == '\v' || line[v] == '\f')) v++;
    value = line.substr(v);
    return true;
}

bool isStrandedKey(const std::string & k) {
    static const char * keys[] = {
        "KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
        "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT", NULL };
    for (int i = 0; keys[i]; i++) if (k == keys[i]) return true;
    return false;
}

bool isReservedKey(const std::string & k) {
    static const char * keys[] = {
        "RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW",
        "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE", NULL };
    for (int i = 0; keys[i]; i++) if (k == keys[i]) return true;
    return false;
}

struct LineRef { std::string content; size_t term_end; int num; };

std::vector<LineRef> splitLines(const char * t, size_t len) {
    std::vector<LineRef> refs;
    size_t i = 0; int n = 1;
    while (i < len) {
        size_t start = i;
        size_t j = start;
        while (j < len && t[j] != '\n') j++;
        size_t line_end = j, term_end = (j < len) ? j + 1 : len;
        std::string content(t + start, line_end - start);
        if (!content.empty() && content.back() == '\r') content.pop_back();
        refs.push_back(LineRef{content, term_end, n});
        n++;
        i = term_end;
    }
    return refs;
}

struct KV { std::string key, value; };

struct Parsed {
    std::string id, sha_str;
    Tier tier = Tier::None;
    std::string repo, base, kind_;
    Priority priority = Priority::None;
    std::string stream;
    std::vector<std::string> paths, news, shared, needs;
    std::string test, done_when, source;
    int32_t deadline = 0;
    std::vector<KV> extras;
};

bool parseTier(const std::string & s, Tier & out) {
    if (s == "flash") { out = Tier::flash; return true; }
    if (s == "pro") { out = Tier::pro; return true; }
    if (s == "heavy") { out = Tier::heavy; return true; }
    if (s == "frontier") { out = Tier::frontier; return true; }
    return false;
}

bool parsePriority(const std::string & s, Priority & out) {
    if (s == "normal") { out = Priority::normal; return true; }
    if (s == "high") { out = Priority::high; return true; }
    if (s == "critical") { out = Priority::critical; return true; }
    if (s == "blocker") { out = Priority::blocker; return true; }
    return false;
}

bool parseDeadline(const std::string & s, int32_t & out) {
    const std::string pre = "finish within ", suf = " minutes.";
    if (s.size() <= pre.size() + suf.size()) return false;
    if (s.compare(0, pre.size(), pre) != 0) return false;
    if (s.compare(s.size() - suf.size(), suf.size(), suf) != 0) return false;
    std::string num = s.substr(pre.size(), s.size() - pre.size() - suf.size());
    if (num.empty()) return false;
    long long v = 0;
    for (char c : num) { if (c < '0' || c > '9') return false; v = v * 10 + (c - '0'); if (v > 100000) return false; }
    out = (int32_t)v;
    return true;
}

bool applyResult(Parsed & p, const std::string & value) {
    size_t i = value.rfind(" tier:");
    if (i == std::string::npos) return false;
    std::string tier_str = trimWs(value.substr(i + 6));
    std::string rest = value.substr(0, i);
    size_t j = rest.rfind(" sha=");
    if (j == std::string::npos) return false;
    std::string id_str = trimWs(rest.substr(0, j));
    std::string sha_str = rest.substr(j + 5);
    if (id_str.size() > 64) return false;
    if (!isHexStr(sha_str) || (sha_str.size() != 12 && sha_str.size() != 40)) return false;
    Tier t;
    if (!parseTier(tier_str, t)) return false;
    p.id = id_str; p.sha_str = sha_str; p.tier = t;
    return true;
}

bool splitListChecked(const std::string & value, size_t bound, bool none_sentinel,
                      std::vector<std::string> & out) {
    out.clear();
    std::string v = trimWs(value);
    if (v.empty() || v == "-") return true;
    if (none_sentinel && v == "none") return true;
    size_t start = 0;
    while (start <= v.size()) {
        size_t comma = v.find(',', start);
        std::string part = (comma == std::string::npos) ? v.substr(start) : v.substr(start, comma - start);
        part = trimWs(part);
        if (!part.empty()) {
            if (part.size() > bound) return false;
            out.push_back(part);
        }
        if (comma == std::string::npos) break;
        start = comma + 1;
    }
    if (out.size() > (size_t)kMaxListEntries) return false;
    return true;
}

bool assignStr(std::string & dst, const std::string & value, size_t bound) {
    if (value.size() > bound) return false;
    dst = value;
    return true;
}

bool applyHeader(Parsed & p, const std::string & key, const std::string & value,
                 std::map<std::string, int> & seen) {
    if (value.find('\r') != std::string::npos || value.find('\n') != std::string::npos) return false;
    if (key == "tier") {
        if (seen.count("tier")) return false;
        seen["tier"] = 1;
        Tier t;
        if (!parseTier(trimWs(value), t)) return false;
        if (t != p.tier) return false;
        return true;
    }
    if (key == "RESULT") return false;
    if (isReservedKey(key)) {
        if (seen.count(key)) return false;
        seen[key] = 1;
        if (key == "REPO") return assignStr(p.repo, value, 96);
        if (key == "BASE") return assignStr(p.base, value, 96);
        if (key == "KIND") return assignStr(p.kind_, value, 24);
        if (key == "PRIORITY") return parsePriority(trimWs(value), p.priority);
        if (key == "PATHS") return splitListChecked(value, 192, true, p.paths);
        if (key == "NEW") return splitListChecked(value, 192, false, p.news);
        if (key == "SHARED") return splitListChecked(value, 192, false, p.shared);
        if (key == "DEPENDS-ON") return splitListChecked(value, 64, false, p.needs);
        if (key == "TEST") return assignStr(p.test, value, 192);
        if (key == "DONE-WHEN") { p.done_when = value; return true; }
        if (key == "DEADLINE") return parseDeadline(value, p.deadline);
        if (key == "SOURCE") { p.source = value; return true; }
        return true;
    }
    if (key.size() > 32) return false;
    p.extras.push_back(KV{key, value});
    return true;
}

void assignFixed(char * dst, int32_t & n, const std::string & s) {
    std::memcpy(dst, s.data(), s.size());
    n = (int32_t)s.size();
}

bool buildRecord(Parsed & p, const std::string & body, Record & out, std::string & refuse_class) {
    CardBuilder b;
    Card * root = b.GetRoot();
    if (root == nullptr) return false;
    assignFixed(root->id, root->id_length, p.id);
    assignFixed(root->sha, root->sha_length, p.sha_str);
    root->tier = p.tier;
    assignFixed(root->repo, root->repo_length, p.repo);
    assignFixed(root->_base, root->_base_length, p.base);
    assignFixed(root->kind, root->kind_length, p.kind_);
    root->priority = p.priority;
    assignFixed(root->stream, root->stream_length, p.stream);
    for (const std::string & e : p.paths) {
        PathText * row = CardPathsAdd(b.main, root->paths);
        if (row == nullptr) return false;
        assignFixed(row->s, row->s_length, e);
    }
    for (const std::string & e : p.news) {
        PathText * row = CardNewAdd(b.main, root->_new);
        if (row == nullptr) return false;
        assignFixed(row->s, row->s_length, e);
    }
    for (const std::string & e : p.shared) {
        PathText * row = CardSharedAdd(b.main, root->shared);
        if (row == nullptr) return false;
        assignFixed(row->s, row->s_length, e);
    }
    for (const std::string & e : p.needs) {
        CardID * row = CardNeedsAdd(b.main, root->needs);
        if (row == nullptr) return false;
        assignFixed(row->s, row->s_length, e);
    }
    assignFixed(root->test, root->test_length, p.test);
    if (!p.done_when.empty()) TableStringEmplace(b.main, root->done, p.done_when.data(), (int64_t)p.done_when.size());
    root->deadline_minutes = p.deadline;
    if (!p.source.empty()) TableStringEmplace(b.main, root->source, p.source.data(), (int64_t)p.source.size());
    for (const KV & e : p.extras) {
        KeyValue * row = CardExtraAdd(b.main, root->extra);
        if (row == nullptr) return false;
        assignFixed(row->key, row->key_length, e.key);
        TableStringEmplace(b.main, row->value, e.value.data(), (int64_t)e.value.size());
    }
    if (!body.empty()) TableStringEmplace(b.main, root->body, body.data(), (int64_t)body.size());

    if (!b.Lock()) return false;
    const Card * packed = b.AsConst();
    bool over_body = false;
    std::string over_list;
    if (!CheckCard(packed, over_body, over_list)) {
        refuse_class = over_body ? "body-bound 65536" : ("list-bound " + over_list + " 512");
        return false;
    }
    int64_t size = CardMeasure(packed);
    if (size < 0) return false;
    std::vector<uint8_t> wire((size_t)size);
    if (CardSave(packed, wire.data(), size) != size) return false;
    TableReport rep;
    if (!CardLoad(wire.data(), wire.size(), out, rep)) return false;
    return true;
}

} // namespace

bool Parse(const char * text, size_t len, Record & out, std::string & refuse_class) {
    refuse_class.clear();
    if (!isUtf8WithoutNul(text, len)) { refuse_class = "malformed"; return false; }
    std::vector<LineRef> lines = splitLines(text, len);
    if (lines.empty()) { refuse_class = "malformed"; return false; }
    std::string key, value;
    if (!headerMatch(lines[0].content, key, value) || key != "RESULT") { refuse_class = "malformed"; return false; }
    Parsed p;
    if (!applyResult(p, value)) { refuse_class = "malformed"; return false; }
    size_t last_header_end = lines[0].term_end;
    std::map<std::string, int> seen;
    seen["RESULT"] = 1;
    bool prose = false;
    for (size_t li = 1; li < lines.size(); li++) {
        const LineRef & ln = lines[li];
        if (isBlankLine(ln.content)) continue;
        std::string k, v;
        if (!headerMatch(ln.content, k, v)) { prose = true; continue; }
        if (prose) {
            if (isStrandedKey(k)) { refuse_class = "stranded-key " + k; return false; }
            continue;
        }
        if (!applyHeader(p, k, v, seen)) { refuse_class = "malformed"; return false; }
        last_header_end = ln.term_end;
    }
    std::string body(text + last_header_end, len - last_header_end);
    return buildRecord(p, body, out, refuse_class);
}

bool Render(const Record & in, std::vector<char> & out, TableReport & report) {
    report = in.report;
    if (in.root == nullptr) return false;
    const Card * root = in.root;
    std::string o;
    const char * tier_name = nullptr;
    switch (root->tier) {
        case Tier::flash: tier_name = "flash"; break;
        case Tier::pro: tier_name = "pro"; break;
        case Tier::heavy: tier_name = "heavy"; break;
        case Tier::frontier: tier_name = "frontier"; break;
        default: return false;
    }
    o += "RESULT: ";
    o += rowString(root->id, root->id_length);
    o += " sha=";
    o += rowString(root->sha, root->sha_length);
    o += " tier: ";
    o += tier_name;
    o += "\n";
    auto emitStr = [&](const char * k, const std::string & v) {
        if (v.empty()) return;
        o += k; o += ": "; o += v; o += "\n";
    };
    emitStr("REPO", rowString(root->repo, root->repo_length));
    emitStr("BASE", rowString(root->_base, root->_base_length));
    emitStr("KIND", rowString(root->kind, root->kind_length));
    switch (root->priority) {
        case Priority::None: break;
        case Priority::normal: o += "PRIORITY: normal\n"; break;
        case Priority::high: o += "PRIORITY: high\n"; break;
        case Priority::critical: o += "PRIORITY: critical\n"; break;
        case Priority::blocker: o += "PRIORITY: blocker\n"; break;
        default: return false;
    }
    auto emitPathList = [&](const char * k, const TableList<PathText> & list) {
        if (list.count <= 0) return;
        o += k; o += ": ";
        for (int32_t i = 0; i < list.count; i++) {
            if (i) o += ", ";
            o += rowString(list[i].s, list[i].s_length);
        }
        o += "\n";
    };
    emitPathList("PATHS", root->paths);
    emitPathList("NEW", root->_new);
    emitPathList("SHARED", root->shared);
    if (root->needs.count > 0) {
        o += "DEPENDS-ON: ";
        for (int32_t i = 0; i < root->needs.count; i++) {
            if (i) o += ", ";
            o += rowString(root->needs[i].s, root->needs[i].s_length);
        }
        o += "\n";
    }
    emitStr("TEST", rowString(root->test, root->test_length));
    if (!root->done.null()) {
        TableStringView v = TableStringAt(root->done);
        o += "DONE-WHEN: "; o.append(v.data, (size_t)v.length); o += "\n";
    }
    if (root->deadline_minutes != 0) {
        o += "DEADLINE: finish within ";
        o += std::to_string(root->deadline_minutes);
        o += " minutes.\n";
    }
    if (!root->source.null()) {
        TableStringView v = TableStringAt(root->source);
        o += "SOURCE: "; o.append(v.data, (size_t)v.length); o += "\n";
    }
    for (int32_t i = 0; i < root->extra.count; i++) {
        const KeyValue & e = root->extra[i];
        o += rowString(e.key, e.key_length);
        o += ": ";
        if (!e.value.null()) {
            TableStringView v = TableStringAt(e.value);
            o.append(v.data, (size_t)v.length);
        }
        o += "\n";
    }
    if (!root->body.null()) {
        TableStringView v = TableStringAt(root->body);
        o.append(v.data, (size_t)v.length);
    }
    out.assign(o.begin(), o.end());
    return true;
}

} // namespace cards

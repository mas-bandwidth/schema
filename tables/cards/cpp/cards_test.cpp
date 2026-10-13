// cards_test.cpp — the card facade tests (tables/cards/cards_test.go).
// One function per named test, each printing PASS or FAIL with its name, and
// exit status nonzero on any failure.
#include "cards.hpp"
#include "CardsView.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

static int gFailures = 0;

static std::string readFile(const char * path) {
    FILE * f = std::fopen(path, "rb");
    if (!f) return std::string();
    std::fseek(f, 0, SEEK_END);
    long n = std::ftell(f);
    std::fseek(f, 0, SEEK_SET);
    std::string s;
    s.resize((size_t)n);
    if (n > 0) {
        size_t got = std::fread(&s[0], 1, (size_t)n, f);
        s.resize(got);
    }
    std::fclose(f);
    return s;
}

#define CHECK(cond) do { if (!(cond)) { \
    std::printf("FAIL %s:%d: %s\n", __func__, __LINE__, #cond); ++gFailures; } \
} while (0)

static void TestCardRoundTripsThePopulatedFixture() {
    std::string wire = readFile("tables/cards/fuzz/card-populated.wire");
    std::string json = readFile("tables/cards/fuzz/card-populated.json");
    CHECK(!wire.empty());
    CHECK(!json.empty());

    cards::TableReport rep;
    cards::Record rec;
    CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));

    std::vector<char> out;
    cards::TableReport rep2;
    CHECK(cards::CardToJSON(rec, out, rep2));
    CHECK((size_t)out.size() == json.size());
    CHECK(out.size() == json.size() && std::memcmp(out.data(), json.data(), json.size()) == 0);

    cards::Record rec2;
    cards::TableReport rep3;
    CHECK(cards::CardFromJSON(json.data(), json.size(), rec2, rep3));
    std::vector<uint8_t> w2 = cards::CardWire(rec2);
    CHECK(w2.size() == wire.size());
    CHECK(w2.size() == wire.size() && std::memcmp(w2.data(), wire.data(), wire.size()) == 0);

    std::vector<char> out2;
    cards::TableReport rep4;
    CHECK(cards::CardToJSON(rec2, out2, rep4));
    CHECK(out2.size() == json.size() && std::memcmp(out2.data(), json.data(), json.size()) == 0);

    // Mutating the caller's input and a Wire result leaves the record unchanged.
    std::string wireCopy = wire;
    cards::Record rec3;
    cards::TableReport rep5;
    CHECK(cards::CardLoad((const uint8_t *)wireCopy.data(), wireCopy.size(), rec3, rep5));
    std::vector<uint8_t> first = cards::CardWire(rec3);
    wireCopy[0] ^= 0xFF;
    std::vector<uint8_t> w3 = cards::CardWire(rec3);
    CHECK(w3.size() == first.size() && std::memcmp(w3.data(), first.data(), first.size()) == 0);
    std::vector<uint8_t> w4 = cards::CardWire(rec3);
    CHECK(w4.size() == first.size() && std::memcmp(w4.data(), first.data(), first.size()) == 0);

    std::printf("PASS TestCardRoundTripsThePopulatedFixture\n");
}

static void expectBodyRefused(int64_t bodyBytes) {
    std::string json = "{\"id\":\"x\",\"body\":\"";
    json.append((size_t)bodyBytes, 'y');
    json += "\"}";
    cards::Record rec;
    cards::TableReport rep;
    if (cards::CardFromJSON(json.data(), json.size(), rec, rep)) {
        std::printf("FAIL: expected body %lld to be refused\n", (long long)bodyBytes);
        ++gFailures;
        return;
    }
}

static void expectListRefused(const char * field, int64_t entries) {
    std::string json = "{\"id\":\"x\",\"";
    json += field;
    json += "\":[";
    for (int64_t i = 0; i < entries; i++) {
        if (i) json += ",";
        json += "{\"s\":\"p\"}";
    }
    json += "]}";
    cards::Record rec;
    cards::TableReport rep;
    if (cards::CardFromJSON(json.data(), json.size(), rec, rep)) {
        std::printf("FAIL: expected %s with %lld entries to be refused\n", field, (long long)entries);
        ++gFailures;
        return;
    }
}

static void expectAccepted(const std::string & json) {
    cards::Record rec;
    cards::TableReport rep;
    if (!cards::CardFromJSON(json.data(), json.size(), rec, rep)) {
        std::printf("FAIL: expected accept, got refuse\n");
        ++gFailures;
    }
}

static void TestCardBoundsRefuseBeforePublishing() {
    {
        std::string json = "{\"id\":\"x\",\"body\":\"";
        json.append(65536, 'y');
        json += "\"}";
        expectAccepted(json);
    }
    expectBodyRefused(65537);

    const char * fields[] = { "paths", "_new", "shared", "needs" };
    for (const char * f : fields) {
        std::string json = "{\"id\":\"x\",\"";
        json += f;
        json += "\":[";
        for (int i = 0; i < 512; i++) {
            if (i) json += ",";
            json += "{\"s\":\"p\"}";
        }
        json += "]}";
        expectAccepted(json);
    }
    for (const char * f : fields) {
        expectListRefused(f, 513);
    }

    std::printf("PASS TestCardBoundsRefuseBeforePublishing\n");
}

static void TestParseRenderMatchesGo() {
    const char * renders[] = {
        "card-populated", "initial-populated", "consumers-populated", "empty-consumers" };
    for (const char * name : renders) {
        std::string wire = readFile((std::string("tables/cards/conformance/cases/") + name + ".wire").c_str());
        std::string exp = readFile((std::string("tables/cards/conformance/expected/") + name + ".txt").c_str());
        CHECK(!wire.empty());
        CHECK(!exp.empty());
        cards::Record rec;
        cards::TableReport rep;
        CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
        std::vector<char> out;
        cards::TableReport rep2;
        CHECK(cards::Render(rec, out, rep2));
        if (out.size() != exp.size() || std::memcmp(out.data(), exp.data(), exp.size()) != 0) {
            std::printf("FAIL render %s: got %zu bytes, want %zu\n", name, out.size(), exp.size());
            ++gFailures;
        }
    }

    {
        std::string txt = readFile("tables/cards/conformance/cases/normalized-valid.md");
        std::string exp = readFile("tables/cards/conformance/expected/normalized-valid.wire");
        CHECK(!txt.empty());
        CHECK(!exp.empty());
        cards::Record rec;
        std::string refuse;
        CHECK(cards::Parse(txt.data(), txt.size(), rec, refuse));
        std::vector<uint8_t> w = cards::CardWire(rec);
        if (w.size() != exp.size() || std::memcmp(w.data(), exp.data(), exp.size()) != 0) {
            std::printf("FAIL parse normalized-valid: got %zu bytes, want %zu\n", w.size(), exp.size());
            ++gFailures;
        }
    }

    {
        std::string txt = readFile("tables/cards/conformance/cases/rejected-legacy.md");
        CHECK(!txt.empty());
        cards::Record rec;
        std::string refuse;
        bool ok = cards::Parse(txt.data(), txt.size(), rec, refuse);
        CHECK(!ok);
        if (refuse != "stranded-key PRIORITY") {
            std::printf("FAIL parse rejected-legacy: refuse=%s\n", refuse.c_str());
            ++gFailures;
        }
    }

    std::printf("PASS TestParseRenderMatchesGo\n");
}

static std::string formatReport(const cards::TableReport & r) {
    std::string o;
    o += "Retained=" + std::to_string(r.retained) + "\n";
    o += "RetainLost=" + std::to_string(r.retain_lost) + "\n";
    o += "Widened=" + std::to_string(r.widened) + "\n";
    o += "Unknown=" + std::to_string(r.unknown) + "\n";
    o += "KindMismatch=" + std::to_string(r.kind_mismatch) + "\n";
    o += "Clamped=" + std::to_string(r.clamped) + "\n";
    o += "Duplicate=" + std::to_string(r.duplicate) + "\n";
    o += std::string("Malformed=") + (r.malformed ? "true" : "false") + "\n";
    return o;
}

static std::string hex32(const uint8_t d[32]) {
    static const char * h = "0123456789abcdef";
    std::string s(64, '0');
    for (int i = 0; i < 32; i++) {
        s[(size_t)i * 2] = h[d[i] >> 4];
        s[(size_t)i * 2 + 1] = h[d[i] & 0xf];
    }
    return s;
}

static bool sameBytes(const std::vector<uint8_t> & a, const std::string & b) {
    return a.size() == b.size() && (a.empty() || std::memcmp(a.data(), b.data(), a.size()) == 0);
}

static void TestUpgradeMatchesGo() {
    const char * ok[] = {"initial-populated", "consumers-populated", "empty-consumers", "future-note"};
    for (const char * name : ok) {
        std::string wire = readFile((std::string("tables/cards/conformance/cases/") + name + ".wire").c_str());
        std::string expW = readFile((std::string("tables/cards/conformance/expected/") + name + ".wire").c_str());
        std::string expR = readFile((std::string("tables/cards/conformance/expected/") + name + ".report").c_str());
        CHECK(!wire.empty());
        CHECK(!expW.empty());
        std::string before = wire;
        std::vector<uint8_t> out;
        cards::TableReport rep;
        std::string cls;
        bool up = cards::Upgrade((const uint8_t *)wire.data(), wire.size(), out, rep, cls);
        CHECK(up);
        CHECK(wire == before);
        if (!up || !sameBytes(out, expW) || formatReport(rep) != expR) {
            std::printf("FAIL upgrade %s: class=%s out=%zu want=%zu report=%s\n", name, cls.c_str(), out.size(), expW.size(), formatReport(rep).c_str());
            ++gFailures;
        }
        std::vector<uint8_t> again;
        cards::TableReport rep2;
        std::string cls2;
        CHECK(cards::Upgrade(out.data(), out.size(), again, rep2, cls2));
        CHECK(again == out);
    }
    {
        std::string wire = readFile("tables/cards/conformance/cases/truncated.wire");
        std::string before = wire;
        std::vector<uint8_t> out;
        cards::TableReport rep;
        std::string cls;
        bool up = cards::Upgrade((const uint8_t *)wire.data(), wire.size(), out, rep, cls);
        CHECK(!up);
        CHECK(cls == "malformed");
        CHECK(out.empty());
        CHECK(wire == before);
    }
    {
        std::string wire = readFile("tables/cards/conformance/cases/future-note.wire");
        std::string before = wire;
        std::vector<uint8_t> out;
        cards::TableReport rep;
        std::string cls;
        bool up = cards::UpgradeRetain((const uint8_t *)wire.data(), wire.size(), cards::kMaxRetainBytes, 0, out, rep, cls);
        CHECK(!up);
        CHECK(cls == "retain-lost");
        CHECK(out.empty());
        CHECK(wire == before);
    }
    {
        std::string wire = readFile("tables/cards/conformance/cases/future-note.wire");
        cards::Record rec;
        std::string cls;
        CHECK(!cards::ReadStrict((const uint8_t *)wire.data(), wire.size(), rec, cls));
        CHECK(rec.root == nullptr);
    }
    {
        std::string wire = readFile("tables/cards/conformance/cases/empty-consumers.wire");
        cards::Record rec;
        std::string cls;
        CHECK(cards::ReadStrict((const uint8_t *)wire.data(), wire.size(), rec, cls));
        CHECK(rec.root != nullptr);
    }
    std::printf("PASS TestUpgradeMatchesGo\n");
}

static void appendU64(std::vector<uint8_t> & f, uint64_t v) {
    for (int i = 0; i < 8; i++) f.push_back((uint8_t)(v >> (8 * i)));
}
static void appendText(std::vector<uint8_t> & f, const char * p, size_t n) {
    appendU64(f, (uint64_t)n);
    if (n) f.insert(f.end(), p, p + n);
}

static void TestTaskHashFramesLogicalFields() {
    auto pin = [](const char * label, const uint8_t * p, size_t n, const char * want) {
        uint8_t d[32];
        cards::Sha256(p, n, d);
        if (hex32(d) != want) {
            std::printf("FAIL sha256 %s: %s\n", label, hex32(d).c_str());
            ++gFailures;
        }
    };
    pin("empty", nullptr, 0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
    const uint8_t abc[] = {'a', 'b', 'c'};
    pin("abc", abc, 3, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    const char * m56 = "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq";
    pin("448-bit", (const uint8_t *)m56, 56, "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1");
    const uint8_t cardsb[] = {'c', 'a', 'r', 'd', 's'};
    pin("cards", cardsb, 5, "6ac9a32c0ca8e84e5bf0c3eccf128cc298ef5cc009ee295209465b79b55a9367");

    auto frameOf = [](const char * body, const char * path) {
        std::vector<uint8_t> f;
        appendText(f, body, std::strlen(body));
        appendU64(f, 1);
        appendText(f, path, std::strlen(path));
        appendU64(f, 0);
        appendU64(f, 0);
        appendText(f, "", 0);
        appendText(f, "", 0);
        return f;
    };
    std::vector<uint8_t> fa = frameOf("a", "bc");
    std::vector<uint8_t> fb = frameOf("ab", "c");
    uint8_t da[32], db[32];
    cards::Sha256(fa.data(), fa.size(), da);
    cards::Sha256(fb.data(), fb.size(), db);
    CHECK(hex32(da) != hex32(db));

    auto loadJson = [](const char * json, cards::Record & rec) {
        cards::TableReport rep;
        return cards::CardFromJSON(json, std::strlen(json), rec, rep);
    };
    const char * ja = "{\"id\":\"t\",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}]}";
    const char * jb = "{\"id\":\"t\",\"body\":\"ab\",\"paths\":[{\"s\":\"c\"}]}";
    cards::Record ra, rb;
    CHECK(loadJson(ja, ra));
    CHECK(loadJson(jb, rb));
    uint8_t ha[32], hb[32];
    std::string cls;
    CHECK(cards::TaskHash(ra, ha, cls));
    CHECK(cards::TaskHash(rb, hb, cls));
    CHECK(hex32(ha) == hex32(da));
    CHECK(hex32(hb) == hex32(db));
    CHECK(hex32(ha) != hex32(hb));

    {
        std::string wire = readFile("tables/cards/conformance/cases/future-note.wire");
        cards::Record rec;
        cards::TableReport rep;
        CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
        uint8_t z[32];
        std::memset(z, 0, 32);
        uint8_t dig[32];
        std::memset(dig, 1, 32);
        CHECK(!cards::TaskHash(rec, dig, cls));
        CHECK(std::memcmp(dig, z, 32) == 0);
        CHECK(cls == "unknown-present");
    }
    const char * hashes[] = {"card-populated", "initial-populated", "empty-consumers"};
    for (const char * name : hashes) {
        std::string wire = readFile((std::string("tables/cards/conformance/cases/") + name + ".wire").c_str());
        std::string exp = readFile((std::string("tables/cards/conformance/expected/") + name + ".hex").c_str());
        cards::Record rec;
        cards::TableReport rep;
        CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
        uint8_t dig[32];
        CHECK(cards::TaskHash(rec, dig, cls));
        if (hex32(dig) != exp) {
            std::printf("FAIL taskhash %s: %s want %s\n", name, hex32(dig).c_str(), exp.c_str());
            ++gFailures;
        }
    }
    std::printf("PASS TestTaskHashFramesLogicalFields\n");
}

static void TestFieldEditLeavesTheTaskHashAlone() {
    std::string cls;
    {
        std::string wire = readFile("tables/cards/conformance/cases/card-populated.wire");
        cards::Record rec;
        cards::TableReport rep;
        CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
        std::vector<uint8_t> orig = cards::CardWire(rec);
        uint8_t hash0[32];
        CHECK(cards::TaskHash(rec, hash0, cls));
        cards::TableStringView body0 = cards::TableStringAt(rec.root->body);
        std::string body(body0.data, (size_t)body0.length);
        auto sameHash = [&](const char * label) {
            uint8_t h[32];
            CHECK(cards::TaskHash(rec, h, cls));
            if (std::memcmp(h, hash0, 32) != 0) {
                std::printf("FAIL hash moved on %s\n", label);
                ++gFailures;
            }
            cards::TableStringView b = cards::TableStringAt(rec.root->body);
            CHECK(std::string(b.data, (size_t)b.length) == body);
            CHECK(cards::CardWire(rec) != orig);
        };
        CHECK(cards::SetNeeds(rec, {"a", "b"}, cls));
        sameHash("needs");
        CHECK(cards::SetTier(rec, cards::Tier::heavy, cls));
        sameHash("tier");
        CHECK(cards::SetPriority(rec, cards::Priority::blocker, cls));
        sameHash("priority");
        CHECK(cards::SetColumn(rec, cards::Column::landed, cls));
        sameHash("column");
        CHECK(cards::SetHolder(rec, "h", cls));
        sameHash("holder");
        std::vector<uint8_t> beforePaths = cards::CardWire(rec);
        CHECK(cards::SetPaths(rec, {"x", "y"}, cls));
        uint8_t hpaths[32];
        CHECK(cards::TaskHash(rec, hpaths, cls));
        CHECK(std::memcmp(hpaths, hash0, 32) != 0);
        CHECK(cards::CardWire(rec) != beforePaths);
        cards::TableStringView b = cards::TableStringAt(rec.root->body);
        CHECK(std::string(b.data, (size_t)b.length) == body);
    }
    {
        std::string wire = readFile("tables/cards/conformance/cases/future-note.wire");
        cards::Record rec;
        cards::TableReport rep;
        CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
        std::vector<uint8_t> before = cards::CardWire(rec);
        CHECK(!cards::SetNeeds(rec, {"a"}, cls));
        CHECK(cls == "unknown-present");
        CHECK(cards::CardWire(rec) == before);
    }
    {
        std::string wire = readFile("tables/cards/conformance/cases/card-populated.wire");
        cards::Record rec;
        cards::TableReport rep;
        CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
        std::vector<uint8_t> before = cards::CardWire(rec);
        CHECK(!cards::SetHolder(rec, std::string(33, 'x'), cls));
        CHECK(cards::CardWire(rec) == before);
        const char bad[] = {'b', 'a', 'd', '\0', 'x'};
        CHECK(!cards::SetHolder(rec, std::string(bad, 5), cls));
        CHECK(cards::CardWire(rec) == before);
    }
    struct EditCase { const char * name; const char * input; const char * kind; const char * a; const char * b; };
    EditCase edits[] = {
        {"needs", "card-populated", "needs", "a", "b"},
        {"tier", "card-populated", "tier", nullptr, nullptr},
        {"priority", "card-populated", "priority", nullptr, nullptr},
        {"paths", "card-populated", "paths", "x", "y"},
        {"column", "card-populated", "column", nullptr, nullptr},
        {"holder", "card-populated", "holder", nullptr, nullptr},
        {"empty-to-growing", "empty-consumers", "paths", "x", nullptr},
    };
    for (const EditCase & e : edits) {
        std::string wire = readFile((std::string("tables/cards/conformance/cases/") + e.input + ".wire").c_str());
        std::string exp = readFile((std::string("tables/cards/conformance/expected/") + e.name + ".wire").c_str());
        cards::Record rec;
        cards::TableReport rep;
        CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
        bool ok = false;
        if (std::strcmp(e.kind, "needs") == 0) ok = cards::SetNeeds(rec, {e.a, e.b}, cls);
        else if (std::strcmp(e.kind, "paths") == 0) {
            if (e.b) ok = cards::SetPaths(rec, {e.a, e.b}, cls);
            else ok = cards::SetPaths(rec, {e.a}, cls);
        } else if (std::strcmp(e.kind, "tier") == 0) ok = cards::SetTier(rec, cards::Tier::heavy, cls);
        else if (std::strcmp(e.kind, "priority") == 0) ok = cards::SetPriority(rec, cards::Priority::blocker, cls);
        else if (std::strcmp(e.kind, "column") == 0) ok = cards::SetColumn(rec, cards::Column::landed, cls);
        else if (std::strcmp(e.kind, "holder") == 0) ok = cards::SetHolder(rec, "h", cls);
        CHECK(ok);
        std::vector<uint8_t> got = cards::CardWire(rec);
        if (!sameBytes(got, exp)) {
            std::printf("FAIL edit %s: got %zu want %zu\n", e.name, got.size(), exp.size());
            ++gFailures;
        }
    }
    std::printf("PASS TestFieldEditLeavesTheTaskHashAlone\n");
}

static const cards::TableFieldInfo * findField(const char * table, const char * field) {
    const cards::UnitViewInfo * uv = cards::UnitView();
    if (uv == nullptr) return nullptr;
    for (int32_t i = 0; i < uv->num_tables; i++) {
        if (std::strcmp(uv->tables[i].name, table) != 0) continue;
        const cards::TableTypeInfo * ty = uv->tables[i].type;
        for (int32_t j = 0; j < ty->num_fields; j++) {
            if (std::strcmp(ty->fields[j].name, field) == 0) return &ty->fields[j];
        }
    }
    return nullptr;
}

static int32_t readI32(const uint8_t * base, uint32_t off) {
    int32_t n = 0;
    std::memcpy(&n, base + off, 4);
    return n;
}

static const char * kViewVocab =
    "Card id string value 64\n"
    "Card sha string value 40\n"
    "Card tier Tier value -\n"
    "Card repo string value 96\n"
    "Card _base string value 96\n"
    "Card kind string value 24\n"
    "Card priority Priority value -\n"
    "Card stream string value 32\n"
    "Card paths PathText list -\n"
    "Card _new PathText list -\n"
    "Card shared PathText list -\n"
    "Card needs CardID list -\n"
    "Card test string value 192\n"
    "Card done string blob -\n"
    "Card deadline_minutes int32 value -\n"
    "Card source string blob -\n"
    "Card epoch int32 value -\n"
    "Card column Column value -\n"
    "Card holder string value 32\n"
    "Card attempt int32 value -\n"
    "Card head string value 40\n"
    "Card branch string value 128\n"
    "Card body string blob -\n"
    "Card extra KeyValue list -\n"
    "Card consumers Consumer list -\n"
    "CardID s string value 64\n"
    "Consumer kind string value 16\n"
    "Consumer who string value 32\n"
    "Consumer route string value 48\n"
    "Consumer model string value 64\n"
    "Consumer started int64 value -\n"
    "Consumer ended int64 value -\n"
    "Consumer outcome string value 16\n"
    "Consumer line string value 200\n"
    "Consumer cost_micro_usd int64 value -\n"
    "KeyValue key string value 32\n"
    "KeyValue value string blob -\n"
    "PathText s string value 192\n";

static void TestViewWalksEveryField() {
    std::string got = cards::WriteView();
    std::string exp = readFile("tables/cards/conformance/expected/view.txt");
    CHECK(got == kViewVocab);
    CHECK(got == exp);
    if (got != exp) {
        std::printf("FAIL view: got %zu want %zu\n", got.size(), exp.size());
        ++gFailures;
    }
    std::string wire = readFile("tables/cards/conformance/cases/card-populated.wire");
    cards::Record rec;
    cards::TableReport rep;
    CHECK(cards::CardLoad((const uint8_t *)wire.data(), wire.size(), rec, rep));
    const uint8_t * base = reinterpret_cast<const uint8_t *>(rec.root);
    const cards::TableFieldInfo * holder = findField("Card", "holder");
    const cards::TableFieldInfo * paths = findField("Card", "paths");
    const cards::TableFieldInfo * body = findField("Card", "body");
    CHECK(holder != nullptr && paths != nullptr && body != nullptr);
    CHECK(holder->counted && !holder->is_pointer && !holder->is_array);
    CHECK(readI32(base, holder->count_offset) == 6);
    CHECK(std::memcmp(base + holder->offset, "holder", 6) == 0);
    CHECK(paths->is_array && paths->array_bound == 0);
    int32_t pathCount = readI32(base, paths->count_offset);
    CHECK(pathCount == 2);
    CHECK(body->is_pointer);
    const cards::TableRef * bodySlot = reinterpret_cast<const cards::TableRef *>(base + body->offset);
    cards::TableStringView bv = cards::TableStringAt(*bodySlot);
    std::string bodyBefore(bv.data, (size_t)bv.length);
    CHECK(!bodyBefore.empty());
    std::string cls;
    CHECK(cards::SetPaths(rec, {"only"}, cls));
    base = reinterpret_cast<const uint8_t *>(rec.root);
    CHECK(readI32(base, paths->count_offset) == 1);
    bodySlot = reinterpret_cast<const cards::TableRef *>(base + body->offset);
    cards::TableStringView bv2 = cards::TableStringAt(*bodySlot);
    CHECK(std::string(bv2.data, (size_t)bv2.length) == bodyBefore);
    CHECK(readI32(base, holder->count_offset) == 6);
    std::printf("PASS TestViewWalksEveryField\n");
}

int main() {
    TestCardRoundTripsThePopulatedFixture();
    TestCardBoundsRefuseBeforePublishing();
    TestParseRenderMatchesGo();
    TestUpgradeMatchesGo();
    TestTaskHashFramesLogicalFields();
    TestFieldEditLeavesTheTaskHashAlone();
    TestViewWalksEveryField();
    if (gFailures != 0) {
        std::printf("cards_test: %d failure(s)\n", gFailures);
        return 1;
    }
    std::printf("cards_test: ok\n");
    return 0;
}

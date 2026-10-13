// cards_test.cpp — the card facade tests (tables/cards/cards_test.go).
// One function per named test, each printing PASS or FAIL with its name, and
// exit status nonzero on any failure.
#include "cards.hpp"

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

int main() {
    TestCardRoundTripsThePopulatedFixture();
    TestCardBoundsRefuseBeforePublishing();
    if (gFailures != 0) {
        std::printf("cards_test: %d failure(s)\n", gFailures);
        return 1;
    }
    std::printf("cards_test: ok\n");
    return 0;
}

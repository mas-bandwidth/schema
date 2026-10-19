// driver.cpp — the C++ card conformance driver
// (tables/cards/conformance/FORMAT.md). It answers list, load, save, render,
// parse, bounds, upgrade, edit, taskhash and view through the owning facade.
#include "cards.hpp"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

namespace {

struct Case {
    std::string surface;
    std::string name;
    std::string input;
    std::string edit;
};

void fatal(const std::string & msg) {
    std::fprintf(stderr, "cards-cpp-driver: %s\n", msg.c_str());
    std::exit(1);
}

bool readFile(const std::string & path, std::vector<char> & out) {
    std::ifstream in(path, std::ios::binary);
    if (!in) return false;
    in.seekg(0, std::ios::end);
    std::streamoff n = in.tellg();
    if (n < 0) return false;
    in.seekg(0, std::ios::beg);
    out.resize((size_t)n);
    if (n > 0 && !in.read(out.data(), n)) return false;
    return true;
}

void writeFile(const std::string & path, const char * data, size_t n) {
    std::ofstream out(path, std::ios::binary);
    if (!out) fatal("cannot write " + path);
    if (n > 0 && !out.write(data, (std::streamsize)n)) fatal("cannot write " + path);
}

void writeFile(const std::string & path, const std::string & data) {
    writeFile(path, data.data(), data.size());
}

void writeFile(const std::string & path, const std::vector<uint8_t> & data) {
    writeFile(path, reinterpret_cast<const char *>(data.data()), data.size());
}

void writeFile(const std::string & path, const std::vector<char> & data) {
    writeFile(path, data.data(), data.size());
}

std::vector<Case> readManifest(const std::string & path) {
    std::ifstream in(path);
    if (!in) fatal("cannot read manifest " + path);
    std::vector<Case> out;
    std::string line;
    while (std::getline(in, line)) {
        size_t a = 0;
        while (a < line.size() && (line[a] == ' ' || line[a] == '\t' || line[a] == '\r')) a++;
        if (a >= line.size() || line[a] == '#') continue;
        std::istringstream ls(line.substr(a));
        Case c;
        if (!(ls >> c.surface >> c.name >> c.input)) fatal("malformed manifest line");
        ls >> c.edit;
        out.push_back(c);
    }
    return out;
}

// writeReport spells the read report the way FORMAT.md fixes it: one Name=value
// line per counter of the Go TableReport, in declaration order
// (docs/SPEC-TABLES.md §16).
std::string writeReport(const cards::TableReport & r) {
    std::ostringstream b;
    b << "Retained=" << r.retained << "\n";
    b << "RetainLost=" << r.retain_lost << "\n";
    b << "Widened=" << r.widened << "\n";
    b << "Unknown=" << r.unknown << "\n";
    b << "KindMismatch=" << r.kind_mismatch << "\n";
    b << "Clamped=" << r.clamped << "\n";
    b << "Duplicate=" << r.duplicate << "\n";
    b << "Malformed=" << (r.malformed ? "true" : "false") << "\n";
    return b.str();
}

void refuse(const std::string & outdir, const std::string & name, const std::string & cls) {
    std::string c = cls.empty() ? "malformed" : cls;
    writeFile(outdir + "/" + name + ".refused", c + "\n");
}

std::vector<std::string> splitList(const std::string & v) {
    if (v.empty()) return {};
    std::vector<std::string> out;
    size_t start = 0;
    while (start <= v.size()) {
        size_t comma = v.find(',', start);
        if (comma == std::string::npos) {
            out.push_back(v.substr(start));
            break;
        }
        out.push_back(v.substr(start, comma - start));
        start = comma + 1;
    }
    return out;
}

bool parseTier(const std::string & s, cards::Tier & out) {
    if (s == "flash") { out = cards::Tier::flash; return true; }
    if (s == "pro") { out = cards::Tier::pro; return true; }
    if (s == "heavy") { out = cards::Tier::heavy; return true; }
    if (s == "frontier") { out = cards::Tier::frontier; return true; }
    return false;
}

bool parsePriority(const std::string & s, cards::Priority & out) {
    if (s == "normal") { out = cards::Priority::normal; return true; }
    if (s == "high") { out = cards::Priority::high; return true; }
    if (s == "critical") { out = cards::Priority::critical; return true; }
    if (s == "blocker") { out = cards::Priority::blocker; return true; }
    return false;
}

bool parseColumn(const std::string & s, cards::Column & out) {
    if (s == "waiting") { out = cards::Column::waiting; return true; }
    if (s == "ready") { out = cards::Column::ready; return true; }
    if (s == "working") { out = cards::Column::working; return true; }
    if (s == "review") { out = cards::Column::review; return true; }
    if (s == "merging") { out = cards::Column::merging; return true; }
    if (s == "landed") { out = cards::Column::landed; return true; }
    if (s == "plan") { out = cards::Column::plan; return true; }
    if (s == "check") { out = cards::Column::check; return true; }
    return false;
}

bool applyEdit(cards::Record & rec, const std::string & edit, std::string & cls) {
    size_t eq = edit.find('=');
    if (eq == std::string::npos) { cls = "malformed"; return false; }
    std::string key = edit.substr(0, eq);
    std::string value = edit.substr(eq + 1);
    if (key == "needs") return cards::SetNeeds(rec, splitList(value), cls);
    if (key == "paths") return cards::SetPaths(rec, splitList(value), cls);
    if (key == "holder") return cards::SetHolder(rec, value, cls);
    if (key == "tier") {
        cards::Tier tier;
        if (!parseTier(value, tier)) { cls = "malformed"; return false; }
        return cards::SetTier(rec, tier, cls);
    }
    if (key == "priority") {
        cards::Priority priority;
        if (!parsePriority(value, priority)) { cls = "malformed"; return false; }
        return cards::SetPriority(rec, priority, cls);
    }
    if (key == "column") {
        cards::Column column;
        if (!parseColumn(value, column)) { cls = "malformed"; return false; }
        return cards::SetColumn(rec, column, cls);
    }
    cls = "malformed";
    return false;
}

std::string hexEncode(const uint8_t d[32]) {
    static const char * h = "0123456789abcdef";
    std::string s(64, '0');
    for (int i = 0; i < 32; i++) {
        s[(size_t)i * 2] = h[d[i] >> 4];
        s[(size_t)i * 2 + 1] = h[d[i] & 0xf];
    }
    return s;
}

void one(const std::string & surface, const Case & c, const std::string & outdir) {
    if (surface == "view") {
        writeFile(outdir + "/" + c.name + ".txt", cards::WriteView());
        return;
    }
    std::vector<char> input;
    if (c.input != "-" && !readFile(c.input, input)) fatal("cannot read " + c.input);
    if (surface == "load") {
        cards::Record rec;
        cards::TableReport rep;
        if (!cards::CardLoad(reinterpret_cast<const uint8_t *>(input.data()), input.size(), rec, rep))
            fatal("load refused " + c.name);
        std::vector<char> json;
        cards::TableReport ignored;
        if (!cards::CardToJSON(rec, json, ignored)) fatal("tojson refused " + c.name);
        writeFile(outdir + "/" + c.name + ".json", json);
        writeFile(outdir + "/" + c.name + ".report", writeReport(rep));
        return;
    }
    if (surface == "save" || surface == "bounds") {
        cards::Record rec;
        cards::TableReport rep;
        std::string cls;
        if (!cards::CardFromJSON(input.data(), input.size(), rec, rep, cls)) {
            refuse(outdir, c.name, cls);
            return;
        }
        writeFile(outdir + "/" + c.name + ".wire", cards::CardWire(rec));
        return;
    }
    if (surface == "render") {
        cards::Record rec;
        cards::TableReport rep;
        if (!cards::CardLoad(reinterpret_cast<const uint8_t *>(input.data()), input.size(), rec, rep))
            fatal("render load refused " + c.name);
        std::vector<char> text;
        cards::TableReport ignored;
        if (!cards::Render(rec, text, ignored)) fatal("render refused " + c.name);
        writeFile(outdir + "/" + c.name + ".txt", text);
        return;
    }
    if (surface == "parse") {
        cards::Record rec;
        std::string cls;
        if (!cards::Parse(input.data(), input.size(), rec, cls)) {
            refuse(outdir, c.name, cls);
            return;
        }
        writeFile(outdir + "/" + c.name + ".wire", cards::CardWire(rec));
        return;
    }
    if (surface == "upgrade") {
        std::vector<uint8_t> wire;
        cards::TableReport rep;
        std::string cls;
        if (!cards::Upgrade(reinterpret_cast<const uint8_t *>(input.data()), input.size(), wire, rep, cls)) {
            refuse(outdir, c.name, cls);
            return;
        }
        writeFile(outdir + "/" + c.name + ".wire", wire);
        writeFile(outdir + "/" + c.name + ".report", writeReport(rep));
        return;
    }
    if (surface == "edit") {
        cards::Record rec;
        cards::TableReport rep;
        if (!cards::CardLoad(reinterpret_cast<const uint8_t *>(input.data()), input.size(), rec, rep))
            fatal("edit load refused " + c.name);
        std::string cls;
        if (!applyEdit(rec, c.edit, cls)) {
            refuse(outdir, c.name, cls);
            return;
        }
        writeFile(outdir + "/" + c.name + ".wire", cards::CardWire(rec));
        return;
    }
    if (surface == "taskhash") {
        cards::Record rec;
        cards::TableReport rep;
        if (!cards::CardLoad(reinterpret_cast<const uint8_t *>(input.data()), input.size(), rec, rep))
            fatal("taskhash load refused " + c.name);
        uint8_t dig[32];
        std::string cls;
        if (!cards::TaskHash(rec, dig, cls)) fatal("taskhash refused " + c.name);
        writeFile(outdir + "/" + c.name + ".hex", hexEncode(dig));
        return;
    }
    fatal("unknown surface " + surface);
}

bool known(const std::string & s) {
    return s == "load" || s == "save" || s == "render" || s == "parse" || s == "bounds" ||
           s == "upgrade" || s == "edit" || s == "taskhash" || s == "view";
}

} // namespace

int main(int argc, char ** argv) {
    if (argc < 3) fatal("usage: <manifest> (list | <surface> <outdir>)");
    std::string verb = argv[2];
    if (verb == "list") {
        std::fputs("load\nsave\nrender\nparse\nbounds\nupgrade\nedit\ntaskhash\nview\n", stdout);
        return 0;
    }
    if (argc < 4) fatal("usage: <manifest> <surface> <outdir>");
    if (!known(verb)) fatal("unknown surface " + verb);
    std::string outdir = argv[3];
    std::vector<Case> cases = readManifest(argv[1]);
    for (const Case & c : cases) {
        if (c.surface != verb) continue;
        one(verb, c, outdir);
    }
    return 0;
}

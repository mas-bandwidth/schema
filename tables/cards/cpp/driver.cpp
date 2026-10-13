// driver.cpp — the C++ card conformance driver
// (tables/cards/conformance/FORMAT.md). It answers list, load, save, render,
// parse and bounds through the owning facade. upgrade, edit, taskhash and view
// exit 2: their card is a later one.
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

void one(const std::string & surface, const Case & c, const std::string & outdir) {
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
    fatal("unknown surface " + surface);
}

bool known(const std::string & s) {
    return s == "load" || s == "save" || s == "render" || s == "parse" || s == "bounds";
}

bool absent(const std::string & s) {
    return s == "upgrade" || s == "edit" || s == "taskhash" || s == "view";
}

} // namespace

int main(int argc, char ** argv) {
    if (argc < 3) fatal("usage: <manifest> (list | <surface> <outdir>)");
    std::string verb = argv[2];
    if (verb == "list") {
        std::fputs("load\nsave\nrender\nparse\nbounds\n", stdout);
        return 0;
    }
    if (absent(verb)) return 2;
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

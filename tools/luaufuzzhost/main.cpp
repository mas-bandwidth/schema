// tools/luaufuzzhost/main.cpp: the native host of the Luau port's nightly
// fuzz (make/schema-luau-nightly-fuzz.mk). It is the minimal C++ driver the
// fuzzer needs, linking the pinned Luau 0.741's official C API and nothing
// else:
//
//   * every chunk (the harness and each module it requires) is compiled with
//     coverageLevel 1, so the VM counts line hits;
//   * __luaufuzz.coverage(needle) walks lua_getcoverage over the chunks whose
//     path contains needle and answers how many (chunk, line) pairs were hit
//     for the first time, which is what steers the harness's mutation loop:
//     an input that reaches a new line is kept in the pool;
//   * __luaufuzz.tick() arms a per-input watchdog; the VM interrupt raises a
//     typed error once one input has run past LUAUFUZZ_HANG_SECONDS, so a hang
//     in a reader is a finding with its input, never a stuck job;
//   * __luaufuzz.read(n) and __luaufuzz.write(buf) are the host's stdin and
//     stdout, the one way a script under this host speaks a pipe (the pinned
//     interpreter has no io library, docs/PORT-LUAU.md, THE HARNESS): read
//     answers a buffer of up to n bytes, shorter only at end of input and
//     empty there, and write puts the buffer's bytes on stdout and flushes, so
//     the wire-fuzz driver (test/conformance/luau/wirefuzz.luau) answers each
//     mutant before it reads the next (test/conformance/README.md, "The
//     wire-fuzz driver");
//   * __luaufuzz.note(line) appends the line to the file the environment
//     variable LUAUFUZZ_NOTE names, and does nothing when it is unset: the one
//     side channel a script under this host has that is neither the pipe nor
//     the harness-captured stderr (the wire-fuzz driver lists the roster
//     entries it answers 0 for there, make/luau-fuzz.mk pins them by name);
//   * require(path) resolves "./" and "../" against the requiring chunk, the
//     way the luau CLI does, and caches each module by its resolved path.
//
// usage: luaufuzzhost <script.luau> [args...]   (args reach the script as ...)
// Exit status: 0 when the script returns, 1 when it raises, 2 on usage.

#include "lua.h"
#include "lualib.h"
#include "luacode.h"

#include "Luau/Common.h"
#include "Luau/ExperimentalFlags.h"

#include <chrono>
#include <stdio.h>
#include <stdlib.h>
#include <fstream>
#include <sstream>
#include <string>
#include <string.h>
#include <unordered_set>
#include <vector>

static const double LUAUFUZZ_HANG_SECONDS = 10.0;

struct Chunk
{
    std::string path;
    int ref;
};

static std::vector<Chunk> gChunks;
static std::unordered_set<uint64_t> gSeen;
static uint64_t gFresh = 0;
static uint32_t gChunkIndex = 0;
static bool gArmed = false;
static std::chrono::steady_clock::time_point gTick;

// The CLI's default: every Luau flag on except the experimental analysis ones.
static void setFlagsDefault()
{
    for (Luau::FValue<bool>* flag = Luau::FValue<bool>::list; flag; flag = flag->next)
        if (strncmp(flag->name, "Luau", 4) == 0 && !Luau::isAnalysisFlagExperimental(flag->name))
            flag->value = true;
}

static bool readFile(const std::string& path, std::string& out)
{
    std::ifstream f(path, std::ios::binary);
    if (!f)
        return false;
    std::stringstream ss;
    ss << f.rdbuf();
    out = ss.str();
    return true;
}

static std::string dirname(const std::string& path)
{
    size_t slash = path.find_last_of('/');
    return slash == std::string::npos ? std::string(".") : path.substr(0, slash);
}

// Collapse "." and ".." segments so one module has one cache key.
static std::string normalize(const std::string& path)
{
    std::vector<std::string> parts;
    std::string seg;
    std::stringstream ss(path);
    bool absolute = !path.empty() && path[0] == '/';
    while (std::getline(ss, seg, '/'))
    {
        if (seg.empty() || seg == ".")
            continue;
        if (seg == ".." && !parts.empty() && parts.back() != "..")
            parts.pop_back();
        else
            parts.push_back(seg);
    }
    std::string out = absolute ? "/" : "";
    for (size_t i = 0; i < parts.size(); ++i)
        out += (i ? "/" : "") + parts[i];
    return out.empty() ? std::string(".") : out;
}

// Compile with line coverage on, load as "@path", and track the chunk.
static bool loadChunk(lua_State* L, const std::string& path)
{
    std::string source;
    if (!readFile(path, source))
    {
        lua_pushfstring(L, "luaufuzzhost: cannot read %s", path.c_str());
        return false;
    }
    lua_CompileOptions opts = {};
    opts.optimizationLevel = 1;
    opts.debugLevel = 1;
    opts.coverageLevel = 1;
    size_t size = 0;
    char* bytecode = luau_compile(source.data(), source.size(), &opts, &size);
    std::string chunkname = "@" + path;
    int status = luau_load(L, chunkname.c_str(), bytecode, size, 0);
    free(bytecode);
    if (status != 0)
        return false;
    gChunks.push_back({path, lua_ref(L, -1)});
    return true;
}

static int hostRequire(lua_State* L)
{
    const char* name = luaL_checkstring(L, 1);
    lua_Debug ar = {};
    std::string base = ".";
    if (lua_getinfo(L, 1, "s", &ar) && ar.source && ar.source[0] == '@')
        base = dirname(ar.source + 1);
    std::string rel = name;
    if (rel.rfind("./", 0) != 0 && rel.rfind("../", 0) != 0)
        luaL_error(L, "luaufuzzhost: require(%s): only ./ and ../ paths resolve", name);
    std::string stem = normalize(base + "/" + rel);
    std::string path;
    std::string probe;
    for (const char* suffix : {".luau", ".lua", "/init.luau", "/init.lua"})
        if (readFile(stem + suffix, probe))
        {
            path = stem + suffix;
            break;
        }
    if (path.empty())
        luaL_error(L, "luaufuzzhost: require(%s): no module at %s", name, stem.c_str());

    lua_getfield(L, LUA_REGISTRYINDEX, "luaufuzzhost.modules");
    lua_getfield(L, -1, path.c_str());
    if (lua_isboolean(L, -1))
        luaL_error(L, "luaufuzzhost: require(%s): cyclic require", name);
    if (!lua_isnil(L, -1))
        return 1;
    lua_pop(L, 1);
    lua_pushboolean(L, 1);
    lua_setfield(L, -2, path.c_str());

    // On any failure the loading sentinel is cleared first, so a later
    // require of the same path reports the real cause, not "cyclic require".
    int modules = lua_gettop(L);
    if (!loadChunk(L, path) || lua_pcall(L, 0, 1, 0) != 0 || lua_isnil(L, -1))
    {
        if (lua_isnil(L, -1))
        {
            lua_pop(L, 1);
            lua_pushfstring(L, "luaufuzzhost: require(%s): the module returned nil", name);
        }
        lua_pushnil(L);
        lua_setfield(L, modules, path.c_str());
        lua_error(L);
    }
    lua_pushvalue(L, -1);
    lua_setfield(L, -3, path.c_str());
    return 1;
}

static void coverageCallback(void* context, const char*, int, int, const int* hits, size_t size)
{
    uint64_t chunk = *static_cast<uint32_t*>(context);
    for (size_t i = 0; i < size; ++i)
        if (hits[i] > 0 && gSeen.insert((chunk << 32) | uint64_t(i)).second)
            ++gFresh;
}

// coverage(needle?) -> the number of (chunk, line) pairs first hit since the
// last call, over the chunks whose path contains needle (all when absent).
static int hostCoverage(lua_State* L)
{
    const char* needle = luaL_optstring(L, 1, "");
    gFresh = 0;
    for (uint32_t i = 0; i < gChunks.size(); ++i)
    {
        if (*needle && gChunks[i].path.find(needle) == std::string::npos)
            continue;
        gChunkIndex = i;
        lua_getref(L, gChunks[i].ref);
        lua_getcoverage(L, -1, &gChunkIndex, coverageCallback);
        lua_pop(L, 1);
    }
    lua_pushnumber(L, double(gFresh));
    return 1;
}

static int hostLines(lua_State* L)
{
    lua_pushnumber(L, double(gSeen.size()));
    return 1;
}

static int hostTick(lua_State*)
{
    gArmed = true;
    gTick = std::chrono::steady_clock::now();
    return 0;
}

// read(n) -> a buffer of up to n bytes from stdin: exactly n unless the input
// ended first, empty at end of input (test/conformance/README.md, "The
// wire-fuzz driver": the stream is read until EOF).
static int hostRead(lua_State* L)
{
    double want = luaL_checknumber(L, 1);
    if (want < 0 || want > double(1u << 31))
        luaL_error(L, "luaufuzzhost: read(%f): out of range", want);
    std::vector<char> got(static_cast<size_t>(want));
    size_t n = 0;
    while (n < got.size())
    {
        size_t r = fread(got.data() + n, 1, got.size() - n, stdin);
        if (r == 0)
            break;
        n += r;
    }
    void* out = lua_newbuffer(L, n);
    if (n)
        memcpy(out, got.data(), n);
    return 1;
}

// write(buf) puts the buffer's bytes on stdout and flushes, so one reply is out
// before the next mutant is read (test/conformance/README.md, "The wire-fuzz
// driver": every reply is flushed before the next mutant is read).
static int hostWrite(lua_State* L)
{
    size_t len = 0;
    const void* data = luaL_checkbuffer(L, 1, &len);
    if (len && fwrite(data, 1, len, stdout) != len)
        luaL_error(L, "%s", "luaufuzzhost: write: stdout closed");
    if (fflush(stdout) != 0)
        luaL_error(L, "%s", "luaufuzzhost: write: stdout closed");
    return 0;
}

// note(line) appends line and a newline to $LUAUFUZZ_NOTE; unset, it is a
// no-op, so a script calls it unconditionally.
static int hostNote(lua_State* L)
{
    size_t len = 0;
    const char* line = luaL_checklstring(L, 1, &len);
    const char* path = getenv("LUAUFUZZ_NOTE");
    if (!path || !*path)
        return 0;
    FILE* f = fopen(path, "ab");
    if (!f)
        luaL_error(L, "luaufuzzhost: note: cannot open %s", path);
    bool ok = fwrite(line, 1, len, f) == len && fputc('\n', f) != EOF;
    if (fclose(f) != 0 || !ok)
        luaL_error(L, "luaufuzzhost: note: cannot write %s", path);
    return 0;
}

static void interrupt(lua_State* L, int gc)
{
    if (gc >= 0 || !gArmed)
        return;
    double ran = std::chrono::duration<double>(std::chrono::steady_clock::now() - gTick).count();
    if (ran > LUAUFUZZ_HANG_SECONDS)
    {
        gArmed = false;
        // lua_error, not luaL_error: no "chunk:line:" position prefix, so
        // a hang is ONE finding wherever the reader happened to be looping.
        lua_pushfstring(L, "luaufuzzhost: hang: one input ran past %d s", int(LUAUFUZZ_HANG_SECONDS));
        lua_error(L);
    }
}

int main(int argc, char** argv)
{
    if (argc < 2)
    {
        fprintf(stderr, "usage: luaufuzzhost <script.luau> [args...]\n");
        return 2;
    }
    setFlagsDefault();
    lua_State* L = luaL_newstate();
    luaL_openlibs(L);
    lua_callbacks(L)->interrupt = interrupt;

    lua_newtable(L);
    lua_setfield(L, LUA_REGISTRYINDEX, "luaufuzzhost.modules");
    lua_pushcfunction(L, hostRequire, "require");
    lua_setglobal(L, "require");
    lua_newtable(L);
    lua_pushcfunction(L, hostCoverage, "coverage");
    lua_setfield(L, -2, "coverage");
    lua_pushcfunction(L, hostLines, "lines");
    lua_setfield(L, -2, "lines");
    lua_pushcfunction(L, hostTick, "tick");
    lua_setfield(L, -2, "tick");
    lua_pushcfunction(L, hostRead, "read");
    lua_setfield(L, -2, "read");
    lua_pushcfunction(L, hostWrite, "write");
    lua_setfield(L, -2, "write");
    lua_pushcfunction(L, hostNote, "note");
    lua_setfield(L, -2, "note");
    lua_setglobal(L, "__luaufuzz");

    std::string script = normalize(argv[1]);
    if (!loadChunk(L, script))
    {
        fprintf(stderr, "%s\n", lua_tostring(L, -1));
        return 1;
    }
    for (int i = 2; i < argc; ++i)
        lua_pushstring(L, argv[i]);
    if (lua_pcall(L, argc - 2, 0, 0) != 0)
    {
        fprintf(stderr, "%s\n", lua_tostring(L, -1));
        return 1;
    }
    fflush(stdout);
    lua_close(L);
    return 0;
}

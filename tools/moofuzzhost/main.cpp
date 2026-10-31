// tools/moofuzzhost/main.cpp: the native host of the moo leg's wire-fuzz
// driver (make/moo-fuzz.mk, tables-moo-wire-fuzz; test/conformance/README.md,
// "The wire-fuzz driver"). The pinned moo CLI has no io library
// (docs/PORT-MOO.md, THE HARNESS), so the harness's pipe is spoken by
// test/conformance/moo/wirefuzz.moo under this host, which links the moo pin's
// own VM and compiler (make/moo.mk, MOO_PIN) and adds nothing but the pipe:
//
//   * __moofuzz.read(n) answers a buffer of up to n bytes from stdin, shorter
//     only at end of input and empty there;
//   * __moofuzz.write(buf) puts the buffer's bytes on stdout and flushes, so
//     each reply is out before the next mutant is read;
//   * __moofuzz.note(line) appends the line to the file the environment
//     variable MOOFUZZ_NOTE names, and does nothing when it is unset (the
//     driver lists the roster entries it answers 0 for there, and
//     make/moo-fuzz.mk pins them by name);
//   * require(path) resolves "./" and "../" against the requiring chunk, as the
//     moo CLI does, probes .moo, .luau and .lua, and caches each module by its
//     resolved path.
//
// The chunks compile at the CLI's levels (optimization 1, debug 1, type info
// 1), with every Luau flag at its CLI default, so the runtime under test runs
// as it runs under dist/moo/moo.
//
// usage: moofuzzhost <script.moo> [args...]   (args reach the script as ...)
// Exit status: 0 when the script returns, 1 when it raises, 2 on usage.

#include "lua.h"
#include "lualib.h"
#include "luacode.h"

#include "Luau/Common.h"
#include "Luau/ExperimentalFlags.h"

#include <stdio.h>
#include <stdlib.h>
#include <fstream>
#include <sstream>
#include <string>
#include <string.h>
#include <vector>

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

static bool loadChunk(lua_State* L, const std::string& path)
{
    std::string source;
    if (!readFile(path, source))
    {
        lua_pushfstring(L, "moofuzzhost: cannot read %s", path.c_str());
        return false;
    }
    lua_CompileOptions opts = {};
    opts.optimizationLevel = 1;
    opts.debugLevel = 1;
    opts.typeInfoLevel = 1;
    size_t size = 0;
    char* bytecode = luau_compile(source.data(), source.size(), &opts, &size);
    std::string chunkname = "@" + path;
    int status = luau_load(L, chunkname.c_str(), bytecode, size, 0);
    free(bytecode);
    return status == 0;
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
        luaL_error(L, "moofuzzhost: require(%s): only ./ and ../ paths resolve", name);
    std::string stem = normalize(base + "/" + rel);
    std::string path;
    std::string probe;
    for (const char* suffix : {".moo", ".luau", ".lua", "/init.moo", "/init.luau", "/init.lua"})
        if (readFile(stem + suffix, probe))
        {
            path = stem + suffix;
            break;
        }
    if (path.empty())
        luaL_error(L, "moofuzzhost: require(%s): no module at %s", name, stem.c_str());

    lua_getfield(L, LUA_REGISTRYINDEX, "moofuzzhost.modules");
    lua_getfield(L, -1, path.c_str());
    if (lua_isboolean(L, -1))
        luaL_error(L, "moofuzzhost: require(%s): cyclic require", name);
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
            lua_pushfstring(L, "moofuzzhost: require(%s): the module returned nil", name);
        }
        lua_pushnil(L);
        lua_setfield(L, modules, path.c_str());
        lua_error(L);
    }
    lua_pushvalue(L, -1);
    lua_setfield(L, -3, path.c_str());
    return 1;
}

// read(n) -> a buffer of up to n bytes from stdin: exactly n unless the input
// ended first, empty at end of input.
static int hostRead(lua_State* L)
{
    int want = luaL_checkinteger(L, 1);
    if (want < 0)
        luaL_error(L, "moofuzzhost: read(%d): out of range", want);
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

// write(buf) puts the buffer's bytes on stdout and flushes.
static int hostWrite(lua_State* L)
{
    size_t len = 0;
    const void* data = luaL_checkbuffer(L, 1, &len);
    if (len && fwrite(data, 1, len, stdout) != len)
        luaL_error(L, "%s", "moofuzzhost: write: stdout closed");
    if (fflush(stdout) != 0)
        luaL_error(L, "%s", "moofuzzhost: write: stdout closed");
    return 0;
}

// note(line) appends line and a newline to $MOOFUZZ_NOTE; unset, it is a no-op.
static int hostNote(lua_State* L)
{
    size_t len = 0;
    const char* line = luaL_checklstring(L, 1, &len);
    const char* path = getenv("MOOFUZZ_NOTE");
    if (!path || !*path)
        return 0;
    FILE* f = fopen(path, "ab");
    if (!f)
        luaL_error(L, "moofuzzhost: note: cannot open %s", path);
    bool ok = fwrite(line, 1, len, f) == len && fputc('\n', f) != EOF;
    if (fclose(f) != 0 || !ok)
        luaL_error(L, "moofuzzhost: note: cannot write %s", path);
    return 0;
}

int main(int argc, char** argv)
{
    if (argc < 2)
    {
        fprintf(stderr, "usage: moofuzzhost <script.moo> [args...]\n");
        return 2;
    }
    setFlagsDefault();
    lua_State* L = luaL_newstate();
    luaL_openlibs(L);

    lua_newtable(L);
    lua_setfield(L, LUA_REGISTRYINDEX, "moofuzzhost.modules");
    lua_pushcfunction(L, hostRequire, "require");
    lua_setglobal(L, "require");
    lua_newtable(L);
    lua_pushcfunction(L, hostRead, "read");
    lua_setfield(L, -2, "read");
    lua_pushcfunction(L, hostWrite, "write");
    lua_setfield(L, -2, "write");
    lua_pushcfunction(L, hostNote, "note");
    lua_setfield(L, -2, "note");
    lua_setglobal(L, "__moofuzz");

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

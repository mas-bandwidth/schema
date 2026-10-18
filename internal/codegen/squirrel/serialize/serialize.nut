// serialize.nut: a bitpacking serializer for Squirrel, one of the serialize
// family (C, C++, C#, Dart, Elixir, Go, Java, JavaScript, Rust, ...), which
// all speak one wire: the same values produce the same bytes in every one.
// STANDARD.md (vendored verbatim from mas-bandwidth/serialize v1.17.0) is the
// authority; this library implements format version 1.1 of it. DESIGN.md is
// the port's design, and every section cited below is one of its sections.
//
// THE ENTRY. This file is the only one a caller runs:
//
//   local serialize = dofile(dir + "/serialize.nut", true);
//   local ws = serialize.WriteStream(blob(64));
//
// It returns the API table (DESIGN.md (a)). Every other file of the library is
// a module loaded through the closed loader below (DESIGN.md (f)), and every
// family module is loaded LAZILY, on the first call that needs it
// (DESIGN.md (h)): while a family's file is absent its names raise
// "serialize: <name> not implemented".

// THE NUMBER MODEL (DESIGN.md (c)): one 64-bit SQInteger and a double SQFloat,
// the pin's sq64 + SQUSEDOUBLE build. A VM built any other way is not this
// design's VM, and the library refuses it by name rather than produce bytes.
if (_intsize_ != 8 || _floatsize_ != 8) {
  throw "serialize: refuses this VM: _intsize_ " + _intsize_ + " and _floatsize_ " + _floatsize_ +
    ", the library needs 8 and 8 (Squirrel built sq64 with -DSQUSEDOUBLE; DESIGN.md (c))";
}

// LOADING IS CLOSED (DESIGN.md (f)). A name resolves to <dir>/<name>.nut,
// where dir is THIS file's directory (from getstackinfos(1).src), never the
// working directory and never a list of places. A name is a basename of
// letters, digits and underscores, so nothing can climb out of dir.
local src = getstackinfos(1).src;
local cut = -1;
local at;
while ((at = src.find("/", cut + 1)) != null) cut = at;
local dir = cut < 0 ? "." : src.slice(0, cut);

local isName = function(name) {
  if (typeof name != "string" || name.len() == 0) return false;
  foreach (c in name) {
    if (!((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_')) return false;
  }
  return true;
};

local exists = function(path) {
  try {
    local f = file(path, "rb");
    f.close();
    return true;
  } catch (e) {
    return false;
  }
};

local cache = {};
local require;

// require(name, who = null): the module <name>, loaded once. A module is a
// script that receives its exports table as vargv[0] and this require as
// vargv[1], and fills the table; the table is cached BEFORE the script runs,
// so a cycle meets the table it is still filling. An absent file raises
// "serialize: <who> not implemented", where who is the API name the caller
// reached (the module name when none is given): the lazy dispatch's error.
// The module runs through a direct call of its closure (bound to the root
// table), never .call or dofile: a throw that crosses a native call is
// printed to stderr by sq even when caught (measured on the pin, DESIGN.md (i)).
require = function(name, who = null) {
  if (!isName(name)) {
    throw "serialize: the loader refuses " + name + ": a module name is a basename beside the loader (DESIGN.md (f))";
  }
  if (name in cache) return cache[name];
  local path = dir + "/" + name + ".nut";
  if (!exists(path)) {
    throw "serialize: " + (who == null ? name : who) + " not implemented (" + path + " is absent)";
  }
  local m = {};
  cache[name] <- m;
  local fn = loadfile(path, true).bindenv(::getroottable());
  fn(m, require);
  return m;
};

local streams = require("serialize_stream");

// a family function reached through the API table, loaded on first use. The
// wrapper has the function's own arity and calls it directly (never acall, a
// native call: a throw across one prints to stderr even when caught).
local lazy = function(family, name, arity) {
  local get = function() {
    local m = require(family, name);
    if (!(name in m)) throw "serialize: " + name + " not implemented (" + family + ".nut does not export it)";
    return m[name];
  };
  switch (arity) {
    case 1: return function(a) { return get()(a); };
    case 2: return function(a, b) { return get()(a, b); };
    case 3: return function(a, b, c) { return get()(a, b, c); };
  }
  throw "serialize: lazy arity " + arity;
};

// THE API (DESIGN.md (a)): serialize.go v1.15.1's exports in serialize.js's
// spelling.
local api = {
  // the error kinds latched on a stream (serialize.go ErrOverflow,
  // ErrValueOutOfRange, ErrAlign); None is a healthy stream
  SerializeError = streams.SerializeError,

  // the three streams (DESIGN.md (g)); serialize_stream.nut
  WriteStream = streams.WriteStream,
  ReadStream = streams.ReadStream,
  MeasureStream = streams.MeasureStream,

  // the bit packer (serialize-squirrel-bitpacker); serialize_bitpacker.nut
  BitWriter = lazy("serialize_bitpacker", "BitWriter", 1),
  BitReader = lazy("serialize_bitpacker", "BitReader", 1),

  // range pricing and zigzag (serialize-squirrel-bits); serialize_bits.nut
  bitsRequired = lazy("serialize_bits", "bitsRequired", 2),
  bitsRequired64 = lazy("serialize_bits", "bitsRequired64", 2),
  bitsRequired128 = lazy("serialize_bits", "bitsRequired128", 2),
  signedToUnsigned = lazy("serialize_bits", "signedToUnsigned", 1),
  unsignedToSigned = lazy("serialize_bits", "unsignedToSigned", 1),

  // 128-bit values as [lo, hi] (serialize-squirrel-int); serialize_int128.nut
  int128From64 = lazy("serialize_int128", "int128From64", 1),
  uint128From64 = lazy("serialize_int128", "uint128From64", 1),

  // the precomputed compressed float (serialize-squirrel-compressed-float)
  compressedFloatParams = lazy("serialize_compressed_float", "compressedFloatParams", 3),

  // the float32 narrowing every float32 write uses (serialize-squirrel-float)
  fround = lazy("serialize_float", "fround", 1),

  // sentinel-driven loops that always terminate (serialize.go Continue and
  // Until): false as soon as the stream has an error. Spelled with a capital
  // because continue is a Squirrel keyword (DESIGN.md (a)).
  Continue = function(stream, more) {
    if (!stream.serializeBool(more)) return false;
    return more.value;
  },
  Until = function(stream, done) {
    if (!stream.serializeBool(done)) return false;
    return !done.value;
  },

  // the loader's own directory, for tests and tools (DESIGN.md (f))
  dir = dir,
};

return api;

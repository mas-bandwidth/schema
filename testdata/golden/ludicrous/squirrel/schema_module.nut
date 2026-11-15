// THE MODULE LOADER (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT). Squirrel has
// no require, so this file is the one the caller runs and every generated
// module is loaded through it:
//
//   local schema = dofile(dir + "/schema_module.nut", true);
//   local Types = schema.require("Types");
//
// A module is a script that receives two arguments, its own exports table
// and this require, and fills the table: `local _M = vargv[0];`. The table is
// cached BEFORE the script runs, so two files that name each other load once
// each and a cycle meets the table it is still filling. Every name is resolved
// beside this file and nowhere else, wherever the caller's working directory
// is: there is no search path to shadow.
local src = getstackinfos(1).src;
local cut = -1;
local at;
while ((at = src.find("/", cut + 1)) != null) cut = at;
local dir = cut < 0 ? "." : src.slice(0, cut);
local cache = {};
// A NAME IS A BASENAME (docs/PORT-SQUIRREL.md, LOADING IS CLOSED): letters,
// digits and underscores only, so no name can climb out of dir, name an
// absolute path, or reach a file that is not <name>.nut beside this loader.
local isName = function(name) {
  if (typeof name != "string" || name.len() == 0) return false;
  foreach (c in name) {
    if (!((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_')) return false;
  }
  return true;
};
local require;
require = function(name) {
  if (!isName(name)) throw "schema loader refuses " + name + ": a module name is a basename beside the loader (docs/PORT-SQUIRREL.md, LOADING IS CLOSED)";
  if (name in cache) return cache[name];
  local m = {};
  cache[name] <- m;
  local fn = loadfile(dir + "/" + name + ".nut", true);
  fn.call(::getroottable(), m, require);
  return m;
};
return { require = require, dir = dir };

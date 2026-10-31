// THE DESCRIPTOR RUNTIME (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT): what
// every table surface reads a descriptor through. A table module calls link
// once with its exports; link indexes each descriptor's fields by wire id, by
// schema name and by text key, so the load, save and text runtimes find a
// field without a per-table function (docs/SPEC-TABLES.md §8.1).
local _M = vargv[0];

_M.link <- function(mod) {
  foreach (name, d in mod.Descriptors) {
    d.byId <- {};
    d.byName <- {};
    d.byKey <- {};
    foreach (f in d.fields) {
      d.byId[f.id] <- f;
      d.byName[f.name] <- f;
      d.byKey[f.key] <- f;
    }
  }
};

// blobOf is the blob holding a string's bytes: a bytes(N) default is written
// in the module as a string literal and stored as a blob.
_M.blobOf <- function(s) {
  local b = blob(0);
  foreach (c in s) b.writen(c & 0xff, 'b');
  return b;
};

// THE KEYED ACCESSOR (docs/PORTING.md M5; docs/SPEC-TABLES.md §2.4): an
// enum-keyed array [E]T is a plain array with key k at slot k - 1, and this
// is the one place a key is checked. get(array, key) and set(array, key,
// value) refuse None (key 0), a negative or non-integer key and any key past
// E.Max by name, `keyed_refused`, in every build; each(array, fn) calls
// fn(key, element) so the caller never does the shift; key(name) is the key
// of a variant name. A generated module reaches it as Keyed(table, field).
_M.keyed <- function(mod, table, field) {
  if (!(table in mod.Descriptors)) throw "keyed_refused: no table " + table;
  local d = mod.Descriptors[table];
  if (!(field in d.byName) || d.byName[field].keyvariants == null)
    throw "keyed_refused: " + table + "." + field + " is not an enum-keyed array";
  local variants = d.byName[field].keyvariants;
  local max = variants.len();
  local slot = function(key) {
    if (typeof key != "integer" || key < 1 || key > max)
      throw "keyed_refused: key " + key + " is not a key of " + table + "." + field + " (1.." + max + ")";
    return key - 1;
  };
  return {
    get = function(array, key) { return array[slot(key)]; },
    set = function(array, key, value) { array[slot(key)] = value; },
    each = function(array, fn) {
      for (local i = 0; i < max; i++) fn(i + 1, array[i]);
    },
    key = function(name) {
      foreach (i, kv in variants) if (kv[0] == name) return i + 1;
      throw "keyed_refused: " + name + " is not a variant of " + table + "." + field;
    },
  };
};

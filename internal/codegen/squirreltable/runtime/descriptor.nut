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

// driver.nut — the Squirrel card conformance driver
// (tables/cards/conformance/FORMAT.md, test/conformance/README.md "The shape").
//
// It answers the driver contract — `<driver> <manifest> list` and
// `<driver> <manifest> <surface> <outdir>` — over the nine surfaces this leg
// implements (load, save, render, parse, bounds, upgrade, edit, taskhash, view)
// through the facade in cards.nut. The sh wrapper beside it (driver) runs it
// under the pinned interpreter and answers the exit codes: sq exits 0 on a
// runtime error, so this script reports a failure on stderr and the wrapper
// fails any run that wrote there (docs/PORT-SQUIRREL.md, THE HARNESS).
//
// The working directory is the repository root, which is the contract: every
// path in the manifest is repo-relative, so this driver never resolves one.
// The generated Card unit is a PATH, so the negative controls and the Makefile
// can point the same driver at another tree: CARDS_SQUIRREL_GENERATED names it,
// the default is what the Makefile writes (build/tables-generated-squirrel/cards).

local GENERATED = ::getenv("CARDS_SQUIRREL_GENERATED");
if (GENERATED == null || GENERATED == "") GENERATED = "build/tables-generated-squirrel/cards";

local T = ::dofile(GENERATED + "/schema_module.nut", true).require("CardsTables");
local cards = ::loadfile("tables/cards/squirrel/cards.nut", true).call(::getroottable(), T);

local SURFACES = ["load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"];

// THE TEST-ONLY SWITCH the negative controls set: CARDS_SQUIRREL_SABOTAGE names
// ONE surface's answer to damage (render, upgrade or hash), so the check must
// go red on that surface alone.
local SABOTAGE = ::getenv("CARDS_SQUIRREL_SABOTAGE");
if (SABOTAGE == null) SABOTAGE = "";

local Failed = class {
  message = null;
  constructor(message) { this.message = message; }
};

local fail = function(message) { throw Failed("cards-driver: " + message); };

local readFile = function(path) {
  local f = ::file(path, "rb");
  local data = f.len() > 0 ? f.readblob(f.len()) : ::blob(0);
  f.close();
  return data;
};

local writeFile = function(path, data) {
  local b = typeof data == "string" ? cards.toBlob(data) : data;
  local f = ::file(path, "wb");
  if (b.len() > 0) f.writeblob(b);
  f.close();
};

local isSpace = function(c) { return c == ' ' || c == '\t' || c == '\r' || c == '\n'; };

// words splits one manifest line on whitespace
local words = function(line) {
  local out = [];
  local i = 0;
  while (i < line.len()) {
    while (i < line.len() && isSpace(line[i])) i++;
    local j = i;
    while (j < line.len() && !isSpace(line[j])) j++;
    if (j > i) out.append(line.slice(i, j));
    i = j;
  }
  return out;
};

local readManifest = function(path) {
  local text = cards.toStr(readFile(path));
  local lines = [];
  local from = 0;
  while (from <= text.len()) {
    local k = text.find("\n", from);
    local raw = text.slice(from, k == null ? text.len() : k);
    local parts = words(raw);
    if (parts.len() > 0 && parts[0][0] != '#') lines.append(parts);
    if (k == null) break;
    from = k + 1;
  }
  return lines;
};

// reportText writes the read report, one Name=value line per counter in
// FORMAT.md's order ("The read report"). The read is strict: a kind mismatch,
// an out-of-range value, a repeat or damage refuses the whole load, so on an
// accepted read the last four lines are zero and false by construction.
local reportText = function(r) {
  return ::format("Retained=%d\nRetainLost=%d\nWidened=%d\nUnknown=%d\n", r.Retained, r.RetainLost, r.Widened,
    r.Unknown) + "KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n";
};

local refuse = function(outdir, name, cls) { writeFile(outdir + "/" + name + ".refused", cls + "\n"); };

local splitList = function(v) {
  local out = [];
  if (v == "") return out;
  local from = 0;
  while (true) {
    local k = v.find(",", from);
    out.append(v.slice(from, k == null ? v.len() : k));
    if (k == null) break;
    from = k + 1;
  }
  return out;
};

// applyEdit applies the one Set* edit a manifest line carries; a refusal is
// the facade's CardError, a malformed line is this driver's own.
local applyEdit = function(record, edit) {
  if (edit.len() != 1) return cards.CardError("an edit line carries exactly one edit", "malformed");
  local eq = edit[0].find("=");
  if (eq == null) return cards.CardError("edit " + edit[0] + " has no =", "malformed");
  local key = edit[0].slice(0, eq), value = edit[0].slice(eq + 1);
  switch (key) {
    case "needs": return cards.SetNeeds(record, splitList(value));
    case "paths": return cards.SetPaths(record, splitList(value));
    case "tier": return cards.SetTier(record, value);
    case "priority": return cards.SetPriority(record, value);
    case "column": return cards.SetColumn(record, value);
    case "holder": return cards.SetHolder(record, value);
  }
  return cards.CardError("unknown edit " + key, "malformed");
};

// view renders the generated view: every field of every table in the unit, in
// table-name order then declaration order, one line each (docs/SPEC-TABLES.md
// §8; FORMAT.md "The view"). Every word comes from the unit's own view rows
// (<Table>TableInfo): the type name, the shape and the bound.
local view = function() {
  local names = [];
  foreach (name, _ in T.Descriptors) names.append(name);
  names.sort();
  local out = "";
  foreach (name in names) {
    foreach (f in T[name + "TableInfo"].fields) {
      local shape = f.isArray ? "list" : (f.pointer ? "blob" : "value");
      local bound = (shape == "value" && f.arrayBound > 0) ? ("" + f.arrayBound) : "-";
      out += name + " " + f.name + " " + f.typeName + " " + shape + " " + bound + "\n";
    }
  }
  return out;
};

local mustRecord = function(v) {
  if (cards.refused(v)) fail(v.cls + ": " + v.message);
  return v;
};

local one = function(surface, c, outdir) {
  local name = c[1], input = c[2];
  local stem = outdir + "/" + name;
  switch (surface) {
    case "load": {
      local record = mustRecord(cards.Load(readFile(input)));
      local text = mustRecord(cards.ToJSON(record));
      writeFile(stem + ".json", text);
      writeFile(stem + ".report", reportText(record.report));
      return;
    }
    case "save":
    case "bounds": {
      local record = cards.FromJSON(readFile(input));
      if (cards.refused(record)) return refuse(outdir, name, record.cls);
      writeFile(stem + ".wire", record.Wire());
      return;
    }
    case "render": {
      local record = mustRecord(cards.Load(readFile(input)));
      local text = mustRecord(cards.Render(record));
      if (SABOTAGE == "render") text = "\x00" + text.slice(1);
      writeFile(stem + ".txt", text);
      return;
    }
    case "parse": {
      local record = cards.Parse(readFile(input));
      if (cards.refused(record)) return refuse(outdir, name, record.cls);
      writeFile(stem + ".wire", record.Wire());
      return;
    }
    case "upgrade": {
      local out = cards.Upgrade(readFile(input));
      if (cards.refused(out)) return refuse(outdir, name, out.cls);
      local wire = out.wire;
      if (SABOTAGE == "upgrade") wire[0] = (wire[0] & 0xff) ^ 0xff;
      writeFile(stem + ".wire", wire);
      writeFile(stem + ".report", reportText(out.report));
      return;
    }
    case "edit": {
      local record = mustRecord(cards.Load(readFile(input)));
      local ok = applyEdit(record, c.slice(3));
      if (cards.refused(ok)) return refuse(outdir, name, ok.cls);
      writeFile(stem + ".wire", record.Wire());
      return;
    }
    case "taskhash": {
      local record = mustRecord(cards.Load(readFile(input)));
      local hex = mustRecord(cards.TaskHash(record));
      if (SABOTAGE == "hash") {
        hex = "";
        for (local i = 0; i < 64; i++) hex += "0";
      }
      writeFile(stem + ".hex", hex);
      return;
    }
    case "view":
      writeFile(outdir + "/view.txt", view());
      return;
  }
};

local main = function(args) {
  if (args.len() < 2) fail("usage: driver <manifest> list | driver <manifest> <surface> <outdir>");
  local manifest = args[0], verb = args[1];
  if (verb == "list") {
    foreach (s in SURFACES) ::print(s + "\n");
    return;
  }
  if (args.len() < 3) fail("usage: driver <manifest> <surface> <outdir>");
  if (SURFACES.find(verb) == null) fail("surface " + verb + " is not one this leg answers");
  foreach (c in readManifest(manifest)) {
    if (c[0] == verb) one(verb, c, args[2]);
  }
};

try {
  main(vargv);
} catch (e) {
  ::error((e instanceof Failed ? e.message : "cards-driver: " + e) + "\n");
}

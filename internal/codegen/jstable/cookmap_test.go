package jstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docs/PORTING.md M19 on the JavaScript cook: a map or list holder is sixteen
// bytes, a self-relative reference and an int32 count, and the cook writer lays
// each holder's whole array in the holder's NODE EXTENT, pre-order, a map's
// entries ascending by key. TestCookWriteAnswersEveryCookWriteRowJavaScript
// holds every cook-write row's bytes to the reference's; these two hold the
// two properties a row of already sorted input cannot show.

// cookMapUnits generates every unit the given cook-write rows name into dir and
// answers each unit's directory.
func cookMapUnits(t *testing.T, dir string, units map[string][]string, rows []cookWriteManifestRow) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range rows {
		if _, done := out[row.unitKey]; done {
			continue
		}
		paths, ok := units[row.unitKey]
		if !ok {
			t.Fatalf("unknown unit %q", row.unitKey)
		}
		files, err := Generate(loadUnitPaths(t, paths))
		if err != nil {
			t.Fatalf("Generate(%s): %v", row.unitKey, err)
		}
		unitDir := filepath.Join(dir, row.unitKey)
		if err := os.MkdirAll(unitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(unitDir, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		out[row.unitKey] = unitDir
	}
	return out
}

func runNodeScript(t *testing.T, node, dir, script string) map[string]string {
	t.Helper()
	path := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, path)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	t.Logf("probe output:\n%s", out)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 {
			got[f[0]+" "+f[1]] = strings.Join(f[2:], " ")
		}
	}
	return got
}

// TestCookMapWriterHoldsTheSortJavaScript: a map is a JavaScript Map, which
// iterates in INSERTION order, so every map-bearing cook-write row is loaded
// from its wire, EVERY Map in the value at every depth is rebuilt in REVERSED
// insertion order, and the cook of the rebuilt value must still be the
// reference's bytes in both orders. The writer holds the sort; a cook that laid
// the entries in the Map's own order reds here
// (tables-js-cook-map-unsorted-negative-control).
func TestCookMapWriterHoldsTheSortJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	units, all := readCookWriteManifest(t, repo)
	var rows []cookWriteManifestRow
	for _, r := range all {
		if r.unitKey == "mapdemo" || (r.unitKey == "listdemo" && r.root == "Army") {
			rows = append(rows, r)
		}
	}
	if len(rows) < 13 {
		t.Fatalf("the manifest names %d map-bearing cook-write rows, want the mapdemo corpus and listdemo's Army", len(rows))
	}
	dir := t.TempDir()
	unitDirs := cookMapUnits(t, dir, units, rows)

	var script strings.Builder
	script.WriteString("import { readFileSync } from \"node:fs\";\n")
	idx := map[string]int{}
	for key, unitDir := range unitDirs {
		idx[key] = len(idx)
		mod := filepath.ToSlash(filepath.Join(unitDir, strings.ToUpper(key[:1])+key[1:]+"Variable.js"))
		fmt.Fprintf(&script, "import * as u%d from %q;\n", idx[key], mod)
	}
	script.WriteString("const cases = [\n")
	for _, r := range rows {
		fmt.Fprintf(&script, "  { name: %q, mod: u%d, root: %q, wire: %q, le: %q, be: %q },\n",
			r.instance, idx[r.unitKey], r.root, filepath.ToSlash(r.wireFile), filepath.ToSlash(r.little), filepath.ToSlash(r.big))
	}
	script.WriteString(cookMapSortDriver)
	got := runNodeScript(t, node, dir, script.String())

	reversed := 0
	for _, r := range rows {
		if v := got["COOK "+r.instance]; v != "OK" {
			t.Errorf("%s (%s.%s): the cook of a value whose Maps were built out of key order is %q, want the reference's bytes in both orders", r.instance, r.unitKey, r.root, v)
		}
		if got["REVERSED "+r.instance] == "YES" {
			reversed++
		}
	}
	if reversed == 0 {
		t.Fatal("no row held a map of two or more entries: the reversal changed nothing and the test proves nothing")
	}
}

const cookMapSortDriver = `];
const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
function reverseMaps(x, seen, flag) {
  if (x === null || typeof x !== "object" || ArrayBuffer.isView(x)) { return x; }
  if (x instanceof Map) {
    if (x.size > 1) { flag.hit = true; }
    return new Map([...x].reverse().map(([k, v]) => [k, reverseMaps(v, seen, flag)]));
  }
  if (seen.has(x)) { return x; }
  seen.add(x);
  if (Array.isArray(x)) {
    for (let i = 0; i < x.length; i++) { x[i] = reverseMaps(x[i], seen, flag); }
    return x;
  }
  for (const k of Object.keys(x)) { x[k] = reverseMaps(x[k], seen, flag); }
  return x;
}
for (const c of cases) {
  const loaded = c.mod[c.root + "Load"](new Uint8Array(readFileSync(c.wire)));
  if (loaded === null) { console.log("COOK " + c.name + " LOAD_FAILED"); continue; }
  const flag = { hit: false };
  const v = reverseMaps(loaded, new Set(), flag);
  console.log("REVERSED " + c.name + " " + (flag.hit ? "YES" : "NO"));
  const n = c.mod[c.root + "CookMeasure"](v);
  if (!(n > 0)) { console.log("COOK " + c.name + " MEASURE_REFUSED"); continue; }
  let verdict = "OK";
  for (const [order, file] of [[1, c.le], [2, c.be]]) {
    const out = new Uint8Array(n);
    if (c.mod[c.root + "CookFrom"](v, out, order) !== true) { verdict = "REFUSED"; break; }
    if (!same(out, new Uint8Array(readFileSync(file)))) { verdict = order === 1 ? "DIFF_LITTLE" : "DIFF_BIG"; break; }
  }
  console.log("COOK " + c.name + " " + verdict);
}
`

// TestCookMapFindJavaScript opens map_full's cook (mapdemo Fleet: string,
// unsigned and signed keys, and a map inside a map value) through the
// generated reader and walks every map IN PLACE: <F>Count and <F>At span the
// entries where they lie, the keys read ascending, <F>Find answers each
// entry's own offset by binary search, and a key no entry holds answers -1.
// The inner maps of loadouts are searched through the entry row's own Find,
// so a search at depth is held too.
func TestCookMapFindJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	units, all := readCookWriteManifest(t, repo)
	var row *cookWriteManifestRow
	for i := range all {
		if all[i].instance == "map_full" {
			row = &all[i]
		}
	}
	if row == nil || row.root != "Fleet" {
		t.Fatal("the manifest names no map_full cook-write row of mapdemo Fleet")
	}
	dir := t.TempDir()
	unitDir := cookMapUnits(t, dir, units, []cookWriteManifestRow{*row})["mapdemo"]
	script := fmt.Sprintf("import { readFileSync } from \"node:fs\";\nimport * as m from %q;\nconst cookPath = %q;\n%s",
		filepath.ToSlash(filepath.Join(unitDir, "FleetCook.js")), filepath.ToSlash(row.little), cookMapFindDriver)
	got := runNodeScript(t, node, dir, script)
	for _, f := range []string{"ships", "by_id", "tiers", "loadouts", "loadouts.value"} {
		v, ok := got["FIND "+f]
		if !ok {
			t.Errorf("%s: the probe did not search it", f)
			continue
		}
		if !strings.HasPrefix(v, "OK ") {
			t.Errorf("%s: %s", f, v)
		}
	}
	if got["FIND searched"] == "0" || got["FIND searched"] == "" {
		t.Error("no entry was searched: the probe proves nothing")
	}
}

const cookMapFindDriver = `
const bytes = new Uint8Array(readFileSync(cookPath));
const h = m.FleetCook.Open(bytes);
if (h === null) { console.log("FIND open REFUSED"); process.exit(0); }
const view = h.View;
const root = h.Region;
let searched = 0;
const cmpBytes = (a, b) => {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) { if (a[i] !== b[i]) { return a[i] - b[i]; } }
  return a.length - b.length;
};
// walk one map: row is the holder's record row, field its member, at the
// holder record's offset, entry the entry row, str whether the key is text
function walk(label, row, field, at, entry, str, inner) {
  const count = row[field + "Count"](view, at);
  const find = row[field + "Find"];
  let prev = null;
  for (let i = 0; i < count; i++) {
    const e = row[field + "At"](view, at, i);
    if (e !== row[field + "At"](view, at, 0) + i * row[field + "Stride"]) { return "BAD_SPAN " + i; }
    const key = str ? entry.Key(bytes, view, e) : entry.Key(view, e);
    if (prev !== null && (str ? cmpBytes(prev, key) >= 0 : !(prev < key))) { return "NOT_ASCENDING " + i; }
    prev = key;
    const hit = str ? find(bytes, view, at, key) : find(view, at, key);
    searched++;
    if (hit !== e) { return "FIND_MISSED entry " + i + " at " + e + " answered " + hit; }
    if (str) {
      const longer = new Uint8Array(key.length + 1);
      longer.set(key);
      longer[key.length] = 0x7f;
      const other = find(bytes, view, at, longer);
      if (other !== -1 && cmpBytes(entry.Key(bytes, view, other), longer) !== 0) { return "FIND_FALSE_HIT"; }
    } else {
      const next = typeof key === "bigint" ? key + 1n : key + 1;
      const other = find(view, at, next);
      if (other !== -1 && entry.Key(view, other) !== next) { return "FIND_FALSE_HIT"; }
    }
    if (inner) { const r = inner(e); if (r !== null) { return r; } }
  }
  if (count > 0) {
    if (str) {
      if (find(bytes, view, at, new Uint8Array([0xff, 0xff])) !== -1) { return "FIND_ABSENT_HIT"; }
    } else if (find(view, at, -32768) !== -1 && entry.Key(view, find(view, at, -32768)) !== -32768) { return "FIND_ABSENT_HIT"; }
  }
  return "OK " + count;
}
console.log("FIND ships " + walk("ships", m.FleetRow, "Ships", root, m.FleetShipsEntryRow, true, null));
console.log("FIND by_id " + walk("by_id", m.FleetRow, "ById", root, m.FleetByIdEntryRow, false, null));
console.log("FIND tiers " + walk("tiers", m.FleetRow, "Tiers", root, m.FleetTiersEntryRow, false, null));
let innerVerdict = "OK 0";
let innerCount = 0;
console.log("FIND loadouts " + walk("loadouts", m.FleetRow, "Loadouts", root, m.FleetLoadoutsEntryRow, true, (e) => {
  const r = walk("loadouts.value", m.FleetLoadoutsEntryRow, "Value", e, m.FleetLoadoutsEntryValueEntryRow, false, null);
  if (!r.startsWith("OK ")) { innerVerdict = r; return r; }
  innerCount += Number(r.slice(3));
  return null;
}));
if (innerVerdict === "OK 0") { innerVerdict = "OK " + innerCount; }
console.log("FIND loadouts.value " + innerVerdict);
console.log("FIND searched " + searched);
`

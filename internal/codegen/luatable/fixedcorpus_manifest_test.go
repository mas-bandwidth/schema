package luatable

// EVERY ROOT OF THE FIXED CORPUS ON THE LUA LEG (EVERY TEST IS A SHARED
// CORPUS, docs/rulings.json every-test-is-a-shared-corpus). The cases are the
// C++ reference's corpus, defined once: `make tables-fixedform-corpus` dumps
// test/tables/fixedform_dump.cpp into build/fixedform-corpus and names every
// file it wrote in manifest.txt. This test walks THAT LIST, never a
// hand-picked one, and answers every root through fixedvector.EveryRoot,
// which judges the answer against the reference's line:
//
//   - a root the reference's own reader refuses (the line's refuse=<reason>:
//     a non-zero dead byte, a value past the writer's range or the reader's
//     bound) must refuse the whole load by that name (§4.5, §4.6;
//     docs/SPEC-TABLES.md §3.3, §3.4, §4);
//   - every other root reads through the generated Lua module of the schema
//     that wrote it, lands the manifest's record count and EVERY VALUE the
//     reference wrote, path by path (values=, with the forged cell and the
//     reader's expect= laid over it), and saves back to the reference's
//     bytes, byte for byte (docs/FIXED-FORM-ALGORITHM.md §3.1, §7); a root
//     whose rewrite the standard canonicalises is held to the bytes the
//     standard names (the compressed float's -0, docs/SPEC-TABLES.md §3.3).
//
// A read and a write wrong the same way round-trip byte for byte, so the
// rewrite alone proves nothing about the values: the landed values are what
// the reference's line holds the read to.
//
// fixedvector.EveryRoot holds the answered set to the manifest's set when the
// test ends, and TestMain (fixedvector.Main) requires the record of this walk
// from the run itself. The corpus missing is red by name, never a skip, once
// the slow gate lets the test run. A root Lua truly answers wrong is in
// luaFixedCorpusOwed with its cause, an exact set: an owed root that starts
// passing is red until its line is removed.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luaFixedCorpusOwed is every corpus root Lua answers wrong today, with the
// cause. An exact set: a listed root that passes is red.
var luaFixedCorpusOwed = map[string]string{}

// luaCorpusVerdict is how the Lua leg reads one corpus root.
type luaCorpusVerdict struct {
	reader string   // the reading schema's base under test/tables (examples/ for tables/examples)
	older  []string // the older writers' bases, handed to the reader as its lineage
	// rewrite is the file the save must equal; nil is the root's own bytes
	rewrite func(t *testing.T, repo string) []byte
	// keepsBytes is false for a read whose rewrite is not the file's bytes:
	// a NEW reader landing an OLD writer's file saves under its own layout
	keepsBytes bool
}

// luaCorpusWriterBase is the schema that wrote a corpus file, from its name
// (the dump's naming, test/tables/fixedform_dump.cpp).
func luaCorpusWriterBase(file string) string {
	name := strings.TrimSuffix(file, ".bin")
	for prefix, base := range map[string]string{"old_": "VOLD_", "new_": "VNEW_", "mid_": "VMID_", "a_": "VBRA_", "b_": "VBRB_"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			return base + rest
		}
	}
	switch {
	case strings.HasPrefix(name, "deprecated_"):
		return "FD1"
	case strings.HasPrefix(name, "dead_"):
		return "DeadBytes"
	}
	return map[string]string{
		"fx1": "FX1", "fx2": "FX2", "p1": "P1", "p3": "P3", "fu1": "FU1", "fu2": "FU2",
		"negative_zero":       "FNZ",
		"keyed":               "examples/Keyed",
		"pack":                "examples/Pack",
		"many_unknown_census": "VOLD_unknown_census",
	}[name]
}

// luaCorpusVerdictOf is the reader one manifest row is read through. A forged
// side (hostile_, past_) is the OLD writer's bytes read by the NEW reader with
// the OLD writer in its lineage, as the reference reads it
// (docs/FIXED-FORM-VERSIONING-TESTS.md); every other row is the writer's own
// schema reading its own bytes back. What the read must come to is the
// manifest's, never this function's.
func luaCorpusVerdictOf(t *testing.T, row fixedvector.ManifestRow) luaCorpusVerdict {
	t.Helper()
	if row.Side == "hostile" || row.Side == "past" {
		return luaCorpusVerdict{reader: "VNEW_" + row.Row, older: []string{"VOLD_" + row.Row}}
	}
	if row.File == fixedvector.NegativeZeroFile {
		// -0 in a compressed float reads as +0 and writes +0's bytes: the
		// canonical twin the dump's own inputs carry (docs/SPEC-TABLES.md §3.3)
		return luaCorpusVerdict{reader: "FNZ", keepsBytes: true, rewrite: func(t *testing.T, repo string) []byte {
			return readFile2(t, filepath.Join(repo, "test", "tables", "fnz", "negative_zero_control.bin"))
		}}
	}
	base := luaCorpusWriterBase(row.File)
	if base == "" {
		t.Fatalf("no schema is known for the corpus root %s (row=%s side=%s root=%s): name it in luaCorpusWriterBase", row.File, row.Row, row.Side, row.Root)
	}
	return luaCorpusVerdict{reader: base, keepsBytes: true}
}

func readFile2(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// luaCorpusSchemaPath is a reader base's schema file.
func luaCorpusSchemaPath(repo, base string) string {
	if rest, ok := strings.CutPrefix(base, "examples/"); ok {
		return filepath.Join(repo, "tables", "examples", rest+".schema")
	}
	return filepath.Join(repo, "test", "tables", base+".schema")
}

// luaCorpusDumper prints a landed value one leaf per line: a table walked in
// key order, text as `s:` and its bytes in hex (a byte buffer may hold a
// newline), a float as its float64 bits, anything else as Lua prints it.
const luaCorpusDumper = `
local function hexof(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
local function dump(p, v)
  if type(v) == "table" then
    local keys = {}
    for k in pairs(v) do keys[#keys + 1] = k end
    table.sort(keys, function(a, b) return tostring(a) < tostring(b) end)
    for _, k in ipairs(keys) do dump(p .. "." .. tostring(k), v[k]) end
  elseif type(v) == "string" then
    print(p .. "=s:" .. hexof(v))
  elseif math.type(v) == "float" then
    print(p .. "=" .. string.format("0x%016X", string.unpack("<I8", string.pack("<d", v))))
  else
    print(p .. "=" .. tostring(v))
  end
end
`

// luaCorpusProbe reads one root and prints the verdict: "refused <name>", or
// "ok <records>", the rewrite's hex and the records.
const luaCorpusProbe = `local T = require(%q)
local I = require("inputs")
%s
local ok, rows = pcall(T.FixedLoad, %q, I.file)
if not ok then
  print("refused " .. (tostring(rows):match("([%%w_]+)$") or tostring(rows)))
  return
end
print(string.format("ok %%d", #rows))
local sok, out = pcall(T.FixedSave, %q, rows)
if not sok then
  print("save refused " .. tostring(out))
  return
end
print("rewrite=" .. (out:gsub(".", function(c) return string.format("%%02x", c:byte()) end)))
for i, r in ipairs(rows) do dump("r" .. (i - 1), r) end
`

// luaManifestIndex is one `[i]` subscript of a manifest path.
var luaManifestIndex = regexp.MustCompile(`\[(\d+)\]`)

// luaKey is a manifest path in the Lua dump's spelling: an array element
// `[i]` is the sequence's `.<i+1>`.
func luaKey(path string) string {
	return luaManifestIndex.ReplaceAllStringFunc(path, func(m string) string {
		i, _ := strconv.Atoi(m[1 : len(m)-1])
		return "." + strconv.Itoa(i+1)
	})
}

// luaEnumAt is the enum a manifest path's leaf is declared as, walking the
// root's fields (a union arm by its name), or nil when the leaf is no enum.
func luaEnumAt(u *ir.Unit, root, path string) *ir.Enum {
	segs := strings.Split(path, ".")
	st, un := u.Tables[root], (*ir.Union)(nil)
	for i, seg := range segs[1:] {
		name, _, _ := strings.Cut(seg, "[")
		var f *ir.Field
		switch {
		case st != nil:
			for _, x := range st.Fields {
				if x.Name == name {
					f = x
				}
			}
		case un != nil:
			for _, v := range un.Variants {
				if v.Name == name {
					f = v.F
				}
			}
		}
		if f == nil || f.Type.Kind != ir.TNamed {
			return nil
		}
		st, un = nil, nil
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			st = r
		case *ir.Union:
			un = r
		case *ir.Enum:
			if i == len(segs)-2 {
				return r
			}
			return nil
		default:
			return nil
		}
	}
	return nil
}

// luaSpell is one dumped Lua leaf in the manifest's spelling: a bool as the
// wire's 1/0, an enum's variant name as its ordinal (variants pack from 1),
// text quoted as the manifest quotes it, anything else as dumped.
func luaSpell(v string, enum *ir.Enum) (string, error) {
	switch v {
	case "true":
		return "1", nil
	case "false":
		return "0", nil
	}
	h, ok := strings.CutPrefix(v, "s:")
	if !ok {
		return v, nil
	}
	raw, err := hex.DecodeString(h)
	if err != nil {
		return "", fmt.Errorf("the dumped text %q is not hex: %w", v, err)
	}
	if enum != nil {
		for i, name := range enum.Variants {
			if name == string(raw) {
				return strconv.Itoa(i + 1), nil
			}
		}
		return "", fmt.Errorf("the enum %s has no variant %q", enum.Name, raw)
	}
	return fixedvector.Text(string(raw)), nil
}

// luaLanded is the value Lua landed at one manifest path, in the manifest's
// spelling. Lua stores some of what the manifest spells as its own path: a
// text or byte buffer's length is the string's, an array's count is the
// sequence's length, a byte buffer's element is a byte of the string, a
// union's selector is `tag`, and an enum at None is nil.
func luaLanded(dump map[string]string, enumAt func(string) *ir.Enum, path string) (string, bool, error) {
	if v, ok := dump[luaKey(path)]; ok {
		s, err := luaSpell(v, enumAt(path))
		return s, err == nil, err
	}
	text := func(key string) ([]byte, bool) {
		h, ok := strings.CutPrefix(dump[key], "s:")
		if !ok {
			return nil, false
		}
		raw, err := hex.DecodeString(h)
		return raw, err == nil
	}
	if base, ok := strings.CutSuffix(path, "_length"); ok {
		if raw, ok := text(luaKey(base)); ok {
			return strconv.Itoa(len(raw)), true, nil
		}
	}
	if base, ok := strings.CutSuffix(path, "_count"); ok {
		prefix := luaKey(base) + "."
		seen := map[string]bool{}
		for k := range dump {
			if rest, ok := strings.CutPrefix(k, prefix); ok {
				idx, _, _ := strings.Cut(rest, ".")
				if _, err := strconv.Atoi(idx); err == nil {
					seen[idx] = true
				}
			}
		}
		return strconv.Itoa(len(seen)), true, nil
	}
	if base, ok := strings.CutSuffix(path, ".type"); ok {
		if v, ok := dump[luaKey(base)+".tag"]; ok {
			return v, true, nil
		}
	}
	if strings.HasSuffix(path, "]") {
		open := strings.LastIndex(path, "[")
		if raw, ok := text(luaKey(path[:open])); ok {
			i, err := strconv.Atoi(path[open+1 : len(path)-1])
			if err == nil && i < len(raw) {
				return strconv.Itoa(int(raw[i])), true, nil
			}
		}
	}
	if enumAt(path) != nil {
		return "0", true, nil // an enum at None is nil in Lua
	}
	return "", false, nil
}

// luaCorpusAnswer runs one root, hands Lua's answer to the harness, and
// returns everything wrong with it: the harness's verdict against the
// manifest, and the rewrite against the reference's bytes.
func luaCorpusAnswer(t *testing.T, repo, corpus string, row fixedvector.ManifestRow, v luaCorpusVerdict, answers *fixedvector.Answers) []string {
	t.Helper()
	unit := luarun.LoadUnit(t, luaCorpusSchemaPath(repo, v.reader))
	var lineage map[string][]FixedLineageEntry
	if len(v.older) > 0 {
		lineage = map[string][]FixedLineageEntry{}
		for _, base := range v.older {
			o := luarun.LoadUnit(t, luaCorpusSchemaPath(repo, base))
			e, ok := FixedLineageOf(o, row.Root)
			if !ok {
				t.Fatalf("%s: no fixed root %s in %s", row.File, row.Root, base)
			}
			lineage[row.Root] = append(lineage[row.Root], e)
		}
	}
	g := luaEvoGen(t, unit, lineage)
	wire := readFile2(t, filepath.Join(corpus, row.File))
	out := g.run(t, fmt.Sprintf(luaCorpusProbe, g.mod, luaCorpusDumper, row.Root, row.Root), map[string][]byte{"file": wire})
	lines := strings.Split(out, "\n")
	head := lines[0]
	if name, ok := strings.CutPrefix(head, "refused "); ok {
		return answers.Check(row, fixedvector.Answer{Refused: name})
	}
	countText, ok := strings.CutPrefix(head, "ok ")
	records, err := strconv.Atoi(countText)
	if !ok || err != nil {
		answers.Check(row, fixedvector.Answer{Refused: "the probe printed no verdict"})
		return []string{fmt.Sprintf("Lua's probe printed no verdict: %q", out)}
	}
	if len(lines) < 2 {
		answers.Check(row, fixedvector.Answer{Refused: "the probe printed no rewrite"})
		return []string{fmt.Sprintf("Lua's probe printed no rewrite: %q", out)}
	}
	dump := map[string]string{}
	for _, l := range lines[2:] {
		k, val, _ := strings.Cut(l, "=")
		dump[k] = val
	}
	enumAt := func(path string) *ir.Enum { return luaEnumAt(unit, row.Root, path) }
	got := fixedvector.Answer{Records: records, Values: map[string]string{}}
	var wrong []string
	for _, lv := range row.Landed {
		s, ok, err := luaLanded(dump, enumAt, lv.Path)
		if err != nil {
			wrong = append(wrong, fmt.Sprintf("%s: %v", lv.Path, err))
		}
		if ok {
			got.Values[lv.Path] = s
		}
	}
	wrong = append(wrong, answers.Check(row, got)...)
	rewriteHex, ok := strings.CutPrefix(lines[1], "rewrite=")
	if !ok {
		return append(wrong, fmt.Sprintf("Lua's save: %s", lines[1]))
	}
	if !v.keepsBytes {
		return wrong
	}
	back, err := hex.DecodeString(rewriteHex)
	if err != nil {
		return append(wrong, fmt.Sprintf("Lua's rewrite is not hex: %v", err))
	}
	want := wire
	if v.rewrite != nil {
		want = v.rewrite(t, repo)
	}
	if !bytes.Equal(back, want) {
		at := 0
		for at < len(back) && at < len(want) && back[at] == want[at] {
			at++
		}
		wrong = append(wrong, fmt.Sprintf("Lua's rewrite differs from the reference's bytes at byte %d (%d bytes, want %d)", at, len(back), len(want)))
	}
	return wrong
}

// TestLuaFixedCorpusEveryRoot answers every root the corpus manifest names.
func TestLuaFixedCorpusEveryRoot(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	answers, rows := fixedvector.EveryRoot(t, "lua", corpus)
	inManifest := map[string]bool{}
	for _, r := range rows {
		inManifest[r.File] = true
	}
	for file := range luaFixedCorpusOwed {
		if !inManifest[file] {
			t.Errorf("luaFixedCorpusOwed names %s, which the corpus manifest does not carry", file)
		}
	}
	for _, row := range rows {
		t.Run(strings.TrimSuffix(row.File, ".bin"), func(t *testing.T) {
			t.Parallel()
			wrong := luaCorpusAnswer(t, repo, corpus, row, luaCorpusVerdictOf(t, row), answers)
			cause, owed := luaFixedCorpusOwed[row.File]
			switch {
			case owed && len(wrong) == 0:
				t.Errorf("%s: Lua now answers it right; remove its luaFixedCorpusOwed line (owed: %s)", row.File, cause)
			case owed:
				t.Logf("%s: owed: %s (%s)", row.File, cause, strings.Join(wrong, "; "))
			default:
				for _, w := range wrong {
					t.Errorf("%s: Lua %s", row.File, w)
				}
			}
		})
	}
}

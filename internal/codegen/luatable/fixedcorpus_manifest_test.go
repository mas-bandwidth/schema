package luatable

// EVERY ROOT OF THE FIXED CORPUS ON THE LUA LEG (EVERY TEST IS A SHARED
// CORPUS). The cases are the C++ reference's corpus, defined once:
// `make tables-fixedform-corpus` dumps test/tables/fixedform_dump.cpp into
// build/fixedform-corpus and names every file it wrote in manifest.txt. This
// test walks THAT LIST, never a hand-picked one, and answers every root:
//
//   - an ordinary root reads through the generated Lua module of the schema
//     that wrote it, lands the manifest's record count, and saves back to the
//     reference's bytes, byte for byte (docs/FIXED-FORM-ALGORITHM.md §3.1, §7);
//   - a root the standard says refuses (a non-zero dead byte, a value past the
//     writer's range or the reader's bound, a non-finite float) refuses the
//     whole load by its name (§4.5, §4.6; docs/SPEC-TABLES.md §3.3, §3.4, §4);
//   - a root whose rewrite the standard canonicalises is held to the bytes the
//     standard names (the compressed float's -0, docs/SPEC-TABLES.md §3.3).
//
// fixedvector.EveryRoot holds the answered set to the manifest's set when the
// test ends: a root dropped from the walk is red by name. The corpus missing is
// red by name, never a skip, once the slow gate lets the test run. A root Lua
// truly answers wrong is in luaFixedCorpusOwed with its cause, an exact set: an
// owed root that starts passing is red until its line is removed.

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
)

// luaFixedCorpusOwed is every corpus root Lua answers wrong today, with the
// cause. An exact set: a listed root that passes is red.
var luaFixedCorpusOwed = map[string]string{}

// luaCorpusVerdict is what one corpus root owes on the Lua leg.
type luaCorpusVerdict struct {
	reader string   // the reading schema's base under test/tables (examples/ for tables/examples)
	older  []string // the older writers' bases, handed to the reader as its lineage
	refuse string   // the refusal's name; "" for a root that reads
	// rewrite is the file the save must equal; nil is the root's own bytes
	rewrite func(t *testing.T, repo, corpus string) []byte
	// dump, for a read whose rewrite the test does not hold, is the values
	// the read must land, Lua dump path to value
	dump map[string]string
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

// luaCorpusVerdictOf is the verdict the standard asks of one manifest row.
// The manifest decides a non-finite root (nonFinite) and a shared vector's
// refusal (internal/fixedvector); the rest is the writer's own schema reading
// its own bytes back.
func luaCorpusVerdictOf(t *testing.T, corpus string, row fixedvector.ManifestRow, nonFinite map[string]bool) luaCorpusVerdict {
	t.Helper()
	for _, vec := range fixedvector.WriterRange {
		if vec.File == row.File {
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if !want.Refused {
				t.Fatalf("%s: the C++ reference landed a writer's-range vector: %+v", row.File, want)
			}
			return luaCorpusVerdict{reader: "VNEW_" + vec.Row, older: []string{"VOLD_" + vec.Row}, refuse: "out_of_range"}
		}
	}
	for _, vec := range fixedvector.DeadBytes {
		if vec.File == row.File {
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused == vec.Reads {
				t.Fatalf("%s: the manifest says refused=%v, DeadBytes says reads=%v", vec.File, want.Refused, vec.Reads)
			}
			if !vec.Reads {
				// a non-zero dead byte REFUSES wire_damaged (docs/SPEC-TABLES.md
				// §3.4's storage table; docs/rulings.json fixed-dead-bytes-are-zero)
				return luaCorpusVerdict{reader: "DeadBytes", refuse: "wire_damaged"}
			}
			return luaCorpusVerdict{reader: "DeadBytes"}
		}
	}
	switch row.File {
	case fixedvector.NegativeZeroFile:
		// -0 in a compressed float reads as +0 and writes +0's bytes: the
		// canonical twin the dump's own inputs carry (docs/SPEC-TABLES.md §3.3)
		return luaCorpusVerdict{reader: "FNZ", rewrite: func(t *testing.T, repo, _ string) []byte {
			return readFile2(t, filepath.Join(repo, "test", "tables", "fnz", "negative_zero_control.bin"))
		}}
	case "new_fixed_I_grow.bin", "new_fixed_I_grow_element.bin":
		// the reference wrote raw 1000 into a fixed(·,4) whose max is 7: past
		// the reader's own bound, so the one strict read refuses it whole
		// (docs/SPEC-TABLES.md §4, docs/plans/strict-only.md)
		return luaCorpusVerdict{reader: luaCorpusWriterBase(row.File), refuse: "out_of_range"}
	case "hostile_cfloat_range_widen.bin":
		// an OLD writer's aim forged to 1.5, inside the NEW reader's [-2, 2]:
		// it lands whole (docs/FIXED-FORM-VERSIONING-TESTS.md, the compressed
		// float hostile pass, schema#1164 B)
		return luaCorpusVerdict{reader: "VNEW_cfloat_range_widen", older: []string{"VOLD_cfloat_range_widen"},
			dump: map[string]string{"r0.lead": "1", "r0.aim": "0x3FF8000000000000", "r0.trail": "2"}}
	case "past_cfloat_range_widen.bin":
		// the same aim forged to 5.0, past the reader's bound too: refused
		return luaCorpusVerdict{reader: "VNEW_cfloat_range_widen", older: []string{"VOLD_cfloat_range_widen"}, refuse: "out_of_range"}
	}
	base := luaCorpusWriterBase(row.File)
	if base == "" {
		t.Fatalf("no schema is known for the corpus root %s (row=%s side=%s root=%s): name it in luaCorpusWriterBase", row.File, row.Row, row.Side, row.Root)
	}
	if nonFinite[row.File] {
		return luaCorpusVerdict{reader: base, refuse: "non_finite_float"}
	}
	return luaCorpusVerdict{reader: base}
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

// luaCorpusNonFiniteValue is one manifest value spelled as a float's bits.
var luaCorpusNonFiniteValue = regexp.MustCompile(`=[^|,]*\|0x([0-9A-Fa-f]{16}|[0-9A-Fa-f]{8})$`)

// luaCorpusNonFinite reads which manifest files carry a NaN or an infinity:
// under the finite rule (docs/SPEC-TABLES.md §3.3) each is a must-refuse root
// until the reference's corpus turns finite, and the manifest decides.
func luaCorpusNonFinite(t *testing.T, corpus string) map[string]bool {
	t.Helper()
	data := readFile2(t, filepath.Join(corpus, "manifest.txt"))
	out := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		head, values, ok := strings.Cut(line, " values=")
		if !ok {
			continue
		}
		file := ""
		for field := range strings.FieldsSeq(head) {
			if v, ok := strings.CutPrefix(field, "file="); ok {
				file = v
			}
		}
		for value := range strings.SplitSeq(values, ",") {
			m := luaCorpusNonFiniteValue.FindStringSubmatch(value)
			if m == nil {
				continue
			}
			bits, err := strconv.ParseUint(m[1], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			if len(m[1]) == 8 && bits&0x7F800000 == 0x7F800000 || len(m[1]) == 16 && bits&0x7FF0000000000000 == 0x7FF0000000000000 {
				out[file] = true
			}
		}
	}
	return out
}

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

// luaCorpusAnswer runs one root and returns what is wrong with Lua's answer,
// "" when it is the verdict's.
func luaCorpusAnswer(t *testing.T, repo, corpus string, row fixedvector.ManifestRow, v luaCorpusVerdict) string {
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
	out := g.run(t, fmt.Sprintf(luaCorpusProbe, g.mod, luaEvoDumper, row.Root, row.Root), map[string][]byte{"file": wire})
	lines := strings.Split(out, "\n")
	head := lines[0]
	if v.refuse != "" {
		if head == "refused "+v.refuse {
			return ""
		}
		return fmt.Sprintf("Lua answered %q, the standard asks refused %s", head, v.refuse)
	}
	if head != fmt.Sprintf("ok %d", row.Records) {
		return fmt.Sprintf("Lua answered %q, the standard asks a clean read of the manifest's %d records", head, row.Records)
	}
	if len(lines) < 2 {
		return fmt.Sprintf("Lua's probe printed no rewrite: %q", out)
	}
	rewriteHex, ok := strings.CutPrefix(lines[1], "rewrite=")
	if !ok {
		return fmt.Sprintf("Lua's save: %s", lines[1])
	}
	if v.dump != nil {
		got := map[string]string{}
		for _, l := range lines[2:] {
			k, val, _ := strings.Cut(l, "=")
			got[k] = val
		}
		for k, want := range v.dump {
			if got[k] != want {
				return fmt.Sprintf("Lua landed %s=%q, the standard asks %s", k, got[k], want)
			}
		}
		return ""
	}
	back, err := hex.DecodeString(rewriteHex)
	if err != nil {
		return fmt.Sprintf("Lua's rewrite is not hex: %v", err)
	}
	want := wire
	if v.rewrite != nil {
		want = v.rewrite(t, repo, corpus)
	}
	if !bytes.Equal(back, want) {
		at := 0
		for at < len(back) && at < len(want) && back[at] == want[at] {
			at++
		}
		return fmt.Sprintf("Lua's rewrite differs from the reference's bytes at byte %d (%d bytes, want %d)", at, len(back), len(want))
	}
	return ""
}

// TestLuaFixedCorpusEveryRoot answers every root the corpus manifest names.
func TestLuaFixedCorpusEveryRoot(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	rows, err := fixedvector.Manifest(corpus)
	if err != nil {
		t.Fatal(err)
	}
	inManifest := map[string]bool{}
	for _, r := range rows {
		inManifest[r.File] = true
	}
	for file := range luaFixedCorpusOwed {
		if !inManifest[file] {
			t.Errorf("luaFixedCorpusOwed names %s, which the corpus manifest does not carry", file)
		}
	}
	nonFinite := luaCorpusNonFinite(t, corpus)
	answers := fixedvector.EveryRoot(t, "lua", rows)
	for _, row := range rows {
		t.Run(strings.TrimSuffix(row.File, ".bin"), func(t *testing.T) {
			t.Parallel()
			v := luaCorpusVerdictOf(t, corpus, row, nonFinite)
			wrong := luaCorpusAnswer(t, repo, corpus, row, v)
			cause, owed := luaFixedCorpusOwed[row.File]
			switch {
			case owed && wrong == "":
				t.Errorf("%s: Lua now answers it right; remove its luaFixedCorpusOwed line (owed: %s)", row.File, cause)
			case owed:
				t.Logf("%s: owed: %s (%s)", row.File, cause, wrong)
			case wrong != "":
				t.Errorf("%s: %s", row.File, wrong)
			}
			answers.Answer(row.File)
		})
	}
}

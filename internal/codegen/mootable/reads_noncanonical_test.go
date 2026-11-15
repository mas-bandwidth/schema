package mootable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMooReadsRefuseNonCanonical holds the moo table reads to the shared
// non-canonical spelling vectors (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4;
// docs/rulings.json reads-refuse-non-canonical): every row of
// testdata/wire/non-canonical/MANIFEST.tsv is generated for this leg and read
// through every read entry it has for the row's form — the plain load, the
// form dispatch's strict read and the retaining load for the variable form,
// the message load for the message form, the announcement read for the
// announcement. A canonical row reads and the read value re-saves to the same
// bytes; every other row is refused. The verdict is the contract, so no row
// compares a refusal's reason. moo refuses in every build: there is no
// release-only check to skip.

type nonCanonicalRow struct {
	file    string
	form    string
	spell   string
	verdict string
}

func readNonCanonicalManifest(t *testing.T) []nonCanonicalRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "non-canonical", "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []nonCanonicalRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d columns, want file, form, case, verdict", i+1, len(f))
		}
		if f[0] == "file" {
			continue
		}
		switch f[1] {
		case "variable", "message", "announcement":
		default:
			t.Fatalf("MANIFEST.tsv:%d: form %q", i+1, f[1])
		}
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q", i+1, f[3])
		}
		rows = append(rows, nonCanonicalRow{file: f[0], form: f[1], spell: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// nonCanonicalUnit is one vectors unit's emitted moo files.
type nonCanonicalUnit struct {
	unit  *ir.Unit
	files map[string][]byte
}

func generateNonCanonicalUnit(t *testing.T, schema string) nonCanonicalUnit {
	t.Helper()
	u, err := compiler.New().Load([]string{filepath.Join(root, schema)})
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	return nonCanonicalUnit{unit: u, files: files}
}

// nonCanonicalProbe is the main for one row: one "entry=verdict" line per
// read entry, and for a read value the re-saved bytes as "save=<hex>".
const nonCanonicalProbe = `local function hexOf(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function verdict(ok: boolean): string
	return if ok then "read" else "refused"
end
local rootName = %q
local form = %q
if form == "announcement" then
	local v = MS.NewVocabulary()
	print("announcement=" .. verdict(MS.AnnounceRead(v, inputs.wire, {})))
elseif form == "variable" then
	local plain = L.load(rootName, inputs.wire, L.newReport())
	print("plain=" .. verdict(plain ~= nil))
	local strict = Rf.ReadStrict(T, rootName, inputs.wire, L.newReport())
	print("strict=" .. verdict(strict ~= nil))
	local store = Rt.newStore(65536)
	local retained = Rt.LoadRetain(rootName, inputs.wire, L.newReport(), store)
	print("retain=" .. verdict(retained ~= nil))
	if plain ~= nil then
		local out = Sv.save(St.lookup(rootName), plain)
		print("save=" .. (if out ~= nil then hexOf(out) else "refused"))
	end
	if retained ~= nil then
		local out = Rt.SaveRetain(rootName, retained, store, L.newReport())
		print("saveretain=" .. (if out ~= nil then hexOf(out) else "refused"))
	end
else
	local v = MS.NewVocabulary()
	if not MS.AnnounceRead(v, inputs.ann, {}) then
		print("announce=refused")
		return
	end
	local n, values = MS.LoadMessages(v, rootName, inputs.wire, 256, {})
	print("message=" .. verdict(n > 0))
	if n > 0 then
		local out = MS.SaveMessages(rootName, values, {})
		print("save=" .. (if out ~= nil then hexOf(out) else "refused"))
	end
end
`

func TestMooReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo, table_retain.moo and table_message.moo)")
	rows := readNonCanonicalManifest(t)
	units := map[string]nonCanonicalUnit{
		"noncanonical": generateNonCanonicalUnit(t, "test/non-canonical/NonCanonical.schema"),
		"one":          generateNonCanonicalUnit(t, "test/message-one-encoding/One.schema"),
	}
	for _, row := range rows {
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()
			unit, rootName, mod := "noncanonical", "Root", "NonCanonicalTable"
			ann := filepath.Join("testdata", "wire", "non-canonical", "announcement.bin")
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				unit, rootName, mod = "one", "One", "OneTable"
				ann = filepath.Join("testdata", "wire", "message-one-encoding", "announcement.bin")
			} else if row.form == "message" {
				rootName = "Message"
			}
			nu := units[unit]
			if _, ok := nu.files[mod+".moo"]; !ok {
				t.Fatalf("unit %s emitted no %s.moo", unit, mod)
			}
			g := writeUnit(t, nu.unit, nu.files)
			inputs := map[string][]byte{"wire": readRoot(t, filepath.Join("testdata", "wire", "non-canonical", row.file))}
			if row.form == "message" {
				inputs["ann"] = readRoot(t, ann)
			}
			main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal Rf = require(\"./table_refusal\")\nlocal Rt = require(\"./table_retain\")\nlocal Sv = require(\"./table_save\")\nlocal MS = require(\"./table_message\")\nlocal St = require(\"./schema_tables\")\n" +
				g.requireAll() + fmt.Sprintf("local T = require(\"./%s\")\n", mod) + fmt.Sprintf(nonCanonicalProbe, rootName, row.form)
			out := g.run(t, main, inputs)
			got := map[string]string{}
			for line := range strings.SplitSeq(out, "\n") {
				if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
					got[k] = v
				}
			}
			var entries, saves []string
			switch row.form {
			case "announcement":
				entries = []string{"announcement"}
			case "variable":
				entries = []string{"plain", "strict", "retain"}
				saves = []string{"save", "saveretain"}
			case "message":
				entries = []string{"message"}
				saves = []string{"save"}
			}
			for _, entry := range entries {
				if got[entry] != row.verdict {
					t.Errorf("%s (%s): %s = %q, want %q\nprobe:\n%s", row.file, row.spell, entry, got[entry], row.verdict, out)
				}
			}
			if row.verdict != "read" {
				return
			}
			want := hex.EncodeToString(inputs["wire"])
			for _, s := range saves {
				if got[s] != want {
					t.Errorf("%s: the read value re-saves (%s) to %q, want %s", row.file, s, got[s], want)
				}
			}
		})
	}
}

// TestLuauReadsRefuseNonCanonical holds the Luau table reads to the shared
// non-canonical spelling vectors (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4;
// docs/rulings.json reads-refuse-non-canonical): every row of
// testdata/wire/non-canonical/MANIFEST.tsv is generated for this leg and read
// through each of its read entries. A canonical row reads and re-saves to the
// same bytes; every other row is refused. The verdict is the contract, so no
// row compares a refusal's reason.
package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type nonCanonicalRow struct {
	file     string
	form     string
	spelling string
	verdict  string
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
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
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
		rows = append(rows, nonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// generateNonCanonicalUnit emits one vectors unit's Luau type and table
// surfaces. The returned bytes are copied into each subtest's own directory,
// so the subtests share the generation and not the files.
func generateNonCanonicalUnit(t *testing.T, schema string) map[string][]byte {
	t.Helper()
	u, err := compiler.New().Load([]string{filepath.Join(root, schema)})
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("%s: luau: %v", schema, err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("%s: luautable: %v", schema, err)
	}
	maps.Copy(files, tables)
	return files
}

// nonCanonicalProbe is the main for one row. It reads the same wire through
// the plain load, the retaining load (strict and walk), and the message load
// (strict and walk), printing one "name=verdict" line each; a canonical row
// also prints the re-saved bytes.
const nonCanonicalProbe = `local inputs = require("./inputs")
local L = require("./schema_load")
local R = require("./schema_retain")
local S = require("./schema_save")
local M = require("./schema_message")
local St = require("./schema_tables")
require("./__MOD__")

local function report(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
end
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end

local root = __ROOT__
local form = __FORM__
if form == "announcement" then
	local vocab = M.NewVocabulary()
	local rep = { malformed = false, verdict = 0, reason = "", unknown = 0 }
	print("announcement=" .. (if M.AnnounceRead(vocab, inputs.wire, rep) then "read" else "refused"))
	return
end
if form == "variable" then
	local plain = L.load(root, inputs.wire, report())
	print("plain=" .. (if plain ~= nil then "read" else "refused"))
	local retained = R.LoadRetain(root, inputs.wire, report(), 65536)
	print("retain=" .. (if retained ~= nil then "read" else "refused"))
	local walked = R.loadRetainWalk(root, inputs.wire, report(), 65536)
	print("retainwalk=" .. (if walked ~= nil then "read" else "refused"))
	if plain ~= nil then
		local saved = S.save(St.lookup(root), plain)
		print("save=" .. (if saved ~= nil then hex(saved) else "refused"))
	end
	return
end
local vocab = M.NewVocabulary()
local ar = { malformed = false, verdict = 0, reason = "", unknown = 0 }
if not M.AnnounceRead(vocab, inputs.ann, ar) then print("announce=refused"); return end
local rep = report()
local count, bodies = M.LoadMessages(vocab, root, inputs.wire, 256, rep)
print("message=" .. (if count > 0 then "read" else "refused"))
local wrep = report()
local wcount = M.loadMessagesWalk(vocab, root, inputs.wire, 256, wrep)
print("messagewalk=" .. (if wcount > 0 then "read" else "refused"))
if count > 0 then
	local saved = M.SaveMessages(vocab, root, bodies, report())
	print("save=" .. (if saved ~= nil then hex(saved) else "refused"))
end
`

func TestLuauReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	rows := readNonCanonicalManifest(t)
	units := map[string]map[string][]byte{}
	for key, schema := range map[string]string{
		"noncanonical": "test/non-canonical/NonCanonical.schema",
		"one":          "test/message-one-encoding/One.schema",
	} {
		units[key] = generateNonCanonicalUnit(t, schema)
	}

	for _, row := range rows {
		row := row
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()
			unit, rootName, announcement := "noncanonical", "Root", ""
			switch {
			case row.form == "announcement":
				rootName = ""
			case strings.HasPrefix(row.file, "../message-one-encoding/"):
				unit, rootName = "one", "One"
				announcement = filepath.Join(root, "testdata", "wire", "message-one-encoding", "announcement.bin")
			case row.form == "message":
				rootName = "Message"
			}
			if announcement == "" && row.form != "announcement" {
				announcement = filepath.Join(root, "testdata", "wire", "non-canonical", "announcement.bin")
			}

			wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "non-canonical", row.file))
			if err != nil {
				t.Fatalf("read vector: %v", err)
			}
			dir := t.TempDir()
			for name, data := range units[unit] {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			inputs := map[string][]byte{"wire": wire}
			if announcement != "" {
				ann, err := os.ReadFile(announcement)
				if err != nil {
					t.Fatal(err)
				}
				inputs["ann"] = ann
			}
			module := "NonCanonicalTable"
			if unit == "one" {
				module = "OneTable"
			}
			main := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(nonCanonicalProbe, "__ROOT__", fmt.Sprintf("%q", rootName)), "__FORM__", fmt.Sprintf("%q", row.form)), "__MOD__", module)
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", inputs)))
			got := map[string]string{}
			for line := range strings.SplitSeq(out, "\n") {
				k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
				if ok {
					got[k] = v
				}
			}

			var entries []string
			switch row.form {
			case "announcement":
				entries = []string{"announcement"}
			case "variable":
				entries = []string{"plain", "retain", "retainwalk"}
			case "message":
				entries = []string{"message", "messagewalk"}
			}
			want := "read"
			if row.verdict == "refused" {
				want = "refused"
			}
			for _, entry := range entries {
				if got[entry] != want {
					t.Errorf("%s: %s = %q, want %q (probe: %q)", row.file, entry, got[entry], want, out)
				}
			}
			if row.verdict != "read" || row.form == "announcement" {
				return
			}
			saved, ok := got["save"]
			if !ok {
				t.Fatalf("%s: the canonical read printed no save (%q)", row.file, out)
			}
			if saved != hex.EncodeToString(wire) {
				t.Errorf("%s: the read value re-saves to %s, want %s", row.file, saved, hex.EncodeToString(wire))
			}
		})
	}
}

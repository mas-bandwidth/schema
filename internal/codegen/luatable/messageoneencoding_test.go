// TestLuaLoadMessagesOneEncoding pins docs/SPEC-TABLES.md §3.3's one-encoding
// rule on the Lua message read: a batch has exactly one encoding for its value,
// so LoadMessages reads the canonical batch and refuses a body that writes a
// field at the default its writer elides, writes a record's fields out of
// declaration order, or writes a union field with the None reference. The
// vectors and their verdicts are testdata/wire/message-one-encoding/'s
// manifest (docs/rulings.json `message-one-encoding`); the refusal's reason is
// per-language detail, so this test asserts the verdict alone. It also pins
// the write side: SaveMessages of the same value reproduces the canonical
// batch's bytes.
package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

type luaMessageOneEncodingRow struct {
	file    string
	verdict string
}

// luaMessageOneEncodingManifest reads the vectors' manifest table: every row
// names one file beside its verdict.
func luaMessageOneEncodingManifest(t *testing.T, path string) []luaMessageOneEncodingRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("message-one-encoding manifest: %v", err)
	}
	var rows []luaMessageOneEncodingRow
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		file := strings.Trim(strings.TrimSpace(cells[1]), "`")
		verdict := strings.TrimSpace(cells[3])
		if !strings.HasSuffix(file, ".bin") || (verdict != "read" && verdict != "refused") {
			continue
		}
		rows = append(rows, luaMessageOneEncodingRow{file: file, verdict: verdict})
	}
	if len(rows) == 0 {
		t.Fatal("message-one-encoding manifest names no vector rows")
	}
	return rows
}

func TestLuaLoadMessagesOneEncoding(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "message-one-encoding")
	rows := luaMessageOneEncodingManifest(t, filepath.Join(vectors, "README.md"))

	var batches []luaMessageOneEncodingRow
	for _, row := range rows {
		if row.file == "announcement.bin" {
			continue // the announcement is read by AnnounceRead, not a batch
		}
		batches = append(batches, row)
	}
	if len(batches) != 4 {
		t.Fatalf("message-one-encoding manifest names %d batch rows, want 4", len(batches))
	}
	canonical := ""
	for _, row := range batches {
		if row.file == "canonical.bin" {
			if row.verdict != "read" {
				t.Fatalf("message-one-encoding manifest gives canonical.bin the verdict %q, want read", row.verdict)
			}
			canonical = row.file
		}
	}
	if canonical == "" {
		t.Fatal("message-one-encoding manifest names no canonical.bin row")
	}

	unit := luarun.LoadUnit(t, filepath.Join(repo, "test", "message-one-encoding", "One.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	inputs := map[string][]byte{"announcement.bin": readFile(t, filepath.Join(vectors, "announcement.bin"))}
	var luaRows strings.Builder
	for _, row := range batches {
		inputs[row.file] = readFile(t, filepath.Join(vectors, row.file))
		fmt.Fprintf(&luaRows, "\t{%q, %q},\n", row.file, row.verdict)
	}

	probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")

local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local ar = {}
assert(T.AnnounceRead(vocab, inputs["announcement.bin"], ar), "announcement refused or damaged")

local cases = {
%s}
for _, c in ipairs(cases) do
  local r = {}
  local values = T.LoadMessages("One", vocab, inputs[c[1]], r)
  if values == nil or #values == 0 then
    print(c[1] .. "=refused")
  else
    local v = values[1]
    local tag = (v.pick and v.pick.tag) or 0
    print(c[1] .. "=read:" .. #values .. ":" .. tostring(v.mode) .. ":" .. tostring(v.count) .. ":" .. tostring(tag))
  end
end

local m = T.NewOne()
m.count = 5
local saved = assert(T.SaveMessages("One", vocab, { m }), "SaveMessages refused the canonical value")
print("saved=" .. (saved:gsub(".", function(ch) return string.format("%%02x", ch:byte()) end)))
`, ModuleName(unit), luaRows.String())

	out, stderr, err := try(t, files, probe, inputs)
	if err != nil {
		t.Fatalf("message-one-encoding probe failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, out)
	}

	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			got[k] = v
		}
	}

	for _, row := range batches {
		want := "refused"
		if row.verdict == "read" {
			// The manifest's value: one body, `mode` at its declared default
			// 7, `count` 5 and `pick` at None.
			want = "read:1:7:5:0"
		}
		if got[row.file] != want {
			t.Errorf("%s: LoadMessages = %s, want %s", row.file, got[row.file], want)
		}
	}

	saved, hasSaved := got["saved"]
	canonicalBytes := inputs[canonical]
	var wantHex strings.Builder
	for _, b := range canonicalBytes {
		fmt.Fprintf(&wantHex, "%02x", b)
	}
	if !hasSaved {
		t.Fatal("the probe printed no SaveMessages bytes")
	}
	if saved != wantHex.String() {
		t.Errorf("SaveMessages of the canonical value = %s, want canonical.bin %s", saved, wantHex.String())
	}
}

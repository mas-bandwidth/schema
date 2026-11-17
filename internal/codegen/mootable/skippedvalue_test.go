package mootable_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooSkippedValueIsValidated runs every row of the shared skipped-value
// vectors through the moo leg's reads (docs/SPEC-TABLES.md §4, §16.3;
// docs/rulings.json skipped-value-is-validated): the JSON rows through the
// reader root's text read, and the message rows through its batch load under
// the newer writer unit's announcement. A value the reader SKIPS is held to
// what a read of it would hold, and damage inside it refuses the whole read.
// The verdict is the contract; a refusal's name is the leg's own diagnostic.
func TestMooSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted skipped-value probe)")

	dir := filepath.Join(root, "testdata", "wire", "skipped-value")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct{ file, form, verdict string }
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifest, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifest, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifest, i+1, f[2])
		}
		rows = append(rows, row{f[0], f[1], f[2]})
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no rows", manifest)
	}

	schemaBytes, err := os.ReadFile(filepath.Join(root, "test", "skipped-value", "reader", "Reader.schema"))
	if err != nil {
		t.Fatal(err)
	}
	g := generateFiles(t, map[string]string{"Reader.schema": string(schemaBytes)})

	annIdx := -1
	for i, r := range rows {
		if r.form == "message" && r.file == "announcement.bin" {
			annIdx = i
		}
	}
	if annIdx < 0 {
		t.Fatalf("%s names no announcement.bin message row", manifest)
	}

	var cases []byte
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		form := byte(2)
		switch {
		case i == annIdx:
			form = 0
		case r.form == "json":
			form = 1
		}
		cases = append(cases, form)
		cases = binary.LittleEndian.AppendUint32(cases, uint32(len(data)))
		cases = append(cases, data...)
	}
	ann, err := os.ReadFile(filepath.Join(dir, "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}

	main := `--!strict
local inputs = require("./inputs")
local MS = require("./table_message")
` + g.requireAll() + `local RT = require("./ReaderTable")
local v = MS.NewVocabulary()
local arep: any = {}
local announced = MS.AnnounceRead(v, inputs.ann, arep)
local cases = inputs.cases
local at = 0
while at < buffer.len(cases) do
	local form = buffer.readu8(cases, at)
	at += 1
	local len = buffer.readu32(cases, at)
	at += 4
	local data = buffer.create(len)
	buffer.copy(data, 0, cases, at, len)
	at += len
	if form == 0 then
		print(if announced then "read" else "refused")
	elseif form == 1 then
		local value = RT.NewRoot()
		local rep: any = {}
		local ok = RT.RootFromJson(value, data, rep)
		print(if ok and not rep.damaged then "read" else "refused")
	else
		local rep: any = {}
		local n, values = MS.LoadMessages(v, "Root", data, 256, rep)
		local ok = n >= 1 and not rep.damaged and not rep.refused and rep.reason == nil
		print(if ok then "read" else "refused")
	end
end
`
	out := g.run(t, main, map[string][]byte{"ann": ann, "cases": cases})
	lines := strings.Split(out, "\n")
	if len(lines) != len(rows) {
		t.Fatalf("the probe answered %d rows of %d:\n%s", len(lines), len(rows), out)
	}
	for i, r := range rows {
		if lines[i] != r.verdict {
			t.Errorf("%s (%s): verdict %s, want %s", r.file, r.form, lines[i], r.verdict)
		}
	}
}

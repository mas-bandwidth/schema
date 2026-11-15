package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauSkippedValueIsValidated runs every row of the shared skipped-value
// vectors through the Luau leg's reads (docs/SPEC-TABLES.md §4, §16.3;
// docs/rulings.json skipped-value-is-validated): the JSON rows through the
// reader root's text read, and the message rows through its batch load under
// the newer writer unit's announcement. A value the reader SKIPS is held to
// what a read of it would hold, and damage inside it refuses the whole read.
// The verdict is the contract; a refusal's name is the leg's own diagnostic.
func TestLuauSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Luau (it runs the emitted readers)")

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

	annIdx := -1
	for i, r := range rows {
		if r.form == "message" && r.file == "announcement.bin" {
			annIdx = i
		}
	}
	if annIdx < 0 {
		t.Fatalf("%s names no announcement.bin message row", manifest)
	}

	genDir, mods, _ := buildMessageUnit(t, []string{"test/skipped-value/reader"})

	inputs := map[string][]byte{}
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		inputs[fmt.Sprintf("row%d", i)] = data
	}
	inputs["announcement"] = inputs[fmt.Sprintf("row%d", annIdx)]

	var src strings.Builder
	src.WriteString("local M = require(\"./schema_message\")\nlocal J = require(\"./schema_json\")\nlocal inputs = require(\"./inputs\")\n")
	for _, m := range mods {
		fmt.Fprintf(&src, "require(\"./%s\")\n", m)
	}
	src.WriteString(`local function report()
	return { malformed = false, verdict = 0, reason = "", unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local v = M.NewVocabulary()
local rep = report()
local announced = M.AnnounceRead(v, inputs.announcement, rep)
`)
	fmt.Fprintf(&src, "print(%q .. \" \" .. (announced and \"read\" or \"refused\"))\n", fmt.Sprintf("%d", annIdx))
	for i, r := range rows {
		if i == annIdx {
			continue
		}
		name := fmt.Sprintf("row%d", i)
		switch r.form {
		case "json":
			fmt.Fprintf(&src, "do local r = report(); local value = J.FromJson(\"Root\", inputs.%s, r); print(\"%d \" .. ((value ~= nil and not r.malformed) and \"read\" or \"refused\")) end\n", name, i)
		case "message":
			fmt.Fprintf(&src, "do local r = report(); local count = M.LoadMessages(v, \"Root\", inputs.%s, 256, r); print(\"%d \" .. ((count >= 0 and not r.malformed) and \"read\" or \"refused\")) end\n", name, i)
		}
	}
	if err := os.WriteFile(filepath.Join(genDir, "main.luau"), []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	out := strings.TrimSpace(string(luaurun.Run(t, genDir, "main.luau", inputs)))
	got := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		var index int
		var verdict string
		if _, err := fmt.Sscanf(line, "%d %s", &index, &verdict); err != nil {
			t.Fatalf("the probe answered %q: %v\n%s", line, err, out)
		}
		got[index] = verdict
	}
	if len(got) != len(rows) {
		t.Fatalf("the probe answered %d rows of %d:\n%s", len(got), len(rows), out)
	}
	for i, r := range rows {
		if got[i] != r.verdict {
			t.Errorf("%s (%s): verdict %s, want %s", r.file, r.form, got[i], r.verdict)
		}
	}
}

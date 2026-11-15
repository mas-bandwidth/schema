package luautable_test

// A JSON COMMENT IS TEXT (docs/SPEC-TABLES.md §16.2): the bytes inside a line
// comment (`//` to the end of the line feed or of the input) and inside a
// block comment (`/* */`) are well-formed UTF-8 and hold no NUL (0x00). A
// comment holding an ill-formed sequence or a zero byte refuses the whole
// read, and a lone carriage return does not end a line comment.
//
// The vectors under testdata/wire/json-comment-text-rules state the verdict
// each text owes at the root ProfileConfig of tables/examples. This test reads
// the comment rows through the generated JSON read, so the runtime's space
// function (schema_json.luau) is held to the rule in both comment forms.

import (
	"bufio"
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

// commentVector is one manifest line: a vector's file name and the verdict the
// read owes it.
type commentVector struct {
	name    string
	verdict string
}

const commentVectorDir = "testdata/wire/json-comment-text-rules"

// commentVectors reads the manifest's comment rows: every line-comment and
// block-comment vector, each named with the verdict it must hold.
func commentVectors(t *testing.T) []commentVector {
	t.Helper()
	f, err := os.Open(filepath.Join(root, commentVectorDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	defer f.Close()
	var rows []commentVector
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("manifest line %q is not <file> TAB <verdict> TAB <what it holds>", line)
		}
		name, verdict := cols[0], cols[1]
		if !strings.HasPrefix(name, "line-comment-") && !strings.HasPrefix(name, "block-comment-") {
			continue
		}
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("the vector %q has verdict %q, want read or refused", name, verdict)
		}
		rows = append(rows, commentVector{name: name, verdict: verdict})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no comment vector: the gate would pass vacuously")
	}
	return rows
}

func TestLuauJsonCommentIsText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	rows := commentVectors(t)
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("luau: %v", err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("luautable: %v", err)
	}
	maps.Copy(files, tables)

	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	inputs := map[string][]byte{}
	var probe strings.Builder
	probe.WriteString(jsonModules(g))
	probe.WriteString(jsonFlags(g))
	probe.WriteString(`local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local rows = {
`)
	for _, r := range rows {
		data, err := os.ReadFile(filepath.Join(root, commentVectorDir, r.name))
		if err != nil {
			t.Fatalf("the vector %q: %v", r.name, err)
		}
		inputs[r.name] = data
		fmt.Fprintf(&probe, "\t{ %q },\n", r.name)
	}
	probe.WriteString(`}
for _, row in rows do
	local name = row[1]
	local report = fresh()
	local v = json.FromJson("ProfileConfig", inputs[name], report)
	print("@" .. name .. "|" .. (if v ~= nil then "read" else "refused"))
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := jsonStrictAnswers(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
	failed := 0
	for _, r := range rows {
		if got[r.name] == "" {
			t.Fatalf("%s: the probe printed no answer", r.name)
		}
		if got[r.name] != r.verdict {
			t.Errorf("%s: got %s, want %s", r.name, got[r.name], r.verdict)
			failed++
		}
	}
	if failed == 0 {
		t.Logf("json comment text: %d comment vectors hold their verdict", len(rows))
	}
}

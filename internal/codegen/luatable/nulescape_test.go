package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaJsonRefusesNulEscapeIntoBoundedText holds the §16.3 refusal: a
// \u0000 escape that would land U+0000 in a bounded string(N) field refuses as
// text_holds_zero, naming the innermost field. A \u0001 reads clean.
func TestLuaJsonRefusesNulEscapeIntoBoundedText(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	var paths []string
	for _, p := range units["tabledemo"] {
		paths = append(paths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit tabledemo: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(nulProbe(ModuleName(u))), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, text, want string
	}{
		{"front", `{ "version_note": "\u0000ab" }`, "refused\ntext_holds_zero: RootConfig.version_note"},
		{"middle", `{ "version_note": "a\u0000b" }`, "refused\ntext_holds_zero: RootConfig.version_note"},
		{"end", `{ "version_note": "ab\u0000" }`, "refused\ntext_holds_zero: RootConfig.version_note"},
		{"nested", `{ "profiles": [{ "name": "a\u0000b" }] }`, "refused\ntext_holds_zero: ProfileConfig.name"},
		{"control_u0001", `{ "version_note": "a\u0001b" }`, "read\n-"},
	}
	for _, c := range cases {
		got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"text": []byte(c.text)})), "\n")
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func nulProbe(module string) string {
	return `local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = J.FromJsonTolerant(T, "RootConfig", inputs.text, report)
print(report.malformed and "refused" or "read")
print(report.Reason or "-")
`
}

func TestLuaJsonRefusesNulEscapeInBoundedMapValue(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")

	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "maps", "Text.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit mapdemo: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := strings.Replace(nulProbe(ModuleName(u)), "RootConfig", "Text", 1)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	text := "{ \"names\": { \"key\": \"a\\u0000b\" } }"
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"text": []byte(text)})), "\n")
	want := "refused\ntext_holds_zero: Text.names"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLuaJsonRefusesNulEscapeInUnionArm(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")

	u := luarun.LoadUnit(t, filepath.Join(repo, "examples-wide", "Caption.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit wide: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := strings.Replace(nulProbe(ModuleName(u)), "RootConfig", "Caption", 1)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	text := "{ \"body\": { \"narrow\": \"a\\u0000b\" } }"
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"text": []byte(text)})), "\n")
	want := "refused\ntext_holds_zero: Caption.body"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

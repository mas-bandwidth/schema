package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLuaLintDedupePreservesFirstNamesAndExactBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fixtures := []struct{ name, content string }{
		{"a/serialize_int.lua", "same\n"},
		{"b/other.lua", "same\n"},
		{"c/one table.lua", "same\r\n"},
		{"d/é.lua", "\x00binary\xff"},
		{"e/é.lua", "other\n"},
		{"f/duplicate.lua", "other\n"},
	}
	var paths []string
	for _, fixture := range fixtures {
		path := filepath.Join(dir, fixture.name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(fixture.content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	output := filepath.Join(dir, "unique")
	command := exec.Command("python3", "../../tools/lua-luacheck-dedupe.py", output)
	command.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	if log, err := command.CombinedOutput(); err != nil {
		t.Fatalf("deduplicate: %v\n%s", err, log)
	}
	want := map[string]string{
		"1-serialize_int.lua": "same\n",
		"2-one table.lua":     "same\r\n",
		"3-é.lua":             "\x00binary\xff",
		"4-é.lua":             "other\n",
	}
	files, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(want) {
		t.Fatalf("selected %d files, want exactly %d", len(files), len(want))
	}
	for _, file := range files {
		content, ok := want[file.Name()]
		if !ok {
			t.Fatalf("unexpected representative %q", file.Name())
		}
		got, err := os.ReadFile(filepath.Join(output, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, []byte(content)) {
			t.Fatalf("representative %q changed bytes", file.Name())
		}
	}
}

func TestLuaLintDedupeRefusesUnreadableInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.lua")
	command := exec.Command("python3", "../../tools/lua-luacheck-dedupe.py", filepath.Join(dir, "unique"))
	command.Stdin = strings.NewReader(missing + "\x00")
	log, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(log), "missing.lua") {
		t.Fatalf("unreadable input did not fail by name: %v\n%s", err, log)
	}
}

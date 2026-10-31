package lua

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestVendoredLuaSerializeIsThePin pins the serialize port vendored into
// runtime/ (docs/PORT-LUA.md, THE RUNTIME LAYOUT: the type-wire cards call
// this library rather than a private bit reader or writer). Every file
// SERIALIZE_PIN lists exists in runtime/ with that sha256, no serialize*.lua
// file is there that the pin does not list, and the pin's tag and commit are
// make/lua-serialize.mk's SERIALIZE_LUA_PIN and SERIALIZE_LUA_COMMIT (a release
// of serialize.lua, never a branch); then the vendored
// entry module loads beside the generated modules under the pinned interpreter
// and round-trips one serializeInt and one serializeFloat through WriteStream
// and ReadStream, compared with the bytes of the two `writer canonical` records
// of the port's vendored corpus (conformance/int.txt and float.txt).
func TestVendoredLuaSerializeIsThePin(t *testing.T) {
	t.Parallel()

	pinBytes, err := os.ReadFile("runtime/SERIALIZE_PIN")
	if err != nil {
		t.Fatalf("read runtime/SERIALIZE_PIN: %v", err)
	}
	type record struct {
		file string
		sum  string
	}
	var pin []record
	pinned := map[string]bool{}
	for line := range strings.SplitSeq(string(pinBytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != "sha256" {
			t.Fatalf("runtime/SERIALIZE_PIN: unrecognized line %q", line)
		}
		pin = append(pin, record{file: f[2], sum: f[1]})
		pinned[f[2]] = true
	}
	if len(pin) == 0 {
		t.Fatal("runtime/SERIALIZE_PIN lists no files")
	}
	// The pin names a RELEASE: its tag and commit are the ones
	// make/lua-serialize.mk clones and refuses to vendor anything else at.
	tag, commit := makeVar(t, "SERIALIZE_LUA_PIN"), makeVar(t, "SERIALIZE_LUA_COMMIT")
	if !strings.HasPrefix(tag, "v") || len(commit) != 40 {
		t.Fatalf("make/lua-serialize.mk: SERIALIZE_LUA_PIN %q is not a release tag or SERIALIZE_LUA_COMMIT %q is not a full commit", tag, commit)
	}
	for _, want := range []string{"# tag " + tag + "\n", "# commit " + commit + "\n"} {
		if !strings.Contains(string(pinBytes), want) {
			t.Errorf("runtime/SERIALIZE_PIN does not carry %q from make/lua-serialize.mk; rewrite it with make vendor-serialize-lua", strings.TrimSpace(want))
		}
	}
	for _, r := range pin {
		b, err := os.ReadFile(filepath.Join("runtime", r.file))
		if err != nil {
			t.Errorf("vendored %s missing: %v", r.file, err)
			continue
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != r.sum {
			t.Errorf("vendored %s sha256 = %s, the pin says %s", r.file, got, r.sum)
		}
	}
	entries, err := os.ReadDir("runtime")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "serialize") && strings.HasSuffix(name, ".lua") {
			if !pinned[name] {
				t.Errorf("runtime/%s is present but the pin does not list it", name)
			}
		}
	}

	dir := t.TempDir()
	luarun.WriteHexProbe(t, dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		b, err := os.ReadFile(filepath.Join("runtime", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `local hexline = require("hexline")
local serialize = require("serialize")

-- serializeInt: value 7 over [0,7] -> canonical "07" (conformance/int.txt)
local w = serialize.WriteStream.new(8)
assert(w:serializeInt({ value = 7 }, 0, 7), w:error())
w:flush()
hexline(w:data())
local r = serialize.ReadStream.new(w:data())
local iv = {}
assert(r:serializeInt(iv, 0, 7), r:error())
print(iv.value)

-- serializeFloat: pi -> canonical "db0f4940" (conformance/float.txt)
local fw = serialize.WriteStream.new(8)
assert(fw:serializeFloat({ value = math.pi }), fw:error())
fw:flush()
hexline(fw:data())
local fr = serialize.ReadStream.new(fw:data())
local fv = {}
assert(fr:serializeFloat(fv), fr:error())
print(string.format("%.9g", fv.value))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("probe printed %d lines, want 4:\n%s", len(lines), out)
	}
	if lines[0] != "07" {
		t.Errorf("serializeInt bytes hex = %q, want \"07\"", lines[0])
	}
	if lines[1] != "7" {
		t.Errorf("round-tripped int = %q, want 7", lines[1])
	}
	if lines[2] != "db0f4940" {
		t.Errorf("serializeFloat bytes hex = %q, want \"db0f4940\"", lines[2])
	}
	if lines[3] != "3.14159274" {
		t.Errorf("round-tripped float = %q, want 3.14159274", lines[3])
	}
}

// makeVar reads one `NAME := value` line of make/lua-serialize.mk.
func makeVar(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "lua-serialize.mk"))
	if err != nil {
		t.Fatalf("read make/lua-serialize.mk: %v", err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == name && f[1] == ":=" {
			return f[2]
		}
	}
	t.Fatalf("make/lua-serialize.mk has no %s := line", name)
	return ""
}

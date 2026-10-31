package luau

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestVendoredLuauSerializeIsThePin holds the vendored serialize.luau port to
// the pin SERIALIZE_PIN names, then runs it: docs/PORT-LUAU.md, THE RUNTIME
// LAYOUT, says runtime/*.luau is go:embed by glob and emitted beside the
// generated modules, and the type-wire cards call this library rather than a
// private bit reader or writer. The two byte records below are `writer
// canonical` records pasted from the port's vendored corpus
// (conformance/int.txt and conformance/float.txt); the test reads no .ref/
// checkout and adds no serialize.go dependency, because none exists in this
// module.
func TestVendoredLuauSerializeIsThePin(t *testing.T) {
	t.Parallel()

	repo, commit, pinned := readSerializePin(t)
	if !strings.Contains(repo, "serialize.luau") {
		t.Fatalf("SERIALIZE_PIN repository %q is not the serialize.luau port", repo)
	}
	if len(commit) != 40 {
		t.Fatalf("SERIALIZE_PIN commit %q is not a full 40-hex commit sha", commit)
	}

	for name, want := range pinned {
		if matched, _ := filepath.Match("serialize*.luau", name); !matched {
			t.Errorf("SERIALIZE_PIN lists %q, which is not a vendored serialize*.luau file", name)
			continue
		}
		data, err := os.ReadFile(filepath.Join("runtime", name))
		if err != nil {
			t.Errorf("runtime/%s is listed in SERIALIZE_PIN but absent: %v", name, err)
			continue
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Errorf("runtime/%s sha256 is %s, SERIALIZE_PIN says %s", name, got, want)
		}
	}

	entries, err := os.ReadDir("runtime")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if matched, _ := filepath.Match("serialize*.luau", name); !matched {
			continue
		}
		if _, ok := pinned[name]; !ok {
			t.Errorf("runtime/%s is present but SERIALIZE_PIN does not list it", name)
		}
	}

	unit := &ir.Unit{
		Package:    "hello",
		ProtocolId: 0x11,
		Files:      []*ir.File{{Base: "Hello"}},
		DeclFile:   map[string]string{},
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	for name := range pinned {
		if _, ok := files[name]; !ok {
			t.Errorf("Generate omitted the vendored runtime %s", name)
		}
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.luau", luaurun.HexProbe+`
local S = require("./serialize")

local function writeInt(value: number, min: number, max: number): buffer
	local w = S.WriteStream.new(buffer.create(8))
	assert(w:serializeInt({ value = value }, min, max))
	w:flush()
	return w:data()
end

local function readInt(data: buffer, min: number, max: number): number
	local r = S.ReadStream.new(data)
	local h = { value = 0 }
	assert(r:serializeInt(h, min, max))
	return h.value
end

local function writeFloat(value: number): buffer
	local w = S.WriteStream.new(buffer.create(8))
	assert(w:serializeFloat({ value = value }))
	w:flush()
	return w:data()
end

local function readFloat(data: buffer): number
	local r = S.ReadStream.new(data)
	local h = { value = 0 }
	assert(r:serializeFloat(h))
	return h.value
end

local int = writeInt(5, 0, 7)
hex(int)
hex(writeInt(readInt(int, 0, 7), 0, 7))

local float = writeFloat(3.141592653589793)
hex(float)
hex(writeFloat(readFloat(float)))
`)
	lines := luaurun.HexLines(t, luaurun.Run(t, dir, "main.luau", nil))
	if len(lines) != 4 {
		t.Fatalf("main.luau printed %d hex lines, want 4", len(lines))
	}

	// conformance/int.txt, int-range-0-7-costs-three-bits
	intRecord := []byte{0x05}
	// conformance/float.txt, float-pi
	floatRecord := []byte{0xdb, 0x0f, 0x49, 0x40}
	for _, tc := range []struct {
		what string
		got  []byte
		want []byte
	}{
		{"serializeInt write", lines[0], intRecord},
		{"serializeInt read then write", lines[1], intRecord},
		{"serializeFloat write", lines[2], floatRecord},
		{"serializeFloat read then write", lines[3], floatRecord},
	} {
		if !bytes.Equal(tc.got, tc.want) {
			t.Errorf("%s is %x, the writer canonical record is %x", tc.what, tc.got, tc.want)
		}
	}
}

func readSerializePin(t *testing.T) (repo, commit string, files map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("runtime", "SERIALIZE_PIN"))
	if err != nil {
		t.Fatalf("runtime/SERIALIZE_PIN: %v", err)
	}
	files = map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "repository":
			if len(fields) != 2 {
				t.Fatalf("SERIALIZE_PIN line %d: repository takes one value", i+1)
			}
			repo = fields[1]
		case "commit":
			if len(fields) != 2 {
				t.Fatalf("SERIALIZE_PIN line %d: commit takes one value", i+1)
			}
			commit = fields[1]
		case "sha256":
			if len(fields) != 3 || len(fields[1]) != 64 {
				t.Fatalf("SERIALIZE_PIN line %d: want sha256  <hash>  <file>", i+1)
			}
			if _, dup := files[fields[2]]; dup {
				t.Fatalf("SERIALIZE_PIN line %d: %s is listed twice", i+1, fields[2])
			}
			files[fields[2]] = fields[1]
		default:
			t.Fatalf("SERIALIZE_PIN line %d: unknown key %q", i+1, fields[0])
		}
	}
	if len(files) == 0 {
		t.Fatal("SERIALIZE_PIN lists no files")
	}
	return repo, commit, files
}

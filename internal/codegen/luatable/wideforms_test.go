package luatable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaWideKindsFixedAndMessageForms pins the two constant-size forms'
// carriage of the wide kinds (docs/SPEC-TABLES.md §3.3, §3.4, §15): a fixed
// table declaring int128, uint128 and fixed(16, 16) writes the same form-3
// record and form-2 batch as the generated Go code, byte for byte. The fixed
// form names no kind — a wide field is its declared storage image — and the
// message form carries the ranged packing the type wire gives it.
func TestLuaWideKindsFixedAndMessageForms(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	schema := `package wideforms

fixed table Root
{
    a int128 | min = -1000, max = 1000
    b uint128
    c fixed(16, 16) | min = -8, max = 8
}
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "wideforms.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schemaPath)

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the Lua unit: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)
	luarun.WriteHexProbe(t, luaDir)
	probe := fmt.Sprintf(`local T = require(%q)
local hexline = require("hexline")
local full = T.NewRoot()
full.a = { lo = 1000, hi = 0 }
full.b = { lo = 0x0123456789abcdef, hi = 0xfedcba9876543210 }
full.c = 196608
local empty = T.NewRoot()
hexline(T.WriteFixed("Root", full))
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local report = { malformed = false, refused = 0 }
assert(T.AnnounceRead(vocab, T.Announcement, report), "announcement refused")
local saved = T.SaveMessages("Root", vocab, { full, empty })
assert(saved ~= nil, "message batch refused")
hexline(saved)
`, ModuleName(u))
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	luaOut := luarun.HexLines(t, luarun.Run(t, luaDir, "main.lua", nil))
	if len(luaOut) != 2 {
		t.Fatalf("the Lua probe returned %d lines, want 2", len(luaOut))
	}

	goDir := t.TempDir()
	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go unit: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go table surface: %v", err)
	}
	maps.Copy(goFiles, tables)
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => %q\n", mustAbs(t, repo)) + "\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ./serialize\n")
	goFiles["serialize/go.mod"] = []byte("module github.com/mas-bandwidth/serialize.go\n\ngo 1.26\n")
	goFiles["serialize/serialize.go"] = []byte(serializeStub)
	goFiles["wide_test.go"] = []byte(wideFormsGoProbe)
	for name, data := range goFiles {
		path := filepath.Join(goDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestGoWideForms$", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated Go oracle: %v\n%s", err, output)
	}

	fixed, err := os.ReadFile(filepath.Join(goDir, "go_fixed.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(luaOut[0], fixed) {
		t.Errorf("fixed form differs from the generated Go writer\n lua %x\n  go %x", luaOut[0], fixed)
	}
	message, err := os.ReadFile(filepath.Join(goDir, "go_message.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(luaOut[1], message) {
		t.Errorf("message form differs from the generated Go writer\n lua %x\n  go %x", luaOut[1], message)
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

const serializeStub = `package serialize

type Int128 struct{ Lo, Hi uint64 }
type Uint128 struct{ Lo, Hi uint64 }

func Int128From64(v int64) Int128 {
	if v < 0 {
		return Int128{Lo: uint64(v), Hi: ^uint64(0)}
	}
	return Int128{Lo: uint64(v)}
}

func (a Int128) Cmp(b Int128) int {
	if int64(a.Hi) != int64(b.Hi) {
		if int64(a.Hi) < int64(b.Hi) {
			return -1
		}
		return 1
	}
	if a.Lo < b.Lo {
		return -1
	}
	if a.Lo > b.Lo {
		return 1
	}
	return 0
}
`

const wideFormsGoProbe = `package wideforms

import (
	"os"
	"testing"

	"github.com/mas-bandwidth/serialize.go"
)

func TestGoWideForms(t *testing.T) {
	full := Root{
		A: serialize.Int128{Lo: 1000},
		B: serialize.Uint128{Lo: 0x0123456789abcdef, Hi: 0xfedcba9876543210},
		C: 196608,
	}
	empty := Root{}
	fixed := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{full}, fixed); n < 0 {
		t.Fatal("the Go fixed writer refused")
	} else if err := os.WriteFile("go_fixed.bin", fixed[:n], 0o644); err != nil {
		t.Fatal(err)
	}
	var report TableReport
	announcement := make([]byte, AnnounceMeasure())
	Announce(announcement)
	var vocab TableVocabulary
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	if !AnnounceRead(&vocab, announcement, &report) {
		t.Fatalf("the Go announcement: %+v", report)
	}
	values := []*Root{&full, &empty}
	message := make([]byte, RootMeasureMessages(values, &report))
	if n := RootSaveMessages(values, message, &report); n < 0 {
		t.Fatalf("the Go message writer refused: %+v", report)
	} else if err := os.WriteFile("go_message.bin", message[:n], 0o644); err != nil {
		t.Fatal(err)
	}
}
`

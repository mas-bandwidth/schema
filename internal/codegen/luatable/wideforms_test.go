package luatable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaWideKindsFixedAndMessageForms pins the two constant-size forms'
// carriage of the wide kinds (docs/SPEC-TABLES.md §3.3, §3.4, §15): a fixed
// table declaring int128, uint128 and fixed(16, 16) writes the same form-3
// record and form-2 batch as the generated Go code, byte for byte, and reads
// the generated Go's bytes back to the same values. The fixed form names no
// kind — a wide field is its declared storage image — and the message form
// carries the ranged packing the type wire gives it. The values exercise a
// negative int128 with a nonzero high half, a uint128 with a nonzero high half,
// a negative fixed(16, 16), and a ranged int128 whose base add carries across
// the 64-bit halves on the read (§3.3, kinds 18/19 and 24/29).
func TestLuaWideKindsFixedAndMessageForms(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	repo := filepath.Join("..", "..", "..")
	schema := `package wideforms

type Wide
{
    a int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
    b uint128
    c fixed(16, 16) | min = -8, max = 8
    d int128 | min = -1, max = 1000
}

fixed table Root
{
    w Wide
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
full.w.a = { lo = 0, hi = 0xFFFFFFFFFFFFFFFE }
full.w.b = { lo = 0x0123456789abcdef, hi = 0xfedcba9876543210 }
full.w.c = -98304
full.w.d = { lo = 5, hi = 0 }
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
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => %q\n", mustAbs(t, repo)) + "\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ./serialize\n")
	goFiles["serialize/go.mod"] = []byte("module github.com/mas-bandwidth/serialize.go/v2\n\ngo 1.26\n")
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

	// THE READ DIRECTION: the Lua runtime loads the generated Go's own bytes and
	// answers the same values Go's reader does (§3.4 names no kind, so the fixed
	// record is the raw storage image; §3.3 reconstructs the ranged wide value
	// by adding the base, which carries across the halves).
	readProbe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local function dump(v)
  return string.format("%%016x%%016x%%016x%%016x%%016x%%016x%%016x",
    v.w.a.lo, v.w.a.hi, v.w.b.lo, v.w.b.hi, v.w.c, v.w.d.lo, v.w.d.hi)
end
print(dump(T.ReadFixed("Root", inputs.fixed)))
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local report = { malformed = false, refused = 0 }
assert(T.AnnounceRead(vocab, T.Announcement, report), "announcement refused")
local values = assert(T.LoadMessages("Root", vocab, inputs.message, report), "message load refused")
print(dump(values[1]))
print(dump(values[2]))
`, ModuleName(u))
	if err := os.WriteFile(filepath.Join(luaDir, "read.lua"), []byte(readProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	luaRead := string(luarun.Run(t, luaDir, "read.lua", map[string][]byte{"fixed": fixed, "message": message}))
	goRead, err := os.ReadFile(filepath.Join(goDir, "go_values.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(luaRead, "\n") != strings.TrimRight(string(goRead), "\n") {
		t.Errorf("the Lua reader differs from the generated Go reader\n lua %q\n  go %q", luaRead, goRead)
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

func Uint128From64(v uint64) Uint128 { return Uint128{Lo: v} }

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

func (a Int128) Int128() Int128 { return a }
func (a Uint128) Int128() Int128 { return Int128{Lo: a.Lo, Hi: a.Hi} }
func (a Int128) Uint128() Uint128 { return Uint128{Lo: a.Lo, Hi: a.Hi} }

type WriteStream struct{ err error }

func (s *WriteStream) Err() error                                  { return s.err }
func (s *WriteStream) SerializeAlign()                             {}
func (s *WriteStream) SerializeBits(v *uint32, bits int)           {}
func (s *WriteStream) SerializeBits64(v *uint64, bits int)         {}
func (s *WriteStream) SerializeInt128(v *Int128, lo, hi Int128)    {}
func (s *WriteStream) SerializeUint128(v *Uint128)                 {}
func (s *WriteStream) SerializeFixed64(v *int64, i, f int, lo, hi int64) {}
func (s *WriteStream) SerializeFixed128(v *Int128, i, f int, lo, hi Int128) {}

type ReadStream struct{ err error }

func (s *ReadStream) Err() error                                  { return s.err }
func (s *ReadStream) SerializeAlign()                             {}
func (s *ReadStream) SerializeBits(v *uint32, bits int)           {}
func (s *ReadStream) SerializeBits64(v *uint64, bits int)         {}
func (s *ReadStream) SerializeInt128(v *Int128, lo, hi Int128)    {}
func (s *ReadStream) SerializeUint128(v *Uint128)                 {}
func (s *ReadStream) SerializeFixed64(v *int64, i, f int, lo, hi int64) {}
func (s *ReadStream) SerializeFixed128(v *Int128, i, f int, lo, hi Int128) {}
`

const wideFormsGoProbe = `package wideforms

import (
	"fmt"
	"os"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestGoWideForms(t *testing.T) {
	full := Root{
		W: Wide{
			A: serialize.Int128{Lo: 0, Hi: 0xFFFFFFFFFFFFFFFE},
			B: serialize.Uint128{Lo: 0x0123456789abcdef, Hi: 0xfedcba9876543210},
			C: -98304,
			D: serialize.Int128{Lo: 5},
		},
	}
	empty := Root{}
	fixed := make([]byte, RootFixedMeasure(1))
	fixedN := RootFixedSave([]Root{full}, fixed)
	if fixedN < 0 {
		t.Fatal("the Go fixed writer refused")
	} else if err := os.WriteFile("go_fixed.bin", fixed[:fixedN], 0o644); err != nil {
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
	messageN := RootSaveMessages(values, message, &report)
	if messageN < 0 {
		t.Fatalf("the Go message writer refused: %+v", report)
	} else if err := os.WriteFile("go_message.bin", message[:messageN], 0o644); err != nil {
		t.Fatal(err)
	}
	plan := make([]TableFixedEntry, RootFixedPlanMax)
	var fixedReport TableReport
	fixedBack := make([]Root, 1)
	if n := RootFixedLoad(fixedBack, fixed[:fixedN], plan, &fixedReport); n != 1 || fixedReport != (TableReport{}) {
		t.Fatalf("the Go fixed reader: n=%d report=%+v", n, fixedReport)
	}
	var messageReport TableReport
	messageBack := make([]Root, 2)
	if n, ok := RootLoadMessages(messageBack, &vocab, message[:messageN], &messageReport); !ok || n != 2 {
		t.Fatalf("the Go message reader: n=%d ok=%v report=%+v", n, ok, messageReport)
	}
	dump := func(v Root) string {
		return fmt.Sprintf("%016x%016x%016x%016x%016x%016x%016x",
			v.W.A.Lo, v.W.A.Hi, v.W.B.Lo, v.W.B.Hi, uint64(int64(v.W.C)), v.W.D.Lo, v.W.D.Hi)
	}
	out := dump(fixedBack[0]) + "\n" + dump(messageBack[0]) + "\n" + dump(messageBack[1]) + "\n"
	if err := os.WriteFile("go_values.txt", []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
`

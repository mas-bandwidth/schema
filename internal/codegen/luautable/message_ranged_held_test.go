package luautable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The reader: bare integers of every width the cases cross.
const luauRangedReaderSchema = `package probe

fixed table Msg
{
    a uint8
    b uint16
    q int64
    r uint64
    h int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
}
`

// The writer under another build: each integer ranged over the top six values
// of its kind in three bits, so offsets 6 and 7 spell a sum past the kind
// (no conforming writer emits them; b is announced as a uint8 and widens into
// the reader's uint16); and s and z, which the reader does not declare and
// skips.
const luauRangedWriterSchema = `package probe

fixed table Msg
{
    a uint8 = 250 | min = 250, max = 255
    b uint8 = 250 | min = 250, max = 255
    q int64 = 9223372036854775802 | min = 9223372036854775802, max = 9223372036854775807
    r uint64 = 18446744073709551610 | min = 18446744073709551610, max = 18446744073709551615
    h int128 = 170141183460469231731687303715884105722 | min = 170141183460469231731687303715884105722, max = 170141183460469231731687303715884105727
    s uint8 = 250 | min = 250, max = 255
    z int128 = 170141183460469231731687303715884105722 | min = 170141183460469231731687303715884105722, max = 170141183460469231731687303715884105727
}
`

const luauRangedProbe = `local inputs = require("./inputs")
require("./ProbeTable")
local M = require("./schema_message")
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
local function unhex(s: string): buffer
	local b = buffer.create(#s // 2)
	for i = 0, #s // 2 - 1 do buffer.writeu8(b, i, tonumber(string.sub(s, 2 * i + 1, 2 * i + 2), 16) :: number) end
	return b
end
for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
	local words = string.split(line, " ")
	local r: any = {}
	local ok, n = pcall(M.LoadMessages, voc, "Msg", unhex(words[2]), 256, r)
	print(words[1] .. " " .. (if ok and n == 1 then "read" else "refused") .. " " .. tostring(if ok then r.reason else n))
end
`

// luauMsgBits is the batch body's bit writer: least significant bit first.
type luauMsgBits struct {
	out []byte
	n   int
}

func (w *luauMsgBits) put(v uint64, bits int) {
	for i := range bits {
		if w.n%8 == 0 {
			w.out = append(w.out, 0)
		}
		if i < 64 && v>>uint(i)&1 != 0 {
			w.out[len(w.out)-1] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

// TestLuauMessageRangedValueHeldToTheAnnouncedKind pins the message form's
// cross-leg rules (1) and (2) of the 2026-11-21 review on the Luau table
// runtime (docs/SPEC-TABLES.md §3.3). A ranged integer read by name is base
// plus offset held to the announced kind's domain, never wrapped: a sum past
// it is refused, for 8, 64 and 128-bit kinds and where the reader is wider
// than the announced kind. The skip path holds the same sum and calls it
// damage. The lawful twin of every case reads.
func TestLuauMessageRangedValueHeldToTheAnnouncedKind(t *testing.T) {
	t.Parallel()
	load := func(text string) *ir.Unit {
		p := filepath.Join(t.TempDir(), "Probe.schema")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load([]string{p})
		if err != nil {
			t.Fatalf("the unit does not load: %v", err)
		}
		return u
	}
	reader, writer := load(luauRangedReaderSchema), load(luauRangedWriterSchema)
	dir := t.TempDir()
	for _, gen := range []func() (map[string][]byte, error){
		func() (map[string][]byte, error) { return luau.Generate(reader) },
		func() (map[string][]byte, error) { return luautable.Generate(reader) },
	} {
		files, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(luauRangedProbe), 0o644); err != nil {
		t.Fatal(err)
	}

	vocab := tablewire.MessageVocabulary(writer)
	refBits := ir.TableMessageRefBits(len(vocab))
	field := map[string]*ir.Field{}
	for _, f := range writer.Tables["Msg"].Fields {
		field[f.Name] = f
	}
	slot := func(name string) uint64 {
		e := ir.TableFieldEntry(field[name])
		for i, v := range vocab {
			if v.Key() == e.Key() {
				return uint64(i + 1)
			}
		}
		t.Fatalf("field %s outside the writer's vocabulary", name)
		return 0
	}
	// ranged is a batch of one body holding field name at an offset in its
	// three bits (the high half of a 128-bit kind's offset is zero there too).
	ranged := func(name string, offset uint64) string {
		e := ir.TableFieldEntry(field[name])
		if bits := ir.TableMessageValueBits(e.Kind, e.Shape); bits != 3 {
			t.Fatalf("field %s rides %d bits, want 3", name, bits)
		}
		w := &luauMsgBits{}
		w.put(slot(name), refBits)
		w.put(offset, 3)
		w.put(0, refBits) // the body's terminator
		return hex.EncodeToString(append([]byte{2, 0}, w.out...))
	}
	cases := []struct{ name, batch, want string }{
		{"a-lawful-255", ranged("a", 5), "read"},
		{"a-256-past-uint8", ranged("a", 6), "refused"},
		{"a-257-past-uint8", ranged("a", 7), "refused"},
		{"b-lawful-255-widened", ranged("b", 5), "read"},
		{"b-257-past-announced-uint8", ranged("b", 7), "refused"},
		{"q-lawful-int64-top", ranged("q", 5), "read"},
		{"q-past-int64", ranged("q", 7), "refused"},
		{"r-lawful-uint64-top", ranged("r", 5), "read"},
		{"r-past-uint64", ranged("r", 7), "refused"},
		{"h-lawful-int128-top", ranged("h", 5), "read"},
		{"h-past-int128", ranged("h", 7), "refused"},
		{"s-skip-lawful", ranged("s", 5), "read"},
		{"s-skip-past-uint8", ranged("s", 7), "refused"},
		{"z-skip-lawful", ranged("z", 5), "read"},
		{"z-skip-past-int128", ranged("z", 7), "refused"},
	}
	var lines []string
	for _, c := range cases {
		lines = append(lines, c.name+" "+c.batch)
	}
	raw := luaurun.Run(t, dir, "main.luau", map[string][]byte{
		"announce": tablewire.Announce(writer),
		"cases":    []byte(strings.Join(lines, "\n") + "\n"),
	})
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		verdict, why, _ := strings.Cut(rest, " ")
		t.Logf("%s %s (%s)", name, verdict, why)
		got[name] = verdict
	}
	for _, c := range cases {
		if got[c.name] != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got[c.name], c.want)
		}
	}
}

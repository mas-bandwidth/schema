package luatable

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// The reader: bare integers of every width the cases cross, and a bytes(4).
const luaRangedReaderSchema = `package probe

fixed table Msg
{
    a uint8
    b uint16
    q int64
    r uint64
    h int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    d bytes(4)
}
`

// The writer under another build: each integer ranged over the top six values
// of its kind in three bits, so offsets 6 and 7 spell a sum past the kind
// (no conforming writer emits them; b is announced as a uint8 and widens into
// the reader's uint16); d a counted [2..4]uint8, whose count rides as
// count - 2 in two bits; and s and z, which the reader does not declare and
// skips.
const luaRangedWriterSchema = `package probe

fixed table Msg
{
    a uint8 = 250 | min = 250, max = 255
    b uint8 = 250 | min = 250, max = 255
    q int64 = 9223372036854775802 | min = 9223372036854775802, max = 9223372036854775807
    r uint64 = 18446744073709551610 | min = 18446744073709551610, max = 18446744073709551615
    h int128 = 170141183460469231731687303715884105722 | min = 170141183460469231731687303715884105722, max = 170141183460469231731687303715884105727
    d [2..4]uint8
    s uint8 = 250 | min = 250, max = 255
    z int128 = 170141183460469231731687303715884105722 | min = 170141183460469231731687303715884105722, max = 170141183460469231731687303715884105727
}
`

const luaRangedProbe = `local T = require(MODULE)
local inputs = require("inputs")
local function unhex(s)
  return (s:gsub("..", function(h) return string.char(tonumber(h, 16)) end))
end
local function show(v)
  if type(v) == "table" then return string.format("%x:%x", v.hi, v.lo) end
  if type(v) == "string" then
    return (v:gsub(".", function(c) return string.format("%02x", string.byte(c)) end))
  end
  return string.format("%x", v)
end
local vocab = {}
local ar = {}
assert(T.AnnounceRead(vocab, inputs.announce, ar), "AnnounceRead: " .. tostring(ar.reason))
for line in inputs.cases:gmatch("[^\n]+") do
  local words = {}
  for w in line:gmatch("%S+") do words[#words + 1] = w end
  local report = {}
  local back = T.LoadMessages("Msg", vocab, unhex(words[2]), report)
  local refused = back == nil or #back ~= 1 or report.damaged or (report.refused or 0) ~= 0
  local got
  if refused then
    got = "refused"
  else
    local parts = {}
    for i = 3, #words do
      local k = words[i]:match("^([^=]+)=")
      if k == "unknown" then
        parts[#parts + 1] = "unknown=" .. tostring(report.unknown)
      elseif k == nil then
        parts[#parts + 1] = "read"
      else
        parts[#parts + 1] = k .. "=" .. show(back[1][k])
      end
    end
    got = table.concat(parts, " ")
  end
  print(words[1] .. " " .. got)
end
`

// luaMsgBits is the batch body's bit writer: least significant bit first.
type luaMsgBits struct {
	out []byte
	n   int
}

func (w *luaMsgBits) put(v uint64, bits int) {
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

func (w *luaMsgBits) raw(b []byte) {
	w.n = len(w.out) * 8
	w.out = append(w.out, b...)
	w.n += 8 * len(b)
}

// TestLuaMessageRangedValueHeldToTheAnnouncedKind pins the message form's
// cross-leg rules (1), (2) and (5) of the 2026-11-21 review on the Lua table
// runtime (docs/SPEC-TABLES.md §3.3). A ranged integer read by name is base
// plus offset held to the announced kind's domain, never wrapped: a sum past
// it is refused out_of_range, for 8, 64 and 128-bit kinds and where the
// reader is wider than the announced kind. The skip path holds the same sum
// and calls it damage. A bytes(N) read from a counted [Min..N]uint8 adds the
// announced minimum back to the count, and a count past the announced max is
// refused. The lawful twin of every case reads.
func TestLuaMessageRangedValueHeldToTheAnnouncedKind(t *testing.T) {
	t.Parallel()
	rdir, wdir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(rdir, "probe.schema"), luaRangedReaderSchema)
	writeFile(t, filepath.Join(wdir, "probe.schema"), luaRangedWriterSchema)
	reader := luarun.LoadUnit(t, rdir)
	writer := luarun.LoadUnit(t, wdir)
	files, err := Generate(reader)
	if err != nil {
		t.Fatalf("generate: %v", err)
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
	batch := func(fill func(w *luaMsgBits)) string {
		w := &luaMsgBits{}
		fill(w)
		w.put(0, refBits) // the body's terminator
		return hex.EncodeToString(append([]byte{2, 0}, w.out...))
	}
	// ranged writes field name at an offset in its three bits (and the high
	// half of a 128-bit kind's offset is zero in three bits too).
	ranged := func(name string, offset uint64) func(w *luaMsgBits) {
		return func(w *luaMsgBits) {
			e := ir.TableFieldEntry(field[name])
			bits := ir.TableMessageValueBits(e.Kind, e.Shape)
			if bits != 3 {
				t.Fatalf("field %s rides %d bits, want 3", name, bits)
			}
			w.put(slot(name), refBits)
			w.put(offset, 3)
		}
	}
	counted := func(low uint64, payload []byte) func(w *luaMsgBits) {
		return func(w *luaMsgBits) {
			e := ir.TableFieldEntry(field["d"])
			w.put(slot("d"), refBits)
			w.put(low, ir.TableMessageCountBits(e.Shape))
			w.raw(payload)
		}
	}
	const top64 = "ffffffffffffffff"
	cases := []struct{ name, batch, want string }{
		{"a-lawful-255", batch(ranged("a", 5)), "a=ff"},
		{"a-256-past-uint8", batch(ranged("a", 6)), "refused"},
		{"a-257-past-uint8", batch(ranged("a", 7)), "refused"},
		{"b-lawful-255-widened", batch(ranged("b", 5)), "b=ff"},
		{"b-257-past-announced-uint8", batch(ranged("b", 7)), "refused"},
		{"q-lawful-int64-top", batch(ranged("q", 5)), "q=7fffffffffffffff"},
		{"q-past-int64", batch(ranged("q", 7)), "refused"},
		{"r-lawful-uint64-top", batch(ranged("r", 5)), "r=" + top64},
		{"r-past-uint64", batch(ranged("r", 7)), "refused"},
		{"h-lawful-int128-top", batch(ranged("h", 5)), "h=7fffffffffffffff:" + top64},
		{"h-past-int128", batch(ranged("h", 7)), "refused"},
		{"s-skip-lawful", batch(ranged("s", 5)), "read"},
		{"s-skip-past-uint8", batch(ranged("s", 7)), "refused"},
		{"z-skip-lawful", batch(ranged("z", 5)), "read"},
		{"z-skip-past-int128", batch(ranged("z", 7)), "refused"},
		{"d-count-3-from-min-2", batch(counted(1, []byte{1, 2, 3})), "d=010203"},
		{"d-count-2-at-min", batch(counted(0, []byte{9, 8})), "d=0908"},
		{"d-count-5-past-max", batch(counted(3, []byte{1, 2, 3, 4, 5})), "refused"},
	}
	var lines []string
	for _, c := range cases {
		lines = append(lines, c.name+" "+c.batch+" "+c.want)
	}
	probe := strings.Replace(luaRangedProbe, "MODULE", `"`+ModuleName(reader)+`"`, 1)
	out, stderr, err := try(t, files, probe, map[string][]byte{
		"announce": tablewire.Announce(writer),
		"cases":    []byte(strings.Join(lines, "\n") + "\n"),
	})
	if err != nil {
		t.Fatalf("probe: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		name, verdict, _ := strings.Cut(line, " ")
		got[name] = verdict
	}
	for _, c := range cases {
		if got[c.name] != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got[c.name], c.want)
		}
	}
}

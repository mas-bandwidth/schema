package luatable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// announceShapeId is the id of the one entry each row adds to the unit's own
// vocabulary: an id the receiver does not declare, so the reader keeps the
// entry only by its announced shape.
const announceShapeId = uint64(0x0a11ce5ba9e0f00d)

// announceShapeEntry is one vocabulary entry under announceShapeId: the id,
// the kind, then the shape's bytes as given.
func announceShapeEntry(kind byte, shape ...[]byte) []byte {
	out := binary.LittleEndian.AppendUint64(nil, announceShapeId)
	out = append(out, kind)
	for _, part := range shape {
		out = append(out, part...)
	}
	return out
}

func announceShapeLeb(v uint64) []byte { return binary.AppendUvarint(nil, v) }

// announceShapeAnnouncement lays out an announcement exactly as the engine's
// Announce does, over the given vocabulary bytes.
func announceShapeAnnouncement(u *ir.Unit, vocabulary []byte) []byte {
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, ir.BuildVersion(u))
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{uint8(ir.TableKindU8)}
	body = binary.AppendUvarint(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = binary.AppendUvarint(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// TestLuaAnnounceReadHoldsShapeCeilings pins docs/SPEC-TABLES.md §3.3's
// SECURITY rule on the Lua runtime's AnnounceRead: A HOSTILE SHAPE IS A
// HOSTILE WIDTH, and every width is checked before it is used. Each row adds
// one entry to the examples unit's vocabulary and states the verdict beside
// the sentence it follows; the Go engine's AnnounceRead is held to the same
// verdict, and a refused announcement sets no vocabulary.
func TestLuaAnnounceReadHoldsShapeCeilings(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	unit := luarun.LoadUnit(t, filepath.Join(repo, "tables", "examples"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	vocabulary := ir.TableVocabularyBytes(tablewire.MessageVocabulary(unit))
	if !bytes.Equal(announceShapeAnnouncement(unit, vocabulary), tablewire.Announce(unit)) {
		t.Fatal("the test's announcement layout is not the engine's")
	}

	const (
		accepted = "accepted"
		refused  = "refused"
	)
	zeroWidthU8 := []byte{uint8(ir.TableKindU8), 1, 0, 0} // ranged, bits 0, base 0
	rawU8 := []byte{uint8(ir.TableKindU8), 0}
	array := func(min, max uint64, elem []byte) []byte {
		return announceShapeEntry(uint8(ir.TableKindArray), announceShapeLeb(min), announceShapeLeb(max), elem)
	}
	keyed := func(max uint64, elem []byte) []byte {
		return announceShapeEntry(uint8(ir.TableKindKeyed), announceShapeLeb(max), elem)
	}
	str := func(max uint64) []byte {
		return announceShapeEntry(uint8(ir.TableKindString), announceShapeLeb(max))
	}
	ranged := func(kind byte, bits uint64, base []byte) []byte {
		return announceShapeEntry(kind, []byte{1}, announceShapeLeb(bits), base)
	}
	wideBase := make([]byte, 16)
	rows := []struct {
		name  string
		entry []byte
		want  string
		rule  string
	}{
		{"own", nil, accepted,
			"the unit's own vocabulary is a lawful announcement"},
		{"array_min_max_2p32_zero_width", array(1<<32, 1<<32, zeroWidthU8), refused,
			"an array, on min as well as on max, is bounded by the 32-bit count an unbounded array announces (§2.9)"},
		{"array_min_max_2p40_zero_width", array(1<<40, 1<<40, zeroWidthU8), refused,
			"an array, on min as well as on max, is bounded by the 32-bit count an unbounded array announces (§2.9)"},
		{"array_min_max_2p64m1", array(^uint64(0), ^uint64(0), zeroWidthU8), refused,
			"a count is bounded while it is still wide"},
		{"array_max_2p32", array(0, 1<<32, rawU8), refused,
			"an array's max is bounded by the 32-bit count (§2.9)"},
		{"array_min_above_max", array(3, 2, rawU8), refused,
			"an array whose min exceeds its max"},
		{"array_min_max_2p32m1_zero_width", array(1<<32-1, 1<<32-1, zeroWidthU8), accepted,
			"a ranged element whose min equals its max rides no bits, under a count within the ceiling"},
		{"array_min_eq_max", array(4, 4, rawU8), accepted,
			"an array whose min equals its max"},
		{"keyed_max_2p32", keyed(1<<32, rawU8), refused,
			"a keyed entry is bounded by the 32-bit count (§2.9)"},
		{"keyed_max_2p32m1", keyed(1<<32-1, rawU8), accepted,
			"a keyed entry's max at the 32-bit count"},
		{"string_max_2p31", str(1 << 31), refused,
			"a string is bounded by the int32 storage cap (SPEC §4.3, §6.1)"},
		{"string_max_2p63", str(1 << 63), refused,
			"a string is bounded by the int32 storage cap, compared while the bound is still wide"},
		{"string_max_2p31m1", str(1<<31 - 1), accepted,
			"a string's max at the int32 storage cap"},
		{"u128_bits_129", ranged(uint8(ir.TableKindU128), 129, wideBase), refused,
			"a bits above 128"},
		{"u128_bits_128", ranged(uint8(ir.TableKindU128), 128, wideBase), accepted,
			"a bits the kind holds"},
		{"ufixed128_bits_129", ranged(uint8(ir.TableKindUFixed128), 129, wideBase), refused,
			"a bits above 128"},
		{"u64_bits_65", ranged(uint8(ir.TableKindU64), 65, []byte{0}), refused,
			"a packed bits sits inside what the kind holds"},
		{"u8_bits_9", ranged(uint8(ir.TableKindU8), 9, []byte{0}), refused,
			"a packed bits sits inside what the kind holds"},
		{"u8_bits_8", ranged(uint8(ir.TableKindU8), 8, []byte{0}), accepted,
			"a packed bits the kind holds"},
		{"fixed8_bits_9", ranged(uint8(ir.TableKindFixed8), 9, wideBase), refused,
			"a packed bits sits inside what the kind holds"},
		{"array_elem_kind_34", array(0, 1, []byte{34}), refused,
			"an element kind outside the closed set"},
		{"array_elem_kind_200", array(0, 1, []byte{200}), refused,
			"an element kind outside the closed set"},
		{"string_shape_past_field", announceShapeEntry(uint8(ir.TableKindString)), refused,
			"a shape running past the vocabulary field's own length"},
	}

	inputs := map[string][]byte{}
	want := map[string]string{}
	for _, row := range rows {
		announcement := announceShapeAnnouncement(unit, append(bytes.Clone(vocabulary), row.entry...))
		inputs[row.name] = announcement
		want[row.name] = row.want
		// the Go engine is the reference for every verdict the table states
		v := &tablewire.Vocabulary{}
		readErr := v.AnnounceRead(announcement, &tabletext.Report{})
		engine := refused
		if readErr == nil && v.Announced() {
			engine = accepted
		}
		if engine != row.want {
			t.Errorf("%s: the Go engine says %s, the table says %s (%s)", row.name, engine, row.want, row.rule)
		}
	}

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.name)
	}
	sort.Strings(names)
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local inputs = require("inputs")
local names = { %s }
for _, name in ipairs(names) do
  local vocab = {}
  local ok = T.AnnounceRead(vocab, inputs[name], {})
  if ok then
    print(name .. "=accepted")
  elseif vocab.announced or vocab.count ~= nil or vocab[1] ~= nil then
    print(name .. "=refused-with-vocabulary")
  else
    print(name .. "=refused")
  end
end
`, ModuleName(unit), luaNameList(names)))
	out := luarun.Run(t, dir, "main.lua", inputs)
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, verdict, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("unexpected probe line %q", line)
		}
		got[name] = verdict
	}
	for _, row := range rows {
		if got[row.name] != row.want {
			t.Errorf("%s: Lua AnnounceRead %s, want %s (%s)", row.name, got[row.name], row.want, row.rule)
		}
	}
}

func luaNameList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}

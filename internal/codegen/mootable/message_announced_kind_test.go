package mootable_test

import (
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A MESSAGE-FORM RANGED INTEGER IS HELD TO THE ANNOUNCED KIND, NAMED OR
// SKIPPED (docs/SPEC-TABLES.md §3.3; review 2026-11-21, cross-leg
// message-form rules 1 and 2). Base plus offset is added whole, never
// wrapped: a uint8 announced at base 250 cannot reach 257 even where this
// build reads the field as a uint16, an int64 or a uint64 cannot wrap past
// its end, and a field this build does not know is held to the same rule on
// the skip path, scalar or array element, so one batch gets one verdict
// whichever fields the reader keeps.

const announcedKindSchema = "package probe\n\nfixed table Ints\n{\n    w uint16\n    q int64\n    r uint64\n}\n"

// announcedPart is one field written in the batch: the vocabulary slot, then
// the bits that follow its reference (an offset, or a count and offsets).
type announcedPart struct {
	slot int
	put  func(w *intDomainBits)
}

type announcedCase struct {
	name  string
	edit  func(entries []ir.TableVocabularyEntry, slot map[string]int) []ir.TableVocabularyEntry
	parts func(slot map[string]int, unknown int) []announcedPart
	want  string
}

func offsetPart(slot int, bits int, offset uint64) announcedPart {
	return announcedPart{slot, func(w *intDomainBits) { w.put(offset, bits) }}
}

func announcedCases() []announcedCase {
	ranged := func(e *ir.TableVocabularyEntry, kind uint8, base *big.Int, bits int64) {
		e.Kind = kind
		e.Shape = ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: bits, Base: base}
	}
	maxI64 := new(big.Int).SetInt64(1<<63 - 1)
	maxU64 := new(big.Int).SetUint64(1<<64 - 1)
	narrow := func(entries []ir.TableVocabularyEntry, slot map[string]int) []ir.TableVocabularyEntry {
		ranged(&entries[slot["w"]-1], ir.TableKindU8, big.NewInt(250), 3)
		return entries
	}
	wide := func(entries []ir.TableVocabularyEntry, slot map[string]int) []ir.TableVocabularyEntry {
		ranged(&entries[slot["q"]-1], ir.TableKindI64, maxI64, 1)
		ranged(&entries[slot["r"]-1], ir.TableKindU64, maxU64, 1)
		return entries
	}
	unknownScalar := func(entries []ir.TableVocabularyEntry, _ map[string]int) []ir.TableVocabularyEntry {
		e := ir.TableVocabularyEntry{Id: 0x5eed0001}
		ranged(&e, ir.TableKindU8, big.NewInt(255), 1)
		return append(entries, e)
	}
	unknownArray := func(entries []ir.TableVocabularyEntry, _ map[string]int) []ir.TableVocabularyEntry {
		inner := ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 1, Base: big.NewInt(65535)}
		return append(entries, ir.TableVocabularyEntry{Id: 0x5eed0002, Kind: ir.TableKindArray,
			Shape: ir.TableMessageShape{Min: 0, Max: 2, Elem: ir.TableKindU16, Inner: &inner}})
	}
	array := func(offset uint64) func(map[string]int, int) []announcedPart {
		return func(_ map[string]int, unknown int) []announcedPart {
			return []announcedPart{{unknown, func(w *intDomainBits) {
				w.put(1, 2) // the count, bits_required(0, 2)
				w.put(offset, 1)
			}}}
		}
	}
	return []announcedCase{
		{"named_narrow_in_kind", narrow, func(s map[string]int, _ int) []announcedPart { return []announcedPart{offsetPart(s["w"], 3, 5)} }, "accept 255"},
		{"named_narrow_past_kind", narrow, func(s map[string]int, _ int) []announcedPart { return []announcedPart{offsetPart(s["w"], 3, 7)} }, "refuse"},
		{"named_int64_past_kind", wide, func(s map[string]int, _ int) []announcedPart { return []announcedPart{offsetPart(s["q"], 1, 1)} }, "refuse"},
		{"named_uint64_past_kind", wide, func(s map[string]int, _ int) []announcedPart { return []announcedPart{offsetPart(s["r"], 1, 1)} }, "refuse"},
		{"named_wide_at_kind", wide, func(s map[string]int, _ int) []announcedPart {
			return []announcedPart{offsetPart(s["q"], 1, 0), offsetPart(s["r"], 1, 0)}
		}, "accept 0"},
		{"skipped_scalar_in_kind", unknownScalar, func(_ map[string]int, u int) []announcedPart { return []announcedPart{offsetPart(u, 1, 0)} }, "accept 0"},
		{"skipped_scalar_past_kind", unknownScalar, func(_ map[string]int, u int) []announcedPart { return []announcedPart{offsetPart(u, 1, 1)} }, "refuse"},
		{"skipped_element_in_kind", unknownArray, array(0), "accept 0"},
		{"skipped_element_past_kind", unknownArray, array(1), "refuse"},
	}
}

func TestMooMessageRangedHeldToTheAnnouncedKind(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Ints.schema": announcedKindSchema})
	got := announcedKindRun(t, g)
	for _, c := range announcedCases() {
		if got[c.name] != c.want {
			t.Errorf("%s: moo %q, want %q", c.name, got[c.name], c.want)
		}
	}
}

// announcedKindRun runs every case through g's message read, returning each
// case's verdict by name.
func announcedKindRun(t *testing.T, g *generatedUnit) map[string]string {
	t.Helper()
	u := g.unit
	byID := map[uint64]string{}
	for _, f := range u.Tables["Ints"].Fields {
		byID[ir.TableFieldWireId(f)] = f.Name
	}
	probe := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + `
local vocabulary = MS.NewVocabulary()
local report: any = {}
if not MS.AnnounceRead(vocabulary, inputs.ann, report) then
	print("ANNOUNCE " .. tostring(report.reason))
	return
end
local n, values = MS.LoadMessages(vocabulary, "Ints", inputs.message, 256, report)
if n < 1 or report.damaged or report.refused or report.reason ~= nil then
	print("refuse")
	return
end
print("accept " .. tostring(values[1].W))
`
	out := map[string]string{}
	for _, c := range announcedCases() {
		entries := append([]ir.TableVocabularyEntry(nil), tablewire.MessageVocabulary(u)...)
		slot := map[string]int{}
		for i, e := range entries {
			slot[byID[e.Id]] = i + 1
		}
		entries = c.edit(entries, slot)
		unknown := len(entries) // an appended entry is the last slot
		refBits := ir.TableMessageRefBits(len(entries))
		var w intDomainBits
		w.put(0, 8)
		for _, p := range c.parts(slot, unknown) {
			w.put(uint64(p.slot), refBits)
			p.put(&w)
		}
		w.put(0, refBits)
		w.align()
		// another build of the record (intDomainWire says why)
		ann := intDomainAnnounce(ir.BuildVersion(u)+1, entries)
		msg := append([]byte{ir.TableWireMessageForm}, w.b...)
		out[c.name] = g.run(t, probe, map[string][]byte{"ann": ann, "message": msg})
	}
	return out
}

// TestMooMessageRangedHeldToTheAnnouncedKindNegativeControl lifts the
// announced-kind hold in an emitted copy of table_message.moo: the rows past
// the announced kind, named and skipped, must then land, so the hold is what
// refuses them.
func TestMooMessageRangedHeldToTheAnnouncedKindNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a patched table_message.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Ints.schema": announcedKindSchema})
	src := string(g.files["table_message.moo"])
	if strings.Count(src, announcedKindAnchor) != 1 {
		t.Fatal("the announced-kind anchor is not in table_message.moo exactly once")
	}
	planted := strings.Replace(src, announcedKindAnchor, announcedKindPlant, 1)
	if err := os.WriteFile(filepath.Join(g.dir, "table_message.moo"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	got := announcedKindRun(t, g)
	for _, name := range []string{"named_narrow_past_kind", "skipped_scalar_past_kind", "skipped_element_past_kind"} {
		if got[name] == "refuse" {
			t.Errorf("%s still refuses with the announced-kind hold lifted: the hold is not what refuses it", name)
		}
	}
}

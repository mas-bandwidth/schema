package mootable_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// §4's ONE NESTING BOUND holds every walk of the binary forms, and the
// announcement is a form-1 file: an announced array shape may nest 128 levels,
// a deeper one is refused, and reading an announcement never raises. The Go
// engine's own shape reader is not the oracle here: this pins the moo
// announcement's bound on its own.
const announceShapeNestingBound = 128

// nestedArrayEntry is one vocabulary entry whose array shape nests `levels`
// levels around a payload-free element, encoded a level at a time so a deep
// shape costs the test no stack.
func nestedArrayEntry(id uint64, levels int) []byte {
	out := binary.LittleEndian.AppendUint64(nil, id)
	out = append(out, ir.TableKindArray)
	for i := range levels {
		out = append(out, 0, 0) // an unbounded count's min and max
		elem := uint8(ir.TableKindArray)
		if i == levels-1 {
			elem = ir.TableKindNoPayload
		}
		out = append(out, elem)
	}
	return out
}

// announcementFor is the form-1 wire an announcement with this vocabulary and
// build version carries, byte for byte as the compiler spells it.
func announcementFor(vocabulary []byte, version uint64) []byte {
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, version)
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{uint8(ir.TableKindU8)}
	body = binary.AppendUvarint(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = binary.AppendUvarint(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	out = binary.LittleEndian.AppendUint32(out, 2)
	return binary.LittleEndian.AppendUint32(out, 0)
}

// announceWith runs one announcement through the emitted runtime and answers
// only its verdict: "announce" or "refuse".
func announceWith(t *testing.T, g *generatedUnit, ann []byte) string {
	t.Helper()
	const main = `--!strict
local inputs = require("./inputs")
local MS = require("./table_message")
local v = MS.NewVocabulary()
local report: any = {}
if MS.AnnounceRead(v, inputs.ann, report) then print("announce") else print("refuse") end
`
	return g.run(t, main, map[string][]byte{"ann": ann})
}

func shapeDepthAnnouncement(t *testing.T, g *generatedUnit, levels int) []byte {
	t.Helper()
	vocabulary := ir.TableVocabularyBytes(tablewire.MessageVocabulary(g.unit))
	vocabulary = append(vocabulary, nestedArrayEntry(0x5a5a5a5a00000001, levels)...)
	return announcementFor(vocabulary, ir.BuildVersion(g.unit))
}

// TestMooAnnounceShapeNestingBound: an announcement carrying one extra
// vocabulary entry whose array shape nests 2 or 128 levels is announced, and
// one nesting 129 or 21,000 is refused without raising.
func TestMooAnnounceShapeNestingBound(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "messagedemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	for _, c := range []struct {
		levels int
		want   string
	}{
		{2, "announce"},
		{announceShapeNestingBound, "announce"},
		{announceShapeNestingBound + 1, "refuse"},
		{21000, "refuse"},
	} {
		if got := announceWith(t, g, shapeDepthAnnouncement(t, g, c.levels)); got != c.want {
			t.Errorf("an array shape nested %d levels: moo answered %q, want %q", c.levels, got, c.want)
		}
	}
}

// TestMooAnnounceShapeNestingBoundNegativeControl removes the level test from
// an emitted copy of table_message.moo and requires the 129-level row to be
// announced, so the bound's test goes red when the bound is gone.
func TestMooAnnounceShapeNestingBoundNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "messagedemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	src := string(g.files["table_message.moo"])
	const anchor = "\tif level > SHAPE_MAX_DEPTH then\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the shape nesting bound's anchor is not in table_message.moo exactly once")
	}
	patched := strings.Replace(src, anchor, "\tif false then\n", 1)
	if err := os.WriteFile(filepath.Join(g.dir, "table_message.moo"), []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got := announceWith(t, g, shapeDepthAnnouncement(t, g, announceShapeNestingBound+1))
	if got != "announce" {
		t.Fatalf("the reader without the level test did not announce the 129-level shape: %s", got)
	}
}

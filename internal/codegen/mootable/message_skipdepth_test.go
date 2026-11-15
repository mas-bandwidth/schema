package mootable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MESSAGE FORM'S SKIP WALK IS BOUNDED AT DEPTH 128 (docs/SPEC-TABLES.md
// §3.3, message_skip_depth): an entry this reader cannot name is skipped
// through the sender's announced shape, and a nested body, array, keyed or map
// body, or a union whose arm is structural, past 128 levels refuses. Hostile
// bytes never raise.
const skipDepthUnknownId = uint64(0x5a5a5a5a00000001)

// skipDepthAnnouncement is the messagedemo unit's own message vocabulary with
// one entry the reader cannot name, of kind 13, appended. It answers the
// announcement, the entry's reference, and the reference width.
func skipDepthAnnouncement(t *testing.T, u *ir.Unit) (ann []byte, ref uint64, refBits int) {
	t.Helper()
	entries := append([]ir.TableVocabularyEntry(nil), tablewire.MessageVocabulary(u)...)
	used := map[uint64]bool{}
	for _, e := range entries {
		used[e.Id] = true
	}
	id := skipDepthUnknownId
	for used[id] {
		id++
	}
	entries = append(entries, ir.TableVocabularyEntry{Id: id, Kind: ir.TableKindTable})
	vocabulary := ir.TableVocabularyBytes(entries)

	out := []byte{ir.TableWireForm, 1, byte(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, ir.BuildVersion(u))
	out = append(out, 2, byte(ir.TableKindArray))
	var body []byte
	body = append(body, byte(ir.TableKindU8))
	body = binary.AppendUvarint(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = binary.AppendUvarint(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out, uint64(len(entries)), ir.TableMessageRefBits(len(entries))
}

// skipDepthBatch is a batch of one body that names the unknown entry at each
// level, then closes every body it opened.
func skipDepthBatch(levels int, ref uint64, refBits int) []byte {
	out := []byte{ir.TableWireMessageForm}
	acc, n := uint64(0), 0
	put := func(v uint64, width int) {
		for i := range width {
			acc |= ((v >> uint(i)) & 1) << uint(n)
			n++
			if n == 8 {
				out = append(out, byte(acc))
				acc, n = 0, 0
			}
		}
	}
	put(0, 8) // M - 1
	for range levels {
		put(ref, refBits)
	}
	for i := 0; i < levels+1; i++ {
		put(0, refBits)
	}
	if n > 0 {
		out = append(out, byte(acc))
	}
	return out
}

// skipDepthProbe loads each named case into the messagedemo User root under
// the custom announcement and prints whether it was accepted, refused, or
// raised out of LoadMessages.
func skipDepthProbe(g *generatedUnit, cases ...string) string {
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal MS = require(\"./table_message\")\n")
	b.WriteString(g.requireAll())
	b.WriteString(`
local function one(name: string, data: buffer)
	local v = MS.NewVocabulary()
	local rep: any = {}
	if not MS.AnnounceRead(v, inputs.ann, rep) then print(name .. " announce") return end
	local ok, n, values = pcall(function()
		return MS.LoadMessages(v, "User", data, 256, rep)
	end)
	if not ok then print(name .. " raise") return end
	if rep.damaged or rep.refused or rep.reason ~= nil or n < 1 then print(name .. " refuse") return end
	print(name .. " accept " .. tostring(n))
end
`)
	for _, c := range cases {
		fmt.Fprintf(&b, "one(%q, inputs.%s)\n", c, c)
	}
	return b.String()
}

func skipDepthInputs(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	ann, ref, refBits := skipDepthAnnouncement(t, u)
	return map[string][]byte{
		"ann":   ann,
		"at128": skipDepthBatch(128, ref, refBits),
		"at129": skipDepthBatch(129, ref, refBits),
		"deep":  skipDepthBatch(20000, ref, refBits),
	}
}

// TestMooMessageSkipWalkDepthBound holds the message form's skip bound on the
// tables/messages User root: an unknown kind-13 entry may nest 128 levels and
// reads, while 129 and 20,000 levels refuse without raising.
func TestMooMessageSkipWalkDepthBound(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "messagedemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	inputs := skipDepthInputs(t, g.unit)
	out := g.run(t, skipDepthProbe(g, "at128", "at129", "deep"), inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 1 {
			got[f[0]] = strings.Join(f[1:], " ")
		}
	}
	if got["at128"] != "accept 1" {
		t.Errorf("128 levels: %q, want accept", got["at128"])
	}
	if got["at129"] != "refuse" {
		t.Errorf("129 levels: %q, want refuse", got["at129"])
	}
	if got["deep"] != "refuse" {
		t.Errorf("20000 levels: %q, want refuse", got["deep"])
	}
}

// TestMooMessageSkipWalkDepthBoundNegativeControl removes the level test from
// an emitted copy of table_message.moo and requires the 129-level row to stop
// refusing.
func TestMooMessageSkipWalkDepthBoundNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "messagedemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	inputs := skipDepthInputs(t, g.unit)
	src := string(g.files["table_message.moo"])
	const anchor = "\tif depth > SKIP_MAX_DEPTH then\n\t\treturn false\n\tend\n"
	if n := strings.Count(src, anchor); n != 2 {
		t.Fatalf("the skip-depth test is in table_message.moo %d times, want 2", n)
	}
	files := map[string][]byte{}
	maps.Copy(files, g.files)
	files["table_message.moo"] = []byte(strings.ReplaceAll(src, anchor, "\tif false then\n\t\treturn false\n\tend\n"))
	patched := writeUnit(t, g.unit, files)
	out := patched.run(t, skipDepthProbe(patched, "at129"), inputs)
	if !strings.Contains(out, "at129 accept") {
		t.Fatalf("the reader with the skip-depth test removed still refuses 129 levels: %s", out)
	}
}

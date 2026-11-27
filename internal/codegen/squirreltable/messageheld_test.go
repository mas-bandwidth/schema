package squirreltable_test

// THE MESSAGE FORM GIVES ONE VERDICT, NAMED OR SKIPPED (docs/SPEC-TABLES.md
// §3.3; the review's across-legs message-form rules 1 to 3): a ranged
// integer's base plus offset is held to the ANNOUNCED kind's domain, exactly
// and never wrapped, whether this reader names the field (and widens it) or
// skips it as unknown, and a union FIELD at arm reference 0 is damage on the
// skip path as on the named read. Each hostile body is paired with the same
// body at the announced kind's end, which reads.

import (
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v3/ir"
)

const messageHeldSchema = `package msgheld

union Pick
{
    k int32
}

fixed table Root
{
    a int32
    w uint64
    p Pick
}
`

func TestSquirrelMessageRangedHeldToAnnouncedKindAndUnionFieldArmZero(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	dir := t.TempDir()
	u := unitFromSchema(t, messageHeldSchema)
	mod := emitUnit(t, dir, "probe", u)
	root := u.Tables["Root"]
	ranged := func(id uint64, kind uint8, bits int64, base *big.Int) ir.TableVocabularyEntry {
		return ir.TableVocabularyEntry{Id: id, Kind: kind, Shape: ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: bits, Base: base}}
	}
	maxU64 := new(big.Int).SetUint64(^uint64(0))
	const unknownInt, unknownUnion = 0x5eed000000000001, 0x5eed000000000002
	entries := []ir.TableVocabularyEntry{
		// slot 1: field a announced as an int8, base 100, read widened to int32
		ranged(ir.TableFieldWireId(findField(t, root, "a")), byte(ir.TableKindI8), 8, big.NewInt(100)),
		// slot 2: field w announced as a uint64 at base 2^64-1
		ranged(ir.TableFieldWireId(findField(t, root, "w")), byte(ir.TableKindU64), 1, maxU64),
		// slot 3: a field this reader does not declare, an int8 at base 100
		ranged(unknownInt, byte(ir.TableKindI8), 8, big.NewInt(100)),
		// slot 4: a union field this reader does not declare
		{Id: unknownUnion, Kind: byte(ir.TableKindUnion)},
	}
	refBits := ir.TableMessageRefBits(len(entries))
	body := func(slot int, raw uint64, width int) []byte {
		b := newMsgBits(1, refBits)
		b.ref(slot)
		b.put(raw, width)
		b.ref(0)
		return b.done()
	}
	inputs := map[string][]byte{
		"ann":            announcementAtBuild(entries, ir.BuildVersion(u)^1),
		"named_over":     body(1, 200, 8), // 300 is past int8
		"named_end":      body(1, 27, 8),  // 127
		"named_wrap":     body(2, 1, 1),   // 2^64 is past uint64, and would wrap to 0
		"named_wrap_end": body(2, 0, 1),   // 2^64-1
		"skip_over":      body(3, 200, 8),
		"skip_end":       body(3, 27, 8),
		"skip_arm_zero":  body(4, 0, refBits),
	}
	var script strings.Builder
	fmt.Fprintf(&script, `local I = dofile("inputs.nut", true);
local T = dofile("probe/schema_module.nut", true).require(%q);
local voc = {};
local ar = {};
if (!T.AnnounceRead(voc, I["ann"], ar)) throw "announce: " + ar.reason;
local read = function(key) {
  local got = [T.Root()];
  local rr = {};
  local k = T.RootLoadMessages(got, voc, I[key], rr);
  print(key + " " + (rr.refused ? "refused" : "read " + got[0].A + " " + got[0].W) + "\n");
};
`, mod)
	for _, k := range []string{"named_over", "named_end", "named_wrap", "named_wrap_end", "skip_over", "skip_end", "skip_arm_zero"} {
		fmt.Fprintf(&script, "read(%q);\n", k)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			got[k] = v
		}
	}
	for _, row := range [][2]string{
		{"named_over", "refused"},
		{"named_end", "read 127 0"},
		{"named_wrap", "refused"},
		{"named_wrap_end", "read 0 -1"},
		{"skip_over", "refused"},
		{"skip_end", "read 0 0"},
		{"skip_arm_zero", "refused"},
	} {
		if got[row[0]] != row[1] {
			t.Errorf("%s: %q, want %q", row[0], got[row[0]], row[1])
		}
	}
}

package luautable_test

// EVERY DEAD BYTE IS ZERO on the Luau leg (docs/SPEC-TABLES.md §3.4, THE
// SLACK RULE; docs/FIXED-FORM-ALGORITHM.md §3.1; docs/rulings.json
// fixed-dead-bytes-zero; the owner's ruling of 2026-11-10, "Every dead byte",
// "Be strict"): an absent optional's payload, a counted array's tail past its
// live count (at the top level and inside a taken arm), a string(N) or
// bytes(N) past its used length, and a union's arm bytes under tag 0, behind a
// narrower arm and under a payload-free arm are slack, and a non-zero byte in
// any of them refuses the whole record as wire_damaged, on the identity read
// and on a newer reader's lineage read of the older writer's record. Each
// stain is a byte that would be lawful if it were live (a bool's 1, an ordinal
// in range), so the slack rule is the only rule that can refuse it. The
// negative control empties the runtime's slack check and requires every stain
// to read.

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const luauDeadOlder = "package probe\nenum Small { One, Two }\nunion Pick\n{\n    narrow uint8\n    wide uint32\n    ping\n    few [..2]uint8\n}\nfixed table D\n{\n    opt ?int32\n    arr [..3]Small\n    s string(4)\n    b bytes(4)\n    u0 Pick\n    u1 Pick\n    u2 Pick\n    u3 Pick\n    tail int32 = 7\n}\n"

// the record image, by hand from §3.4's sizes: opt 1 + 4 at 0, arr 4 + 3 at
// 5, s 4 + 4 at 12, b 4 + 4 at 20, four Picks of tag 1 + widest 6 (few: a
// count and two bytes) at 28, 35, 42 and 49, and tail at 56; 60 bytes.
var luauDeadStains = []struct {
	name string
	at   int
}{
	{"an absent optional's payload", 1},
	{"an absent optional's last payload byte", 4},
	{"a counted array's tail past its count", 10},
	{"a counted array's last tail slot", 11},
	{"a string past its used length", 18},
	{"a string's last capacity byte", 19},
	{"a byte buffer past its used length", 26},
	{"a byte buffer's last capacity byte", 27},
	{"the arm bytes under tag 0", 29},
	{"the arm bytes under tag 0, the last", 34},
	{"the arm bytes behind a narrower arm", 37},
	{"the arm bytes behind a narrower arm, the last", 41},
	{"the arm bytes under a payload-free arm", 43},
	{"the arm bytes under a payload-free arm, the last", 48},
	{"a counted array's tail inside a taken arm", 55},
}

func luauDeadBody() []byte {
	b := make([]byte, 60)
	// opt absent: 0..4 stay zero
	binary.LittleEndian.PutUint32(b[5:], 1) // arr = [One], slots 10..11 the tail
	b[9] = 1
	binary.LittleEndian.PutUint32(b[12:], 2) // s = "hi"
	b[16], b[17] = 'h', 'i'
	binary.LittleEndian.PutUint32(b[20:], 1) // b = 05
	b[24] = 5
	// u0: tag 0 at 28, arm bytes 29..34 zero
	b[35], b[36] = 1, 9 // u1: narrow = 9, bytes 37..41 zero
	b[42] = 3           // u2: ping, bytes 43..48 zero
	b[49] = 4           // u3: few = [8], the count at 50, 8 at 54, 55 the tail
	binary.LittleEndian.PutUint32(b[50:], 1)
	b[54] = 8
	binary.LittleEndian.PutUint32(b[56:], 7)
	return b
}

func TestLuauFixedDeadBytesRefuse(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	older := luauCutSource(t, luauDeadOlder)
	// the lineage read: the same table with a field appended, reading the
	// older writer's record under ITS layout (FIXED-FORM-ALGORITHM.md §5.2)
	newer := luauCutSource(t, strings.Replace(luauDeadOlder, "    tail int32 = 7\n", "    tail int32 = 7\n    added int32 = 5\n", 1))
	e := luauCutEntry(t, older, "D")
	lineage := luauCutLineage(t, older, "D")
	clean := luauDeadBody()
	stained := func(at int) []byte {
		b := luauCutClone(clean)
		b[at] = 1
		return b
	}
	g := luauCutGen(t, older, nil)
	// THE WRITER LEAVES ZEROS IN SLACK: a default record with a value set
	// under the absent optional saves zeros everywhere but the tail's 7
	saved := g.save(t, "D", "local v = T.NewD(); v.Opt = 77; v.OptPresent = false; values[1] = v")
	want := make([]byte, 60)
	binary.LittleEndian.PutUint32(want[56:], 7)
	if len(saved) < len(want) || string(saved[len(saved)-len(want):]) != string(want) {
		t.Fatalf("the writer's default record:\n%x, want its body\n%x", saved, want)
	}
	file := func(body []byte) []byte { return luauCutFile(e.Wire, e.Layout, body) }
	reads := []struct {
		name string
		g    *luauCutUnit
	}{{"identity", g}, {"lineage", luauCutGen(t, newer, lineage)}}
	for _, r := range reads {
		if got := r.g.load(t, "D", file(clean)); !strings.HasPrefix(got.head, "ok 1 ") {
			t.Fatalf("%s: the clean record: %s %v", r.name, got.head, got.dump)
		}
		for _, s := range luauDeadStains {
			if got := r.g.load(t, "D", file(stained(s.at))).head; got != "refused wire_damaged" {
				t.Errorf("%s: a stain in %s (body byte %d): %s, want refused wire_damaged", r.name, s.name, s.at, got)
			}
		}
	}
	// THE NEGATIVE CONTROL: a runtime whose slack check passes everything
	// reads every stained record, on both reads.
	for _, r := range []struct {
		name string
		g    *luauCutUnit
	}{{"identity", luauCutGen(t, older, nil)}, {"lineage", luauCutGen(t, newer, lineage)}} {
		r.g.plant(t, "local function deadZero(buf: buffer, at: number, n: number)\n\tlocal i = 0\n", "local function deadZero(buf: buffer, at: number, n: number)\n\tlocal i = 0\n\tn = 0\n")
		for _, s := range luauDeadStains {
			if got := r.g.load(t, "D", file(stained(s.at))).head; got == "refused wire_damaged" {
				t.Errorf("negative control, %s: a stain in %s still refuses with the slack check gone", r.name, s.name)
			}
		}
	}
}

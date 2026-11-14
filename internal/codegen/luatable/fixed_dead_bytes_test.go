package luatable

// EVERY DEAD BYTE IS ZERO on the Lua leg (docs/SPEC-TABLES.md §3.4, THE SLACK
// RULE; docs/FIXED-FORM-ALGORITHM.md §3.1; docs/rulings.json
// fixed-dead-bytes-zero; the owner's ruling of 2026-11-10, "Every dead byte",
// "Be strict"): an absent optional's payload, a counted array's tail past its
// live count, a string(N) or bytes(N) past its used length, and a union's arm
// bytes under tag 0, behind a narrower arm and under a payload-free arm are
// slack, and a non-zero byte in any of them refuses the whole record as
// wire_damaged, on the identity read and on an older writer's lineage read.
// Each stain is a byte that would be lawful if it were live (a bool's 1, an
// ordinal in range), so the slack rule is the only rule that can refuse it.
// The negative control empties the slack check and requires every stain to
// read.

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const luaDeadOlder = "package probe\nenum Small { One, Two }\nunion Pick\n{\n    narrow uint8\n    wide uint32\n    ping\n}\nfixed table D\n{\n    opt ?int32\n    arr [..3]Small\n    s string(4)\n    b bytes(4)\n    none Pick\n    narrow Pick\n    ping Pick\n    tail int32 = 7\n}\n"

// the record image, by hand from §3.4's sizes: opt 1 + 4 at 0, arr 4 + 3 at
// 5, s 4 + 4 at 12, b 4 + 4 at 20, three Picks of tag 1 + widest 4 at 28, 33
// and 38, and tail at 43; 47 bytes in all.
var luaDeadStains = []struct {
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
	{"the arm bytes under tag 0, the last", 32},
	{"the arm bytes behind a narrower arm", 35},
	{"the arm bytes behind a narrower arm, the last", 37},
	{"the arm bytes under a payload-free arm", 39},
	{"the arm bytes under a payload-free arm, the last", 42},
}

func luaDeadBody() []byte {
	b := make([]byte, 47)
	// opt absent: 0..4 stay zero
	binary.LittleEndian.PutUint32(b[5:], 1) // arr = { One }, slots 10..11 the tail
	b[9] = 1
	binary.LittleEndian.PutUint32(b[12:], 2) // s = "hi"
	b[16], b[17] = 'h', 'i'
	binary.LittleEndian.PutUint32(b[20:], 1) // b = "\x05"
	b[24] = 5
	// none: tag 0 at 28, arm bytes 29..32 zero
	b[33], b[34] = 1, 9 // narrow: tag 1, narrow = 9, bytes 35..37 zero
	b[38] = 3           // ping: tag 3, bytes 39..42 zero
	binary.LittleEndian.PutUint32(b[43:], 7)
	return b
}

func TestLuaFixedDeadBytesRefuse(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	older := luaEvoSource(t, luaDeadOlder)
	// the lineage reader: the same table with a field appended, so an older
	// writer's record is read under ITS layout (FIXED-FORM-ALGORITHM.md §5.2)
	newer := luaEvoSource(t, luaDeadOlder[:len(luaDeadOlder)-2]+"    added int32 = 5\n}\n")
	e := luaEvoEntry(t, older, "D")
	clean := luaDeadBody()
	stained := func(at int) []byte {
		b := luaEvoClone(clean)
		b[at] = 1
		return b
	}
	// THE WRITER LEAVES ZEROS IN SLACK: the module's own save of the same
	// value is the hand-built image, byte for byte, even with a value set
	// under the absent optional.
	saved := luaEvoGen(t, older, nil).save(t, "D", `local v = T.NewD(); v.opt = 77; v.opt_present = false; v.arr = { "One" }; v.s = "hi"; v.b = "\5"
v.none = { tag = 0 }; v.narrow = { tag = 1, narrow = 9 }; v.ping = { tag = 3 }; values[1] = v`)
	if len(saved) < len(clean) || string(saved[len(saved)-len(clean):]) != string(clean) {
		t.Fatalf("the writer's record:\n%x, want\n%x", saved, clean)
	}
	lineage := luaEvoLineage(t, older, "D")
	reads := []struct {
		name string
		g    *luaEvoUnit
		want string
	}{
		{"identity", luaEvoGen(t, older, nil), "ok 1 w0 u0"},
		{"lineage", luaEvoGen(t, newer, lineage), "ok 1 w0 u0"},
	}
	for _, r := range reads {
		got := r.g.load(t, "D", luaEvoFile(e.Wire, e.Layout, clean))
		if got.head != r.want {
			t.Fatalf("%s: the clean record: %s, want %s", r.name, got.head, r.want)
		}
		for k, w := range map[string]string{"r0.opt_present": "false", "r0.arr.1": `"One"`, "r0.s": `"hi"`, "r0.none.tag": "0", "r0.narrow.tag": "1", "r0.narrow.narrow": "9", "r0.ping.tag": "3", "r0.tail": "7"} {
			if got.dump[k] != w {
				t.Errorf("%s: the clean record's %s landed %q, want %s", r.name, k, got.dump[k], w)
			}
		}
		for _, s := range luaDeadStains {
			if got := r.g.load(t, "D", luaEvoFile(e.Wire, e.Layout, stained(s.at))).head; got != "refused wire_damaged" {
				t.Errorf("%s: a stain in %s (body byte %d): %s, want refused wire_damaged", r.name, s.name, s.at, got)
			}
		}
	}
	// THE NEGATIVE CONTROL: a reader that never checks slack reads every
	// stained record, on both reads.
	for _, r := range []struct {
		name string
		g    *luaEvoUnit
	}{{"identity", luaEvoGen(t, older, nil)}, {"lineage", luaEvoGen(t, newer, lineage)}} {
		r.g.plant(t, `  if n > 0 and strfind(string.sub(buf, pos, pos + n - 1), "[^\0]") then error("wire_damaged", 0) end`, "")
		for _, s := range luaDeadStains {
			if got := r.g.load(t, "D", luaEvoFile(e.Wire, e.Layout, stained(s.at))).head; got == "refused wire_damaged" {
				t.Errorf("the negative control, %s: a stain in %s still refuses with the slack check gone", r.name, s.name)
			}
		}
	}
}

package baseline_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

func TestExplore(t *testing.T) {
	cases := []struct {
		name, before, after, table string
		jsons                      []string
	}{
		{"same 2..4", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [2..4]int32\n    n int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[0,0]}`, `{"xs":[0,0,0]}`, `{"xs":[1,0]}`}},
		{"min 2 to 1", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [1..4]int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[0,0]}`, `{"xs":[0,0,0]}`, `{"xs":[1,0]}`}},
		{"min 1 to 2", "package fixture\ntable Save\n{\n    xs [1..4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[0]}`, `{"xs":[5]}`, `{"xs":[1,0]}`}},
		{"field deprecated", "package fixture\ntable Save\n{\n    x int32\n    n int32\n}\n", "package fixture\ntable Save\n{\n    x int32 | deprecated\n    n int32\n}\n", "Save",
			[]string{`{"x":5,"n":6}`}},
		{"variant deprecated", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    t Team = Red\n    ts [..4]Team\n}\n", "package fixture\nenum Team\n{\n    Red\n    Blue | deprecated\n}\ntable Save\n{\n    t Team = Red\n    ts [..4]Team\n}\n", "Save",
			[]string{`{"t":"Blue","ts":["Blue","Red"]}`}},
		{"key variant removed", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    values [Team]uint8\n}\n", "package fixture\nenum Team\n{\n    Red\n}\ntable Save\n{\n    values [Team]uint8\n}\n", "Save",
			[]string{`{"values":{"Red":11,"Blue":22}}`}},
		{"partial rekey", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\nenum Side\n{\n    Red\n    Green\n}\ntable Save\n{\n    values [Team]uint8\n    s Side\n}\n", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\nenum Side\n{\n    Red\n    Green\n}\ntable Save\n{\n    values [Side]uint8\n    s Side\n    t Team\n}\n", "Save",
			[]string{`{"values":{"Red":11,"Blue":22}}`}},
		{"text elem cap", "package fixture\ntable Save\n{\n    rules [..4]string(8)\n}\n", "package fixture\ntable Save\n{\n    rules [..4]string(2)\n}\n", "Save",
			[]string{`{"rules":["abcdef"]}`}},
		{"float range", "package fixture\ntable Save\n{\n    x float32 | min = 0.0, max = 100.0, resolution = 0.5\n}\n", "package fixture\ntable Save\n{\n    x float32 | min = 0.0, max = 50.0, resolution = 0.5\n}\n", "Save",
			[]string{`{"x":75.0}`}},
		{"float resolution", "package fixture\ntable Save\n{\n    x float32 | min = 0.0, max = 100.0, resolution = 0.5\n}\n", "package fixture\ntable Save\n{\n    x float32 | min = 0.0, max = 100.0, resolution = 10.0\n}\n", "Save",
			[]string{`{"x":75.5}`}},
		{"fixed range", "package fixture\ntable Save\n{\n    x fixed(8, 8) | min = -100, max = 100\n}\n", "package fixture\ntable Save\n{\n    x fixed(8, 8) | min = -100, max = 50\n}\n", "Save",
			[]string{`{"x":75.5}`}},
		{"frac", "package fixture\ntable Save\n{\n    x fixed(8, 8)\n}\n", "package fixture\ntable Save\n{\n    x fixed(12, 4)\n}\n", "Save",
			[]string{`{"x":1.5}`}},
		{"bytes size", "package fixture\ntable Save\n{\n    x bytes(8)\n}\n", "package fixture\ntable Save\n{\n    x bytes(2)\n}\n", "Save",
			[]string{`{"x":"aGVsbG8="}`}},
		{"bits width", "package fixture\ntable Save\n{\n    x bits(7)\n}\n", "package fixture\ntable Save\n{\n    x bits(5)\n}\n", "Save",
			[]string{`{"x":95}`}},
		{"map value range", "package fixture\ntable Save\n{\n    m map[uint16]int32 | min = -100, max = 100\n}\n", "package fixture\ntable Save\n{\n    m map[uint16]int32 | min = -100, max = 50\n}\n", "Save",
			[]string{`{"m":{"1":75}}`}},
		{"arm range", "package fixture\nunion Pick\n{\n    a int32 | min = -100, max = 100\n    b int32\n}\ntable Save\n{\n    pick Pick\n}\n", "package fixture\nunion Pick\n{\n    a int32 | min = -100, max = 50\n    b int32\n}\ntable Save\n{\n    pick Pick\n}\n", "Save",
			[]string{`{"pick":{"a":75}}`}},
		{"int128 range", "package fixture\ntable Save\n{\n    x int128 | min = -100, max = 100\n}\n", "package fixture\ntable Save\n{\n    x int128 | min = -100, max = 50\n}\n", "Save",
			[]string{`{"x":75}`}},
		{"pointer retarget", "package fixture\ntable A\n{\n    x int32\n}\ntable B\n{\n    x int32\n}\ntable Save\n{\n    a *A\n    b *B\n}\n", "package fixture\ntable A\n{\n    x int32\n}\ntable B\n{\n    x int32\n}\ntable Save\n{\n    a *B\n    b *B\n    c *A\n}\n", "Save",
			[]string{`{"a":{"x":1}}`}},
		{"optional to plain", "package fixture\ntable Save\n{\n    x ?int32\n}\n", "package fixture\ntable Save\n{\n    x int32\n}\n", "Save",
			[]string{`{}`, `{"x":0}`, `{"x":5}`}},
		{"list elem string cap", "package fixture\ntable Save\n{\n    rules []string(8)\n}\n", "package fixture\ntable Save\n{\n    rules []string(2)\n}\n", "Save",
			[]string{`{"rules":["abcdef"]}`}},
		{"map string value cap", "package fixture\ntable Save\n{\n    m map[uint16]string(8)\n}\n", "package fixture\ntable Save\n{\n    m map[uint16]string(2)\n}\n", "Save",
			[]string{`{"m":{"1":"abcdef"}}`}},
		{"keyed string cap", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    m [Team]string(8)\n}\n", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    m [Team]string(2)\n}\n", "Save",
			[]string{`{"m":{"Red":"abcdef"}}`}},
		{"enum max", "package fixture\nenum Team | max = 300\n{\n    Red\n    Blue\n}\ntable Save\n{\n    t Team\n}\n", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    t Team\n}\n", "Save",
			[]string{`{"t":"Blue"}`}},
		{"timestamp to int64", "package fixture\ntable Save\n{\n    t timestamp\n}\n", "package fixture\ntable Save\n{\n    t int64\n}\n", "Save",
			[]string{`{"t":"2026-01-01T00:00:00Z"}`}},
		{"key keybound unbounded", "package fixture\ntable Save\n{\n    m map[string]int32\n}\n", "package fixture\ntable Save\n{\n    m map[string(2)]int32\n}\n", "Save",
			[]string{`{"m":{"abcdef":1}}`}},
		{"total added", "package fixture\ntable Save\n{\n    rules [..4]string(8)\n}\n", "package fixture\ntable Save\n{\n    rules [..4]string(8) | total = 3\n}\n", "Save",
			[]string{`{"rules":["ab","cd"]}`}},
		{"range added", "package fixture\ntable Save\n{\n    x int32\n}\n", "package fixture\ntable Save\n{\n    x int32 | min = 0, max = 50\n}\n", "Save",
			[]string{`{"x":75}`}},
		{"retired readded", "package fixture\ntable Save\n{\n    hp int32\n    n int32\n}\n", "package fixture\ntable Save\n{\n    hp int32\n    n int32\n}\n", "Save",
			[]string{`{"hp":75}`}},
		{"field min", "package fixture\ntable Save\n{\n    xs [..4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[]}`, `{"xs":[7]}`, `{"xs":[7,8]}`, `{"xs":[0]}`, `{"xs":[0,0]}`}},
		{"arm min", "package fixture\nunion Pick\n{\n    items [..4]int32\n    other int32\n}\ntable Save\n{\n    pick Pick\n}\n", "package fixture\nunion Pick\n{\n    items [2..4]int32\n    other int32\n}\ntable Save\n{\n    pick Pick\n}\n", "Save",
			[]string{`{"pick":{"items":[]}}`, `{"pick":{"items":[7]}}`, `{"pick":{"items":[7,8]}}`}},
		{"map min", "package fixture\ntable Save\n{\n    by map[uint16][..4]int32\n}\n", "package fixture\ntable Save\n{\n    by map[uint16][2..4]int32\n}\n", "Save",
			[]string{`{"by":{"1":[]}}`, `{"by":{"1":[7]}}`, `{"by":{"1":[7,8]}}`}},
		{"nested min", "package fixture\ntype Inner\n{\n    xs [..4]int32\n}\ntable Save\n{\n    inner Inner\n}\n", "package fixture\ntype Inner\n{\n    xs [2..4]int32\n}\ntable Save\n{\n    inner Inner\n}\n", "Save",
			[]string{`{"inner":{"xs":[7]}}`, `{"inner":{"xs":[7,8]}}`}},
		{"fixed to counted min", "package fixture\ntable Save\n{\n    xs [4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[7,0,0,0]}`, `{"xs":[7,8,9,1]}`}},
		{"list to counted min", "package fixture\ntable Save\n{\n    xs []int32\n}\n", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[7]}`, `{"xs":[7,8]}`}},
		{"counted min to fixed", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [4]int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[7,8]}`}},
		{"min lowered", "package fixture\ntable Save\n{\n    xs [2..4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [..4]int32\n}\n", "Save",
			[]string{`{}`, `{"xs":[7,8]}`, `{"xs":[0,0]}`}},
		{"pair to map", "package fixture\ntable Pair\n{\n    key uint16\n    value int32\n}\ntable Save\n{\n    items [..4]Pair\n}\n", "package fixture\ntable Save\n{\n    items map[uint16]int32\n}\n", "Save",
			[]string{`{"items":[{"key":1,"value":11},{"key":2,"value":22}]}`, `{"items":[{"key":2,"value":22},{"key":1,"value":11}]}`, `{"items":[{"key":1,"value":11},{"key":1,"value":22}]}`}},
		{"map to pair", "package fixture\ntable Save\n{\n    items map[uint16]int32\n}\n", "package fixture\ntable Pair\n{\n    key uint16\n    value int32\n}\ntable Save\n{\n    items [..4]Pair\n}\n", "Save",
			[]string{`{"items":{"1":11,"2":22}}`, `{"items":{"1":11,"2":22,"3":1,"4":1,"5":1}}`}},
		{"rekey", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    values [Team]uint8\n}\n", "package fixture\nenum Axis\n{\n    East\n    West\n}\ntable Save\n{\n    values [Axis]uint8\n}\n", "Save",
			[]string{`{"values":{"Red":11,"Blue":22}}`}},
		{"rekey both declared", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\nenum Axis\n{\n    East\n    West\n}\ntable Save\n{\n    values [Team]uint8\n    other Axis\n    t Team\n}\n", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\nenum Axis\n{\n    East\n    West\n}\ntable Save\n{\n    values [Axis]uint8\n    other Axis\n    t Team\n}\n", "Save",
			[]string{`{"values":{"Red":11,"Blue":22}}`}},
		{"keyed to positional", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    values [Team]uint8\n}\n", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    values [2]uint8\n    t Team\n}\n", "Save",
			[]string{`{"values":{"Red":11,"Blue":22}}`}},
		{"variant removed", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    t Team\n    n int32\n}\n", "package fixture\nenum Team\n{\n    Red\n}\ntable Save\n{\n    t Team\n    n int32\n}\n", "Save",
			[]string{`{"t":"Blue","n":5}`}},
		{"arm removed", "package fixture\nunion Pick\n{\n    a int32\n    b int32\n}\ntable Save\n{\n    pick Pick\n    n int32\n}\n", "package fixture\nunion Pick\n{\n    a int32\n}\ntable Save\n{\n    pick Pick\n    n int32\n}\n", "Save",
			[]string{`{"pick":{"b":5},"n":5}`}},
		{"kind changed", "package fixture\ntable Save\n{\n    x int32\n    n int32\n}\n", "package fixture\ntable Save\n{\n    x float32\n    n int32\n}\n", "Save",
			[]string{`{"x":5,"n":5}`}},
		{"kind narrowed", "package fixture\ntable Save\n{\n    x int32\n    n int32\n}\n", "package fixture\ntable Save\n{\n    x int16\n    n int32\n}\n", "Save",
			[]string{`{"x":5,"n":5}`}},
		{"elem changed", "package fixture\ntable Save\n{\n    xs [..4]int32\n}\n", "package fixture\ntable Save\n{\n    xs [..4]float32\n}\n", "Save",
			[]string{`{"xs":[5]}`}},
		{"default changed", "package fixture\ntable Save\n{\n    x int32 = 21\n}\n", "package fixture\ntable Save\n{\n    x int32 = 25\n}\n", "Save",
			[]string{`{"x":21}`, `{}`}},
		{"frac changed", "package fixture\ntable Save\n{\n    x fixed(16, 8)\n}\n", "package fixture\ntable Save\n{\n    x fixed(16, 4)\n}\n", "Save",
			[]string{`{"x":1.5}`}},
		{"required added", "package fixture\ntable Save\n{\n    x int32\n}\n", "package fixture\ntable Save\n{\n    x int32 | required\n}\n", "Save",
			[]string{`{}`, `{"x":5}`}},
		{"enum dropped to uint16", "package fixture\nenum Team\n{\n    Red\n    Blue\n}\ntable Save\n{\n    t Team\n}\n", "package fixture\ntable Save\n{\n    t uint16\n}\n", "Save",
			[]string{`{"t":"Blue"}`}},
		{"flags moved", "package fixture\nflags Perks\n{\n    A\n    B\n}\ntable Save\n{\n    p Perks\n}\n", "package fixture\nflags Perks\n{\n    B\n    A\n}\ntable Save\n{\n    p Perks\n}\n", "Save",
			[]string{`{"p":["A"]}`}},
		{"flags removed", "package fixture\nflags Perks\n{\n    A\n    B\n}\ntable Save\n{\n    p Perks\n}\n", "package fixture\nflags Perks\n{\n    A\n}\ntable Save\n{\n    p Perks\n}\n", "Save",
			[]string{`{"p":["B"]}`, `{"p":["A"]}`}},
		{"nested repoint", "package fixture\ntable A\n{\n    x int32\n    y int32\n}\ntable B\n{\n    x int32\n    z int32\n}\ntable Save\n{\n    a A\n    b B\n}\n", "package fixture\ntable A\n{\n    x int32\n    y int32\n}\ntable B\n{\n    x int32\n    z int32\n}\ntable Save\n{\n    a B\n    b B\n    c A\n}\n", "Save",
			[]string{`{"a":{"x":1,"y":2}}`}},
		{"union repoint", "package fixture\nunion P\n{\n    a int32\n    b int32\n}\nunion Q\n{\n    a int32\n    c int32\n}\ntable Save\n{\n    p P\n    q Q\n}\n", "package fixture\nunion P\n{\n    a int32\n    b int32\n}\nunion Q\n{\n    a int32\n    c int32\n}\ntable Save\n{\n    p Q\n    q Q\n    r P\n}\n", "Save",
			[]string{`{"p":{"b":2}}`}},
		{"enum repoint", "package fixture\nenum P\n{\n    A\n    B\n}\nenum Q\n{\n    A\n    C\n}\ntable Save\n{\n    p P\n    q Q\n}\n", "package fixture\nenum P\n{\n    A\n    B\n}\nenum Q\n{\n    A\n    C\n}\ntable Save\n{\n    p Q\n    q Q\n    r P\n}\n", "Save",
			[]string{`{"p":"B"}`}},
		{"was chain", "package fixture\ntable Save\n{\n    speed int32 | was = \"velocity\"\n}\n", "package fixture\ntable Save\n{\n    max_speed int32 | was = \"speed\"\n}\n", "Save",
			[]string{`{"speed":5}`}},
		{"bare rename", "package fixture\ntable Save\n{\n    speed int32\n}\n", "package fixture\ntable Save\n{\n    velocity int32\n}\n", "Save",
			[]string{`{"speed":5}`}},
		{"map key kind", "package fixture\ntable Save\n{\n    m map[uint16]int32\n}\n", "package fixture\ntable Save\n{\n    m map[string(8)]int32\n}\n", "Save",
			[]string{`{"m":{"1":5}}`}},
		{"blob type", "package fixture\ntable Save\n{\n    b *bytes\n}\n", "package fixture\ntable Save\n{\n    b *string\n}\n", "Save",
			[]string{`{"b":"aGk="}`}},
		{"table vanished", "package fixture\ntable A\n{\n    x int32\n}\ntable Save\n{\n    a *A\n    n int32\n}\n", "package fixture\ntable Save\n{\n    n int32\n}\n", "Save",
			[]string{`{"a":{"x":1},"n":2}`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("PANIC %v", r)
				}
			}()
			t.Logf("BASELINE:\n%s", baseline.Render(unit(t, c.before)).Text())
			got := baseline.Diff(committed(t, c.before), baseline.Render(unit(t, c.after)), baseline.DefaultTokenPolicy)
			t.Logf("FINDINGS:%s", summary(got))
			for _, j := range c.jsons {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Logf("  %s => PANIC %v", j, r)
						}
					}()
					r := readOldSave(t, c.before, c.after, c.table, j)
					t.Logf("  %s => ok=%t reason=%q unknown=%d widened=%d text=%s", j, r.OK, r.Reason, r.Report.Unknown, r.Report.Widened, strings.TrimSpace(r.Text))
				}()
			}
		})
	}
}

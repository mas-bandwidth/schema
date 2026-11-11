package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const mapLinearSchema = `package maplinear
table Keys {
 ints map[int64]int32
 names map[string(16)]int32
}
`

// mapLinearText spells a one-field map with n distinct keys.
func mapLinearText(field string, n int, stringKey bool) string {
	var b strings.Builder
	b.WriteString(`{"` + field + `":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if stringKey {
			fmt.Fprintf(&b, `"k%08d":1`, i)
		} else {
			fmt.Fprintf(&b, `"%d":1`, i)
		}
	}
	b.WriteString(`}}`)
	return b.String()
}

// mapLinearRepeated spells n distinct keys with key 0 repeated at the end.
func mapLinearRepeated(field string, n int, stringKey bool) string {
	text := mapLinearText(field, n, stringKey)
	key := "0"
	if stringKey {
		key = "k00000000"
	}
	return text[:len(text)-2] + `,"` + key + `":2}}`
}

// mapLinearProbe reads each input with a counter planted in the runtime's
// repeated-key test and prints the counter's final value and the verdict.
const mapLinearProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("maplinear/schema_module.nut", true);
local T = schema.require(%q);
local J = schema.require("schema_table_json");
::mapRepeatSteps <- 0;
foreach (key in %s) {
  ::mapRepeatSteps = 0;
  local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
  local v = J.FromJson(T, "Keys", inputs[key], r);
  print(key + " " + ::mapRepeatSteps + " " + (v == null ? "refused" : "read") + "\n");
}
`

// TestSquirrelJsonMapReadIsLinear pins the repeated-key test of the JSON read
// (docs/SPEC-TABLES.md §2.8, §16.2): its cost is linear in the number of keys,
// measured as counted steps at n and 2n keys with the counter planted in a
// copy of the emitted runtime, and a repeated key still refuses the whole
// read. The Go engine answers the same inputs first: distinct keys read, one
// repeated key refuses.
func TestSquirrelJsonMapReadIsLinear(t *testing.T) {
	t.Parallel()
	u := loadSource(t, mapLinearSchema)

	const n = 200
	cases := []struct {
		key       string
		field     string
		count     int
		stringKey bool
		repeated  bool
	}{
		{"intn", "ints", n, false, false},
		{"int2n", "ints", 2 * n, false, false},
		{"strn", "names", n, true, false},
		{"str2n", "names", 2 * n, true, false},
		{"intrep", "ints", n, false, true},
		{"strrep", "names", n, true, true},
	}
	inputs := map[string][]byte{}
	var order []string
	for _, c := range cases {
		var text string
		if c.repeated {
			text = mapLinearRepeated(c.field, c.count, c.stringKey)
		} else {
			text = mapLinearText(c.field, c.count, c.stringKey)
		}
		inputs[c.key] = []byte(text)
		order = append(order, c.key)
	}

	t.Run("go engine", func(t *testing.T) {
		m := tabletext.NewModel(u)
		root := m.Lookup("Keys")
		for _, c := range cases {
			var r tabletext.Report
			ok := m.Read(m.New(root), inputs[c.key], &r)
			if c.repeated {
				if ok || !r.Refused {
					t.Fatalf("%s: the Go engine read %s with ok=%t report=%+v, want refused", c.key, inputs[c.key], ok, r)
				}
				continue
			}
			if !ok || r.Refused || !r.Silent() {
				t.Fatalf("%s: the Go engine refused a map of distinct keys: %+v", c.key, r)
			}
		}
	})

	t.Run("squirrel", func(t *testing.T) {
		dir := t.TempDir()
		mod := emitUnit(t, dir, "maplinear", u)

		runtime := string(squirreltable.Runtime()["schema_table_json.nut"])
		const anchor = "      if (ent[kd.member] in seen) { r.bad = true; return false; }\n"
		if got := strings.Count(runtime, anchor); got != 1 {
			t.Fatalf("the repeat test's anchor appears %d times in the emitted runtime, want exactly once", got)
		}
		patched := strings.Replace(runtime, anchor,
			"      ::mapRepeatSteps = ::mapRepeatSteps + 1;\n"+anchor, 1)
		writeFile(t, filepath.Join(dir, "maplinear", "schema_table_json.nut"), []byte(patched))

		var keys []string
		for _, key := range order {
			keys = append(keys, fmt.Sprintf("%q", key))
		}
		writeFile(t, filepath.Join(dir, "main.nut"), []byte(fmt.Sprintf(mapLinearProbe, mod, "["+strings.Join(keys, ",")+"]")))
		got := map[string]struct {
			steps   int
			verdict string
		}{}
		for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
			f := strings.Fields(line)
			if len(f) != 3 {
				t.Fatalf("probe line %q is not a key, its steps and its verdict", line)
			}
			var steps int
			if _, err := fmt.Sscan(f[1], &steps); err != nil {
				t.Fatalf("probe line %q: %v", line, err)
			}
			got[f[0]] = struct {
				steps   int
				verdict string
			}{steps, f[2]}
		}

		for _, c := range cases {
			g, ok := got[c.key]
			if !ok {
				t.Fatalf("%s: the probe printed no row", c.key)
			}
			want := "read"
			if c.repeated {
				want = "refused"
			}
			if g.verdict != want {
				t.Fatalf("%s: the Squirrel read answered %s, want %s", c.key, g.verdict, want)
			}
		}

		for _, kind := range []struct{ small, large string }{
			{"intn", "int2n"},
			{"strn", "str2n"},
		} {
			small, large := got[kind.small].steps, got[kind.large].steps
			if small <= 0 {
				t.Fatalf("%s: the repeated-key test counted no steps", kind.small)
			}
			// under three: n to 2n is two when one key costs one step
			if large >= 3*small {
				t.Fatalf("%s to %s: the repeated-key test counted %d steps at n keys and %d at 2n, a ratio of %.2f, want under 3",
					kind.small, kind.large, small, large, float64(large)/float64(small))
			}
		}
	})
}

package goosetable

// A FLAGS BIT ABOVE THE DECLARED WIDTH IS REFUSED BY THE MESSAGE FORM
// (docs/SPEC-TABLES.md §3.3, "the write refuses it rather than drop it";
// docs/SPEC.md §5.1 row 9; docs/rulings.json
// message-flags-width-is-caller-error). A `flags` mask rides the message
// form at its declared width W, narrower than its u64 storage; a held bit at
// or above W is outside the message domain, and SaveMessages and
// MeasureMessages refuse it invalid_value rather than let msg_put mask it off.
// The lawful twin, every declared bit set, saves.
//
// Unit-test rules: no real time, no socket, no network; each run is `goose
// run` of a generated program in t.TempDir().

import (
	"os"
	"path/filepath"
	"testing"
)

const gooseMsgFlagsWidth = `package msgflagswidth

flags Caps { Read, Write, Exec }

fixed table Perm
{
    caps Caps
    list [2]Caps
}
`

func TestGooseMessageFormRefusesFlagsBitAboveWidth(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Perm.schema")
	if err := os.WriteFile(path, []byte(gooseMsgFlagsWidth), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatalf("load the flags schema: %v", err)
	}
	cases := []struct {
		name   string
		values []string
		refuse bool
	}{
		{"caps-bit-3", []string{".Caps = 8"}, true},
		{"caps-top-bit", []string{".Caps = 9223372036854775808"}, true},
		{"element-bit-3", []string{".List[1] = 12"}, true},
		{"every-declared-bit", []string{".Caps = 7", ".List[0] = 7", ".List[1] = 5"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "Perm", values: [][]string{c.values}})
			if c.refuse {
				if r.saveN != -1 || r.measureN != -1 || r.saveReason != "invalid_value" {
					t.Fatalf("%v: save %d measure %d %s, want -1 -1 invalid_value:\n%s", c.values, r.saveN, r.measureN, r.saveReason, r.raw)
				}
				return
			}
			if r.saveN < 0 || r.saveReason != "ok" {
				t.Fatalf("%v: the lawful mask was refused: save %d %s:\n%s", c.values, r.saveN, r.saveReason, r.raw)
			}
		})
	}
}

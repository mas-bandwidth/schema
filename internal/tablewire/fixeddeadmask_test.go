package tablewire_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedDeadBytes names exactly the dead bytes the read checks, and no live
// byte: the conformance harness holds relax/dead-bytes-read-<lang> to that
// mask, so a byte it names wrongly would widen the relaxation.
func TestFixedDeadBytesMaskIsTheDeadBytes(t *testing.T) {
	t.Parallel()
	m := listModel(t, deadBytesUnit)
	st := m.Lookup("Root")
	at := func(name string) int { return deadFieldAt(t, st, name) }
	inst := place(t, m, "Root", `{"list": [5], "name": "ab", "pick": {"small": 3}, "inners": [{"n": 1}], "keep": 9}`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	body := len(wire) - int(ir.TableFixedTypeBytes(st))
	mask := tablewire.FixedDeadBytes(m, m.New(st), wire)
	if mask == nil {
		t.Fatal("the mask refused a lawful record")
	}
	for _, r := range []struct {
		name string
		at   int
		dead bool
	}{
		{"list's count", at("list"), false},
		{"list's live slot", at("list") + 4, false},
		{"list's slot past the count", at("list") + 4 + 4, true},
		{"maybe's present byte", at("maybe"), false},
		{"maybe's absent payload", at("maybe") + 1, true},
		{"name's used length", at("name"), false},
		{"name's live text", at("name") + 4 + 1, false},
		{"name past its used length", at("name") + 4 + 2, true},
		{"blob past an empty length", at("blob") + 4, true},
		{"pick's tag", at("pick"), false},
		{"pick's small arm", at("pick") + 1, false},
		{"pick past the narrower arm", at("pick") + 1 + 1, true},
		{"inners' live n", at("inners") + 4, false},
		{"an absent optional inside a live slot", at("inners") + 4 + 5, true},
		{"keep", at("keep"), false},
	} {
		if mask[body+r.at] != r.dead {
			t.Errorf("%s (body offset %d): dead=%v, want %v", r.name, r.at, mask[body+r.at], r.dead)
		}
	}
	for i := 0; i < body; i++ {
		if mask[i] {
			t.Fatalf("header byte %d named dead", i)
		}
	}
	stained := append([]byte(nil), wire...)
	stained[body+at("maybe")+1] = 0x04
	if tablewire.FixedDeadBytes(m, m.New(st), stained) != nil {
		t.Fatal("the mask answered for a record the read refuses")
	}
}

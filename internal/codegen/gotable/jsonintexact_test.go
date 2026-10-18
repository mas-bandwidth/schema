package gotable

// AN INTEGER FIELD IS READ EXACTLY, NEVER THROUGH FLOAT64 (docs/SPEC-TABLES.md
// §16.2, Numbers). This is the generated walk's half: 9007199254740993.0 lands
// as itself rather than the mantissa's 9007199254740992, 1e2 lands 100, a
// genuinely fractional token is one kind_mismatch with the field unchanged, and
// -0.0 lands 0. The outer test also reads the same texts with the tool's engine
// (internal/tabletext) and requires the two to land the same wire bytes and the
// same counters.

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const intExactSchema = `package probe
table Root {
    n int64
}
`

const intExactProbe = `package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestIntExactProbe(t *testing.T) {
	cases := []struct {
		token          string
		want           int64
		mismatch, clamp int32
	}{
		{"9007199254740993.0", 9007199254740993, 0, 0},
		{"1e2", 100, 0, 0},
		{"1.0000000000000001", 0, 1, 0},
		{"1.5", 0, 1, 0},
		{"-0.0", 0, 0, 0},
	}
	for _, c := range cases {
		var b RootBuilder
		if !b.Init() {
			t.Fatal("init")
		}
		var r TableReport
		if !RootFromJsonTolerant(&b, []byte("{\"n\":"+c.token+"}"), &r) {
			t.Fatalf("%s: read failed: %+v", c.token, r)
		}
		if !b.Lock() {
			t.Fatal("lock")
		}
		root := b.AsConst()
		if root.N != c.want {
			t.Fatalf("%s: n=%d, want %d (report %+v)", c.token, root.N, c.want, r)
		}
		if r.KindMismatch != c.mismatch || r.Clamped != c.clamp || r.Malformed || r.Unknown != 0 || r.Duplicate != 0 {
			t.Fatalf("%s: report %+v, want kind_mismatch=%d clamped=%d", c.token, r, c.mismatch, c.clamp)
		}
		wire := make([]byte, RootMeasure(root))
		if RootSave(root, wire) != int64(len(wire)) {
			t.Fatal("save")
		}
		fmt.Printf("INTEXACT\t%s\t%s\t%d\t%d\n", c.token, hex.EncodeToString(wire), r.KindMismatch, r.Clamped)
		b.Shutdown()
	}
}
`

func TestJsonIntegerFieldReadsExactly(t *testing.T) {
	t.Parallel()
	out, err := runGeneratedResult(t, intExactSchema, intExactProbe, "-v", "-run", "^TestIntExactProbe$")
	if err != nil {
		t.Fatalf("generated walk: %v\n%s", err, out)
	}
	u := unitFrom(t, intExactSchema)
	m := tabletext.NewModel(u)
	st := m.Lookup("Root")
	seen := 0
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || fields[0] != "INTEXACT" {
			continue
		}
		token := fields[1]
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, []byte(`{"n":`+token+`}`), &r) {
			t.Fatalf("%s: engine read returned false: %+v", token, r)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: engine wire: %v", token, err)
		}
		if got := hex.EncodeToString(wire); fields[2] != got {
			t.Errorf("%s: generated wire %s, engine wire %s", token, fields[2], got)
		}
		if got := fmt.Sprint(r.KindMismatch); fields[3] != got {
			t.Errorf("%s: generated kind_mismatch %s, engine %s", token, fields[3], got)
		}
		if got := fmt.Sprint(r.Clamped); fields[4] != got {
			t.Errorf("%s: generated clamped %s, engine %s", token, fields[4], got)
		}
		seen++
	}
	if seen != 5 {
		t.Fatalf("the generated probe reported %d cases, want 5\n%s", seen, out)
	}
}

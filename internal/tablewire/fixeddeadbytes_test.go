package tablewire_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero; docs/SPEC-TABLES.md
// §3.4; docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3 step 11). The dead bytes are a
// counted array's slots past its count, an absent optional's payload, a
// string(N)'s or bytes(N)'s bytes past its used length, and an inactive union
// arm: the bytes under tag 0 and the bytes past a narrower taken arm. Each row
// builds a lawful image, stains one dead byte, and asserts the read refuses
// wire_damaged; the unstained twin reads.
const deadBytesUnit = `package deadpin

fixed table Inner
{
    n   int32
    opt ?int16
}

union Pick
{
    small int8
    big   int64
}

fixed table Root
{
    list   [..3]int32
    maybe  ?int32
    name   string(6)
    blob   bytes(5)
    pick   Pick
    inners [..2]Inner
    keep   int32
}
`

// deadFieldAt is the offset of a field of Root inside one record's body.
func deadFieldAt(t *testing.T, st *ir.Struct, name string) int {
	t.Helper()
	off := int64(0)
	for _, f := range st.Fields {
		if f.Name == name {
			return int(off)
		}
		off += ir.TableFixedFieldBytes(f)
	}
	t.Fatalf("Root has no field %s", name)
	return 0
}

func TestFixedDeadBytesRefused(t *testing.T) {
	t.Parallel()
	m := listModel(t, deadBytesUnit)
	st := m.Lookup("Root")
	at := func(name string) int { return deadFieldAt(t, st, name) }
	// one Inner: n (4 bytes), then opt's present byte and its two payload bytes
	const inner = 4 + 1 + 2
	rows := []struct {
		name  string
		value string
		dead  int // offset of the stained byte inside the record body
	}{
		{"counted array, count zero, first slot", `{}`, at("list") + 4},
		{"counted array, count zero, last slot", `{}`, at("list") + 4 + 3*4 - 1},
		{"counted array, slot past a count of one", `{"list": [5]}`, at("list") + 4 + 4},
		{"absent optional's payload", `{}`, at("maybe") + 1},
		{"absent optional's last payload byte", `{}`, at("maybe") + 4},
		{"string past its used length", `{"name": "ab"}`, at("name") + 4 + 2},
		{"empty string's capacity", `{}`, at("name") + 4},
		{"bytes past an empty length", `{}`, at("blob") + 4 + 4},
		{"union under tag 0", `{}`, at("pick") + 1},
		{"union past a narrower taken arm", `{"pick": {"small": 3}}`, at("pick") + 1 + 1},
		{"union past a narrower arm, last byte", `{"pick": {"small": 3}}`, at("pick") + 1 + 7},
		{"absent optional inside a live slot", `{"inners": [{"n": 1}]}`, at("inners") + 4 + 5},
		{"a struct slot past the count", `{"inners": [{"n": 1}]}`, at("inners") + 4 + inner},
		{"a struct slot, count zero", `{}`, at("inners") + 4 + inner + 4},
	}
	for _, r := range rows {
		inst := place(t, m, "Root", r.value)
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: encode: %v", r.name, err)
		}
		body := len(wire) - int(ir.TableFixedTypeBytes(st))
		if wire[body+r.dead] != 0 {
			t.Fatalf("%s: the writer left byte %d at %#x, want a dead zero", r.name, r.dead, wire[body+r.dead])
		}
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(st), wire, &report); !ok || err != nil {
			t.Errorf("%s: the all-zero twin: ok=%v err=%v report=%+v, want it read", r.name, ok, err, report)
			continue
		}
		stained := append([]byte(nil), wire...)
		stained[body+r.dead] = 0x04
		report = tabletext.Report{}
		ok, err := tablewire.Decode(m, m.New(st), stained, &report)
		if ok || !tablewire.Refused(err) || !report.Refused || report.Reason != "wire_damaged" {
			t.Errorf("%s: a non-zero dead byte at %d: ok=%v err=%v report=%+v, want the wire_damaged refusal", r.name, r.dead, ok, err, report)
		}
	}
	// THE SAME RULE THROUGH THE LOCK: an older writer's record is checked
	// against the writer's own layout, so a stained byte past its used text
	// length refuses.
	t.Run("through the lock", deadBytesThroughTheLock)
}

func deadBytesThroughTheLock(t *testing.T) {
	old, now := lockedPair(t)
	wire := lineageWrite(t, old, `{"speed": 7, "name": "ab"}`)
	// name follows speed's two bytes: its four-byte length, then "ab", then dead
	rec := len(wire) - int(ir.TableFixedTypeBytes(old.Unit.Tables["Cfg"]))
	dead := rec + 2 + 4 + 2
	var report tabletext.Report
	if ok, err := tablewire.Decode(now, now.New(now.Lookup("Cfg")), wire, &report); !ok || err != nil {
		t.Fatalf("the all-zero twin through the lock: ok=%v err=%v report=%+v", ok, err, report)
	}
	if wire[dead] != 0 {
		t.Fatalf("the writer left %#x in a dead byte", wire[dead])
	}
	wire[dead] = 0x04
	report = tabletext.Report{}
	ok, err := tablewire.Decode(now, now.New(now.Lookup("Cfg")), wire, &report)
	if ok || !tablewire.Refused(err) || !report.Refused || report.Reason != "wire_damaged" {
		t.Fatalf("a non-zero dead byte through the lock: ok=%v err=%v report=%+v, want the wire_damaged refusal", ok, err, report)
	}
}

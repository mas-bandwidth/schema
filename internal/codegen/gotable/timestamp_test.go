package gotable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// timestampSchema is the FIXED class: a fixed table carries the variable wire's
// entry points (Measure, Save, Load), the fixed form's (FixedSave, FixedLoad)
// and the text form's, in one unit. timestampVariableSchema is the VARIABLE
// class, which a unit generates regionally and reaches through a builder
// (docs/USAGE.md, "Tables from Go").
const timestampSchema = `package probe
fixed table Session
{
    n         int32
    started   timestamp
    ended     timestamp
}
`

const timestampVariableSchema = `package probe
table Event
{
    id        uint32
    opened_at timestamp
    closed_at timestamp
}
`

// TestTimestampKindInGo pins the `timestamp` kind in Go (SPEC.md §4.13,
// docs/SPEC-TABLES.md §2.11, §3 kind 36, §3.4, §16.2): an int64 count of
// nanoseconds since the Unix epoch, 0 the zero time, time.Time in Go and UTC on
// read, an RFC 3339 string in the text form. Every value below is a constant —
// no clock is read. The generated unit is compiled and run in a t.TempDir() by
// runGenerated, the package's house pattern.
//
// At BASE the schema does not parse (`timestamp` is an undefined type), so the
// test fails there on the very first line; at head it runs the table below.
func TestTimestampKindInGo(t *testing.T) {
	t.Parallel()

	// THE UNIT READS AND GENERATES: at BASE `timestamp` is an undefined type and
	// unitFrom stops here. These checks need no toolchain, so they run in every
	// leg; the round trips below compile and run the generated unit and so need
	// SCHEMA_SLOW=1, which the make legs set.
	u := unitFrom(t, timestampSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var tableGo, jsonGo string
	for name, data := range files {
		switch {
		case strings.HasSuffix(name, "TableJson.go"):
			jsonGo = string(data)
		case strings.HasSuffix(name, "Table.go"):
			tableGo += string(data)
		}
	}
	for _, want := range []string{
		"func (v *Session) StartedTime() time.Time",           // the Go value is a time.Time, UTC on read
		"func (v *Session) SetStartedTime(t time.Time) error", // and the write refuses what UnixNano cannot count
		"func SessionFixedLoad(",
		"Kind: 36", // the descriptor names the timestamp kind
	} {
		if !strings.Contains(tableGo, want) {
			t.Errorf("the generated tables lack %q", want)
		}
	}
	for _, want := range []string{"func tableJsonWriteTimestamp(", "func tableJsonReadTimestamp(", "time.RFC3339Nano", "timestamp_not_rfc3339", "type TableTimestampError struct", "func tableTimestampNanos(t time.Time) (int64, bool)"} {
		if !strings.Contains(jsonGo, want) {
			t.Errorf("the generated text form lacks %q", want)
		}
	}

	// THE KIND IS ITS OWN, AND NOTHING WIDENS INTO IT (§3, §4)
	var started *ir.Field
	for _, st := range u.Files[0].Tables {
		if st.Name == "Session" {
			started = st.Fields[1]
		}
	}
	if started == nil || ir.TableScalarKind(started) != 36 {
		t.Fatalf("Session.started did not resolve to a timestamp: %+v", started)
	}
	if started.Type.Width != 64 || !started.Type.Signed {
		t.Errorf("a timestamp is a signed 64 bit count, got width %d signed %t", started.Type.Width, started.Type.Signed)
	}
	for _, pair := range [][2]int{{ir.TableKindI64, 36}, {36, ir.TableKindI64}, {ir.TableKindI32, 36}, {ir.TableKindU64, 36}} {
		if ir.TableKindWidens(pair[0], pair[1]) {
			t.Errorf("kind %d widens into kind %d", pair[0], pair[1])
		}
	}
	// and the FIXED layout names the slot as the int64's own, since its kind set
	// is the closed one of docs/FIXED-FORM-ALGORITHM.md §1
	for _, e := range ir.TableFixedWalkRoot(u.Files[0].Tables[0]) {
		if e.Note == "started" && (e.Kind != ir.TableKindI64 || e.Size != 8) {
			t.Errorf("the fixed layout entry for a timestamp is kind %d size %d, want 5 and 8", e.Kind, e.Size)
		}
	}

	t.Run("the fixed class: variable wire, fixed form and text", func(t *testing.T) {
		runGenerated(t, timestampSchema, `package probe
import ("bytes";"errors";"math";"strings";"testing";"time")

// the zero time, both ends of UnixNano's range, a nanosecond count, one and a
// pre-epoch one: every value a constant, no clock read
var tsCases = []struct{ name string; n int64 }{
 {"zero", 0},
 {"min", math.MinInt64},
 {"max", math.MaxInt64},
 {"nanosecond", 1760531645123456789},
 {"one", 1},
 {"before the epoch", -1},
}

// the variable wire (form 1) of a fixed table: kind 36, eight bytes, UTC on read
func TestVariableWire(t *testing.T) {
 for _, c := range tsCases {
  v := Session{N: 7, Started: c.n, Ended: -c.n / 2}
  b := make([]byte, SessionMeasure(&v))
  if SessionSave(&v, b) != int64(len(b)) { t.Fatalf("%s: save", c.name) }
  var got Session
  var r TableReport
  if !SessionLoad(&got, b, &r) || r != (TableReport{}) { t.Fatalf("%s: load %+v", c.name, r) }
  if got != v { t.Fatalf("%s: %+v vs %+v", c.name, got, v) }
  if got.StartedTime().UnixNano() != c.n || got.EndedTime().UnixNano() != -c.n/2 { t.Fatalf("%s: %v %v", c.name, got.StartedTime(), got.EndedTime()) }
  if got.StartedTime().Location() != time.UTC { t.Fatalf("%s: not UTC: %v", c.name, got.StartedTime().Location()) }
 }
}

// a fresh value is the zero time and costs no byte, Go's own zero Time{} stores
// the zero time, and the count 0 reads back as the epoch
func TestZeroTimeIsAbsent(t *testing.T) {
 var fresh Session
 SessionReset(&fresh)
 gozero := Session{Started: 99, Ended: 99}
 if err := gozero.SetStartedTime(time.Time{}); err != nil { t.Fatal(err) }
 if err := gozero.SetEndedTime(time.Time{}); err != nil { t.Fatal(err) }
 a := make([]byte, SessionMeasure(&fresh))
 b := make([]byte, SessionMeasure(&gozero))
 SessionSave(&fresh, a); SessionSave(&gozero, b)
 if !bytes.Equal(a, b) { t.Fatalf("zero time rides: %x vs %x", a, b) }
 if len(a) >= 16 { t.Fatalf("two zero timestamps cost %d bytes", len(a)) }
 var v Session
 var r TableReport
 if !SessionLoad(&v, a, &r) || !v.StartedTime().Equal(time.Unix(0, 0)) || v.StartedTime().UnixNano() != 0 { t.Fatalf("zero time reads as %v", v.StartedTime()) }
}

// a time outside Time.UnixNano's domain has no count: the setter REFUSES it,
// by name, stores nothing and wraps nothing; both ends of the range are legal
func TestOutOfRangeWriteRefuses(t *testing.T) {
 for _, bad := range []time.Time{
  time.Date(1677, 9, 21, 0, 12, 43, 145224191, time.UTC), // one nanosecond below the minimum
  time.Date(2262, 4, 11, 23, 47, 16, 854775808, time.UTC), // one nanosecond above the maximum
  time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC),
  time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC),
 } {
  v := Session{Ended: 41}
  err := v.SetEndedTime(bad)
  var named TableTimestampError
  if !errors.As(err, &named) || named.Field != "Session.Ended" { t.Fatalf("%v: refusal %v", bad, err) }
  if !strings.HasPrefix(err.Error(), "timestamp_out_of_range: Session.Ended ") { t.Fatalf("%v: unnamed %q", bad, err) }
  if v.Ended != 41 { t.Fatalf("%v: the field moved to %d", bad, v.Ended) }
 }
 for _, ok := range []struct{ n int64; at time.Time }{
  {math.MinInt64, time.Unix(0, math.MinInt64)},
  {math.MaxInt64, time.Unix(0, math.MaxInt64)},
 } {
  var v Session
  if err := v.SetEndedTime(ok.at); err != nil || v.Ended != ok.n { t.Fatalf("%d refused: %v", ok.n, err) }
 }
}

func TestFixedForm(t *testing.T) {
 for _, c := range tsCases {
  in := []Session{{N: -3, Started: c.n, Ended: math.MaxInt64 - c.n}, {N: 4}}
  buf := make([]byte, SessionFixedMeasure(int64(len(in))))
  if SessionFixedSave(in, buf) != int64(len(buf)) { t.Fatalf("%s: save", c.name) }
  out := make([]Session, 2)
  plan := make([]TableFixedEntry, SessionFixedPlan.Count)
  var r TableReport
  if SessionFixedLoad(out, buf, plan, &r) != 2 || r != (TableReport{}) { t.Fatalf("%s: load %+v", c.name, r) }
  if out[0] != in[0] || out[1] != in[1] { t.Fatalf("%s: %+v vs %+v", c.name, out, in) }
  if out[0].StartedTime().UnixNano() != c.n || out[0].StartedTime().Location() != time.UTC { t.Fatalf("%s: %v", c.name, out[0].StartedTime()) }
 }
 // the slot is the int64's own: the record is the n, then eight bytes, then eight
 if SessionFixedBodyBytes != 4+8+8 { t.Fatalf("record body %d", SessionFixedBodyBytes) }
}

func TestJson(t *testing.T) {
 for _, c := range tsCases {
  v := Session{Started: c.n}
  text := make([]byte, SessionToJsonMeasure(&v))
  if SessionToJson(&v, text) != int64(len(text)) { t.Fatalf("%s: write", c.name) }
  var got Session
  var r TableReport
  if !SessionFromJson(&got, text, &r) || r != (TableReport{}) || got != v { t.Fatalf("%s: read %q %+v %+v", c.name, text, r, got) }
 }
 // the spelling: RFC 3339 with nanoseconds and a Z, the zero time as the epoch
 v := Session{Started: 1760531645123456789}
 text := make([]byte, SessionToJsonMeasure(&v))
 SessionToJson(&v, text)
 if !strings.Contains(string(text), "\"started\": \"2025-10-15T12:34:05.123456789Z\"") { t.Fatalf("text: %s", text) }
 if !strings.Contains(string(text), "\"ended\": \"1970-01-01T00:00:00Z\"") { t.Fatalf("zero text: %s", text) }
 // an offset spelling of the same instant lands as the same count
 var got Session
 var r TableReport
 if !SessionFromJson(&got, []byte("{\"started\":\"1970-01-01T01:00:00.000000001+01:00\"}"), &r) || got.Started != 1 { t.Fatalf("offset spelling: %d %+v", got.Started, r) }
}

func TestJsonRefusesByName(t *testing.T) {
 for _, bad := range []string{
  "{\"started\":\"yesterday\"}",
  "{\"started\":\"2025-10-15\"}",
  "{\"started\":\"2025-10-15T12:34:05\"}",
  "{\"started\":\"\"}",
  "{\"started\":\"1500-01-01T00:00:00Z\"}", // parses, but names no count
 } {
  got := Session{N: 9}
  var r TableReport
  if SessionFromJson(&got, []byte(bad), &r) { t.Fatalf("%s accepted", bad) }
  if !r.Malformed || !strings.HasPrefix(r.Reason, "timestamp_not_rfc3339: started") { t.Fatalf("%s: unnamed refusal %+v", bad, r) }
 }
 // a number token is the wrong JSON type: skipped and counted, never coerced
 var got Session
 var r TableReport
 if !SessionFromJson(&got, []byte("{\"started\":1760531645123456789}"), &r) || r.KindMismatch != 1 || got.Started != 0 { t.Fatalf("number: %+v %d", r, got.Started) }
}

// the kind is its own on the variable wire: an int64 count under kind 5 is a
// kind mismatch for a timestamp field, counted and never reinterpreted.
// (The other direction, a kind-36 field met by a reader that does not carry the
// kind, needs the shared skip width of gotable/wire.go and the byte oracle of
// internal/tablewire to learn 36 together: both treat it as damage, identically,
// until the card that owns them lands that; docs/SPEC-TABLES.md §2.11.)
func TestKindIsItsOwn(t *testing.T) {
 v := Session{Started: 1760531645123456789}
 b := make([]byte, SessionMeasure(&v))
 SessionSave(&v, b)
 // the field is [reference 1][kind 36][eight bytes], a fixed-width kind carrying
 // no length: respell the kind as i64
 i := bytes.Index(b, []byte{1, 36})
 if i < 0 { t.Fatalf("no kind 36 in %x", b) }
 b[i+1] = 5
 var got Session
 var r TableReport
 SessionLoad(&got, b, &r)
 if r.KindMismatch != 1 || got.Started != 0 { t.Fatalf("int64 under a timestamp field: %+v %d", r, got.Started) }
}
`)
	})
	t.Run("the variable class", func(t *testing.T) {
		runGenerated(t, timestampVariableSchema, `package probe
import ("math";"testing";"time")

// the VARIABLE class, through a builder and its arena: the wire is the same
// kind 36, the Go value the same time.Time
func TestVariableClass(t *testing.T) {
 for _, n := range []int64{0, math.MinInt64, math.MaxInt64, 1760531645123456789, -1} {
  var source EventBuilder
  if !source.Init() { t.Fatal("builder") }
  defer source.Shutdown()
  root := source.GetRoot()
  root.Id = 7
  if err := root.SetOpenedAtTime(time.Unix(0, n)); err != nil { t.Fatal(err) }
  root.ClosedAt = -n / 2
  size, err := EventMeasureReason(root, &source.Arena)
  if err != nil { t.Fatal(err) }
  wire := make([]byte, size)
  if EventSave(root, wire, &source.Arena) != size { t.Fatal("save") }
  var loaded EventBuilder
  if !loaded.Init() { t.Fatal("builder") }
  defer loaded.Shutdown()
  var report TableReport
  if !EventLoadBuilder(&loaded, wire, &report) || report != (TableReport{}) { t.Fatalf("load %+v", report) }
  if !loaded.Lock() { t.Fatal("lock") }
  got := loaded.AsConst()
  if got.OpenedAt != n || got.ClosedAt != -n/2 || got.Id != 7 { t.Fatalf("%d: %+v", n, *got) }
  if got.OpenedAtTime().UnixNano() != n || got.OpenedAtTime().Location() != time.UTC { t.Fatalf("%d: %v", n, got.OpenedAtTime()) }
 }
}
`)
	})
}

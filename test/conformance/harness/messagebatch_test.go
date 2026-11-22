package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// batchCapacityLegs is every leg's package, each holding its own copy of the
// capacity fixture under its testdata (docs/SPEC-TABLES.md §3.3: the capacity
// rows "run in the leg's own tests; the shared suites assert verdicts only,
// never a caller's capacity").
var batchCapacityLegs = []string{
	"ctable", "cpptable", "cstable", "unitytable", "darttable", "elixirtable",
	"gotable", "goosetable", "javatable", "jstable", "lobstertable", "luatable",
	"luautable", "mootable", "pythontable", "rubytable", "rusttable",
	"squirreltable",
}

// BatchCapacityFixture is the file name of the capacity fixture in each leg's
// testdata: two LoginRequest bodies of the backenddemo unit, each with
// client_build 40, written by the Go engine under testdata/wire/tables/
// backend_conn.bin. A leg's own test loads it into storage for ONE body whose
// client_build is the sentinel 17 and owes a count of 0 with the sentinel
// untouched ("A refused batch returns a count of `0`", §3.3; "refuses before
// it decodes anything"), then into storage for two and owes 2 and 40.
const BatchCapacityFixture = "batch_capacity_two.bin"

// batchCapacityWire is the fixture as the Go engine writes it.
func batchCapacityWire(t *testing.T, model *tabletext.Model) []byte {
	t.Helper()
	login := model.Lookup("LoginRequest")
	pair := []*tabletext.Instance{model.New(login), model.New(login)}
	for _, inst := range pair {
		setU64(t, inst, "client_build", 40)
	}
	wire, err := tablewire.EncodeMessages(model, pair)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg pins every leg's copy of the
// capacity fixture to the Go engine's bytes (SCHEMA_UPDATE_WIRE_GOLDENS=1
// re-pins them), and holds the engine itself to the fixture's answers: storage
// for one refuses with a count of 0 and the sentinel untouched, storage for
// two delivers both. Red if a copy drifts from the engine, or if the engine
// hands back the wire's count or decodes a body before the capacity refusal.
func TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg(t *testing.T) {
	_, model, vocabulary := backend(t)
	built := batchCapacityWire(t, model)
	for _, leg := range batchCapacityLegs {
		path := filepath.Join("internal", "codegen", leg, "testdata", BatchCapacityFixture)
		if got := pinnedWire(t, path, built); !bytes.Equal(got, built) {
			t.Errorf("%s holds %x, and the Go engine writes %x: re-pin with SCHEMA_UPDATE_WIRE_GOLDENS=1", path, got, built)
		}
	}
	login := model.Lookup("LoginRequest")
	one := []*tabletext.Instance{model.New(login)}
	setU64(t, one[0], "client_build", 17)
	var report tabletext.Report
	count, ok, err := tablewire.DecodeMessages(model, one, built, vocabulary, &report)
	if ok || err == nil || !report.Refused {
		t.Fatalf("two bodies into storage for one: ok=%v err=%v report=%+v, want a refusal", ok, err, report)
	}
	if count != 0 {
		t.Errorf("two bodies into storage for one: the returned count reads %d, and a refused batch returns 0 (§3.3)", count)
	}
	if got := instU64(t, one[0], "client_build"); got != 17 {
		t.Errorf("two bodies into storage for one: client_build reads %d, not the sentinel 17: the refusal decoded a body", got)
	}
	two := []*tabletext.Instance{model.New(login), model.New(login)}
	report = tabletext.Report{}
	count, ok, err = tablewire.DecodeMessages(model, two, built, vocabulary, &report)
	if !ok || err != nil || count != 2 {
		t.Fatalf("two bodies into storage for two: count=%d ok=%v err=%v", count, ok, err)
	}
	for i, inst := range two {
		if got := instU64(t, inst, "client_build"); got != 40 {
			t.Errorf("two bodies into storage for two: body %d client_build reads %d, not 40", i, got)
		}
	}
}

// TestMessageBatchVerdictScoresTheCountOnly holds the scorer of the shared
// `message-batch` rows to the ruling: a refused row owes `count 0` and a
// delivered row the wire's count, and nothing else is read. Red if a refusal
// that hands back the wire's count (the defect of F-178 and F-179), or -1,
// passes, or if a count of 0 fails.
func TestMessageBatchVerdictScoresTheCountOnly(t *testing.T) {
	wire := []byte{2, 1, 0xff}
	refused := &MessageBatch{Name: "r", Verdict: "refused"}
	delivered := &MessageBatch{Name: "d", Verdict: "delivered"}
	for _, c := range []struct {
		b     *MessageBatch
		count string
		pass  bool
	}{
		{refused, "0", true},
		{refused, "2", false},
		{refused, "-1", false},
		{refused, "256", false},
		{delivered, "2", true},
		{delivered, "0", false},
		{delivered, "1", false},
	} {
		why := messageBatchVerdict(c.b, c.count, wire)
		if (why == "") != c.pass {
			t.Errorf("%s with count %s: pass=%v (%q), want pass=%v", c.b.Verdict, c.count, why == "", why, c.pass)
		}
	}
	for _, c := range []struct {
		answer string
		ok     bool
	}{
		{"count 0\n", true},
		{"count 2", true},
		{"count 0\nstorage 00\n", false},
		{"0\n", false},
	} {
		_, err := parseMessageBatchAnswer([]byte(c.answer))
		if (err == nil) != c.ok {
			t.Errorf("%q: parse error %v, want ok=%v", c.answer, err, c.ok)
		}
	}
}

package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

// planReadCSV builds one round's CSV the way a plan-read runner prints it.
func planReadCSV(id string, rows ...string) []byte {
	var b strings.Builder
	b.WriteString(header + "\n")
	for _, r := range rows {
		b.WriteString(strings.ReplaceAll(r, "ID", id) + "\n")
	}
	return []byte(b.String())
}

const (
	identityRow = "rust,bench_fixed_read,read,6400,1264.3,1,7000000,7000000,7000000,8000.00,0.00,ID,table,crate,contract,O3,unknown"
	packetRow   = "rust,bench_mixed_read,read,64000,400,1,5000000,5000000,5000000,2000.00,0.00,ID,gen,crate,removed,O3,unknown"
	planRow     = "rust,bench_plan_read,read,6400,1100,1,6000000,6000000,6000000,6000.00,0.00,ID,table,crate,contract,O3,unknown"
)

var planReadCounts = map[string]int64{"table": 6400, "packet": 64000}

// The parser accepts exactly the plan-read arm's rows: the identity and
// packet rows both present, the plan row optional, each with its own family
// and checks axis and its own wire's iteration count, one corpus id for all.
func TestPlanReadRowsAcceptTheArmsRows(t *testing.T) {
	t.Parallel()
	rows, err := parsePlanReadRows(planReadCSV("abc", identityRow, packetRow), "rust", "abc", planReadCounts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows["bench_fixed_read"].rate != 7000000 || rows["bench_mixed_read"].bytes != 400 {
		t.Fatalf("rows = %+v", rows)
	}
	rows, err = parsePlanReadRows(planReadCSV("abc", identityRow, planRow, packetRow), "rust", "abc", planReadCounts)
	if err != nil || len(rows) != 3 {
		t.Fatalf("with the plan row: %v %d", err, len(rows))
	}
}

// Every way a row can fail to belong to this sitting is a refusal, never a
// skipped row: the comparand missing, a different corpus, a derived or other
// path, a family or checks axis that is not the row's, a count not asked for,
// a duplicate, and a per-round row that is not one run.
func TestPlanReadRowsRefuseRowsThatDoNotBelong(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		"no packet comparand":   planReadCSV("abc", identityRow),
		"no identity lane":      planReadCSV("abc", packetRow),
		"other corpus":          planReadCSV("abc", identityRow, strings.Replace(packetRow, "ID", "zzz", 1)),
		"derived path":          planReadCSV("abc", strings.Replace(identityRow, ",read,", ",round_trip,", 1), packetRow),
		"packet as table":       planReadCSV("abc", identityRow, strings.Replace(packetRow, ",gen,", ",table,", 1)),
		"wrong checks":          planReadCSV("abc", identityRow, strings.Replace(packetRow, ",removed,", ",contract,", 1)),
		"table count on packet": planReadCSV("abc", identityRow, strings.Replace(packetRow, ",64000,", ",6400,", 1)),
		"duplicate":             planReadCSV("abc", identityRow, identityRow, packetRow),
		"seven runs":            planReadCSV("abc", strings.Replace(identityRow, ",1,7000000", ",7,7000000", 1), packetRow),
		"unknown bench":         planReadCSV("abc", identityRow, packetRow, strings.Replace(planRow, "bench_plan_read", "bench_fixed", 1)),
		"other language":        planReadCSV("abc", strings.Replace(identityRow, "rust,", "cpp,", 1), packetRow),
	}
	for name, data := range cases {
		if _, err := parsePlanReadRows(data, "rust", "abc", planReadCounts); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func stat(spread float64, best float64) planReadStat {
	return planReadStat{BestNs: best, MedianNs: best, WorstNs: best, Spread: spread}
}

// The GATE is faster-or-FAIL on best against best, and a row §2.3 invalidates
// yields no verdict rather than one it cannot support.
func TestGateVerdictIsFasterOrFail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lane, packet planReadStat
		want         string
	}{
		{stat(1, 140), stat(1, 200), "PASS"},
		{stat(1, 200), stat(1, 200), "FAIL"}, // equal is not faster
		{stat(1, 250), stat(1, 200), "FAIL"},
		{stat(30, 140), stat(1, 200), "PASS"}, // noisy still judges
		{stat(41, 140), stat(1, 200), "INVALID"},
		{stat(1, 140), stat(41, 200), "INVALID"},
	}
	for _, c := range cases {
		if got := gateVerdict(c.lane, c.packet); got != c.want {
			t.Errorf("gate(%v, %v) = %s, want %s", c.lane, c.packet, got, c.want)
		}
	}
}

// One leg's statistic over the rounds: best is the fastest round, median
// beside it, spread over the rates; a lane with no rows is NAMED with its
// reason and its GATE says NOT MEASURED, never PASS.
func TestPlanReadLegNamesAnAbsentPlanLane(t *testing.T) {
	t.Parallel()
	round := func(identity, packet float64) map[string]sample {
		return map[string]sample{
			"bench_fixed_read": {rate: identity, bytes: 1264.3},
			"bench_mixed_read": {rate: packet, bytes: 400},
		}
	}
	leg, err := buildPlanReadLeg("rust", []map[string]sample{round(5e6, 4e6), round(5.5e6, 4.2e6), round(5.2e6, 4.1e6)})
	if err != nil {
		t.Fatal(err)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9*math.Abs(b) }
	if !near(leg.Identity.BestNs, 1e9/5.5e6) || !near(leg.Packet.BestNs, 1e9/4.2e6) || !near(leg.Identity.MedianNs, 1e9/5.2e6) || !near(leg.Identity.WorstNs, 1e9/5e6) {
		t.Fatalf("identity %+v packet %+v", leg.Identity, leg.Packet)
	}
	if want := (5.5e6 - 5e6) / 5.2e6 * 100; !near(leg.Identity.Spread, want) {
		t.Fatalf("spread %v, want %v", leg.Identity.Spread, want)
	}
	if leg.GateIdentity != "PASS" || leg.GatePlan != "NOT MEASURED" || leg.Plan != nil || leg.PlanAbsent == "" {
		t.Fatalf("verdicts %+v", leg)
	}
	missing := round(5e6, 4e6)
	delete(missing, "bench_mixed_read")
	if _, err := buildPlanReadLeg("rust", []map[string]sample{round(5e6, 4e6), missing}); err == nil {
		t.Fatal("a row missing from one round was accepted")
	}
}

// The window is OK only with no foreign work and every row at normal spread.
func TestWindowVerdict(t *testing.T) {
	t.Parallel()
	quiet := []planReadLeg{{Language: "rust", Identity: stat(2, 1), Packet: stat(3, 1)}}
	if got := windowVerdict(nil, quiet); got != "OK" {
		t.Fatalf("quiet = %s", got)
	}
	if got := windowVerdict([]string{"foreign"}, quiet); !strings.HasPrefix(got, "NOISY") {
		t.Fatalf("foreign = %s", got)
	}
	noisy := []planReadLeg{{Language: "rust", Identity: stat(20, 1), Packet: stat(3, 1)}}
	if got := windowVerdict(nil, noisy); !strings.Contains(got, "identity read noisy") {
		t.Fatalf("noisy = %s", got)
	}
}

// The plan-read id is §1.6's hash over the files the invocation loads (the
// same function corpusID uses): adding the packet pair to the table pass's
// five files makes it a different id, so a plan-read row never divides
// against a row of the ordinary table pass.
func TestPlanReadCorpusIDIsItsOwn(t *testing.T) {
	t.Parallel()
	contents := func(path string) ([]byte, error) { return []byte(path), nil }
	all := planReadCorpusFiles()
	tableOnly := map[string]string{}
	for name, path := range all {
		if !strings.HasPrefix(name, "bench_mixed") {
			tableOnly[name] = path
		}
	}
	a, err := fnvCorpusID(all, contents)
	if err != nil {
		t.Fatal(err)
	}
	b, err := fnvCorpusID(tableOnly, contents)
	if err != nil || a == b {
		t.Fatalf("ids %s %s %v", a, b, err)
	}
	if _, err := fnvCorpusID(all, func(string) ([]byte, error) { return nil, errors.New("gone") }); err == nil {
		t.Fatal("an unreadable corpus file produced an id")
	}
}

// The board prints the standard's one line per leg, both GATE columns, the
// plan lane named, and every leg not in the sitting named with its reason.
func TestPlanReadBoardShape(t *testing.T) {
	t.Parallel()
	leg := planReadLeg{Language: "rust", Identity: stat(2, 141.9), Packet: stat(3, 203.5), GateIdentity: "PASS", GatePlan: "NOT MEASURED", PlanAbsent: planLaneAbsent}
	b := planReadBoard{Host: "h", Rounds: 7, CorpusID: "abc", Counts: planReadCounts, Window: "OK", Legs: []planReadLeg{leg}}
	for _, lang := range nineLanguages {
		if lang != "rust" {
			b.Absent = append(b.Absent, skipped{Language: lang, Reason: "no plan-read arm in this leg's runner on this tree"})
		}
	}
	text := b.markdown()
	for _, want := range []string{"leg      identity read    plan read    packet read    GATE identity  GATE plan", "141.9", "203.5", "PASS", "NOT MEASURED", "corpus_id(identity) abc", "window: OK", fmt.Sprintf("%-8s", "cpp")} {
		if !strings.Contains(text, want) {
			t.Errorf("board lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "no plan-read arm in this leg's runner on this tree") != len(nineLanguages)-1 {
		t.Errorf("absent legs not each named:\n%s", text)
	}
}

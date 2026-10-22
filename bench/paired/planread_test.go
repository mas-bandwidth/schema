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

// The parser accepts exactly the plan-read arm's rows: the identity, plan and
// packet rows, each with its own family and checks axis and its own wire's
// iteration count, one corpus id for all. The plan row is optional only for a
// leg that reads no lineage corpus; rust reads one, so its absence refuses.
func TestPlanReadRowsAcceptTheArmsRows(t *testing.T) {
	t.Parallel()
	rows, err := parsePlanReadRows(planReadCSV("abc", identityRow, planRow, packetRow), "rust", "abc", planReadCounts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows["bench_fixed_read"].rate != 7000000 || rows["bench_plan_read"].bytes != 1100 || rows["bench_mixed_read"].bytes != 400 {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := parsePlanReadRows(planReadCSV("abc", identityRow, packetRow), "rust", "abc", planReadCounts); err == nil || !strings.Contains(err.Error(), "bench_plan_read") {
		t.Fatalf("a lineage leg with no plan row was accepted: %v", err)
	}
	if !contains(planReadLineageLegs, "rust") {
		t.Fatal("rust reads the lineage corpus and must be held to its plan row")
	}
}

// Every way a row can fail to belong to this sitting is a refusal, never a
// skipped row: the comparand missing, a different corpus, a derived or other
// path, a family or checks axis that is not the row's, a count not asked for,
// a duplicate, and a per-round row that is not one run.
func TestPlanReadRowsRefuseRowsThatDoNotBelong(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		"no packet comparand":   planReadCSV("abc", identityRow, planRow),
		"no identity lane":      planReadCSV("abc", planRow, packetRow),
		"no plan lane":          planReadCSV("abc", identityRow, packetRow),
		"other corpus":          planReadCSV("abc", identityRow, planRow, strings.Replace(packetRow, "ID", "zzz", 1)),
		"plan of other corpus":  planReadCSV("abc", identityRow, strings.Replace(planRow, "ID", "zzz", 1), packetRow),
		"derived path":          planReadCSV("abc", strings.Replace(identityRow, ",read,", ",round_trip,", 1), planRow, packetRow),
		"packet as table":       planReadCSV("abc", identityRow, planRow, strings.Replace(packetRow, ",gen,", ",table,", 1)),
		"plan as packet":        planReadCSV("abc", identityRow, strings.Replace(planRow, ",table,", ",gen,", 1), packetRow),
		"wrong checks":          planReadCSV("abc", identityRow, planRow, strings.Replace(packetRow, ",removed,", ",contract,", 1)),
		"table count on packet": planReadCSV("abc", identityRow, planRow, strings.Replace(packetRow, ",64000,", ",6400,", 1)),
		"packet count on plan":  planReadCSV("abc", identityRow, strings.Replace(planRow, ",6400,", ",64000,", 1), packetRow),
		"duplicate":             planReadCSV("abc", identityRow, planRow, planRow, packetRow),
		"seven runs":            planReadCSV("abc", strings.Replace(identityRow, ",1,7000000", ",7,7000000", 1), planRow, packetRow),
		"unknown bench":         planReadCSV("abc", identityRow, planRow, packetRow, strings.Replace(planRow, "bench_plan_read", "bench_fixed", 1)),
		"other language":        planReadCSV("abc", strings.Replace(identityRow, "rust,", "cpp,", 1), planRow, packetRow),
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

// A MEASURED plan lane is judged on its own against the same packet read:
// faster is PASS, not faster is FAIL with both numbers standing, and the
// plan/identity ratio is carried for the board to report, never to judge.
func TestPlanReadLegJudgesAMeasuredPlanLane(t *testing.T) {
	t.Parallel()
	round := func(identity, plan, packet float64) map[string]sample {
		return map[string]sample{
			"bench_fixed_read": {rate: identity, bytes: 1264.3},
			"bench_plan_read":  {rate: plan, bytes: 995.4},
			"bench_mixed_read": {rate: packet, bytes: 437},
		}
	}
	leg, err := buildPlanReadLeg("rust", []map[string]sample{round(7e6, 6e6, 5e6), round(7.1e6, 6.1e6, 5.1e6)})
	if err != nil {
		t.Fatal(err)
	}
	if leg.Plan == nil || leg.GatePlan != "PASS" || leg.GateIdentity != "PASS" || leg.PlanAbsent != "" || leg.Plan.Bytes != 995.4 {
		t.Fatalf("faster plan lane: %+v", leg)
	}
	slow, err := buildPlanReadLeg("rust", []map[string]sample{round(7e6, 4e6, 5e6), round(7.1e6, 4.1e6, 5.1e6)})
	if err != nil {
		t.Fatal(err)
	}
	if slow.GatePlan != "FAIL" || slow.GateIdentity != "PASS" || slow.Plan == nil {
		t.Fatalf("slower plan lane must FAIL on its own: %+v", slow)
	}
	text := planReadBoard{Rounds: 2, CorpusID: "abc", Counts: planReadCounts, Window: "OK", Lane: "taskset:3", Legs: []planReadLeg{slow}}.markdown()
	for _, want := range []string{"FAIL", fmt.Sprintf("%.1f", slow.Plan.BestNs), fmt.Sprintf("%.1f", slow.Packet.BestNs), fmt.Sprintf("%.2f", slow.Plan.BestNs/slow.Identity.BestNs)} {
		if !strings.Contains(text, want) {
			t.Errorf("a FAIL board lacks %q:\n%s", want, text)
		}
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
	// The lineage corpus is in the id, the LOCK with the bytes: a lock edit
	// changes which plan ran, so it must change the id too.
	for _, name := range lineageCorpusFiles {
		if all[name] != rustLineageDir+"/"+name {
			t.Fatalf("plan-read id does not cover %s: %v", name, all)
		}
	}
	edited := func(path string) ([]byte, error) {
		if path == rustLineageDir+"/schema.lock" {
			return []byte("edited"), nil
		}
		return []byte(path), nil
	}
	if c, err := fnvCorpusID(all, edited); err != nil || c == a {
		t.Fatalf("a lock edit left the id at %s (%v)", c, err)
	}
}

// The plan binary's package is build wiring: the TRACKED runner source by
// absolute path, the lineage feature on by default, the lineage-aware unit
// under the runner's own crate name and the VOLD generation beside it.
func TestRustPlanManifest(t *testing.T) {
	t.Parallel()
	m := rustPlanManifest("/repo/bench/tables/rust/src/main.rs")
	for _, want := range []string{
		`name = "table-rust-plan"`,
		`path = "/repo/bench/tables/rust/src/main.rs"`,
		`default = ["lineage"]`,
		`benchpaired = { package = "benchlineage", path = "../new" }`,
		`benchvold = { path = "../old" }`,
		`path = "../../serialize.rs"`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest lacks %q:\n%s", want, m)
		}
	}
	c := rustCrateManifest("benchvold", "x")
	if !strings.Contains(c, `name = "benchvold"`) || !strings.Contains(c, `path = "lib.rs"`) {
		t.Errorf("crate manifest:\n%s", c)
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
	for _, want := range []string{"leg      identity read    plan read    packet read    GATE identity  GATE plan", "141.9", "203.5", "PASS", "NOT MEASURED", "corpus_id(identity) abc", "corpus_id(plan) — (plan lane not measured)", "lane none", "window: OK", fmt.Sprintf("%-8s", "cpp")} {
		if !strings.Contains(text, want) {
			t.Errorf("board lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "no plan-read arm in this leg's runner on this tree") != len(nineLanguages)-1 {
		t.Errorf("absent legs not each named:\n%s", text)
	}

	// With the plan lane measured, its corpus id is the SAME invocation's, the
	// caption names the record size of all three files (§5.3 rule 2), and the
	// pin is recorded.
	plan := stat(2, 158.7)
	plan.Bytes, leg.Identity.Bytes, leg.Packet.Bytes = 995.4, 1264.3, 437
	leg.Plan, leg.GatePlan, leg.PlanAbsent = &plan, "PASS", ""
	b.Legs, b.Lane = []planReadLeg{leg}, "taskset:3"
	text = b.markdown()
	for _, want := range []string{"corpus_id(plan) abc (one invocation", "fixed 1264.3 B/record (identity), 995.4 B/record (plan", "packet 437 B/record", "lane taskset:3", "158.7", "1.12"} {
		if !strings.Contains(text, want) {
			t.Errorf("measured board lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "NOT MEASURED") {
		t.Errorf("a measured plan lane printed NOT MEASURED:\n%s", text)
	}
}

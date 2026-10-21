package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// THE READ ARM'S RULES, held without a machine: no clock, no process, no file.

func readArmCSV(rows ...string) []byte {
	return []byte(header + "\n" + strings.Join(rows, "\n") + "\n")
}

// rate is records per second for ns per record, as the runner prints it.
func readArmRowLine(bench, family, checks, id string, iters int64, bytes, ns float64) string {
	rate := 1e9 / ns
	return fmt.Sprintf("cpp,%s,read,%d,%g,1,%.0f,%.0f,%.0f,1.00,0.00,%s,%s,hdr,%s,O3,unknown", bench, iters, bytes, rate, rate, rate, id, family, checks)
}

func TestReadArmParsesExactlyItsTwoMeasuredRows(t *testing.T) {
	t.Parallel()
	const id = "0123456789abcdef"
	const n = int64(4000000)
	fixed := readArmRowLine("bench_fixed_read", "table", "contract", id, n, 1264.3, 200)
	packet := readArmRowLine("bench_mixed_read", "gen", "removed", id, n, 438, 150)
	f, p, err := parseReadArm(readArmCSV(fixed, packet), id, n)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(f.Ns-200) > 0.01 || math.Abs(p.Ns-150) > 0.01 || f.Bytes != 1264.3 || p.Bytes != 438 {
		t.Fatalf("parsed %+v %+v", f, p)
	}
	refusals := map[string][]byte{
		"packet row only":          readArmCSV(packet),
		"duplicate row":            readArmCSV(fixed, fixed, packet),
		"another corpus":           readArmCSV(readArmRowLine("bench_fixed_read", "table", "contract", "fedcba9876543210", n, 1264.3, 200), packet),
		"packet row with checks":   readArmCSV(fixed, readArmRowLine("bench_mixed_read", "gen", "contract", id, n, 438, 150)),
		"table row as family gen":  readArmCSV(readArmRowLine("bench_fixed_read", "gen", "contract", id, n, 1264.3, 200), packet),
		"another iteration count":  readArmCSV(readArmRowLine("bench_fixed_read", "table", "contract", id, n/2, 1264.3, 200), packet),
		"a sample under the floor": readArmCSV(readArmRowLine("bench_fixed_read", "table", "contract", id, 64000, 1264.3, 200), readArmRowLine("bench_mixed_read", "gen", "removed", id, 64000, 438, 150)),
		"a derived read":           readArmCSV(strings.Replace(fixed, ",read,", ",round_trip,", 1), packet),
	}
	for name, data := range refusals {
		iters := n
		if name == "a sample under the floor" {
			iters = 64000
		}
		if _, _, err := parseReadArm(data, id, iters); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTheGateIsEachLaneAgainstTheSameLegsPacketReadBestAgainstBest(t *testing.T) {
	t.Parallel()
	quick := statsOf([]float64{100, 101, 102})
	slow := statsOf([]float64{150, 151, 152})
	if got := faster(quick, slow, true); got != "PASS" {
		t.Errorf("faster lane: %s", got)
	}
	if got := faster(slow, quick, true); got != "FAIL" {
		t.Errorf("slower lane: %s", got)
	}
	if got := faster(quick, quick, true); got != "FAIL" {
		t.Errorf("a tie is not faster: %s", got)
	}
	if got := faster(quick, slow, false); !strings.Contains(got, "window INVALID") {
		t.Errorf("an INVALID window must carry no verdict: %s", got)
	}
	noisy := statsOf([]float64{100, 120, 125})
	if got := faster(noisy, slow, true); got != "PASS (noisy)" {
		t.Errorf("a noisy row keeps its mark: %s", got)
	}
	invalid := statsOf([]float64{100, 120, 200})
	if got := faster(invalid, slow, true); !strings.Contains(got, "spread") {
		t.Errorf("an invalid spread must carry no verdict: %s", got)
	}
}

func TestSpreadIsOverRatesAndBestIsTheFastestRound(t *testing.T) {
	t.Parallel()
	s := statsOf([]float64{200, 100, 125})
	if s.BestNs != 100 || s.WorstNs != 200 || s.MedianNs != 125 {
		t.Fatalf("%+v", s)
	}
	// rates 10, 8, 5 (per µs): (10 − 5) / 8 = 62.5%
	if math.Abs(s.SpreadPct-62.5) > 1e-9 || spreadMark(s) != "invalid" {
		t.Fatalf("spread %.3f mark %q", s.SpreadPct, spreadMark(s))
	}
}

const anchorReport = `hand layout == emitter, 64 records, 1244 bytes each
plan entries (identity, root): 12
record bytes: 1244  (8 hash + 1236 body); packet 438

READ   A plan-driven (identity)      %s ns/record
READ   A3 shipped loop, runtime plan    60.0 ns/record
READ   A2 the harness's interpreter    70.0 ns/record
READ   B straight-line                %s ns/record
       ratio A/B                      1.250x
`

func TestTheAnchorNeedsItsLayoutGateItsTwoArmsAndTheFloor(t *testing.T) {
	t.Parallel()
	got, err := parseAnchor([]byte(fmt.Sprintf(anchorReport, "50.0", "40.0")), 100000)
	if err != nil || got.IdentityNs != 50 || got.StraightLineNs != 40 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := parseAnchor([]byte(strings.Replace(fmt.Sprintf(anchorReport, "50.0", "40.0"), "hand layout == emitter", "hand layout != emitter", 1)), 100000); err == nil {
		t.Error("a reference whose own gate did not print was accepted")
	}
	if _, err := parseAnchor([]byte(fmt.Sprintf(anchorReport, "50.0", "garbage")), 100000); err == nil {
		t.Error("a reference with no arm B was accepted")
	}
	if _, err := parseAnchor([]byte(fmt.Sprintf(anchorReport, "50.0", "40.0")), 1000); err == nil {
		t.Error("a sample under the 200ms floor was accepted")
	}
}

func TestTheAnchorIsBestOverBestAgainstOnePointFive(t *testing.T) {
	t.Parallel()
	pass := anchorOf([]anchorSample{{50, 40}, {48, 41}, {52, 39}}, "", true)
	if pass.Verdict != "PASS" || math.Abs(pass.Ratio-48.0/39.0) > 1e-12 || len(pass.RoundRatios) != 3 {
		t.Fatalf("%+v", pass)
	}
	fail := anchorOf([]anchorSample{{80, 40}, {82, 41}}, "", true)
	if fail.Verdict != "FAIL" {
		t.Fatalf("2.0x passed: %+v", fail)
	}
	refused := anchorOf(nil, "does not compile: x", true)
	if refused.Verdict != "— (refused)" || refused.Ratio != 0 {
		t.Fatalf("a refused anchor printed a ratio: %+v", refused)
	}
	if invalid := anchorOf([]anchorSample{{50, 40}}, "", false); strings.Contains(invalid.Verdict, "PASS") {
		t.Fatalf("an INVALID window passed: %+v", invalid)
	}
}

func TestThePlanLaneNamesWhatTheTreeLacks(t *testing.T) {
	t.Parallel()
	none := planLaneRefusal(func(string) bool { return false })
	for _, p := range lineageCorpus {
		if !strings.Contains(none, p) {
			t.Errorf("refusal does not name %s: %s", p, none)
		}
	}
	all := planLaneRefusal(func(string) bool { return true })
	if strings.Contains(all, "absent") || !strings.Contains(all, "no plan lane") {
		t.Errorf("with the corpus present the refusal is the runner's: %s", all)
	}
}

func TestTheReadArmAcceptsOnlyTheBuildsOwnRegeneratedFiles(t *testing.T) {
	t.Parallel()
	got, err := dirtyPaths(" M generated/bench/paired/cpp/BenchTable.h\n")
	if err != nil || len(got) != 1 || got[0] != "generated/bench/paired/cpp/BenchTable.h" {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := dirtyPaths(""); err != nil || len(got) != 0 {
		t.Fatalf("clean: %v %v", got, err)
	}
	for _, status := range []string{" M bench/paired/main.go\n", "?? generated/stray.h\n", " M generated/x.h\n M README.md\n"} {
		if _, err := dirtyPaths(status); err == nil {
			t.Errorf("accepted %q", status)
		}
	}
}

func TestTheReadArmBoardHasTheStandardsShape(t *testing.T) {
	t.Parallel()
	b := readArmBoard{
		Host: "bench", OS: "linux", Arch: "amd64", CPU: "cpu", Revision: "abc", Serialize: "def", Rounds: 7, Iterations: 4000000,
		AnchorReps: anchorReps, CorpusID: "0123456789abcdef", PlanRefusal: planLaneRefusal(func(string) bool { return false }),
		Window: "OK", FixedBytes: 1264.3, PacketBytes: 438,
		Identity: statsOf([]float64{200, 201}), Packet: statsOf([]float64{150, 151}), GatePlan: "— (absent)",
		Anchor: anchorOf(nil, "does not compile: test/bench/fixedform_measure.cpp:1: error: x", true), Absent: otherLegs(),
	}
	b.GateIdentity = faster(b.Identity, b.Packet, b.windowOK())
	text := b.markdown()
	for _, want := range []string{
		"# fixed form: the identity lane, the plan lane, and the same leg's packet wire",
		"# corpus_id(identity) 0123456789abcdef / corpus_id(plan) — (no lineage corpus on this tree",
		"# bytes_per_op: fixed 1264.3 B/record, packet 438 B/record",
		"# checks: table=contract, packet C/C++/C#=removed, packet Go=always",
		"# C++ anchor: — test/bench/fixedform_measure.cpp does not compile",
		"# window: OK",
		"leg      identity read    plan read    packet read    GATE identity  GATE plan    plan/identity",
		"FAIL",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("board lacks %q:\n%s", want, text)
		}
	}
	for _, lang := range nineLanguages {
		if lang == "cpp" {
			continue
		}
		if !strings.Contains(text, "\n"+lang+" ") {
			t.Errorf("board does not name the %s leg", lang)
		}
	}
	if strings.Contains(text, "ratio") && strings.Contains(text, "= 0.00x") {
		t.Error("a refused anchor printed a ratio")
	}
	b.Window, b.WindowReason = "INVALID", "competing process"
	b.GateIdentity = faster(b.Identity, b.Packet, b.windowOK())
	invalid := b.markdown()
	if strings.Contains(invalid, " FAIL ") || !strings.Contains(invalid, "The window is INVALID") {
		t.Errorf("an INVALID window printed a verdict:\n%s", invalid)
	}
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A committed results file as the driver writes one: the §5.2 preamble stamps
// the ledger reads (date, host/arch, and — for a PASS — §2.6's window verdict)
// and the CSV rows. `window` empty writes no verdict line at all, which is what
// a sitting looks like: no control legs, nothing certified.
func writeResults(t *testing.T, dir, name, date, host, arch, window string, rows ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# schema bench results\n")
	b.WriteString("# date: " + date + "\n")
	b.WriteString("# host: " + host + "  arch: " + arch + "  os: Darwin 25.6.0\n")
	b.WriteString("# rounds: 7   interleaved: yes\n")
	if window != "" {
		b.WriteString("# control_delta_pct: 1.0   window: " + window + "\n")
	}
	b.WriteString("# corpus_id: 6b213fbfa1a03a99\n")
	b.WriteString("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n")
	b.WriteString(strings.Join(rows, "\n") + "\n")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rt is one family-gen bench_mixed round_trip row at a given best rate and
// spread — the only row the lock and the gate read.
func rt(lang string, maxRate int, spread string) string {
	return lang + ",bench_mixed,round_trip,4000000,100," +
		"14,4000000," + strconv.Itoa(maxRate) + "," + strconv.Itoa(maxRate) + ",1800.00," + spread +
		",6b213fbfa1a03a99,gen,hdr,removed,O3,unknown"
}

// The pair lock, green side: a PASS whose c sits inside §2.8's band is the
// normal committed state and must not red. 4,400,000 vs 4,500,000 msg/s is
// c at 102.3% of cpp, inside the 3.0-point floor.
func TestPairLockPassInsideTheBandIsGreen(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "in-band-pass.csv", "2026-09-07T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 0 {
		t.Fatalf("the lock red a pass inside the band (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "LEDGER") {
		t.Errorf("a pass inside the band printed a verdict at all:\n%s", errOut)
	}
	if !strings.Contains(errOut, "pair lock: in-band-pass.csv 102.3% against 3.0 points: inside") {
		t.Errorf("the lock did not name the pass it holds:\n%s", errOut)
	}
}

// The pair lock, red side: the axis's newest certified PASS, whose c leaves
// the band, goes RED, and the message names the CSV, the percentage, the band,
// both spreads, the axis it is the newest pass on, and the finding line.
// 4,200,000 vs 4,500,000 is c at 107.1% of cpp against a 3.0-point band.
func TestPairLockPassOutsideTheBandIsRed(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "out-of-band-pass.csv", "2026-09-07T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4200000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 1 {
		t.Fatalf("a pass outside the band did not red (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	want := "LEDGER RED: c measured 107.1% of cpp in out-of-band-pass.csv, outside the §2.8 tie band of 3.0 points (spread c 1.00 + spread cpp 1.00, floor 3.0) — the pass certifies its window (§2.6) and is the newest certified pass that publishes a figure on [arm64 studio 6b213fbfa1a03a99], so this is where the pair stands now: a separation beyond the combined spread is a finding to investigate, not a number to publish"
	if !strings.Contains(errOut, want) {
		t.Errorf("the pair lock's message is not the one the record promises\nwant: %s\ngot:  %s", want, errOut)
	}
}

// The band is the two spreads summed, not the floor, once the spreads clear
// it: the same 107.1% separation sits INSIDE a band built from two 4-point
// spreads, so a noisy pass does not red on noise.
func TestPairLockBandWidensWithTheSpreads(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "noisy-pass.csv", "2026-09-07T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "4.00"), rt("c", 4200000, "4.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 0 {
		t.Fatalf("the lock red a separation inside the summed spreads (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
}

// THE LOCK IS ABOUT THE PAIR'S CURRENT STATE. An older certified pass on the
// axis is outside the band, the newest is inside: GREEN, because the older
// pass is the record of a finding that the newer one closed — which is exactly
// what happened on the Studio between the serialize-c-1.10.0 pin pass (c at
// 107.4% of cpp, the 7% read-path gap) and the c-read-guard pass that closed
// it at 98.2%. The older pass still PRINTS, as history, so the closed finding
// stays readable in the same output that shows the pass which closed it. The
// time axis is silent here by construction: c gets faster (238.1 -> 227.3
// ns/msg) and cpp does not move.
func TestPairLockGatesOnlyTheNewestCertifiedPassOnTheAxis(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-finding-pass.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4200000, "1.00"))
	writeResults(t, dir, "b-fix-pass.csv", "2026-09-02T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 0 {
		t.Fatalf("a historical pass whose finding a later pass closed still red the tree (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "LEDGER RED") {
		t.Errorf("the lock red on a pass it no longer holds:\n%s", errOut)
	}
	want := "pair history: a-finding-pass.csv 107.1% against 3.0 points: outside"
	if !strings.Contains(errOut, want) {
		t.Errorf("the closed finding is not in the output as history\nwant: %s\ngot:  %s", want, errOut)
	}
	// And the output names the pass the lock DOES hold, green though it is:
	// history lines are only readable next to the pass under the gate.
	held := "pair lock: b-fix-pass.csv 102.3% against 3.0 points: inside — the newest certified pass that publishes a figure on [arm64 studio 6b213fbfa1a03a99], and the one the lock holds"
	if !strings.Contains(errOut, held) {
		t.Errorf("the output does not name the pass the lock holds\nwant: %s\ngot:  %s", held, errOut)
	}
}

// The other side of the same rule: when the NEWEST certified pass on the axis
// is the one outside the band, that is a regression from parity and it reds
// the moment it is committed — with the older, inside pass printed as history
// so the output shows what the pair moved away from. cpp moves too (222.2 ->
// 200.0 ns/msg) and c improves in absolute terms (227.3 -> 217.4), so nothing
// here is the time axis's doing: this is the pair separating.
func TestPairLockRedsWhenTheNewestCertifiedPassLeavesTheBand(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-parity-pass.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	writeResults(t, dir, "b-regressed-pass.csv", "2026-09-02T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 5000000, "1.00"), rt("c", 4600000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 1 {
		t.Fatalf("the newest pass left the band and the lock did not red (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"LEDGER RED: c measured 108.7% of cpp in b-regressed-pass.csv",
		"outside the §2.8 tie band of 3.0 points",
		"is the newest certified pass that publishes a figure on [arm64 studio 6b213fbfa1a03a99]",
		"pair history: a-parity-pass.csv 102.3% against 3.0 points: inside",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the red on the newest pass lacks %q:\n%s", want, errOut)
		}
	}
}

// A NEWER pass §2.3 refuses does not disarm the axis. A leg whose spread is
// over the INVALID line publishes no figure, so that pass cannot be the figure
// the lock holds — the newest pass that DOES publish one stays under the gate
// and its separation still reds. Without this, one noisy sitting-shaped pass
// committed after a regression would silently unlock the axis. Both files
// carry cpp at the same rate and c faster in the newer one, so the time axis
// says nothing here: this is the pair lock alone.
func TestANewerPassRefusedByRuleTwoThreeDoesNotDisarmTheAxis(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-separated-pass.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4200000, "1.00"))
	writeResults(t, dir, "b-noisy-pass.csv", "2026-09-02T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "41.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 1 {
		t.Fatalf("a §2.3-refused newer pass disarmed the axis (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"LEDGER RED: c measured 107.1% of cpp in a-separated-pass.csv",
		"outside the §2.8 tie band of 3.0 points",
		"is the newest certified pass that publishes a figure on [arm64 studio 6b213fbfa1a03a99]",
		"ledger: pair-lock skipped — b-noisy-pass.csv",
		"a leg's spread is over §2.3's 40% INVALID line (spread c 41.00, spread cpp 1.00)",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the output lacks %q:\n%s", want, errOut)
		}
	}
	// The refused pass yields no verdict at all — not a red, not history.
	if strings.Contains(errOut, "b-noisy-pass.csv 97") || strings.Contains(errOut, "pair history: b-noisy-pass.csv") {
		t.Errorf("the refused pass published a figure anyway:\n%s", errOut)
	}
}

// A SITTING outside the band warns and exits 0. A sitting has no control legs
// and no window verdict, so §2.6 does not let a ratio publish from it — the
// separation is printed as the finding it is, not enforced as a verdict.
func TestPairLockSittingOutsideTheBandWarnsAndExitsZero(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "out-of-band-sitting.csv", "2026-09-07T10:00:00Z", "studio", "arm64", "",
		rt("cpp", 4500000, "1.00"), rt("c", 4200000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 0 {
		t.Fatalf("a sitting outside the band red instead of warning (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"LEDGER WARN: c measured 107.1% of cpp in out-of-band-sitting.csv",
		"outside the §2.8 tie band of 3.0 points (spread c 1.00 + spread cpp 1.00, floor 3.0)",
		"a sitting has no control legs and no window verdict, so it warns rather than reds (§2.6)",
		"a separation beyond the combined spread is a finding to investigate, not a number to publish",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the sitting warning lacks %q:\n%s", want, errOut)
		}
	}
}

// A pass whose window is stamped INVALID locks nothing: §2.6 already refuses
// to publish ratios from it, so gating on it would gate on evidence the
// standard threw away.
func TestPairLockDoesNotLockAnInvalidWindow(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "invalid-window-pass.csv", "2026-09-07T10:00:00Z", "studio", "arm64", "INVALID",
		rt("cpp", 4500000, "1.00"), rt("c", 4200000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 0 {
		t.Fatalf("an INVALID window locked (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "pair-lock skipped") || !strings.Contains(errOut, "window: INVALID") {
		t.Errorf("the skip did not name itself and its reason:\n%s", errOut)
	}
}

// The time axis, C leg (the 2026-09-07 lock): a C regression beyond the noise
// gate on the same machine goes RED, exactly as the cpp leg's has since #194.
// Both files carry both legs, because the axis is built from both-leg files
// only; cpp holds its rate, so only the c curve moves.
func TestCLegRegressionOnTheSameMachineIsRed(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-before.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	writeResults(t, dir, "b-after.csv", "2026-09-02T10:00:00Z", "studio", "arm64", "",
		rt("cpp", 4500000, "1.00"), rt("c", 3000000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 1 {
		t.Fatalf("a C regression did not red (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"LEDGER RED: c round_trip regressed 46.7% on [arm64 studio 6b213fbfa1a03a99]",
		"against the axis's recent best",
		"(227.3 -> 333.3 ns/msg, a-before.csv -> b-after.csv), gate 5.0%",
		"a perf regression goes red, it does not drift",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the C regression message lacks %q:\n%s", want, errOut)
		}
	}
}

// The time axis, cpp leg: the check the ledger has always had must survive
// being generalized over the locked-leg list and over the both-leg filter.
func TestCppLegRegressionIsStillRed(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-before.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	writeResults(t, dir, "b-after.csv", "2026-09-02T10:00:00Z", "studio", "arm64", "",
		rt("cpp", 3000000, "1.00"), rt("c", 4400000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 1 {
		t.Fatalf("a cpp regression did not red (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "LEDGER RED: cpp round_trip regressed 50.0% on [arm64 studio 6b213fbfa1a03a99]") {
		t.Errorf("the cpp regression message changed shape:\n%s", errOut)
	}
}

// THE HOLE THE COLD READ FOUND, closed. Comparing only the newest two points
// on an axis let ONE COMMIT defeat the time axis: land two ~50%-regressed
// CSVs together and the newest is measured against the other regressed file,
// so the step between them is ~0% and the gate exits 0 with the axis quietly
// reset at the worse level. Against the BEST of the previous three points the
// second file cannot launder the first: both legs go RED. The two later files
// hold the pair's own ratio exactly (102.3%, inside the 3.0-point floor), so
// nothing here is the pair lock's doing — this is the time axis alone.
func TestTwoRegressedFilesInOneCommitCannotResetTheAxis(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-baseline.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	writeResults(t, dir, "b-regressed.csv", "2026-09-02T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 3000000, "1.00"), rt("c", 2933333, "1.00"))
	writeResults(t, dir, "c-regressed.csv", "2026-09-02T11:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 3000000, "1.00"), rt("c", 2933333, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 1 {
		t.Fatalf("two files landing together laundered a 50%% regression (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"LEDGER RED: c round_trip regressed 50.0% on [arm64 studio 6b213fbfa1a03a99] against the axis's recent best",
		"a-baseline.csv -> c-regressed.csv",
		"LEDGER RED: cpp round_trip regressed 50.0% on [arm64 studio 6b213fbfa1a03a99] against the axis's recent best",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the best-of-window gate did not name what it compared: want %q\n%s", want, errOut)
		}
	}
}

// The time axis is built from BOTH-LEG files only, and this is why: on the
// Studio the c axis was interleaved with single-leg C experiment CSVs
// (packet-void-c, c-defaults-c, c-utf8-c, c-wide-c) whose 228 -> 240 ns/msg
// step is +5.35% against a 5.00% floor gate — so the next single-leg C
// experiment committed last would have red CI for a difference that is not a
// regression of the locked pair at all. The lock is a claim about the pair;
// a file with one leg in it is by construction not a like measurement of it.
func TestASingleLegExperimentFileIsNotAPointOnTheAxis(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-pass.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	// The committed Studio pair: 228.17 -> 240.38 ns/msg, +5.35% on a 5.0%
	// gate, and the second file carries the c leg alone.
	writeResults(t, dir, "b-c-defaults-c.csv", "2026-09-02T10:00:00Z", "studio", "arm64", "",
		rt("c", 4382609, "1.08"))
	writeResults(t, dir, "c-c-utf8-c.csv", "2026-09-02T11:00:00Z", "studio", "arm64", "",
		rt("c", 4160000, "0.88"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 0 {
		t.Fatalf("a single-leg C experiment gated the locked pair's axis (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "LEDGER RED") {
		t.Errorf("a single-leg experiment red the time axis:\n%s", errOut)
	}
	// It still appears in the printed series — the ledger prints the record,
	// it only gates the like-for-like part of it.
	if !strings.Contains(out, "b-c-defaults-c.csv") || !strings.Contains(out, "c-c-utf8-c.csv") {
		t.Errorf("the excluded experiments vanished from the printed series:\n%s", out)
	}
}

// Absolute rates do not compare across machines (§2), so the same regression
// split across two hosts is not a regression at all — the axis leads with the
// machine and each host has one point.
func TestARegressionAcrossMachinesIsNotAnAxis(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "a-studio.csv", "2026-09-01T10:00:00Z", "studio", "arm64", "OK",
		rt("cpp", 4500000, "1.00"), rt("c", 4400000, "1.00"))
	writeResults(t, dir, "b-laptop.csv", "2026-09-02T10:00:00Z", "laptop", "arm64", "OK",
		rt("cpp", 3000000, "1.00"), rt("c", 2950000, "1.00"))
	out, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
	if code != 0 {
		t.Fatalf("the gate compared two machines (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
}

// renderAwk runs the COMMITTED bench/render.awk over one results CSV exactly
// as the --quick sweep tail and --render invoke it, and returns its table and
// notes. Nothing is reimplemented here: the point of the test below is that
// the awk the record is printed with and the Go the record is gated with rule
// the same way on the same bytes.
func renderAwk(t *testing.T, file string) string {
	t.Helper()
	cmd := exec.Command("awk", "-F,", "-v", "skips=", "-f", "../render.awk", file)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("render.awk on %s: %v\n%s", file, err, errb.String())
	}
	return out.String()
}

var (
	awkTie    = regexp.MustCompile(`§2\.8 TIE: c measured ([0-9.]+)% of cpp`)
	awkRow    = regexp.MustCompile(`(?m)^\s+c\s+([0-9]+)%\s*$`)
	awkNoFig  = regexp.MustCompile(`§2\.3 INVALID: c spread`)
	ledgerFig = regexp.MustCompile(`c measured ([0-9.]+)% of cpp in `)
)

// awkVerdict reads render.awk's ruling on the c row out of its own output:
// "tie" when §2.8's note fired (c printed 100% and the note carries the
// measured figure), "refused" when §2.3 made the row print no number at all,
// and "figure" with the printed percentage otherwise.
func awkVerdict(t *testing.T, out string) (string, float64) {
	t.Helper()
	if awkNoFig.MatchString(out) {
		return "refused", 0
	}
	if m := awkTie.FindStringSubmatch(out); m != nil {
		return "tie", mustFloat(t, m[1])
	}
	if m := awkRow.FindStringSubmatch(out); m != nil {
		return "figure", mustFloat(t, m[1])
	}
	t.Fatalf("render.awk printed no c row at all:\n%s", out)
	return "", 0
}

// ledgerVerdict reads the pair lock's ruling on the same sitting out of the
// check's stderr: silence is "tie" (inside the band, nothing to say), the
// §2.3 skip is "refused", and a RED or WARN carries the figure.
func ledgerVerdict(t *testing.T, errOut, base string) (string, float64) {
	t.Helper()
	for line := range strings.SplitSeq(errOut, "\n") {
		if !strings.Contains(line, base) {
			continue
		}
		if strings.Contains(line, "pair-lock skipped") && strings.Contains(line, "§2.3") {
			return "refused", 0
		}
		if m := ledgerFig.FindStringSubmatch(line); m != nil {
			return "figure", mustFloat(t, m[1])
		}
	}
	return "tie", 0
}

func mustFloat(t *testing.T, s string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// THE TABLE AND THE GATE MUST AGREE, and the only way to know they do is to
// run both over the same bytes. render.awk prints the record; the ledger gates
// it; the 3.0-point tie floor is written out TWICE, once in each language, and
// §2.3's refusal was written in only one of them until this test was added.
// So: one sitting inside the band, one outside, one refused by §2.3, and the
// verdicts have to match on every one.
func TestRenderAwkAndThePairLockAgreeOnTheSameSitting(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		cpp, c     int
		cppSp, cSp string
		want       string // the verdict both tools must reach
	}{
		// 102.3% of cpp against the 3.0-point floor: a tie both ways.
		{"inside the band", "inside.csv", 4500000, 4400000, "1.00", "1.00", "tie"},
		// 107.1% against the same floor: a real figure both ways.
		{"outside the band", "outside.csv", 4500000, 4200000, "1.00", "1.00", "figure"},
		// c's spread is over §2.3's 40% INVALID line. render.awk prints "—"
		// and refuses a number. The separation is 150% of cpp against a band
		// of 46.0 points, so a pair lock WITHOUT the §2.3 rule would have gone
		// RED here on a figure the table declines to print — the exact
		// disagreement this clause removes.
		{"refused by §2.3", "invalid-spread.csv", 4500000, 3000000, "1.00", "45.00", "refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeResults(t, dir, tc.file, "2026-09-07T10:00:00Z", "studio", "arm64", "OK",
				rt("cpp", tc.cpp, tc.cppSp), rt("c", tc.c, tc.cSp))
			aKind, aPct := awkVerdict(t, renderAwk(t, filepath.Join(dir, tc.file)))
			_, errOut, code := runTool(t, "ledger", "--check", "--dir", dir)
			lKind, lPct := ledgerVerdict(t, errOut, tc.file)
			if aKind != tc.want || lKind != tc.want {
				t.Fatalf("the table and the gate disagree (want %q): render.awk says %q, the ledger says %q\n--- ledger stderr ---\n%s",
					tc.want, aKind, lKind, errOut)
			}
			if tc.want == "figure" {
				// awk prints the row rounded to whole points, the ledger to
				// one place; they must name the same measurement.
				if d := aPct - lPct; d > 0.5 || d < -0.5 {
					t.Errorf("same sitting, two figures: render.awk %.1f%%, ledger %.1f%%", aPct, lPct)
				}
				if code != 1 {
					t.Errorf("a PASS outside the band did not red (exit %d):\n%s", code, errOut)
				}
			}
			if tc.want != "figure" && code != 0 {
				t.Errorf("exit %d on a sitting the table publishes no separation for:\n%s", code, errOut)
			}
		})
	}
}

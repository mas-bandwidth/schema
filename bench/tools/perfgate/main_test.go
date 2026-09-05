package main

import (
	"os"
	"strings"
	"testing"
)

// The planter is the negative control's whole mechanism, and a planter that
// silently stops matching would leave the control passing for the wrong
// reason. These run against the REAL generated trees, so a change in what the
// emitters produce is caught here rather than in a timed sitting.

func TestPlantReachesTheReadPathAndOnlyTheReadPath(t *testing.T) {
	b, err := os.ReadFile("../../../generated/bench/cpp/BenchWire.h")
	if err != nil {
		t.Skip("no generated packet unit in this tree")
	}
	out, n := plantPacket(string(b))

	// THE ORACLE IS AN INDEPENDENT COUNT, not a second copy of the planter's
	// own state machine. Every field read on this wire is a read_* macro call on
	// a line of its own, and those calls appear on the read path and nowhere
	// else, so a plain scan of the untransformed file says how many sites there
	// are. A planter that started matching write_ too, or stopped matching a
	// macro the emitter renamed, disagrees with this number.
	sites := 0
	for line := range strings.SplitSeq(string(b), "\n") {
		if readMacro.MatchString(strings.TrimSpace(line)) {
			sites++
		}
	}
	if sites < 20 {
		t.Fatalf("the packet unit has %d read sites, too few for this test to mean anything", sites)
	}
	if n != sites {
		t.Fatalf("planted %d sites, an independent scan finds %d", n, sites)
	}
	if got := strings.Count(out, "SCHEMA_PERFGATE_PLANT()"); got != sites {
		t.Fatalf("the planted text carries %d plants for %d sites", got, sites)
	}
}

func TestTablePlanterReachesTheLoadPathAndOnlyTheLoadPath(t *testing.T) {
	b, err := os.ReadFile("../../../generated/bench/tables/cpp/BenchTableTable.h")
	if err != nil {
		t.Skip("no generated table unit in this tree")
	}
	out, n := plantTable(string(b))
	if n < 20 {
		t.Fatalf("planted %d sites, which is too few to be the read path", n)
	}

	// Here the filter is doing real work and the test has to prove it. Case
	// labels appear in the enum-id lookup helper as well as in the decode
	// bodies, and a plant in the helper would price enum values rather than
	// fields. So the planted count must be strictly under the file's total,
	// and every plant must sit immediately after a case label rather than
	// wherever the walk happened to leave it.
	total := 0
	for line := range strings.SplitSeq(string(b), "\n") {
		if caseLine.MatchString(strings.TrimSpace(line)) {
			total++
		}
	}
	if n >= total {
		t.Fatalf("planted %d of %d case labels, so the load-path filter filtered nothing", n, total)
	}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "SCHEMA_PERFGATE_PLANT()") || i == 0 {
			continue
		}
		if !caseLine.MatchString(strings.TrimSpace(lines[i-1])) {
			t.Fatalf("line %d planted a cost that does not follow a case label: %q", i+1, lines[i-1])
		}
	}
}

func TestPlantBlockLandsAfterPragmaOnce(t *testing.T) {
	got := injectBlock("// header\n#pragma once\n\n#include <x>\n")
	pragma := strings.Index(got, "#pragma once")
	block := strings.Index(got, "SCHEMA_PERFGATE_PLANT_DEFINED")
	inc := strings.Index(got, "#include <x>")
	if pragma < 0 || block < pragma || inc < block {
		t.Fatalf("the block must sit between the pragma and the first include, got:\n%s", got)
	}
}

// The pin file is the gate's authority, so the refusals that keep an
// unmeasured or malformed one out are worth their own control.

func TestValidateRefusesTheUnmeasuredSkeleton(t *testing.T) {
	p := skeletonPins()
	if err := p.validate(); err == nil {
		t.Fatal("a pin file full of zeros validated, so an unmeasured gate would run")
	}
}

func TestValidateRefusesAWidthOfZero(t *testing.T) {
	p := skeletonPins()
	for _, want := range gateRows {
		p.rates[want[0]+"/"+want[1]] = 1e6
	}
	p.directives["spread.max.pct"] = "4.0"
	if err := p.validate(); err == nil {
		t.Fatal("band.pct of zero validated, and a gate with no width refuses everything or nothing")
	}
	p.directives["band.pct"] = "3.0"
	if err := p.validate(); err != nil {
		t.Fatalf("a fully stated pin file was refused: %v", err)
	}
}

func TestValidateRefusesAnUnstatedSitting(t *testing.T) {
	p := statedPins()
	delete(p.directives, "sitting.uptime")
	if err := p.validate(); err == nil {
		t.Fatal("a pin file with no sitting.uptime validated, so a pin could carry no provenance")
	}
}

// The verdict itself: slower past the band is red, faster is not, and a noisy
// sitting renders no green verdict at all.

func TestCompareIsRedPastTheBandAndOnlyPastIt(t *testing.T) {
	p := statedPins()
	for _, want := range gateRows {
		p.rates[want[0]+"/"+want[1]] = 1e6
	}

	within := sittingAt(map[string]float64{"": 0.98e6}) // 2% slower, inside a 3% band
	if _, red := compare(within, p, 3.0, 5.0); red {
		t.Fatal("2% slower went red inside a 3% band")
	}
	past := sittingAt(map[string]float64{"": 0.95e6}) // 5% slower
	if _, red := compare(past, p, 3.0, 5.0); !red {
		t.Fatal("5% slower stayed green inside a 3% band, so a real regression would merge")
	}
	faster := sittingAt(map[string]float64{"": 1.20e6})
	if _, red := compare(faster, p, 3.0, 5.0); red {
		t.Fatal("faster than the pin went red")
	}
}

func TestANoisySittingRendersNoVerdict(t *testing.T) {
	p := statedPins()
	for _, want := range gateRows {
		p.rates[want[0]+"/"+want[1]] = 1e6
	}
	s := sittingAt(map[string]float64{"": 1e6})
	for k, r := range s.rows {
		r.spread = 9.0
		s.rows[k] = r
	}
	vs, red := compare(s, p, 3.0, 5.0)
	if !red {
		t.Fatal("a sitting over the spread cap passed, and a gate that passes on noise passes on anything")
	}
	if !strings.Contains(vs[0].note, "NOISY") {
		t.Fatalf("the refusal did not name the noise: %q", vs[0].note)
	}
}

func TestADriftedCorpusIsNotAComparison(t *testing.T) {
	p := statedPins()
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		p.rates[k] = 1e6
		p.corpus[k] = "aaaaaaaaaaaaaaaa"
	}
	s := sittingAt(map[string]float64{"": 1e6})
	vs, red := compare(s, p, 3.0, 5.0)
	if !red {
		t.Fatal("a run against a different corpus was compared to the pins as if it were the same work")
	}
	if !strings.Contains(vs[0].note, "CORPUS MOVED") {
		t.Fatalf("the refusal did not name the corpus: %q", vs[0].note)
	}
}

// helpers

func skeletonPins() *pins {
	p := &pins{directives: map[string]string{}, rates: map[string]float64{}, corpus: map[string]string{}}
	for _, k := range requiredDirectives {
		p.directives[k] = "stated"
	}
	p.directives["band.pct"] = "0.0"
	p.directives["spread.max.pct"] = "0.0"
	for _, want := range gateRows {
		p.rates[want[0]+"/"+want[1]] = 0
	}
	return p
}

func statedPins() *pins {
	p := skeletonPins()
	p.directives["band.pct"] = "3.0"
	p.directives["spread.max.pct"] = "5.0"
	for _, want := range gateRows {
		p.rates[want[0]+"/"+want[1]] = 1e6
	}
	return p
}

// sittingAt builds a sitting whose every gate row carries rates[""].
func sittingAt(rates map[string]float64) *sitting {
	s := &sitting{rows: map[string]row{}}
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		s.rows[k] = row{bench: want[0], path: want[1], rate: rates[""], spread: 1.0, corpusID: "bbbbbbbbbbbbbbbb"}
	}
	return s
}

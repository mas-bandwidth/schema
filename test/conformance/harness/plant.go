package main

import (
	"fmt"
	"os"
)

// THE PLANTED ORACLE, for the shared gates' negative controls on every leg
// (make/widen-18-gate-controls.mk verify-message-fuzz-negative-control-<lang> and
// verify-sabotage-{fixed,variable,retain}-negative-control-<lang>;
// 2026-11-24). The emitter sabotages prove those gates red for JavaScript
// only. Here the ORACLE is made wrong instead, the same way for every leg,
// so a leg whose gate really compares its answers goes red by name, and a leg
// whose gate compared nothing would stay green and fail its control.
//
// SCHEMA_GATE_PLANT names the class:
//
//	message-fuzz  the message-fuzz oracle's verdict is inverted on every frame
//	fixed         `run`: the fixed-table evolution read fixedevo_v1_as_v3
//	              is expected refused (it reads)
//	variable      `run`: the variable-table arm row a1_arm_body_as_a2 is
//	              expected read (it is refused)
//	retain        `run`: retain_node_record's retain-save bytes are expected
//	              with their last byte inverted
//
// Each planted run prints PLANTED <class> on stderr, and a run whose manifest
// lacks the case says so by name; the control requires the planted case's own
// failure line, so an unplanted run can never pass it.
const plantEnv = "SCHEMA_GATE_PLANT"

// runPlants names, per `run` class, the surface and case it plants.
var runPlants = map[string]struct{ surface, name string }{
	"fixed":    {"report", "fixedevo_v1_as_v3"},
	"variable": {"report", "a1_arm_body_as_a2"},
	"retain":   {"retain-save", "retain_node_record"},
}

// plantedMessageFuzz reports whether the message-fuzz oracle is planted.
func plantedMessageFuzz() bool { return os.Getenv(plantEnv) == "message-fuzz" }

// plantMessageAnswer inverts the oracle's verdict: an accepted batch is
// expected refused, and a refused one expected accepted.
func plantMessageAnswer(ans oracleAnswer) oracleAnswer {
	if ans.accepted {
		return oracleAnswer{report: Counts{Refused: true}}
	}
	return oracleAnswer{accepted: true, report: Counts{}}
}

// plantExpectations applies the `run` plant SCHEMA_GATE_PLANT names to the
// expectations of every surface. An unset plant, or message-fuzz (which is
// not a `run` plant), changes nothing.
func plantExpectations(want map[string][]expectation) error {
	plant := os.Getenv(plantEnv)
	if plant == "" || plant == "message-fuzz" {
		return nil
	}
	p, ok := runPlants[plant]
	if !ok {
		return fmt.Errorf("%s=%q: the conformance harness plants message-fuzz, fixed, variable or retain", plantEnv, plant)
	}
	es := want[p.surface]
	for i := range es {
		if es[i].name != p.name {
			continue
		}
		switch plant {
		case "fixed":
			if string(es[i].want) != "read\n" {
				return fmt.Errorf("%s=fixed: %s is expected %q, not read: the plant's case has moved", plantEnv, p.name, es[i].want)
			}
			es[i] = expectation{name: es[i].name, want: []byte("refused\n"), damaged: true}
		case "variable":
			if string(es[i].want) != "refused\n" {
				return fmt.Errorf("%s=variable: %s is expected %q, not refused: the plant's case has moved", plantEnv, p.name, es[i].want)
			}
			es[i] = expectation{name: es[i].name, want: []byte("read\n")}
		case "retain":
			if len(es[i].want) == 0 {
				return fmt.Errorf("%s=retain: %s expects no bytes: nothing to plant", plantEnv, p.name)
			}
			w := append([]byte(nil), es[i].want...)
			w[len(w)-1] ^= 0xff
			es[i].want = w
		}
		fmt.Fprintf(os.Stderr, "PLANTED %s: the %s expectation of %s is wrong; every leg must now go red\n", plant, p.surface, p.name)
		return nil
	}
	// A run over another manifest (a card corpus, a rows corpus a gate runs
	// before the leg's own) has no such case: it is said, and the run is left
	// unplanted. The control never passes on it: it requires the planted
	// case's own failure line from the leg's run.
	fmt.Fprintf(os.Stderr, "PLANT %s: this manifest's %s surface has no case %s; this run is not planted\n", plant, p.surface, p.name)
	return nil
}
